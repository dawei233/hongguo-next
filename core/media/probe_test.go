// 本仓新增（M3 媒体管线测试）。
//
// 探针的验收点有两个：
//  1. 字段表与 merge-plan.js 逐字段一致（顺序也一致，否则签名会变、多集合并会静默退化）；
//  2. extradata_hash 必须真的取到 —— 少了它 merge-plan.js 的判据会退化成「必须重编码」，
//     而不报错。因此这里对真实 CENC 样本断言 hash 非空。
package media_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dawei233/hongguo-next/core/media"
)

// merge-plan.js:2-3 的原文（转写基准；改这两行前请先改 JS 或明确记录分歧）。
var mergePlanVideoFields = []string{"codec_name", "codec_tag_string", "profile", "level", "width",
	"height", "pix_fmt", "sample_aspect_ratio", "r_frame_rate", "extradata_hash",
	"color_range", "color_space", "color_transfer", "color_primaries", "field_order"}

var mergePlanAudioFields = []string{"codec_name", "profile", "sample_rate", "channels",
	"channel_layout", "extradata_hash"}

func TestSignatureFieldsMatchMergePlanJs(t *testing.T) {
	if strings.Join(media.VideoSignatureFields, ",") != strings.Join(mergePlanVideoFields, ",") {
		t.Fatalf("视频签名字段与 merge-plan.js 不一致:\n got=%v\nwant=%v",
			media.VideoSignatureFields, mergePlanVideoFields)
	}
	if strings.Join(media.AudioSignatureFields, ",") != strings.Join(mergePlanAudioFields, ",") {
		t.Fatalf("音频签名字段与 merge-plan.js 不一致:\n got=%v\nwant=%v",
			media.AudioSignatureFields, mergePlanAudioFields)
	}
}

func TestSignatureRendersMissingFieldsAsNull(t *testing.T) {
	stream := media.StreamInfo{Codec: "h264", Width: 1920, Height: 1080, RFrameRate: "25/1",
		PixFmt: "yuv420p", ExtradataHash: "MD5:abc"}
	signature := media.Signature(&stream, media.VideoSignatureFields)
	var decoded []any
	if err := json.Unmarshal([]byte(signature), &decoded); err != nil {
		t.Fatalf("签名不是 JSON 数组: %v (%s)", err, signature)
	}
	if len(decoded) != len(media.VideoSignatureFields) {
		t.Fatalf("签名元素个数应为 %d，实际 %d", len(media.VideoSignatureFields), len(decoded))
	}
	// 第 1 个字段 codec_name 有值、第 2 个 codec_tag_string 缺失 → null（对应 JS 的 ?? null）。
	if decoded[0] != "h264" {
		t.Fatalf("codec_name 位置应为 h264，实际 %v", decoded[0])
	}
	if decoded[1] != nil {
		t.Fatalf("缺失字段应为 null，实际 %v", decoded[1])
	}
	if media.Signature(nil, media.VideoSignatureFields) != "" {
		t.Fatal("无流时签名应为空串（对应 JS 的 stream ? … : ''）")
	}
	// level/width/height 在 JS 里是数值型，null 与 0 的区分没有意义，但顺序必须稳定。
	if decoded[3] != float64(0) || decoded[4] != float64(1920) {
		t.Fatalf("level/width 位置不对: %v %v", decoded[3], decoded[4])
	}
}

func TestProbeRealFixtureFields(t *testing.T) {
	ffmpeg := probeTool(t)
	result, err := ffmpeg.Probe(context.Background(), filepath.Join(testdataDir, "real_faststart_remux.in.mp4"),
		media.ProbeOptions{})
	if err != nil {
		t.Fatalf("探测失败: %v", err)
	}
	video, ok := result.Video()
	if !ok {
		t.Fatal("应探测到视频流")
	}
	if video.Codec != "h264" || video.Width != 320 || video.Height != 180 {
		t.Fatalf("视频流字段不符: %+v", video)
	}
	if video.RFrameRate != "15/1" {
		t.Fatalf("r_frame_rate 应为 15/1，实际 %q", video.RFrameRate)
	}
	// 关键断言：extradata_hash 必须取到（否则要显式加 -show_data_hash md5）。
	if !strings.HasPrefix(video.ExtradataHash, "MD5:") {
		t.Fatalf("extradata_hash 缺失或算法不对（多了它 merge-plan.js 才敢 copy）: %q", video.ExtradataHash)
	}
	if video.CodecTagString == "" || video.PixFmt == "" {
		t.Fatalf("codec_tag_string/pix_fmt 是签名必填字段，不应为空: %+v", video)
	}
	if video.TimeBase == "" {
		t.Fatal("time_base 缺失（CompatibilitySignature 用它判 remux）")
	}
	audio, ok := result.Audio()
	if !ok {
		t.Fatal("应探测到音频流")
	}
	if audio.Codec != "aac" || audio.SampleRate == 0 || audio.Channels == 0 {
		t.Fatalf("音频流字段不符: %+v", audio)
	}
	if result.DurationS < 2.5 || result.DurationS > 3.5 {
		t.Fatalf("时长应约 3 秒，实际 %v", result.DurationS)
	}
	if result.Format == "" || result.SizeBytes == 0 {
		t.Fatalf("format 字段缺失: %+v", result)
	}
	if result.VideoSummary() == "" || strings.Contains(result.VideoSummary(), "无视频流") {
		t.Fatalf("VideoSummary 异常: %q", result.VideoSummary())
	}
	// 两个样本（加密 / 解密）的解码器配置相同 → 视频签名必须相等，
	// 这正是「同一集的不同形态可以 copy 合并」的判据。
	encrypted, err := ffmpeg.Probe(context.Background(),
		filepath.Join(testdataDir, "real_cenc_decrypted.in.mp4"), media.ProbeOptions{})
	if err != nil {
		t.Fatalf("探测加密样本失败: %v", err)
	}
	if encrypted.VideoSignature() != result.VideoSignature() {
		t.Fatalf("加密与解密样本的解码器配置应一致:\n encrypted=%s\n decrypted=%s",
			encrypted.VideoSignature(), result.VideoSignature())
	}
}

func TestProbePlayableRejectsGarbage(t *testing.T) {
	ffmpeg := probeTool(t)
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.mp4")
	if err := os.WriteFile(garbage, []byte(strings.Repeat("not a video at all", 64)), 0o600); err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := ffmpeg.ProbePlayable(context.Background(), garbage, media.ProbeOptions{}); err == nil {
		t.Fatal("非媒体内容应被拒绝")
	}
	// 只有 ftyp+mdat、没有 moov 的文件同样不可播。
	if _, err := ffmpeg.ProbePlayable(context.Background(),
		filepath.Join(testdataDir, "synthetic_no_moov.in.mp4"), media.ProbeOptions{}); err == nil {
		t.Fatal("无 moov 的文件应被拒绝")
	}
	// 空路径是入参错误，不该拉起进程。
	if _, err := ffmpeg.Probe(context.Background(), "  ", media.ProbeOptions{}); err == nil {
		t.Fatal("空路径应报错")
	}
}

func TestProbeRemoteWithHeaders(t *testing.T) {
	ffmpeg := probeTool(t)
	body, err := os.ReadFile(filepath.Join(testdataDir, "real_faststart_remux.in.mp4"))
	if err != nil {
		t.Fatalf("%v", err)
	}
	var sawReferer string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		sawReferer = request.Header.Get("Referer")
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write(body)
	}))
	defer server.Close()

	result, err := ffmpeg.Probe(context.Background(), server.URL+"/movie.mp4", media.ProbeOptions{
		Headers: map[string]string{"Referer": "https://site.example.test/watch"},
	})
	if err != nil {
		t.Fatalf("远端探测失败（ffprobe 走 HTTP 时 -headers 必须能带上）: %v", err)
	}
	if _, ok := result.Video(); !ok {
		t.Fatal("远端探测应解析出视频流")
	}
	if !strings.Contains(sawReferer, "site.example.test") {
		t.Fatalf("服务端未收到 Referer，实际 %q", sawReferer)
	}
}

func TestHeaderBlockStableOrder(t *testing.T) {
	var sawHeaders string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		sawHeaders = request.Header.Get("X-One") + "|" + request.Header.Get("X-Two")
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write([]byte("x"))
	}))
	defer server.Close()
	ffmpeg := probeTool(t)
	_, _ = ffmpeg.Probe(context.Background(), server.URL, media.ProbeOptions{
		Headers: map[string]string{"X-Two": "b", "X-One": "a"},
	})
	if sawHeaders != "a|b" {
		t.Fatalf("自定义头未按 CRLF 块传出: %q", sawHeaders)
	}
}

// probeTool 返回已解析出可执行文件位置的 FFmpeg；环境缺工具时跳过。
func probeTool(t *testing.T) media.FFmpeg {
	t.Helper()
	ffprobePath(t) // 不存在时内部 t.Skip
	resolved, err := media.FFmpeg{}.Resolve()
	if err != nil {
		t.Skipf("环境里没有 ffmpeg/ffprobe，跳过: %v", err)
	}
	return resolved
}

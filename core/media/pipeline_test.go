// 本仓新增（M3 媒体管线测试）。
//
// 端到端验收：真实 CENC 加密 MP4（testdata，ffmpeg -encryption_scheme cenc-aes-ctr 产出）
// → Resolve → Fetch → Decrypt（ffmpeg -decryption_key）→ Sanitize（Go 转写）
// → Probe，且产物必须**零诊断解码出 45 帧**（3s × 15fps）。
// 这条链是 M3 的核心能力，也是 ADR-0004 里「必须二次净化」的现场证据。
package media_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dawei233/hongguo-next/core/media"
	"github.com/dawei233/hongguo-next/core/source"
)

const pipelineCENCKey = "000102030405060708090a0b0c0d0e0f"

// fixtureServer 把 testdata 目录挂到 HTTP 上（ffmpeg/ffprobe 走 HTTP 读，含 Range）。
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.FileServer(http.Dir(testdataDir)))
	t.Cleanup(server.Close)
	return server
}

func resolverFor(stream source.Stream) media.ResolverFunc {
	return func(context.Context, string, source.Quality) (source.Stream, error) {
		return stream, nil
	}
}

// TestPipelineDirectPlaybackSkipsEverything 覆盖「直链优先」：未加密 MP4 不走 ffmpeg。
func TestPipelineDirectPlaybackSkipsEverything(t *testing.T) {
	server := fixtureServer(t)
	pipeline := media.Pipeline{FFmpeg: toolFFmpeg(t)}
	result, err := pipeline.Run(context.Background(), "ep-1", source.QualityAuto, resolverFor(source.Stream{
		URL:       server.URL + "/real_faststart_remux.in.mp4",
		Container: source.ContainerMP4,
		Quality:   string(source.Quality1080p),
	}))
	if err != nil {
		t.Fatalf("直链播放失败: %v", err)
	}
	defer result.Close()
	if result.Local || result.Decrypted || result.Sanitized != nil {
		t.Fatalf("未加密直链不该产出本地文件: %+v", result)
	}
	if !strings.HasPrefix(result.Playback, server.URL) {
		t.Fatalf("播放地址应仍是远端直链，实际 %s", result.Playback)
	}
	assertStageOrder(t, result)
	for _, name := range []string{"fetch", "decrypt", "sanitize"} {
		stage, _ := result.Stage(name)
		if !stage.Skipped || stage.Reason == "" {
			t.Errorf("阶段 %s 应被跳过且给出原因，实际 %+v", name, stage)
		}
	}
	if video, ok := result.Probe.Video(); !ok || video.Width != 320 {
		t.Fatalf("探测结果不符: %+v", result.Probe)
	}
}

// TestPipelineDecryptThenSanitizeRealCENC 是 M3 的核心链路测试。
func TestPipelineDecryptThenSanitizeRealCENC(t *testing.T) {
	ffmpeg := toolFFmpeg(t)
	server := fixtureServer(t)
	pipeline := media.Pipeline{FFmpeg: ffmpeg, TempDir: t.TempDir()}
	result, err := pipeline.Run(context.Background(), "ep-cenc", source.Quality1080p, resolverFor(source.Stream{
		URL:           server.URL + "/real_cenc_decrypted.in.mp4",
		Encrypted:     true,
		DecryptionKey: pipelineCENCKey,
		Container:     source.ContainerMP4,
		NeedsSanitize: true,
		Quality:       string(source.Quality1080p),
		Headers:       map[string]string{"Referer": "https://site.example.test/watch"},
	}))
	if err != nil {
		t.Fatalf("解密+净化链路失败: %v", err)
	}
	defer result.Close()

	if !result.Decrypted || !result.Local || result.Sanitized == nil {
		t.Fatalf("应走完解密与净化: %+v", result)
	}
	// 净化的输入**恒为 faststart 产物**（moov 已前置），因此 delta 必为负：
	// 剔除 sgpd(26B)+sbgp(28B) 使 moov 变短，mdat 因此前移 54 字节。
	if result.Sanitized.Delta != -54 {
		t.Fatalf("delta 应为 -54，实际 %d", result.Sanitized.Delta)
	}
	if result.Sanitized.MoovSize != 2861 || result.Sanitized.Passes != 3 {
		t.Fatalf("剥离结果与实测不符: moov=%d passes=%d", result.Sanitized.MoovSize, result.Sanitized.Passes)
	}
	// 净化剔除集**恰好**是 sgpd+sbgp —— 这本身就是 ADR-0004 的证据链：
	// CENC 信令（senc/saio/saiz/sinf/encv/tenc）在 ffmpeg 解密 + faststart 这一步
	// 已被去掉，但 Sample Group 会留下来，只能靠净化剔掉。
	// （若直接净化**未解密**的原文件，剔除集会含 senc/saio/saiz ——
	//   见 sanitize_test.go 的 TestSanitizeRealFixtureFacts，那条路径管线不走。）
	if got := strings.Join(result.Sanitized.RemovedStblBoxes, ","); got != "sgpd,sbgp" {
		t.Fatalf("净化剔除集应为 sgpd,sbgp，实际 %q", got)
	}
	if !strings.HasSuffix(result.Playback, ".mp4") {
		t.Fatalf("产物必须保留 .mp4 后缀，实际 %s", result.Playback)
	}
	if _, err := os.Stat(result.Playback); err != nil {
		t.Fatalf("产物不存在: %v", err)
	}
	// 阶段记录里必须能看到 ffmpeg 的命令行，且**不含密钥**。
	decrypt, _ := result.Stage("decrypt")
	if !strings.Contains(decrypt.Detail, "-decryption_key <redacted>") {
		t.Fatalf("解密阶段应记录脱敏后的命令行，实际 %q", decrypt.Detail)
	}
	if strings.Contains(decrypt.Detail, pipelineCENCKey) {
		t.Fatalf("阶段记录泄漏了密钥: %q", decrypt.Detail)
	}
	assertStageOrder(t, result)

	video, ok := result.Probe.Video()
	if !ok || video.Codec != "h264" || video.Width != 320 || video.Height != 180 {
		t.Fatalf("产物探测不符: %+v", video)
	}

	// 强证据：净化产物必须零诊断解码，且读满 45 帧（stco 偏移写对）
	// 并且应带有效的 extradata_hash（说明 moov/stsd 未被破坏）。
	if !strings.HasPrefix(video.ExtradataHash, "MD5:") {
		t.Fatalf("净化产物的 extradata_hash 丢失（stsd 被破坏？）: %q", video.ExtradataHash)
	}
	command := exec.Command(ffmpeg.Path, "-v", "error", "-i", result.Playback, "-f", "null", "-")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("净化产物解码失败: %v\n%s", err, stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("净化产物解码有诊断输出:\n%s", stderr.String())
	}
	probe := exec.Command(ffmpeg.ProbePath, "-v", "error", "-count_frames", "-select_streams", "v:0",
		"-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", result.Playback)
	out, err := probe.Output()
	if err != nil {
		t.Fatalf("ffprobe 失败: %v", err)
	}
	if frames := strings.TrimSpace(string(out)); frames != "45" {
		t.Fatalf("应为 45 帧，实际 %q", frames)
	}

	// Close 清掉全部中间产物（解密产物 + 净化产物）。
	paths := append([]string(nil), result.Cleanup...)
	if len(paths) < 2 {
		t.Fatalf("应有两份中间产物（解密 + 净化），实际 %v", paths)
	}
	result.Close()
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("Close 后仍存在: %s", path)
		}
	}
}

// TestPipelineWrongKeyIsCaughtByDecodeVerify 固化一条重要实测事实：
//
// ffmpeg **不校验** -decryption_key。用错密钥解同一个 CENC 样本时，它同样退出 0，
// 产物大小与正确密钥**完全相同**（91178 字节 —— 只有 mdat 的样本被解密，
// moov/stsd/stco 未加密），ffprobe 也全绿（h264 320x180、nb_frames=45、
// extradata_hash 与正确产物一致）。唯一的差别是真解码时吐出成片
// `SEI … truncated` / `slice type … too large` / `no frame!` 并以非零码退出。
//
// 因此「错密钥/过期密钥」这类最常见的站源故障，**只能**靠解码验收发现 ——
// 这正是 Pipeline 在本地产物上跑 VerifyDecode 的理由。
func TestPipelineWrongKeyIsCaughtByDecodeVerify(t *testing.T) {
	ffmpeg := toolFFmpeg(t)
	server := fixtureServer(t)
	pipeline := media.Pipeline{FFmpeg: ffmpeg, TempDir: t.TempDir(), DecryptTimeout: 90 * time.Second}
	stream := source.Stream{
		URL:           server.URL + "/real_cenc_decrypted.in.mp4",
		Encrypted:     true,
		DecryptionKey: strings.Repeat("f", 32),
		Container:     source.ContainerMP4,
		NeedsSanitize: true,
	}
	result, err := pipeline.Run(context.Background(), "ep-bad-key", source.QualityAuto, resolverFor(stream))
	if err == nil {
		t.Fatalf("错密钥必须失败，不能返回静默损坏的播放地址: %+v", result)
	}
	if !errors.Is(err, media.ErrDecryptFailed) {
		t.Fatalf("错误应归入 ErrDecryptFailed，实际: %v", err)
	}
	// 先证明「只靠探测确实拦不住」：ffmpeg 用错密钥也产出了结构合法的文件。
	if result.Sanitized == nil {
		t.Fatal("错密钥时应已走完解密+净化（说明失败发生在解码验收这一步）")
	}
	if video, ok := result.Probe.Video(); !ok || video.Width != 320 || video.NBFrames != 45 {
		t.Fatalf("错密钥产物的 ffprobe 反而是全绿的（这正是必须解码验收的原因）: %+v", video)
	}
	probe, _ := result.Stage("probe")
	if !strings.Contains(probe.Detail, "解码验收失败") {
		t.Fatalf("阶段记录应写明解码验收失败，实际 %q", probe.Detail)
	}
	if strings.Contains(probe.Detail, strings.Repeat("f", 32)) {
		t.Fatalf("阶段记录泄漏了密钥: %q", probe.Detail)
	}
}

// TestPipelineDecryptHardFailureIsFatal 覆盖 ffmpeg 直接失败的情形：
// 不得退化成「净化加密原文件」那条 A 的兜底（server.py:1104）。
func TestPipelineDecryptHardFailureIsFatal(t *testing.T) {
	server := fixtureServer(t)
	pipeline := media.Pipeline{FFmpeg: toolFFmpeg(t), TempDir: t.TempDir()}
	// synthetic_no_moov.in.mp4 不是有效媒体：ffmpeg 会直接失败。
	stream := source.Stream{
		URL:           server.URL + "/synthetic_no_moov.in.mp4",
		Encrypted:     true,
		DecryptionKey: strings.Repeat("a", 32),
		Container:     source.ContainerMP4,
		NeedsSanitize: true,
	}
	result, err := pipeline.Run(context.Background(), "ep-hard-fail", source.QualityAuto, resolverFor(stream))
	if err == nil {
		t.Fatal("无效输入应失败")
	}
	if !errors.Is(err, media.ErrDecryptFailed) {
		t.Fatalf("应归入 ErrDecryptFailed，实际 %v", err)
	}
	if result.Sanitized != nil {
		t.Fatal("解密失败时绝不能退化成「净化加密原文件」")
	}
	if len(result.Cleanup) != 0 {
		t.Fatalf("失败时不该留下本地产物: %v", result.Cleanup)
	}
	decrypt, _ := result.Stage("decrypt")
	if decrypt.Skipped {
		t.Fatal("decrypt 阶段不该被标记为跳过")
	}
	// 诊断里不得出现密钥。
	if strings.Contains(decrypt.Detail, strings.Repeat("a", 32)) {
		t.Fatalf("阶段记录泄漏了密钥: %q", decrypt.Detail)
	}
}

func TestPipelineHLSRemux(t *testing.T) {
	ffmpeg := toolFFmpeg(t)
	dir := t.TempDir()
	// 用 fixture 现场生成一份本地 HLS（分片 + 清单），再由 httptest 提供。
	sourcePath := filepath.Join(testdataDir, "real_faststart_remux.in.mp4")
	generate := exec.Command(ffmpeg.Path, "-v", "error", "-y", "-i", sourcePath, "-c", "copy",
		"-f", "hls", "-hls_time", "1", "-hls_list_size", "0", filepath.Join(dir, "index.m3u8"))
	if out, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("生成 HLS 失败: %v\n%s", err, out)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer server.Close()

	pipeline := media.Pipeline{FFmpeg: ffmpeg, TempDir: t.TempDir()}
	result, err := pipeline.Run(context.Background(), "ep-hls", source.Quality720p, resolverFor(source2Stream(server.URL+"/index.m3u8")))
	if err != nil {
		t.Fatalf("HLS 链路失败: %v", err)
	}
	defer result.Close()
	if !result.Remuxed || !result.Local {
		t.Fatalf("HLS 必须转封装成本地 MP4（Chromium 播不了 m3u8）: %+v", result)
	}
	if result.Sanitized != nil {
		t.Fatal("HLS 是分片级解密，不该走 MP4 净化")
	}
	sanitize, _ := result.Stage("sanitize")
	if !sanitize.Skipped {
		t.Fatal("sanitize 阶段应被跳过")
	}
	video, ok := result.Probe.Video()
	if !ok || video.Width != 320 || video.Height != 180 {
		t.Fatalf("HLS 转封装产物探测不符: %+v", video)
	}
}

func TestPipelineHLSAllowRemote(t *testing.T) {
	dir := t.TempDir()
	// 只放一份不加密的媒体清单，够 AllowRemoteHLS 分支使用（不转封装、只探测）。
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXTINF:2.0,\nseg0.ts\n#EXT-X-ENDLIST\n"
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(playlist), 0o600); err != nil {
		t.Fatalf("%v", err)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer server.Close()
	pipeline := media.Pipeline{FFmpeg: toolFFmpeg(t), TempDir: t.TempDir(), AllowRemoteHLS: true}
	// 该清单没有可播分片，probe 会失败 —— 因此这里只断言「没有进入转封装」，
	// 用阶段记录而不是最终成功作为判据。
	result, _ := pipeline.Run(context.Background(), "ep-hls-remote", source.QualityAuto,
		resolverFor(source2Stream(server.URL+"/index.m3u8")))
	if result.Remuxed || result.Local {
		t.Fatalf("AllowRemoteHLS 时不该转封装: %+v", result)
	}
	if !strings.HasPrefix(result.Playback, server.URL) {
		t.Fatalf("应把远端清单地址交给播放器，实际 %s", result.Playback)
	}
	decrypt, ok := result.Stage("decrypt")
	if !ok || !decrypt.Skipped {
		t.Fatalf("decrypt 阶段应跳过: %+v", decrypt)
	}
}

func TestPipelineRejectsBadInputs(t *testing.T) {
	pipeline := media.Pipeline{FFmpeg: toolFFmpeg(t), TempDir: t.TempDir()}
	ctx := context.Background()

	// 未提供 Resolver。
	if _, err := pipeline.Run(ctx, "x", source.QualityAuto, nil); !errors.Is(err, media.ErrResolveFailed) {
		t.Fatalf("缺 Resolver 应报 ErrResolveFailed，实际 %v", err)
	}
	// Resolver 自身报错。
	broken := media.ResolverFunc(func(context.Context, string, source.Quality) (source.Stream, error) {
		return source.Stream{}, errors.New("源站 502")
	})
	if _, err := pipeline.Run(ctx, "x", source.QualityAuto, broken); !errors.Is(err, media.ErrResolveFailed) {
		t.Fatalf("应包装 Resolver 错误，实际 %v", err)
	}
	// 播放凭证过期（core 侧同款判据：app_playback_routes.go:84）。
	expired := source.Stream{URL: "https://a.test/x.mp4", Container: source.ContainerMP4,
		ExpiresAt: time.Now().Add(-time.Minute)}
	if _, err := pipeline.Run(ctx, "x", source.QualityAuto, resolverFor(expired)); !errors.Is(err, media.ErrExpiredPlan) {
		t.Fatalf("过期计划应报 ErrExpiredPlan，实际 %v", err)
	}
	// 空地址 / 声明加密却无密钥 / 密钥长度不对 / 封装形态未知。
	bad := []source.Stream{
		{Container: source.ContainerMP4},
		{URL: "https://a.test/x.mp4", Encrypted: true, Container: source.ContainerMP4},
		{URL: "https://a.test/x.mp4", Encrypted: true, DecryptionKey: "abcd", Container: source.ContainerMP4},
		{URL: "https://a.test/x.mp4", Encrypted: true, DecryptionKey: strings.Repeat("z", 32), Container: source.ContainerMP4},
		{URL: "https://a.test/x.mp4", Container: "mkv"},
		{URL: "https://a.test/x.mp4"},
	}
	for index, stream := range bad {
		if _, err := pipeline.Run(ctx, "x", source.QualityAuto, resolverFor(stream)); !errors.Is(err, media.ErrInvalidStream) {
			t.Errorf("第 %d 个非法流应报 ErrInvalidStream，实际 %v", index, err)
		}
	}
}

// TestPipelineStagesAreDeterministic 断言阶段序列恒为五段且顺序固定（可审计性前提）。
func TestPipelineStagesAreDeterministic(t *testing.T) {
	server := fixtureServer(t)
	pipeline := media.Pipeline{FFmpeg: toolFFmpeg(t)}
	result, err := pipeline.Run(context.Background(), "ep", source.QualityAuto, resolverFor(source.Stream{
		URL: server.URL + "/real_faststart_remux.in.mp4", Container: source.ContainerMP4}))
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer result.Close()
	assertStageOrder(t, result)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Result 应可序列化（喂给 bridge/ts 契约）: %v", err)
	}
	if bytes.Contains(encoded, []byte(pipelineCENCKey)) {
		t.Fatal("序列化结果里不该出现任何密钥")
	}
}

func assertStageOrder(t *testing.T, result media.Result) {
	t.Helper()
	if len(result.Stages) != len(media.StageNames) {
		t.Fatalf("阶段数应为 %d，实际 %d: %+v", len(media.StageNames), len(result.Stages), result.Stages)
	}
	for index, name := range media.StageNames {
		if result.Stages[index].Name != name {
			t.Fatalf("第 %d 个阶段应为 %s，实际 %s", index, name, result.Stages[index].Name)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// toolFFmpeg 返回已解析的 FFmpeg；环境缺工具时跳过。探针与 ffmpeg 都必须存在。
func toolFFmpeg(t *testing.T) media.FFmpeg {
	t.Helper()
	ffmpegPath(t)
	ffprobePath(t)
	resolved, err := media.FFmpeg{}.Resolve()
	if err != nil {
		t.Skipf("环境里没有 ffmpeg/ffprobe，跳过: %v", err)
	}
	return resolved
}

// source2Stream 造一条未加密 HLS 契约流（避免与 source 包名冲突的局部辅助）。
func source2Stream(address string) source.Stream {
	return source.Stream{URL: address, Container: source.ContainerHLS, Quality: string(source.QualityAuto)}
}

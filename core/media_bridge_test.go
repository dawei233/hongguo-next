// 本仓新增（M3 媒体管线桥测试）。
//
// 本文件是 core/media_bridge.go 的验收，同时也是「core 内部 → 契约 → 媒体管线」
// 这条链路的契约测试。三件事：
//
//  1. 桥的出口形状与 core/source/adapter 的镜像**逐字段一致**
//     （直接读 core/provider_media.go 的字段名/类型与 adapter.ProviderMedia 对比，
//     防止两侧字段名漂移 —— 那是跨语言/跨包契约里最难查的一类故障）；
//  2. 桥的入口校验与 core 自身一致（请求格式、剧集编号、引擎未就绪）；
//  3. 把桥的出口 JSON 真喂给 adapter + media.Pipeline 跑完真实 CENC 样本，
//     证明「桥 → 适配层 → 媒体管线」这条 M3 主链路是通的。
//
// 说明：桥的**真实解析**（ProviderMediaJSON 命中站源）需要联网，因此这里只覆盖
// 形状与错误路径；联网解析由 nightly 源健康巡检负责（§6 的 nightly-sources.yml）。
package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/dawei233/hongguo-next/core"
	"github.com/dawei233/hongguo-next/core/media"
	"github.com/dawei233/hongguo-next/core/source"
	"github.com/dawei233/hongguo-next/core/source/adapter"
)

const bridgeMediaFixtureDir = "media/testdata"

func TestMediaBridgeInfoAndReady(t *testing.T) {
	if err := core.InitializeMediaBridge(t.TempDir()); err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	if !core.EngineReady() {
		t.Fatal("初始化后引擎应就绪")
	}
	// 幂等：再次初始化同一/另一目录不报错。
	if err := core.InitializeMediaBridge(t.TempDir()); err != nil {
		t.Fatalf("重复初始化应幂等: %v", err)
	}
	raw, err := core.MediaBridgeInfoJSON()
	if err != nil {
		t.Fatalf("取桥信息失败: %v", err)
	}
	var info struct {
		Version    int  `json:"version"`
		Ready      bool `json:"ready"`
		AllSources bool `json:"allSources"`
	}
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatalf("桥信息不是合法 JSON: %v (%s)", err, raw)
	}
	if info.Version != core.MediaBridgeVersion {
		t.Fatalf("版本不符: %d", info.Version)
	}
	if !info.Ready || !info.AllSources {
		t.Fatalf("就绪状态/全量站源标记不符: %+v", info)
	}
	// 相对目录被拒绝（与 core 自身的 newNativeEngine 校验一致）。
	if err := core.InitializeMediaBridge("relative/dir"); err == nil {
		t.Fatal("相对目录应被拒绝")
	}
}

func TestMediaBridgeRejectsBadRequests(t *testing.T) {
	if err := core.InitializeMediaBridge(t.TempDir()); err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	for _, request := range []string{"", "{", "[]", `{"drama":{"id":""},"index":1}`, `{"drama":{"id":"not-a-drama-id"},"index":1}`} {
		if _, err := core.ProviderMediaJSON(request); err == nil {
			t.Errorf("ProviderMediaJSON(%q) 应报错", request)
		}
		if _, err := core.PlaybackPlanJSON(request); err == nil {
			t.Errorf("PlaybackPlanJSON(%q) 应报错", request)
		}
	}
	// 超长入参被拒绝（与 NativeRequest 的 1MB 上限一致）。
	huge := `{"drama":{"id":"hongguo:1"},"index":1,"pad":"` + strings.Repeat("x", 1<<20) + `"}`
	if _, err := core.ProviderMediaJSON(huge); err == nil {
		t.Error("超长入参应被拒绝")
	}
	// 引擎未就绪时给出明确错误（若本测试进程已因其它测试初始化过引擎，则跳过这一条）。
	if !core.EngineReady() {
		if _, err := core.ProviderMediaJSON(`{"drama":{"id":"hongguo:1"},"index":1}`); !errors.Is(err, core.ErrMediaBridgeNotReady) {
			t.Errorf("引擎未就绪应报 ErrMediaBridgeNotReady，实际 %v", err)
		}
	}
}

// TestProviderMediaJSONShapeMatchesAdapterMirror 是桥的核心契约测试：
// core.providerMedia 的字段名/类型必须与 adapter.ProviderMedia 逐字段一致
// （桥的出口就是直接 json.Marshal 前者，调用方用后者解码）。
func TestProviderMediaJSONShapeMatchesAdapterMirror(t *testing.T) {
	fields := parseProviderMediaFields(t)
	if len(fields) == 0 {
		t.Fatal("未能从 provider_media.go 解析出 providerMedia 字段")
	}
	mirror := reflect.TypeOf(adapter.ProviderMedia{})
	for name, wantType := range fields {
		field, ok := mirror.FieldByName(name)
		if !ok {
			t.Errorf("adapter.ProviderMedia 缺少字段 %s（core 侧为 %s）", name, wantType)
			continue
		}
		if got, want := normalizeType(field.Type.String()), normalizeType(wantType); got != want {
			t.Errorf("字段 %s 类型不一致：core=%s adapter=%s", name, want, got)
		}
	}
	if mirror.NumField() != len(fields) {
		names := make([]string, 0, mirror.NumField())
		for index := 0; index < mirror.NumField(); index++ {
			names = append(names, mirror.Field(index).Name)
		}
		t.Errorf("adapter.ProviderMedia 字段数 %d 与 core 的 %d 不一致（core: %v）",
			mirror.NumField(), len(fields), names)
	}
	// 序列化形态：HLSKey/CENCKey 必须走 base64（[]byte 的标准编码），
	// Duration 必须是纳秒整数 —— 这两条是 M1 adapter 注释里写明的依赖。
	encoded, err := json.Marshal(adapter.ProviderMedia{URL: "https://a.test/x.mp4",
		CENCKey: bytes.Repeat([]byte{1}, 16), Duration: 1500000000})
	if err != nil {
		t.Fatalf("%v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("%v", err)
	}
	if decoded["Duration"] != float64(1500000000) {
		t.Errorf("Duration 应为纳秒整数: %v", decoded["Duration"])
	}
	if decoded["CENCKey"] != "AQEBAQEBAQEBAQEBAQEBAQ==" {
		t.Errorf("CENCKey 应为 base64: %v", decoded["CENCKey"])
	}
}

// normalizeType 把两侧的类型名收敛到可比较的形态：
// []byte 与 []uint8 是同一个类型；[]providerMedia 与 []adapter.ProviderMedia
// 只差包限定（镜像的目的就是跨包同名），因此只比较「结构形状」。
func normalizeType(name string) string {
	name = strings.ReplaceAll(name, "[]uint8", "[]byte")
	name = strings.ReplaceAll(name, "adapter.ProviderMedia", "ProviderMedia")
	name = strings.ReplaceAll(name, "[]providerMedia", "[]ProviderMedia")
	return name
}

// parseProviderMediaFields 读 core/provider_media.go，抽出 providerMedia 的
// **导出字段**（json.Marshal 会输出它们；未导出的 credentials 不在线上形态里）。
func parseProviderMediaFields(t *testing.T) map[string]string {
	t.Helper()
	body, err := os.ReadFile("provider_media.go")
	if err != nil {
		t.Fatalf("读取 provider_media.go 失败: %v", err)
	}
	block := regexp.MustCompile(`(?s)type providerMedia struct \{(.*?)\n\}`).FindSubmatch(body)
	if block == nil {
		t.Fatal("未找到 providerMedia 结构体定义")
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(block[1]), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 || !regexp.MustCompile(`^[A-Z]`).MatchString(parts[0]) {
			continue
		}
		fields[parts[0]] = parts[1]
	}
	return fields
}

// TestBridgeOutputFeedsMediaPipeline 把桥的出口线格式真喂给
// adapter → media.Pipeline，跑完真实 CENC 样本（解密 + 净化 + 探测 + 解码验收）。
func TestBridgeOutputFeedsMediaPipeline(t *testing.T) {
	ffmpeg, err := media.FFmpeg{}.Resolve()
	if err != nil {
		t.Skipf("环境里没有 ffmpeg/ffprobe，跳过: %v", err)
	}
	// 1) 起一个本地 HTTP 源，提供真实的 CENC 加密样本。
	server := httptest.NewServer(http.FileServer(http.Dir(bridgeMediaFixtureDir)))
	defer server.Close()

	// 2) 构造「桥出口」的线格式：字段名与 core.providerMedia 一致
	//    （由上面的契约测试保证），这里用 adapter 的镜像序列化出来。
	payload, err := json.Marshal(adapter.ProviderMedia{
		URL:      server.URL + "/real_cenc_decrypted.in.mp4",
		Referer:  "https://site.example.test/watch",
		CENCKey:  mustHex(t, "000102030405060708090a0b0c0d0e0f"),
		Quality:  1080,
		Playlist: "",
	})
	if err != nil {
		t.Fatalf("%v", err)
	}
	// 3) 适配层把线格式映射成契约 Stream（这是 apps/desktop 的接线方式）。
	stream, err := adapter.StreamFromProviderMediaValue(json.RawMessage(payload))
	if err != nil {
		t.Fatalf("适配层映射失败: %v", err)
	}
	if !stream.Encrypted || !stream.NeedsSanitize || stream.Container != source.ContainerMP4 {
		t.Fatalf("契约 Stream 判据不符: %+v", stream)
	}
	if stream.Headers["Referer"] != "https://site.example.test/watch" {
		t.Fatalf("Referer 未透传: %+v", stream.Headers)
	}
	// 4) 媒体管线跑完整链路。
	pipeline := media.Pipeline{FFmpeg: ffmpeg, TempDir: t.TempDir()}
	result, err := pipeline.Run(context.Background(), "bridge-ep", source.Quality(stream.Quality),
		media.ResolverFunc(func(context.Context, string, source.Quality) (source.Stream, error) {
			return stream, nil
		}))
	if err != nil {
		t.Fatalf("管线失败: %v", err)
	}
	defer result.Close()
	if result.Sanitized == nil || !result.Decrypted {
		t.Fatalf("应走完解密+净化: %+v", result)
	}
	if got := strings.Join(result.Sanitized.RemovedStblBoxes, ","); got != "sgpd,sbgp" {
		t.Fatalf("净化剔除集不符: %q", got)
	}
	if video, ok := result.Probe.Video(); !ok || video.Width != 320 || video.Height != 180 {
		t.Fatalf("产物探测不符: %+v", video)
	}
	probe, _ := result.Stage("probe")
	if !strings.Contains(probe.Detail, "解码验收 30 帧通过") {
		t.Fatalf("探测阶段应含解码验收记录，实际 %q", probe.Detail)
	}
}

// TestBridgeCannotImportAdapter 固化一条架构约束：
// core（父包）不得 import core/source/adapter —— adapter 反向 import core，
// 一旦父包 import 它就成了循环依赖，Go 会直接编译失败。
// 本测试以「源码里不出现该 import」的方式表达该约束（比注释更难违反）。
func TestBridgeCannotImportAdapter(t *testing.T) {
	body, err := os.ReadFile("media_bridge.go")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if regexp.MustCompile(`"github\.com/dawei233/hongguo-next/core/source/adapter"`).Match(body) {
		t.Fatal("core/media_bridge.go 不得 import adapter（会造成 core ⇄ adapter 循环依赖）")
	}
	imports := regexp.MustCompile(`(?s)^import \((.*?)\)`).FindSubmatch(body)
	allowed := map[string]bool{
		`"context"`: true, `"encoding/json"`: true, `"errors"`: true,
		`"fmt"`: true, `"path/filepath"`: true, `"time"`: true,
	}
	if imports != nil {
		var found []string
		for _, line := range strings.Split(string(imports[1]), "\n") {
			line = strings.TrimSpace(strings.TrimSpace(line))
			if line == "" || !strings.HasPrefix(line, `"`) {
				continue
			}
			found = append(found, line)
			if !allowed[line] {
				t.Errorf("桥只应依赖标准库（新增依赖 %s 需明确理由）", line)
			}
		}
		sort.Strings(found)
		t.Logf("media_bridge.go 的依赖面: %v", found)
	}
}

func mustHex(t *testing.T, text string) []byte {
	t.Helper()
	out := make([]byte, len(text)/2)
	for index := 0; index < len(out); index++ {
		high := strings.IndexByte("0123456789abcdef", text[index*2])
		low := strings.IndexByte("0123456789abcdef", text[index*2+1])
		if high < 0 || low < 0 {
			t.Fatalf("非法十六进制: %s", text)
		}
		out[index] = byte(high<<4 | low)
	}
	return out
}

// 本仓新增（M3 媒体管线测试）。
//
// HLS 的验收点：
//   - 清单校验与 hls-media.js 对齐（拒绝直播/EXT-X-DEFINE/非 AES-128/错误页）；
//   - 选档两种口径都保留：media.py 的「最后一档胜出」逐字复刻，另有按带宽/分辨率选档；
//   - AES-128 本地化：自己取密钥、把 URI="…" 指向本地密钥、分片 URI 提升为绝对地址
//     （后者是本仓为修掉「本地化后分片 404」补的一步，已在此断言）。
package media_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dawei233/hongguo-next/core/media"
)

const masterPlaylist = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360
360/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720
720/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1200000,RESOLUTION=960x540
540/index.m3u8
`

const mediaPlaylist = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:4
#EXT-X-KEY:METHOD=AES-128,URI="key.bin",IV=0x00000000000000000000000000000001
#EXTINF:4.0,
seg0.ts
#EXTINF:4.0,
seg1.ts
#EXT-X-ENDLIST
`

func TestIsHLS(t *testing.T) {
	cases := map[string]bool{
		"https://a.test/x/index.m3u8":          true,
		"https://a.test/x/index.M3U8":          true,
		"https://a.test/x/index.m3u8?t=1&s=2":  true,
		"https://a.test/x/index.mp4":           false,
		"https://a.test/m3u8/index":            false,
		"https://a.test/x/index.m3u8.mp4":      false,
		"https://a.test/x/index.M3U8?sig=a.m3": true,
		"":                                     false,
	}
	for address, want := range cases {
		if got := media.IsHLS(address); got != want {
			t.Errorf("IsHLS(%q) = %v，期望 %v", address, got, want)
		}
	}
}

func TestParsePlaylistMaster(t *testing.T) {
	playlist, err := media.ParsePlaylist(masterPlaylist)
	if err != nil {
		t.Fatalf("解析 master 失败: %v", err)
	}
	if len(playlist.Variants) != 3 {
		t.Fatalf("应解析出 3 档，实际 %d", len(playlist.Variants))
	}
	first := playlist.Variants[0]
	if first.URI != "360/index.m3u8" || first.Bandwidth != 800000 || first.Resolution != "640x360" ||
		first.Width != 640 || first.Height != 360 || first.Line != 4 {
		t.Fatalf("第一档字段不符: %+v", first)
	}
	if playlist.Encrypted() {
		t.Fatal("master 清单未声明加密")
	}
	// 按带宽（hls-media.js 语义）与按分辨率各选一次。
	if best, ok := playlist.HighestBandwidthVariant(); !ok || best.URI != "720/index.m3u8" {
		t.Fatalf("最高带宽档应为 720/index.m3u8，实际 %+v", best)
	}
	if best, ok := playlist.HighestResolutionVariant(); !ok || best.Height != 720 {
		t.Fatalf("最高分辨率档高度应为 720，实际 %+v", best)
	}
	// media.py 的 _pick_variant 在「每个 URI 前都有 STREAM-INF」时等价于「最后一档胜出」。
	if last, ok := playlist.LastVariant(); !ok || last.URI != "540/index.m3u8" {
		t.Fatalf("LastVariant 应复刻 media.py 的行为（最后一档），实际 %+v", last)
	}
}

func TestParsePlaylistMedia(t *testing.T) {
	playlist, err := media.ParsePlaylist(mediaPlaylist)
	if err != nil {
		t.Fatalf("解析 media 失败: %v", err)
	}
	if len(playlist.Segments) != 2 {
		t.Fatalf("应解析出 2 个分片，实际 %d", len(playlist.Segments))
	}
	if playlist.Segments[0].URI != "seg0.ts" || playlist.Segments[0].DurationS != 4 {
		t.Fatalf("分片字段不符: %+v", playlist.Segments[0])
	}
	if !playlist.EndList || playlist.Live {
		t.Fatalf("含 ENDLIST 的点播清单不该被判为直播: %+v", playlist)
	}
	if !playlist.Encrypted() || playlist.KeyURI() != "key.bin" {
		t.Fatalf("密钥解析不符: %v / %q", playlist.Encrypted(), playlist.KeyURI())
	}
	if len(playlist.Keys) != 1 || playlist.Keys[0].Method != "AES-128" ||
		playlist.Keys[0].IV != "0x00000000000000000000000000000001" {
		t.Fatalf("密钥标签字段不符: %+v", playlist.Keys)
	}
}

func TestParsePlaylistRejections(t *testing.T) {
	cases := map[string]string{
		"缺 #EXTM3U":    "#EXT-X-TARGETDURATION:4\nseg0.ts\n",
		"直播清单":         "#EXTM3U\n#EXTINF:4.0,\nseg0.ts\n",
		"EXT-X-DEFINE": "#EXTM3U\n#EXT-X-DEFINE:NAME=\"a\",VALUE=\"b\"\n#EXT-X-ENDLIST\n",
		"不支持的加密方式":     "#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"k\"\n#EXT-X-ENDLIST\n",
		"URI 与声明数量不一致": "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"a\" URI=b\n#EXT-X-ENDLIST\n",
	}
	for name, body := range cases {
		if _, err := media.ParsePlaylist(body); err == nil {
			t.Errorf("%s：应报错", name)
		}
	}
	// NONE 是合法声明（不加密）。
	if _, err := media.ParsePlaylist("#EXTM3U\n#EXT-X-KEY:METHOD=NONE\n#EXT-X-ENDLIST\n"); err != nil {
		t.Errorf("METHOD=NONE 应被接受: %v", err)
	}
}

// hlsServer 起一个极小的 HLS 站点：master、两档 media、16 字节密钥。
func hlsServer(t *testing.T, keyBody []byte, mediaBody string) (*httptest.Server, *http.Request) {
	t.Helper()
	lastRequest := &http.Request{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/master.m3u8":
			writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = writer.Write([]byte(masterPlaylist))
		case "/720/index.m3u8":
			*lastRequest = *request
			writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = writer.Write([]byte(mediaBody))
		case "/720/key.bin", "/k.bin":
			writer.Header().Set("Content-Type", "application/octet-stream")
			_, _ = writer.Write(keyBody)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, lastRequest
}

func TestLocalizeHLSEncrypted(t *testing.T) {
	key := []byte("0123456789abcdef")
	server, lastRequest := hlsServer(t, key, mediaPlaylist)
	workDir := t.TempDir()
	localized, err := media.LocalizeHLS(context.Background(), server.URL+"/master.m3u8", media.LocalizeOptions{
		WorkDir: workDir,
		Referer: "https://site.example.test/watch",
	})
	if err != nil {
		t.Fatalf("本地化失败: %v", err)
	}
	defer localized.Close()

	if localized.Warning != "" {
		t.Fatalf("不该降级: %s", localized.Warning)
	}
	if !localized.Encrypted || localized.KeyPath == "" {
		t.Fatalf("应判定为加密并落本地密钥: %+v", localized)
	}
	if !strings.Contains(localized.Input, workDir) || !strings.HasSuffix(localized.Input, ".m3u8") {
		t.Fatalf("输入应指向本地 playlist: %s", localized.Input)
	}
	if localized.Variant != server.URL+"/720/index.m3u8" {
		t.Fatalf("选档应取最高分辨率档，实际 %s", localized.Variant)
	}
	if lastRequest.Header.Get("Referer") != "https://site.example.test/watch" {
		t.Fatalf("清单请求未带 Referer: %q", lastRequest.Header.Get("Referer"))
	}
	keyBytes, err := os.ReadFile(localized.KeyPath)
	if err != nil {
		t.Fatalf("密钥文件未落盘: %v", err)
	}
	if string(keyBytes) != string(key) {
		t.Fatalf("密钥内容不符: %q", keyBytes)
	}
	body, err := os.ReadFile(localized.Input)
	if err != nil {
		t.Fatalf("本地 playlist 未落盘: %v", err)
	}
	text := string(body)
	// 密钥 URI 指向本地文件（media.py:135 的改写）。
	if !strings.Contains(text, `URI="`+localized.KeyPath+`"`) {
		t.Fatalf("密钥 URI 未改写为本地路径:\n%s", text)
	}
	// 分片 URI 必须提升为绝对地址，否则本地 playlist 解析相对路径会 404。
	if !strings.Contains(text, server.URL+"/720/seg0.ts") || !strings.Contains(text, server.URL+"/720/seg1.ts") {
		t.Fatalf("分片 URI 未提升为绝对地址:\n%s", text)
	}
	// 本地 playlist 输入时不能带 -headers（media.py:17 实测 ffmpeg 报 Option not found）。
	joined := strings.Join(localized.ExtraArgs, " ")
	if strings.Contains(joined, "-headers") || strings.Contains(joined, "-user_agent") {
		t.Fatalf("本地 playlist 不该带 -headers/-user_agent: %v", localized.ExtraArgs)
	}
	if !strings.Contains(joined, "-protocol_whitelist file,http,https,tcp,tls,crypto") ||
		!strings.Contains(joined, "-allowed_extensions ALL") {
		t.Fatalf("应放开协议白名单与扩展名: %v", localized.ExtraArgs)
	}
	if len(localized.Cleanup) != 2 {
		t.Fatalf("清理列表应含密钥与 playlist，实际 %v", localized.Cleanup)
	}
	localized.Close()
	if _, err := os.Stat(localized.KeyPath); !os.IsNotExist(err) {
		t.Fatalf("Close 后密钥文件应已删除: %v", err)
	}
}

func TestLocalizeHLSUnencrypted(t *testing.T) {
	plain := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\nseg0.ts\n#EXT-X-ENDLIST\n"
	server, _ := hlsServer(t, nil, plain)
	localized, err := media.LocalizeHLS(context.Background(), server.URL+"/master.m3u8", media.LocalizeOptions{
		WorkDir: t.TempDir(),
		Referer: "https://site.example.test/watch",
	})
	if err != nil {
		t.Fatalf("本地化失败: %v", err)
	}
	if localized.Encrypted || len(localized.Cleanup) != 0 {
		t.Fatalf("未加密清单不该落地任何文件: %+v", localized)
	}
	if localized.Input != server.URL+"/720/index.m3u8" {
		t.Fatalf("未加密应远端直连选中档位，实际 %s", localized.Input)
	}
	joined := strings.Join(localized.ExtraArgs, " ")
	if !strings.Contains(joined, "-user_agent") || !strings.Contains(joined, "-headers") {
		t.Fatalf("远端直连应带 UA/Referer: %v", localized.ExtraArgs)
	}
}

func TestLocalizeHLSDegradesWhenKeyInvalid(t *testing.T) {
	// 密钥只有 12 字节 → 软降级为远端直连（media.py:121-126），但必须留痕。
	server, _ := hlsServer(t, []byte("short-key-12"), mediaPlaylist)
	localized, err := media.LocalizeHLS(context.Background(), server.URL+"/master.m3u8", media.LocalizeOptions{
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("软降级不该报错: %v", err)
	}
	if localized.Encrypted {
		t.Fatal("密钥长度非法时不该声称已本地化")
	}
	if localized.Warning == "" {
		t.Fatal("软降级必须留痕（避免静默降级成未加密）")
	}
	if !strings.Contains(localized.Warning, "12") {
		t.Fatalf("留痕应含实际密钥长度: %s", localized.Warning)
	}
	if len(localized.Cleanup) != 0 {
		t.Fatalf("降级后不应有临时文件: %v", localized.Cleanup)
	}
}

func TestLocalizeHLSRejectsBrokenUpstream(t *testing.T) {
	dir := t.TempDir()
	// 错误页（Content-Type 是 html/JSON）。
	html := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte("<html>404</html>"))
	}))
	defer html.Close()
	if _, err := media.LocalizeHLS(context.Background(), html.URL+"/master.m3u8",
		media.LocalizeOptions{WorkDir: dir}); err == nil {
		t.Fatal("HTML 错误页应被拒绝")
	}
	// 直播清单。
	live := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = writer.Write([]byte("#EXTM3U\n#EXTINF:4.0,\nseg0.ts\n"))
	}))
	defer live.Close()
	if _, err := media.LocalizeHLS(context.Background(), live.URL+"/master.m3u8",
		media.LocalizeOptions{WorkDir: dir}); err == nil {
		t.Fatal("未结束的直播清单应被拒绝")
	}
	// 非 m3u8 的 404。
	missing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer missing.Close()
	if _, err := media.LocalizeHLS(context.Background(), missing.URL+"/master.m3u8",
		media.LocalizeOptions{WorkDir: dir}); err == nil {
		t.Fatal("HTTP 404 应被拒绝")
	}
	// 空地址。
	if _, err := media.LocalizeHLS(context.Background(), " ", media.LocalizeOptions{}); err == nil {
		t.Fatal("空地址应报错")
	}
}

func TestLocalizeHLSPlainURLNotPlaylist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write([]byte("not a playlist"))
	}))
	defer server.Close()
	localized, err := media.LocalizeHLS(context.Background(), server.URL+"/movie.mp4", media.LocalizeOptions{})
	if err != nil {
		t.Fatalf("非 playlist 不该报错: %v", err)
	}
	if localized.Warning == "" || localized.Input != server.URL+"/movie.mp4" {
		t.Fatalf("应原样交给 ffmpeg 并留痕: %+v", localized)
	}
}

func TestLocalizeHLSWithSessionKeyOnly(t *testing.T) {
	// 有 #EXT-X-KEY 但没有 URI（复用上一个密钥）→ 交给 ffmpeg 处理并留痕。
	body := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"/k.bin\"\n#EXTINF:4.0,\nseg0.ts\n#EXT-X-ENDLIST\n"
	server, _ := hlsServer(t, []byte("0123456789abcdef"), body)
	localized, err := media.LocalizeHLS(context.Background(), server.URL+"/720/index.m3u8", media.LocalizeOptions{
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if localized.KeyPath == "" {
		t.Fatal("带 URI 的密钥应被本地化（密钥在站点根 /k.bin）")
	}
}

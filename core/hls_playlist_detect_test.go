package core

import (
	"strings"
	"testing"
)

// [红果漫剧补丁] HLS 清单判定回归测试：
// 黄豆/剧果/黄果等 AES-128 源的 session.key 非空，但 .ts 分片绝不能被判成清单
// （否则读全量后 #EXTM3U 校验失败 → 502 → ffmpeg crypto Unable to open resource）。
func TestPlaylistDetectionWithHlsKey(t *testing.T) {
	// 复刻 app_stream.go 的判定逻辑
	detect := func(assetContentType, upstreamContentType, finalPath string, sessionKeyLen int) bool {
		finalPath = strings.ToLower(finalPath)
		mediaExt := strings.HasSuffix(finalPath, ".ts") || strings.HasSuffix(finalPath, ".mp4") ||
			strings.HasSuffix(finalPath, ".m4s") || strings.HasSuffix(finalPath, ".aac") ||
			strings.HasSuffix(finalPath, ".m4a") || strings.HasSuffix(finalPath, ".mp3") ||
			strings.HasSuffix(finalPath, ".vtt")
		return !mediaExt && (strings.Contains(assetContentType, "mpegurl") ||
			strings.Contains(upstreamContentType, "mpegurl") ||
			strings.HasSuffix(finalPath, ".m3u8") || strings.Contains(finalPath, "m3u8") ||
			(sessionKeyLen > 0 && !strings.Contains(finalPath, ".")))
	}

	cases := []struct {
		name              string
		assetCT, upCT     string
		path              string
		keyLen            int
		want              bool
	}{
		// AES-128 源：.ts 分片必须不是清单（本次修复的核心）
		{"AES128源-ts分片", "", "video/mp2t", "/x/abc.ts", 16, false},
		{"AES128源-m4s分片", "", "video/iso.segment", "/x/abc.m4s", 16, false},
		{"AES128源-mp4", "video/mp4", "video/mp4", "/x/abc.mp4", 16, false},
		// AES-128 源：清单本身仍要识别
		{"AES128源-清单后缀", "", "application/vnd.apple.mpegurl", "/x/index.m3u8", 16, true},
		{"AES128源-无扩展名清单(兜底)", "application/vnd.apple.mpegurl", "", "/x/playlist", 16, true},
		// 无 key 的普通源
		{"普通源-ts分片", "", "video/mp2t", "/x/abc.ts", 0, false},
		{"普通源-m3u8", "", "", "/x/index.m3u8", 0, true},
		// 红果 CENC 源（mp4 直链，query 里有巧合 "hls" 子串但 path 干净）
		{"红果-mp4直链", "video/mp4", "video/mp4", "/video/tos/cn/xxx/", 0, false},
		// ★ 剧果：分片路径含 /hls/ 目录名 + .ts 扩展名，绝不能判为清单
		{"剧果-hls目录下ts分片", "", "video/mp2t", "/hls/10362/480/seg-014.ts", 0, false},
		{"剧果-hls目录下ts分片带key", "", "video/mp2t", "/hls/1/2/seg-1.ts", 16, false},
		{"剧果-主清单", "", "application/vnd.apple.mpegurl", "/hls/10362/480/index.m3u8", 0, true},
		{"剧果-无扩展名清单", "", "", "/hls/10362/480/playlist", 16, true},
	}
	for _, tc := range cases {
		got := detect(tc.assetCT, tc.upCT, tc.path, tc.keyLen)
		if got != tc.want {
			t.Errorf("%s: got playlist=%v want %v", tc.name, got, tc.want)
		}
	}
}

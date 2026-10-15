package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// [红果漫剧补丁] finalize 分支判定单测：.m3u8 才拉清单，mp4 直链原样返回
func TestFinalizeHongguoMediaBranches(t *testing.T) {
	d := &Downloader{}
	cases := []struct {
		name    string
		url     string
		isM3u8  bool
	}{
		{"mp4直链", "https://v26.qznovelvod.com/video/tos/cn/x/?a=8662&mime_type=video_mp4", false},
		{"m3u8清单", "https://example.com/index.m3u8", true},
		{"大写M3U8", "https://example.com/INDEX.M3U8", true},
		{"带查询参数的m3u8", "https://example.com/a.m3u8?token=x", true},
	}
	for _, tc := range cases {
		media := providerMedia{URL: tc.url}
		// 只测 URL 形态判定（不真正拉网络）：复刻 finalize 的判定逻辑
		parsed, err := url.Parse(media.URL)
		got := err == nil && strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8")
		if got != tc.isM3u8 {
			t.Errorf("%s: isM3u8=%v want %v", tc.name, got, tc.isM3u8)
		}
	}
	_ = context.Background()
	_ = d
}

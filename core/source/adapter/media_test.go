// 本仓新增（M1 站源适配层测试）。
//
// 覆盖 core.providerMedia / core.Chapter / core.nativePlan → 契约 Stream：
// 容器判定、AES-128 密钥的十六进制线格式、NeedsSanitize 规则、过期时刻单位换算，
// 以及空地址、内核中间态协议、非 16 字节密钥、本地文件计划的拒绝路径。
package adapter_test

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dawei233/hongguo-next/core"
	"github.com/dawei233/hongguo-next/core/source"
	"github.com/dawei233/hongguo-next/core/source/adapter"
)

var testKeyBytes = []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

const testKeyHex = "00112233445566778899aabbccddeeff"

func TestStreamFromProviderMediaEncryptedMP4(t *testing.T) {
	stream, err := adapter.StreamFromProviderMedia(adapter.ProviderMedia{
		URL:      "https://cdn.example.com/a.mp4",
		Referer:  "https://novel.snssdk.com/",
		CENCKey:  testKeyBytes,
		Quality:  1080,
		Variants: []adapter.ProviderMedia{{URL: "https://cdn.example.com/a-720.mp4", Quality: 720}},
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !stream.Encrypted || stream.DecryptionKey != testKeyHex {
		t.Fatalf("解密材料不符: %+v", stream)
	}
	if stream.Container != source.ContainerMP4 {
		t.Fatalf("Container = %q", stream.Container)
	}
	// 加密 MP4 的解密产物必须二次净化（ADR-0004；M3 逐源实测复核）。
	if !stream.NeedsSanitize {
		t.Fatal("加密 MP4 的 NeedsSanitize 必须为 true")
	}
	if stream.Quality != "1080p" {
		t.Fatalf("Quality = %q", stream.Quality)
	}
	if stream.Headers["Referer"] != "https://novel.snssdk.com/" {
		t.Fatalf("Headers = %v", stream.Headers)
	}
	// providerMedia 不带过期信息：契约用零值表示未知。
	if !stream.ExpiresAt.IsZero() {
		t.Fatalf("ExpiresAt 应为零值，得到 %v", stream.ExpiresAt)
	}
}

func TestStreamFromProviderMediaHLS(t *testing.T) {
	// core 取回播放列表后会把 URL 换成最终地址，故容器判定必须优先看 Playlist。
	stream, err := adapter.StreamFromProviderMedia(adapter.ProviderMedia{
		URL:      "https://cdn.example.com/redirect/play",
		Playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128\n",
		HLSKey:   testKeyBytes,
		Quality:  720,
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if stream.Container != source.ContainerHLS || !stream.Encrypted || stream.DecryptionKey != testKeyHex {
		t.Fatalf("HLS 映射不符: %+v", stream)
	}
	// HLS 的 AES-128 是分片级解密，不经 MP4 净化。
	if stream.NeedsSanitize {
		t.Fatal("HLS 不应要求 MP4 净化")
	}
	// 仅按后缀也能判出 hls。
	suffixOnly, err := adapter.StreamFromProviderMedia(adapter.ProviderMedia{URL: "https://cdn.example.com/play.m3u8"})
	if err != nil || suffixOnly.Container != source.ContainerHLS {
		t.Fatalf("按后缀判定失败: %+v, %v", suffixOnly, err)
	}
	// 明文直链：无解密材料、无附加头、清晰度未知 → auto。
	plain, err := adapter.StreamFromProviderMedia(adapter.ProviderMedia{URL: "https://cdn.example.com/b.mp4"})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if plain.Encrypted || plain.DecryptionKey != "" || plain.Headers != nil || plain.NeedsSanitize {
		t.Fatalf("明文直链映射不符: %+v", plain)
	}
	if plain.Quality != string(source.QualityAuto) {
		t.Fatalf("未知清晰度必须归一为 auto，得到 %q", plain.Quality)
	}
	// 两个 facet 各自给出密钥时以 CENC 优先；另一把长度异常不影响判定。
	preferCENC, err := adapter.StreamFromProviderMedia(adapter.ProviderMedia{
		URL: "https://cdn.example.com/c.mp4", CENCKey: testKeyBytes, HLSKey: []byte{1, 2, 3},
	})
	if err != nil || preferCENC.DecryptionKey != testKeyHex {
		t.Fatalf("CENC 应优先: %+v, %v", preferCENC, err)
	}
}

func TestStreamFromProviderMediaRejectsBadInput(t *testing.T) {
	cases := []struct {
		name  string
		media adapter.ProviderMedia
	}{
		{"空地址", adapter.ProviderMedia{}},
		{"相对地址", adapter.ProviderMedia{URL: "/media/a.mp4"}},
		{"内核中间态协议", adapter.ProviderMedia{URL: "hongguo-cenc://7301"}},
		{"非 http 协议", adapter.ProviderMedia{URL: "ftp://cdn.example.com/a.mp4"}},
		{"短 CENC 密钥", adapter.ProviderMedia{URL: "https://cdn.example.com/a.mp4", CENCKey: []byte{1, 2, 3}}},
		{"短 HLS 密钥", adapter.ProviderMedia{URL: "https://cdn.example.com/a.mp4", HLSKey: make([]byte, 8)}},
	}
	for _, item := range cases {
		if _, err := adapter.StreamFromProviderMedia(item.media); !errors.Is(err, adapter.ErrInvalidStream) {
			t.Errorf("%s: 期望 ErrInvalidStream，得到 %v", item.name, err)
		}
	}
}

func TestStreamFromProviderMediaValue(t *testing.T) {
	// any/JSON 入口：[]byte 经 base64 往返，密钥必须还原成同一把。
	stream, err := adapter.StreamFromProviderMediaValue(adapter.ProviderMedia{
		URL: "https://cdn.example.com/a.mp4", CENCKey: testKeyBytes, Quality: 720,
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if stream.DecryptionKey != testKeyHex || stream.Quality != "720p" {
		t.Fatalf("JSON 往返不符: %+v", stream)
	}
	for _, value := range []any{nil, 7, "text"} {
		if _, err := adapter.StreamFromProviderMediaValue(value); !errors.Is(err, adapter.ErrInvalidStream) {
			t.Errorf("StreamFromProviderMediaValue(%v) 应报 ErrInvalidStream，得到 %v", value, err)
		}
	}
}

func TestStreamFromChapter(t *testing.T) {
	stream, err := adapter.StreamFromChapter(core.Chapter{
		ID: "guipian:1:x", Source: "guipian", VideoURL: "https://cdn.example.com/a.mp4", Referer: "https://guipianwu.com/",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if stream.Container != source.ContainerMP4 || stream.Encrypted || stream.NeedsSanitize {
		t.Fatalf("直链映射不符: %+v", stream)
	}
	if stream.Headers["Referer"] != "https://guipianwu.com/" {
		t.Fatalf("Headers = %v", stream.Headers)
	}
	// 红果一类的加密源在 Chapter.VideoURL 里放的是内核中间态标记，必须拒绝：
	// 那种地址要先在 core 内经 providerMedia 解析，再走 StreamFromProviderMedia。
	for _, raw := range []string{"hongguo-cenc://7301", "", "/media/a.mp4", "guipian-cenc://x"} {
		if _, err := adapter.StreamFromChapter(core.Chapter{VideoURL: raw}); !errors.Is(err, adapter.ErrInvalidStream) {
			t.Errorf("Chapter.VideoURL=%q 应被拒绝，得到 %v", raw, err)
		}
	}
}

func TestStreamFromNativePlan(t *testing.T) {
	expires := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	plan := adapter.NativePlan{
		URL:       "https://cdn.example.com/a.mp4",
		Headers:   map[string]string{"Referer": "https://novel.snssdk.com/"},
		Key:       strings.ToUpper(testKeyHex), // 上游若给大写，契约要求归一为小写
		Quality:   1080,
		Qualities: []int{1080, 720},
		ExpiresAt: expires.UnixMilli(),
		Session:   "session-1",
	}
	stream, err := adapter.StreamFromNativePlan(plan)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if stream.DecryptionKey != testKeyHex {
		t.Fatalf("密钥未归一为小写: %q", stream.DecryptionKey)
	}
	if !stream.NeedsSanitize || stream.Quality != "1080p" {
		t.Fatalf("计划映射不符: %+v", stream)
	}
	// core 的 ExpiresAt 是 Unix 毫秒，契约是 RFC3339 绝对时刻。
	if !stream.ExpiresAt.Equal(expires) {
		t.Fatalf("ExpiresAt = %v，期望 %v", stream.ExpiresAt, expires)
	}
	// 头是副本：改动结果不能污染 core 的计划。
	stream.Headers["Referer"] = "tampered"
	if plan.Headers["Referer"] != "https://novel.snssdk.com/" {
		t.Fatal("Headers 必须是副本")
	}
	// 过期时间为 0 表示未知。
	noExpiry, err := adapter.StreamFromNativePlan(adapter.NativePlan{URL: "https://cdn.example.com/a.mp4"})
	if err != nil || !noExpiry.ExpiresAt.IsZero() {
		t.Fatalf("零值过期时间处理不符: %+v, %v", noExpiry, err)
	}

	cases := []struct {
		name string
		plan adapter.NativePlan
	}{
		// 本地计划：URL 是本地路径，不是「怎么取流」，不能伪装成网络流。
		{"本地文件计划", adapter.NativePlan{Local: true, URL: "/data/downloads/a.mp4"}},
		{"空地址", adapter.NativePlan{}},
		{"密钥长度不足", adapter.NativePlan{URL: "https://cdn.example.com/a.mp4", Key: "abcd"}},
		{"密钥非十六进制", adapter.NativePlan{URL: "https://cdn.example.com/a.mp4", Key: strings.Repeat("z", 32)}},
	}
	for _, item := range cases {
		if _, err := adapter.StreamFromNativePlan(item.plan); !errors.Is(err, adapter.ErrInvalidStream) {
			t.Errorf("%s: 期望 ErrInvalidStream，得到 %v", item.name, err)
		}
	}
}

func TestStreamFromNativePlanValue(t *testing.T) {
	stream, err := adapter.StreamFromNativePlanValue(map[string]any{
		"url":           "https://cdn.example.com/play.m3u8",
		"headers":       map[string]string{"Referer": "https://cdn.example.com/"},
		"decryptionKey": testKeyHex,
		"quality":       float64(720),
		"expiresAt":     float64(1700000000000),
		"local":         false,
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if stream.Container != source.ContainerHLS || !stream.Encrypted || stream.Quality != "720p" {
		t.Fatalf("JSON 往返不符: %+v", stream)
	}
	if !stream.ExpiresAt.Equal(time.UnixMilli(1700000000000).UTC()) {
		t.Fatalf("ExpiresAt = %v", stream.ExpiresAt)
	}
	if _, err := adapter.StreamFromNativePlanValue("not-an-object"); !errors.Is(err, adapter.ErrInvalidStream) {
		t.Errorf("非对象形态应报 ErrInvalidStream，得到 %v", err)
	}
}

func TestQualityLabel(t *testing.T) {
	cases := map[int]string{
		0:     "auto",
		-1:    "auto",
		99:    "auto",
		100:   "100p",
		360:   "360p",
		480:   "480p",
		720:   "720p",
		1080:  "1080p",
		1440:  "1440p",
		2160:  "2160p",
		9999:  "9999p",
		10000: "auto",
	}
	for height, want := range cases {
		if got := adapter.QualityLabel(height); got != want {
			t.Errorf("QualityLabel(%d) = %q，期望 %q", height, got, want)
		}
	}
	if height := adapter.HeightFromQuality(source.QualityAuto); height != 0 {
		t.Errorf("HeightFromQuality(auto) = %d", height)
	}
	if height := adapter.HeightFromQuality(source.Quality1080p); height != 1080 {
		t.Errorf("HeightFromQuality(1080p) = %d", height)
	}
	// 非法/未知标签一律当作「自选」。
	for _, label := range []source.Quality{"", "4k", "p", "1080", "1080px", "auto "} {
		if height := adapter.HeightFromQuality(label); height != 0 {
			t.Errorf("HeightFromQuality(%q) = %d，期望 0", label, height)
		}
	}
	if got := adapter.QualityLabel(adapter.HeightFromQuality(source.Quality720p)); got != "720p" {
		t.Errorf("往返后 = %q", got)
	}
	// 线格式必须与 core 的十六进制形态一致。
	expected := hex.EncodeToString(testKeyBytes)
	if testKeyHex != expected {
		t.Fatalf("测试密钥常量与 core 的编码形态不一致: %q vs %q", testKeyHex, expected)
	}
}

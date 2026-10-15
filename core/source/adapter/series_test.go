// 本仓新增（M1 站源适配层测试）。
//
// 覆盖 core.Drama / core.nativeDrama（镜像形态 + any/JSON 形态）→ 契约 Series / Page，
// 含封面归一、年份与集数抽取，以及未知源、空 ID 的拒绝路径。
//
// 说明：core.nativeDrama 未导出，包外无法做编译期断言，因此这里用「黄金 JSON」夹具
// ——tag 与形态手工摘录自 core 的 nativeDrama 定义（core/app_runtime.go）。M2 接线时
// 应在 core 内补一条无损反序列化断言；本次里程碑不改动 core 目录，故不在此越界。
package adapter_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dawei233/hongguo-next/core"
	"github.com/dawei233/hongguo-next/core/source"
	"github.com/dawei233/hongguo-next/core/source/adapter"
)

func TestSeriesFromDrama(t *testing.T) {
	drama := core.Drama{
		ID:           "hongguo:12345",
		Source:       "hongguo",
		SourceID:     "12345",
		Title:        "都市之巅",
		Cover:        "https://cdn.example.com/cover.jpg",
		TotalEpisode: "24",
		CategoryName: "都市",
		Score:        "9.2分",
		OnlineDate:   "2024-05-01",
	}
	series, err := adapter.SeriesFromDrama(drama)
	if err != nil {
		t.Fatalf("SeriesFromDrama 失败: %v", err)
	}
	if series.ID != source.ID("hongguo:12345") || series.SourceID != source.Hongguo {
		t.Fatalf("ID/SourceID 不符: %+v", series)
	}
	if series.Title != "都市之巅" || series.Cover != "https://cdn.example.com/cover.jpg" {
		t.Fatalf("标题/封面不符: %+v", series)
	}
	if series.Year != 2024 || series.Total != 24 {
		t.Fatalf("年份/集数不符: %+v", series)
	}
	if series.Tag != "都市" || series.Score != "9.2分" {
		t.Fatalf("标签/评分不符: %+v", series)
	}
}

func TestSeriesFromDramaCoverVariants(t *testing.T) {
	cases := []struct {
		name  string
		drama core.Drama
		want  string
	}{
		{"协议相对", core.Drama{ID: "hongguo:1", Cover: "//cdn.example.com/a.jpg"}, "https://cdn.example.com/a.jpg"},
		{"内核代理形态", core.Drama{ID: "hongguo:1", Cover: "/api/ui/image?url=https%3A%2F%2Fcdn.example.com%2Fb.jpg"}, "https://cdn.example.com/b.jpg"},
		{"候选顺序与 map 形态", core.Drama{ID: "hongguo:1", Cover: map[string]any{"url": "https://cdn.example.com/c.jpg"}}, "https://cdn.example.com/c.jpg"},
		{"封面回退到 CoverURL", core.Drama{ID: "hongguo:1", CoverURL: "https://cdn.example.com/d.jpg"}, "https://cdn.example.com/d.jpg"},
		// 相对路径不满足契约的「绝对 URL 或空串」，必须清空而不是伪装成地址。
		{"相对路径清空", core.Drama{ID: "hongguo:1", Cover: "covers/a.jpg"}, ""},
		{"无封面", core.Drama{ID: "hongguo:1"}, ""},
	}
	for _, item := range cases {
		series, err := adapter.SeriesFromDrama(item.drama)
		if err != nil {
			t.Errorf("%s: %v", item.name, err)
			continue
		}
		if series.Cover != item.want {
			t.Errorf("%s: Cover = %q，期望 %q", item.name, series.Cover, item.want)
		}
	}
}

func TestSeriesFromDramaFieldFallbacks(t *testing.T) {
	// 集数取候选字段的最大值（与 core 的 nativeNormalize 同序）。
	drama := core.Drama{ID: "hongguo:1", TotalEpisode: "12", ChapterCount: float64(24), Episodes: "6"}
	series, err := adapter.SeriesFromDrama(drama)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if series.Total != 24 {
		t.Fatalf("Total = %d，期望 24（取最大值）", series.Total)
	}
	// 年份：RFC3339 与纯日期都能取到；无法识别时为 0（未知）。
	for raw, want := range map[string]int{
		"2024-05-01T10:00:00Z": 2024,
		"2023-01-02":           2023,
		"暂无":                   0,
		"":                     0,
	} {
		got, err := adapter.SeriesFromDrama(core.Drama{ID: "hongguo:1", OnlineDate: raw})
		if err != nil {
			t.Fatalf("OnlineDate=%q: %v", raw, err)
		}
		if got.Year != want {
			t.Errorf("OnlineDate=%q: Year = %d，期望 %d", raw, got.Year, want)
		}
	}
	// 无标题时与 core 的 DisplayTitle 兜底一致。
	bare, err := adapter.SeriesFromDrama(core.Drama{ID: "hongguo:1"})
	if err != nil || bare.Title != "短剧" {
		t.Fatalf("标题兜底 = %q, %v", bare.Title, err)
	}
	// 标签退回 ChannelName（core 的 nativeDramaCategory 白名单）。
	channel, err := adapter.SeriesFromDrama(core.Drama{ID: "hongguo:1", ChannelName: "漫剧"})
	if err != nil || channel.Tag != "漫剧" {
		t.Fatalf("ChannelName 标签 = %q, %v", channel.Tag, err)
	}
	unknownChannel, err := adapter.SeriesFromDrama(core.Drama{ID: "hongguo:1", ChannelName: "其他"})
	if err != nil || unknownChannel.Tag != "" {
		t.Fatalf("白名单外的 ChannelName 不应成为标签: %q, %v", unknownChannel.Tag, err)
	}
}

func TestSeriesFromDramaIdentity(t *testing.T) {
	// 没有剧 ID 时用 Source + SourceID 组装；facet 名归一为统一站源 ID。
	facet, err := adapter.SeriesFromDrama(core.Drama{Source: "huangguo-video", SourceID: "77", Title: "剧"})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if facet.ID != source.ID("huangguo:77") || facet.SourceID != source.Huangguo {
		t.Fatalf("facet 归一失败: %+v", facet)
	}
	// 只有剧 ID 也能推出站源。
	byID, err := adapter.SeriesFromDrama(core.Drama{ID: "huangguoai:88"})
	if err != nil || byID.SourceID != source.Huangguo || byID.ID != source.ID("huangguo:88") {
		t.Fatalf("按剧 ID 推导失败: %+v, %v", byID, err)
	}

	cases := []struct {
		name  string
		drama core.Drama
	}{
		{"全空", core.Drama{}},
		{"只有未知站源", core.Drama{Source: "bilibili", SourceID: "1"}},
		{"缺源内 ID", core.Drama{Source: "hongguo"}},
		{"未知前缀的剧 ID", core.Drama{ID: "bilibili:1"}},
	}
	for _, item := range cases {
		if _, err := adapter.SeriesFromDrama(item.drama); !errors.Is(err, adapter.ErrInvalidSeries) {
			t.Errorf("%s: 期望 ErrInvalidSeries，得到 %v", item.name, err)
		}
	}
}

// nativeDramaGolden 是 core 写入 sources.json / nativeInput.drama 的形态。
const nativeDramaGolden = `{
  "metadataSchema": 1,
  "id": "hongguo:12345",
  "source": "hongguo",
  "sourceId": "12345",
  "title": "都市之巅",
  "description": "简介",
  "cover": "https://cdn.example.com/cover.jpg",
  "episodes": 24,
  "category": "都市",
  "vip": true,
  "heat": "1.2万",
  "views": "3万",
  "onlineDate": "2024-05-01",
  "tags": ["都市"],
  "releaseStatus": ""
}`

func TestSeriesFromNativeDramaValue(t *testing.T) {
	series, err := adapter.SeriesFromNativeDramaValue(json.RawMessage(nativeDramaGolden))
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if series.ID != source.ID("hongguo:12345") || series.SourceID != source.Hongguo {
		t.Fatalf("身份不符: %+v", series)
	}
	if series.Title != "都市之巅" || series.Tag != "都市" || series.Total != 24 || series.Year != 2024 {
		t.Fatalf("字段不符: %+v", series)
	}
	// core.nativeDrama 不携带评分：镜像路径的 Score 必须如实为空。
	if series.Score != "" {
		t.Fatalf("镜像路径的 Score 应为空，得到 %q", series.Score)
	}
	// 缺标题时同样有兜底。
	bare, err := adapter.SeriesFromNativeDrama(adapter.NativeDrama{ID: "hongguo:1"})
	if err != nil || bare.Title != "短剧" {
		t.Fatalf("标题兜底 = %q, %v", bare.Title, err)
	}
	// 未知站源与非对象形态都要被拒绝。
	if _, err := adapter.SeriesFromNativeDrama(adapter.NativeDrama{ID: "bilibili:1"}); !errors.Is(err, adapter.ErrInvalidSeries) {
		t.Errorf("未知站源应报 ErrInvalidSeries，得到 %v", err)
	}
	for _, value := range []any{nil, 42, "text", []string{"a"}} {
		if _, err := adapter.SeriesFromNativeDramaValue(value); !errors.Is(err, adapter.ErrInvalidSeries) {
			t.Errorf("SeriesFromNativeDramaValue(%v) 应报 ErrInvalidSeries，得到 %v", value, err)
		}
	}
}

func TestPageFromNativeCatalogValue(t *testing.T) {
	page, err := adapter.PageFromNativeCatalogValue(map[string]any{
		"items":   []any{json.RawMessage(nativeDramaGolden)},
		"hasMore": true,
		"page":    2,
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if len(page.Items) != 1 || !page.HasMore {
		t.Fatalf("Page = %+v", page)
	}
	// core 的目录结果不带总数：契约用 0 表示未知，不得猜测。
	if page.Total != 0 {
		t.Fatalf("Total 应为 0（未知），得到 %d", page.Total)
	}
	// 单项非法即整页失败。
	if _, err := adapter.PageFromNativeCatalogValue(map[string]any{"items": []any{map[string]any{"id": "bilibili:1"}}}); !errors.Is(err, adapter.ErrInvalidSeries) {
		t.Errorf("非法项应报 ErrInvalidSeries，得到 %v", err)
	}
	empty, err := adapter.PageFromNativeCatalog(adapter.NativeCatalogPage{})
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("空目录应返回空切片: %+v, %v", empty, err)
	}
}

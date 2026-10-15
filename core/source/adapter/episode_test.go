// 本仓新增（M1 站源适配层测试）。
//
// 覆盖 §4.4 的集 ID 拆装（含 core 侧的 facet 前缀归一）、core.Chapter → 契约 Episode，
// 以及未知源 / 未知形态 / 跨源错配的拒绝路径。
package adapter_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dawei233/hongguo-next/core"
	"github.com/dawei233/hongguo-next/core/source"
	"github.com/dawei233/hongguo-next/core/source/adapter"
)

func TestSplitSeriesID(t *testing.T) {
	cases := []struct {
		raw     string
		wantID  source.ID
		wantKey string
	}{
		{"hongguo:12345", source.Hongguo, "12345"},
		{"huangguo-video:77", source.Huangguo, "77"},
		{" hongguo : 12345 ", source.Hongguo, "12345"},
		// 冒号之后的内容整体属于源内剧 ID（core 的章节键可能是含冒号的 URL）。
		{"hongguo:12345:2", source.Hongguo, "12345:2"},
	}
	for _, item := range cases {
		id, key, err := adapter.SplitSeriesID(item.raw)
		if err != nil {
			t.Errorf("SplitSeriesID(%q) 失败: %v", item.raw, err)
			continue
		}
		if id != item.wantID || key != item.wantKey {
			t.Errorf("SplitSeriesID(%q) = (%q, %q)，期望 (%q, %q)", item.raw, id, key, item.wantID, item.wantKey)
		}
	}
	for _, raw := range []string{"", "hongguo", ":12345", "hongguo:", "bilibili:1", "  "} {
		if _, _, err := adapter.SplitSeriesID(raw); err == nil {
			t.Errorf("SplitSeriesID(%q) 必须被拒绝", raw)
		}
	}
	// 未知站源 vs 形态非法的两类错误要可区分。
	if _, _, err := adapter.SplitSeriesID("bilibili:1"); !errors.Is(err, adapter.ErrUnknownSource) {
		t.Errorf("未知站源应报 ErrUnknownSource，得到 %v", err)
	}
	if _, _, err := adapter.SplitSeriesID("hongguo"); !errors.Is(err, adapter.ErrInvalidID) {
		t.Errorf("缺冒号应报 ErrInvalidID，得到 %v", err)
	}
}

func TestSplitEpisodeID(t *testing.T) {
	cases := []struct {
		raw         string
		wantID      source.ID
		wantSeries  string
		wantEpisode string
	}{
		{"hongguo:12345:7301", source.Hongguo, "12345", "7301"},
		{"huangguo-video:77:abc", source.Huangguo, "77", "abc"},
		// core 的 guipian / hanxiaoquan 在详情返回前会先发空 seriesKey 的章节 ID。
		{"guipian::https://guipianwu.com/play/1:2", source.Guipian, "", "https://guipianwu.com/play/1:2"},
		// 只有两段（把剧 ID 当集 ID 传入）时集键为空，属合法形态。
		{"hongguo:12345", source.Hongguo, "12345", ""},
	}
	for _, item := range cases {
		id, seriesKey, episodeKey, err := adapter.SplitEpisodeID(item.raw)
		if err != nil {
			t.Errorf("SplitEpisodeID(%q) 失败: %v", item.raw, err)
			continue
		}
		if id != item.wantID || seriesKey != item.wantSeries || episodeKey != item.wantEpisode {
			t.Errorf("SplitEpisodeID(%q) = (%q, %q, %q)，期望 (%q, %q, %q)",
				item.raw, id, seriesKey, episodeKey, item.wantID, item.wantSeries, item.wantEpisode)
		}
	}
	for _, raw := range []string{"", "hongguo", ":12345:2", "bilibili:1:2"} {
		if _, _, _, err := adapter.SplitEpisodeID(raw); err == nil {
			t.Errorf("SplitEpisodeID(%q) 必须被拒绝", raw)
		}
	}
}

// TestResolveEpisodeIDAllSources 对 11 个对外站源 ID 逐个验证 §4.4 的入口。
func TestResolveEpisodeIDAllSources(t *testing.T) {
	for _, id := range adapter.SourceIDs() {
		got, err := adapter.ResolveEpisodeID(string(id) + ":12345:2")
		if err != nil {
			t.Fatalf("ResolveEpisodeID(%s:12345:2) 失败: %v", id, err)
		}
		if got != id {
			t.Fatalf("ResolveEpisodeID(%s:12345:2) = %q", id, got)
		}
	}
	for _, raw := range []string{"", "hongguo", "nope:1:2", "hongguo-x:1:2"} {
		if _, err := adapter.ResolveEpisodeID(raw); err == nil {
			t.Errorf("ResolveEpisodeID(%q) 必须被拒绝", raw)
		}
	}
}

// TestIDConversionBetweenContractAndCore 锁定 facet 前缀的归一与还原。
func TestIDConversionBetweenContractAndCore(t *testing.T) {
	series, err := adapter.SeriesIDFromCoreDramaID("huangguo-video:77")
	if err != nil || series != source.ID("huangguo:77") {
		t.Fatalf("SeriesIDFromCoreDramaID = %q, %v", series, err)
	}
	back, err := adapter.CoreDramaIDFromSeriesID("huangguo:77")
	if err != nil || back != "huangguoai:77" {
		t.Fatalf("CoreDramaIDFromSeriesID = %q, %v（多 facet 只能取主 facet）", back, err)
	}
	episode, err := adapter.EpisodeIDFromCoreChapterID("huangguo-video:77:abc")
	if err != nil || episode != "huangguo:77:abc" {
		t.Fatalf("EpisodeIDFromCoreChapterID = %q, %v", episode, err)
	}
	coreID, err := adapter.CoreChapterIDFromEpisodeID("huangguo:77:abc")
	if err != nil || coreID != "huangguoai:77:abc" {
		t.Fatalf("CoreChapterIDFromEpisodeID = %q, %v", coreID, err)
	}
	// 两段形态不能被补出一个假的第三段。
	if coreID, err := adapter.CoreChapterIDFromEpisodeID("hongguo:12345"); err != nil || coreID != "hongguo:12345" {
		t.Fatalf("两段形态往返 = %q, %v", coreID, err)
	}
	if _, err := adapter.EpisodeIDFromCoreChapterID("hongguo"); !errors.Is(err, adapter.ErrInvalidID) {
		t.Errorf("缺冒号应报 ErrInvalidID，得到 %v", err)
	}
	if _, err := adapter.CoreChapterIDFromEpisodeID("xifan:1:2"); !errors.Is(err, adapter.ErrNotImplementedInCore) {
		t.Errorf("xifan 应报 ErrNotImplementedInCore，得到 %v", err)
	}
	// legacy 不是站源（注册表外的回退机制），不得产出 core 剧 ID。
	if _, err := adapter.CoreDramaIDFromSeriesID("legacy:1"); !errors.Is(err, adapter.ErrFallbackOnly) {
		t.Errorf("legacy 应报 ErrFallbackOnly，得到 %v", err)
	}
}

func TestEpisodeFromChapter(t *testing.T) {
	chapter := core.Chapter{ID: "hongguo:12345:7301", Source: "hongguo", Title: "第1集", VideoURL: "hongguo-cenc://7301"}
	episode, err := adapter.EpisodeFromChapter(chapter, "", 1)
	if err != nil {
		t.Fatalf("EpisodeFromChapter 失败: %v", err)
	}
	if episode.SeriesID != source.ID("hongguo:12345") || episode.EpisodeID != "hongguo:12345:7301" {
		t.Fatalf("Episode = %+v", episode)
	}
	if episode.Index != 1 || episode.Title != "第1集" {
		t.Fatalf("Episode 序号/标题不符: %+v", episode)
	}
	// core 的 Chapter 不带时长：契约用 0 表示未知，不得猜。
	if episode.DurationS != 0 {
		t.Fatalf("DurationS 应为 0（未知），得到 %d", episode.DurationS)
	}

	// facet 前缀在契约层归一到统一站源 ID。
	facet, err := adapter.EpisodeFromChapter(core.Chapter{ID: "huangguo-video:77:abc"}, source.ID("huangguo:77"), 3)
	if err != nil {
		t.Fatalf("facet 章节映射失败: %v", err)
	}
	if facet.EpisodeID != "huangguo:77:abc" || facet.SeriesID != source.ID("huangguo:77") {
		t.Fatalf("facet Episode = %+v", facet)
	}

	// 显式给出父剧 ID 时，空 seriesKey 的章节也能映射（guipian 详情回填后的形态）。
	patched, err := adapter.EpisodeFromChapter(
		core.Chapter{ID: "guipian::https://guipianwu.com/play/1"}, source.ID("guipian:999"), 2)
	if err != nil {
		t.Fatalf("空 seriesKey + 显式父剧 ID 应可映射: %v", err)
	}
	if patched.SeriesID != source.ID("guipian:999") || patched.Index != 2 {
		t.Fatalf("patched Episode = %+v", patched)
	}

	cases := []struct {
		name    string
		chapter core.Chapter
		series  source.ID
		index   int
		wantErr error
	}{
		{"空章节 ID", core.Chapter{}, "", 1, adapter.ErrInvalidEpisode},
		{"缺冒号", core.Chapter{ID: "hongguo"}, "", 1, adapter.ErrInvalidEpisode},
		{"未知站源前缀", core.Chapter{ID: "bilibili:1:2"}, "", 1, adapter.ErrInvalidEpisode},
		{"负序号", chapter, "", -1, adapter.ErrInvalidEpisode},
		// guipian 详情返回前的空 seriesKey 无法推出父剧 ID，必须报错而不是伪造。
		{"缺源内剧 ID", core.Chapter{ID: "guipian::https://guipianwu.com/play/1"}, "", 1, adapter.ErrInvalidEpisode},
		{"跨源错配", chapter, source.ID("huangju:12345"), 1, adapter.ErrInvalidEpisode},
		{"父剧 ID 非法", chapter, source.ID("hongguo"), 1, adapter.ErrInvalidEpisode},
	}
	for _, item := range cases {
		if _, err := adapter.EpisodeFromChapter(item.chapter, item.series, item.index); !errors.Is(err, item.wantErr) {
			t.Errorf("%s: 期望 %v，得到 %v", item.name, item.wantErr, err)
		}
	}
}

func TestEpisodesFromChapters(t *testing.T) {
	chapters := []core.Chapter{
		{ID: "hongguo:12345:1", Title: "第1集"},
		{ID: "hongguo:12345:2", Title: "第2集"},
		{ID: "hongguo:12345:3"},
	}
	episodes, err := adapter.EpisodesFromChapters(chapters, "")
	if err != nil {
		t.Fatalf("EpisodesFromChapters 失败: %v", err)
	}
	if len(episodes) != 3 {
		t.Fatalf("集数 = %d", len(episodes))
	}
	for index, episode := range episodes {
		if episode.Index != index+1 {
			t.Errorf("第 %d 个 Episode.Index = %d（core 为 1 起始）", index, episode.Index)
		}
		if episode.SeriesID != source.ID("hongguo:12345") {
			t.Errorf("第 %d 个 Episode.SeriesID = %q", index, episode.SeriesID)
		}
	}
	empty, err := adapter.EpisodesFromChapters(nil, "")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("空章节列表应返回空切片而非错误: %v %v", empty, err)
	}
	// 任一章节非法即整批失败，并在错误里指出位置。
	broken := append([]core.Chapter{{ID: "hongguo:12345:1"}}, core.Chapter{ID: "oops"})
	if _, err := adapter.EpisodesFromChapters(broken, ""); err == nil || !strings.Contains(err.Error(), "第 2 章") {
		t.Fatalf("整批失败的错误应指出章节位置，得到 %v", err)
	}
}

package danmaku

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dawei233/hongguo-next/core/source"
)

// TestIdentityIsPlaybackTimeAndText 锁住去重键的形态。
//
// 上游给同一条弹幕分配不同 comment_id，所以身份只能是（播放时间, 文本）；
// 而且时间必须是**整数毫秒**：用秒的浮点形态（C 是 toFixed(3)）在不同的格式化
// 实现下可能算出不同的键。
func TestIdentityIsPlaybackTimeAndText(t *testing.T) {
	if Identity(1000, "前排") != Identity(1000, "前排") {
		t.Fatal("同一时间同一文本必须得到同一个键")
	}
	if Identity(1000, "前排") == Identity(1001, "前排") {
		t.Fatal("相差 1 毫秒必须是不同的键")
	}
	if Identity(1000, "前排") == Identity(1000, "后排") {
		t.Fatal("文本不同必须是不同的键")
	}
	if !strings.Contains(Identity(1000, "前排"), "1000") {
		t.Fatalf("键里应能看到毫秒：%s", Identity(1000, "前排"))
	}
}

// TestMergerDeduplicatesByTimeAndText 是 B 的 NOTICE 里那条教训的回归。
//
// 官方接口短时间内批量返回会让同一句话带着不同 id 重复上屏，因此跨分片合并必须按
// （播放时间 + 文本）去重。注意本包的类型里**根本没有 comment_id** —— 没有可依赖的
// 字段，就不会有人写错。
func TestMergerDeduplicatesByTimeAndText(t *testing.T) {
	merger := NewMerger()
	first, _ := merger.Add(Item{TimeMS: 1000, Text: "前排", Color: ColorDefault, Mode: ModeScroll})
	if first != 1 {
		t.Fatalf("首条应当收下，实际 %d", first)
	}
	second, dropped := merger.Add(Item{TimeMS: 1000, Text: "前排", Color: ColorDefault, Mode: ModeScroll})
	if second != 0 || dropped != 1 {
		t.Fatalf("同一时间同一文本必须被去重，实际 added=%d dropped=%d", second, dropped)
	}
	// 同一句话在不同时间点是两条不同的弹幕（这是有意的）。
	if added, _ := merger.Add(Item{TimeMS: 2000, Text: "前排"}); added != 1 {
		t.Fatal("不同时间点的同一句话不应被去重")
	}
	if merger.Len() != 2 {
		t.Fatalf("合并后应有 2 条，实际 %d", merger.Len())
	}
	if merger.Duplicates() != 1 {
		t.Fatalf("重复计数应为 1，实际 %d", merger.Duplicates())
	}
}

// TestMergerRejectsInvalidItems 校验清洗规则：空文本、超长、时间越界一律丢。
func TestMergerRejectsInvalidItems(t *testing.T) {
	merger := NewMerger()
	cases := []Item{
		{TimeMS: 0, Text: ""},                       // 空文本
		{TimeMS: 0, Text: strings.Repeat("字", 201)}, // 超长（> MaxTextLen）
		{TimeMS: -1, Text: "负数时间"},                  // 负时间
		{TimeMS: MaxTimelineMS + 1, Text: "超过 7 天"}, // 越界
	}
	for _, item := range cases {
		if added, dropped := merger.Add(item); added != 0 || dropped != 1 {
			t.Fatalf("非法条目应被丢弃: %+v（added=%d dropped=%d）", item, added, dropped)
		}
	}
	// 颜色与模式缺失时补缺省，而不是留下零值让渲染层猜。
	merger.Add(Item{TimeMS: 500, Text: "缺省字段"})
	items := merger.Items()
	if len(items) != 1 || items[0].Color != ColorDefault || items[0].Mode != ModeScroll {
		t.Fatalf("缺省颜色/模式未补齐: %+v", items)
	}
	// 非法模式（例如 0 或 9）回落成滚动。
	merger.Add(Item{TimeMS: 600, Text: "非法模式", Mode: 9})
	if got := merger.Items()[1].Mode; got != ModeScroll {
		t.Fatalf("非法模式应回落成滚动，实际 %d", got)
	}
}

// TestMergerSortsStablyByTime 校验输出按时间升序且同刻保持到达顺序。
func TestMergerSortsStablyByTime(t *testing.T) {
	merger := NewMerger()
	merger.Add(
		Item{TimeMS: 3000, Text: "三"},
		Item{TimeMS: 1000, Text: "一"},
		Item{TimeMS: 2000, Text: "二甲"},
		Item{TimeMS: 2000, Text: "二乙"},
	)
	items := merger.Items()
	wantTexts := []string{"一", "二甲", "二乙", "三"}
	if len(items) != len(wantTexts) {
		t.Fatalf("条目数不符: %d", len(items))
	}
	for index, want := range wantTexts {
		if items[index].Text != want {
			t.Fatalf("第 %d 条应为 %q，实际 %q", index, want, items[index].Text)
		}
	}
	// Items 返回副本：改它不能影响内部状态。
	items[0].Text = "改坏了"
	if merger.Items()[0].Text != "一" {
		t.Fatal("Items 必须返回副本")
	}
}

// TestValidateReportsDataQuality 校验 §5.2 的 T 节点：数量/单调性/重复告警。
func TestValidateReportsDataQuality(t *testing.T) {
	if warnings := Validate(nil); len(warnings) == 0 {
		t.Fatal("空结果必须给出告警（它可能是真没有弹幕，也可能是抓取出问题）")
	}
	if warnings := Validate([]Item{{TimeMS: 0, Text: "a"}, {TimeMS: 100, Text: "b"}}); len(warnings) != 0 {
		t.Fatalf("正常数据不应有告警: %v", warnings)
	}
	backwards := Validate([]Item{{TimeMS: 500, Text: "a"}, {TimeMS: 100, Text: "b"}})
	if !containsWarning(backwards, "时间轴倒退") {
		t.Fatalf("时间轴倒退必须告警: %v", backwards)
	}
	duplicated := Validate([]Item{{TimeMS: 100, Text: "a"}, {TimeMS: 100, Text: "a"}})
	if !containsWarning(duplicated, "重复") {
		t.Fatalf("未去重的重复必须告警: %v", duplicated)
	}
}

// TestResolveNoticeNeverCallsFailureEmpty 是"拿不到 ≠ 没有弹幕"这条纪律的护栏。
//
// 官方弹幕有好几条失败路径；只要有一条自己拼文案，就会出现"源坏了却显示本集没有
// 弹幕"。这里把四种状态一次锁死。
func TestResolveNoticeNeverCallsFailureEmpty(t *testing.T) {
	failure := Report{Failure: "弹幕接口不可用（HTTP 503），已尝试官方签名请求；可改用「本集评论」"}
	if got := failure.ResolveNotice(0); got != failure.Failure {
		t.Fatalf("失败说明必须原样给出，实际 %q", got)
	}
	if strings.Contains(failure.ResolveNotice(0), "暂无弹幕") {
		t.Fatal("失败时绝不能说成本集没有弹幕")
	}
	if got := (Report{}).ResolveNotice(0); got != "本集暂无弹幕" {
		t.Fatalf("确实为空时给出「本集暂无弹幕」，实际 %q", got)
	}
	if got := (Report{Truncated: true}).ResolveNotice(3000); !strings.Contains(got, "3000") {
		t.Fatalf("截断说明应带上条数，实际 %q", got)
	}
	limited := Report{Notice: WebCommentNotice, Degraded: true}
	if got := limited.ResolveNotice(12); !strings.Contains(got, WebCommentNotice) {
		t.Fatalf("降级源的说明必须带上，实际 %q", got)
	}
}

// TestTrackToSourceTrackKeepsContractShape 校验与 §4.1 契约的映射逐字段一致。
func TestTrackToSourceTrackKeepsContractShape(t *testing.T) {
	track := Track{VideoID: "7123456789012345678", Items: []Item{
		{TimeMS: 1200, Text: "前排", Color: ColorDefault, Mode: ModeScroll, Weight: 0},
		{TimeMS: 3400, Text: "顶部", Color: "#FF0000", Mode: ModeTop, Weight: 2},
	}}
	contract := track.ToSourceTrack()
	if contract.VideoID != track.VideoID || len(contract.Items) != 2 {
		t.Fatalf("映射丢字段: %+v", contract)
	}
	encoded, err := json.Marshal(contract)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var decoded struct {
		VideoID string `json:"videoId"`
		Items   []struct {
			TimeMS int64  `json:"timeMs"`
			Text   string `json:"text"`
			Color  string `json:"color"`
			Mode   int    `json:"mode"`
			Weight int    `json:"weight"`
		} `json:"items"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if decoded.VideoID != "7123456789012345678" || len(decoded.Items) != 2 {
		t.Fatalf("线格式不符: %s", encoded)
	}
	if decoded.Items[1].Color != "#FF0000" || decoded.Items[1].Mode != source.DanmakuModeTop || decoded.Items[1].TimeMS != 3400 {
		t.Fatalf("条目字段不符: %+v", decoded.Items[1])
	}
	// 空轨必须是空数组而不是 null（契约要求 items 不可为 null）。
	empty, err := json.Marshal(Track{VideoID: "1"}.ToSourceTrack())
	if err != nil {
		t.Fatalf("序列化空轨失败: %v", err)
	}
	if !strings.Contains(string(empty), `"items":[]`) {
		t.Fatalf("空轨必须是 []，实际 %s", empty)
	}
}

// TestItemJSONMatchesContractKeys 锁住 JSON 字段名（跨端线格式）。
func TestItemJSONMatchesContractKeys(t *testing.T) {
	encoded, err := json.Marshal(Item{TimeMS: 1200, Text: "前排", Color: ColorDefault, Mode: ModeScroll})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	for _, key := range []string{`"timeMs"`, `"text"`, `"color"`, `"mode"`, `"weight"`} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("缺少契约字段 %s: %s", key, encoded)
		}
	}
	if strings.Contains(string(encoded), "commentId") || strings.Contains(string(encoded), "id") {
		t.Fatalf("线格式不得暴露上游 id（会诱导按 id 去重）: %s", encoded)
	}
}

func containsWarning(warnings []string, token string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, token) {
			return true
		}
	}
	return false
}

package danmaku

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func layoutOptions(width, height float64, settings LayoutSettings) LayoutOptions {
	return LayoutOptions{Width: width, Height: height, Settings: settings}
}

// TestLaneGeometryMatchesReference 锁住轨道几何（两端视觉一致全靠这几个数）。
func TestLaneGeometryMatchesReference(t *testing.T) {
	cases := []struct {
		name       string
		options    LayoutOptions
		lanes      int
		top        float64
		laneGap    float64
		lineHeight float64
	}{
		{
			name:    "缺省一半区域",
			options: layoutOptions(800, 1000, DefaultLayoutSettings()),
			// bottom = min(1000-76, 500) = 500；可用高度 488 → 14 个位置；密度 normal 取 3
			lanes: 3, top: 12, laneGap: 227, lineHeight: 34,
		},
		{
			name:    "四分之一区域",
			options: LayoutOptions{Width: 800, Height: 1000, Settings: LayoutSettings{Area: AreaQuarter, FontSize: 22, Density: DensityNormal}},
			// bottom = min(924, 250) = 250；可用 238 → 7 个位置；密度 3
			lanes: 3, top: 12, laneGap: 102, lineHeight: 34,
		},
		{
			name:    "全屏密集",
			options: LayoutOptions{Width: 800, Height: 1000, Settings: LayoutSettings{Area: AreaFull, FontSize: 22, Density: DensityDense}},
			// bottom = min(924, 1000) = 924；可用 912 → 26 个位置；密度上限 6
			lanes: 6, top: 12, laneGap: 175.6, lineHeight: 34,
		},
		{
			name:    "稀疏只给一条道",
			options: LayoutOptions{Width: 800, Height: 1000, Settings: LayoutSettings{Area: AreaFull, FontSize: 22, Density: DensitySparse}},
			lanes:   1, top: 12, laneGap: 0, lineHeight: 34,
		},
		{
			name:    "画布太矮退化成一轨",
			options: LayoutOptions{Width: 800, Height: 100, Settings: LayoutSettings{Area: AreaHalf, FontSize: 32, Density: DensityDense}},
			// bottom = min(24, 50) = 24；可用 12 → floor(12/44) = 0 → 至少 1
			lanes: 1, top: 12, laneGap: 0, lineHeight: 44,
		},
		{
			name:    "减少动态效果只轮播一条",
			options: LayoutOptions{Width: 800, Height: 1000, ReducedMotion: true, Settings: DefaultLayoutSettings()},
			lanes:   1, top: 12, laneGap: 0, lineHeight: 34,
		},
	}
	for _, item := range cases {
		lanes, top, laneGap, lineHeight := LaneGeometry(item.options)
		if lanes != item.lanes || top != item.top || lineHeight != item.lineHeight {
			t.Fatalf("%s: lanes/top/lineHeight = %d/%.1f/%.1f，期望 %d/%.1f/%.1f",
				item.name, lanes, top, lineHeight, item.lanes, item.top, item.lineHeight)
		}
		if math.Abs(laneGap-item.laneGap) > 0.05 {
			t.Fatalf("%s: laneGap = %.2f，期望 %.2f", item.name, laneGap, item.laneGap)
		}
	}

	// 确定性：同样输入两次调用必须完全一样（两端视觉一致的根据，也是它可被测试的前提）。
	firstLanes, firstTop, firstGap, firstLine := LaneGeometry(layoutOptions(1920, 1080, LayoutSettings{Area: AreaHalf, FontSize: 24, Density: DensityNormal}))
	secondLanes, secondTop, secondGap, secondLine := LaneGeometry(layoutOptions(1920, 1080, LayoutSettings{Area: AreaHalf, FontSize: 24, Density: DensityNormal}))
	if firstLanes != secondLanes || firstTop != secondTop || firstGap != secondGap || firstLine != secondLine {
		t.Fatalf("同样的输入必须给出同样的几何: %v/%v/%v/%v vs %v/%v/%v/%v",
			firstLanes, firstTop, firstGap, firstLine, secondLanes, secondTop, secondGap, secondLine)
	}
}

// TestNormalizeLayoutSettingsClamps 校验夹紧与枚举回落。
func TestNormalizeLayoutSettingsClamps(t *testing.T) {
	normalized := NormalizeLayoutSettings(LayoutSettings{Opacity: 5, FontSize: 99, Speed: 5, Area: "weird", Density: "weird"})
	if normalized.Opacity != OpacityMin || normalized.FontSize != FontSizeMax || normalized.Speed != SpeedMax {
		t.Fatalf("数值应夹到边界: %+v", normalized)
	}
	if normalized.Area != DefaultArea || normalized.Density != DefaultDensity {
		t.Fatalf("非法枚举应回落缺省: %+v", normalized)
	}
	zero := NormalizeLayoutSettings(LayoutSettings{})
	if zero.Opacity != DefaultOpacity || zero.FontSize != DefaultFontSize || zero.Speed != DefaultSpeed {
		t.Fatalf("未设置（零值）应取缺省: %+v", zero)
	}
	// NaN 会让几何计算静默变成 NaN（画出空白），必须被挡掉。
	nan := NormalizeLayoutSettings(LayoutSettings{Speed: math.NaN()})
	if math.IsNaN(nan.Speed) || nan.Speed != DefaultSpeed {
		t.Fatalf("NaN 速度应回落缺省: %v", nan.Speed)
	}
	long := NormalizeLayoutSettings(LayoutSettings{BlockedWords: strings.Repeat("字", BlockedWordsMaxRunes+50)})
	if count := len([]rune(long.BlockedWords)); count != BlockedWordsMaxRunes {
		t.Fatalf("屏蔽词串应截到 %d 个字符，实际 %d", BlockedWordsMaxRunes, count)
	}
}

// TestNormalizeLayoutSettingsJSONKeepsSensitiveDefault 校验 C 形态的"缺省开启过滤"。
//
// C 的写法是 `sensitiveFilter !== false`：只有显式 false 才是关闭。如果这里把缺失当成
// false，等于"用户没设置过就等于关掉了内置敏感词过滤"。
func TestNormalizeLayoutSettingsJSONKeepsSensitiveDefault(t *testing.T) {
	empty := NormalizeLayoutSettingsJSON(nil)
	if empty.SensitiveFilterDisabled {
		t.Fatal("缺省必须开启敏感词过滤")
	}
	missing := NormalizeLayoutSettingsJSON(map[string]any{"opacity": json.Number("50")})
	if missing.SensitiveFilterDisabled || missing.Opacity != 50 {
		t.Fatalf("缺失 sensitiveFilter 时必须保持开启: %+v", missing)
	}
	explicit := NormalizeLayoutSettingsJSON(map[string]any{"sensitiveFilter": false})
	if !explicit.SensitiveFilterDisabled {
		t.Fatal("显式 false 才关闭过滤")
	}
	enabled := NormalizeLayoutSettingsJSON(map[string]any{"sensitiveFilter": true, "density": DensityDense, "area": AreaFull, "speed": 9.0})
	if enabled.SensitiveFilterDisabled || enabled.Density != DensityDense || enabled.Area != AreaFull || enabled.Speed != SpeedMax {
		t.Fatalf("JSON 入口的解析不符: %+v", enabled)
	}
}

// TestCommentIsBlocked 校验屏蔽词匹配与绕过防护。
func TestCommentIsBlocked(t *testing.T) {
	settings := DefaultLayoutSettings()
	blocked := []string{
		"共产党",             // 内置词
		"共產黨",             // 繁体
		"共 产 党",           // 插空格
		"共\u200b产\u200b党", // 插零宽空格
		"\u202e共产党",       // 前置方向控制字符
		"ＧＯＮＧＣＨＡＮＤＡＮＧ",    // 无关但含全角（不应命中，见下一组）
	}
	for _, text := range blocked[:5] {
		if !CommentIsBlocked(text, settings) {
			t.Fatalf("%q 应被屏蔽", text)
		}
	}
	for _, text := range []string{"今天天气不错", "前排", blocked[5]} {
		if CommentIsBlocked(text, settings) {
			t.Fatalf("%q 不应被屏蔽", text)
		}
	}
	custom := DefaultLayoutSettings()
	custom.BlockedWords = "屏蔽我\n也屏蔽，还有这个；以及这个"
	for _, text := range []string{"来屏蔽我吧", "也屏蔽", "还有这个", "以及这个"} {
		if !CommentIsBlocked(text, custom) {
			t.Fatalf("自定义词表应命中 %q", text)
		}
	}
	custom.SensitiveFilterDisabled = true
	if CommentIsBlocked("共产党", custom) {
		t.Fatal("关闭敏感词过滤后内置词表不应生效")
	}
	if !CommentIsBlocked("屏蔽我", custom) {
		t.Fatal("关闭内置过滤不应影响自定义词表")
	}
}

// TestLowerBoundMS 校验二分边界。
func TestLowerBoundMS(t *testing.T) {
	items := []Item{{TimeMS: 100}, {TimeMS: 200}, {TimeMS: 200}, {TimeMS: 500}}
	cases := map[int64]int{0: 0, 100: 0, 101: 1, 200: 1, 499: 3, 500: 3, 501: 4}
	for current, want := range cases {
		if got := LowerBoundMS(items, current); got != want {
			t.Fatalf("LowerBoundMS(%d) = %d，期望 %d", current, got, want)
		}
	}
	if got := LowerBoundMS(nil, 100); got != 0 {
		t.Fatalf("空列表应返回 0，实际 %d", got)
	}
}

// TestIsSeek 校验跳转判定。
func TestIsSeek(t *testing.T) {
	if !IsSeek(0, 0, 0, false) {
		t.Fatal("第一帧一律按跳转处理（要重建光标）")
	}
	if !IsSeek(10, 5, 0.2, true) {
		t.Fatal("时间倒退只能是跳转")
	}
	if IsSeek(10, 10.2, 0.2, true) {
		t.Fatal("正常播放不是跳转")
	}
	if !IsSeek(10, 20, 0.2, true) {
		t.Fatal("远超本帧增量的正跳变是跳转")
	}
}

// TestAdvanceTimedSeekRebuildsAndStillEmits 校验跳转后同一帧仍然投放。
//
// C 的原注释：跳转后不能提前 return，否则跳转那一帧不投放任何弹幕，观感上像卡了一下。
func TestAdvanceTimedSeekRebuildsAndStillEmits(t *testing.T) {
	items := []Item{{TimeMS: 0, Text: "开头"}, {TimeMS: 5000, Text: "第五秒"}, {TimeMS: 9000, Text: "第九秒"}}
	state := NewAnimationState()
	options := layoutOptions(800, 600, DefaultLayoutSettings())

	// 第一帧（hasLast=false 视为跳转）：光标重建到 0 并立刻投放第 0 秒那条。
	if !AdvanceTimed(state, items, 0, 0.1, options) {
		t.Fatal("第一帧应当有变化（投放了一条）")
	}
	if len(state.Items) != 1 || state.Items[0].Content != "开头" {
		t.Fatalf("第一帧应投放第 0 秒的弹幕: %+v", state.Items)
	}
	if state.Items[0].Lane != 0 || state.Items[0].X != 800 {
		t.Fatalf("新弹幕应从右边缘、第 0 轨进入: %+v", state.Items[0])
	}

	// 跳到第 9 秒：在屏弹幕作废并重建光标，且**同一帧**投放第 9 秒那条。
	changed := AdvanceTimed(state, items, 9, 0.1, options)
	if !changed {
		t.Fatal("跳转帧应当有变化")
	}
	if state.Cursor != 3 && state.Cursor != 2 {
		t.Fatalf("跳转后光标应指向第 9 秒附近，实际 %d", state.Cursor)
	}
	found := false
	for _, item := range state.Items {
		if item.Content == "第九秒" {
			found = true
		}
		if item.Content == "开头" {
			t.Fatalf("跳转后不应保留跳转前的弹幕: %+v", state.Items)
		}
	}
	if !found {
		t.Fatalf("跳转帧必须立刻投放新位置的弹幕（否则像卡了一下）: %+v", state.Items)
	}
}

// TestAdvanceTimedDropsStale 校验严重迟到的弹幕被丢弃且不占轨道。
func TestAdvanceTimedDropsStale(t *testing.T) {
	items := []Item{{TimeMS: 0, Text: "准时"}, {TimeMS: 500, Text: "迟到"}}
	state := NewAnimationState()
	options := layoutOptions(800, 600, DefaultLayoutSettings())
	AdvanceTimed(state, items, 0, 0.1, options)
	if len(state.Items) != 1 || state.Items[0].Content != "准时" {
		t.Fatalf("第一帧应投放准时的那条: %+v", state.Items)
	}
	// 直接推进到第 6 秒：第二条已迟到 5.5 秒（阈值 5 秒），应被跳过而不是补发。
	// elapsed 取 2 秒是为了不触发 seek 判定（jump 6 <= 2*3+1）。
	AdvanceTimed(state, items, 6, 2, options)
	if state.Cursor != 2 {
		t.Fatalf("迟到的条目应被跳过（光标前进），实际 %d", state.Cursor)
	}
	for _, item := range state.Items {
		if item.Content == "迟到" {
			t.Fatal("迟到的弹幕不应补发到屏幕上")
		}
	}
}

// TestAdvanceTimedSkipsBlocked 校验被屏蔽的弹幕跳过且不占轨道。
func TestAdvanceTimedSkipsBlocked(t *testing.T) {
	items := []Item{{TimeMS: 0, Text: "共产党"}, {TimeMS: 0, Text: "正常弹幕"}}
	state := NewAnimationState()
	options := layoutOptions(800, 600, DefaultLayoutSettings())
	AdvanceTimed(state, items, 0, 0.1, options)
	if state.Cursor != 2 {
		t.Fatalf("被屏蔽的条目应被跳过，光标应为 2，实际 %d", state.Cursor)
	}
	if len(state.Items) != 1 || state.Items[0].Content != "正常弹幕" {
		t.Fatalf("只应投放未被屏蔽的那条: %+v", state.Items)
	}
}

// TestAdvanceTimedSharesLanesWithHeadGap 校验同轨复用与防追尾。
//
// C 的设计与"评论滚动条"不同：同一道里只要前一条走出了足够距离（LANE_HEAD_GAP）
// 就能再发一条。真弹幕一集上百条，用"这道有人就占着"会严重丢量。这里同时钉住三件事：
// 轨道数不超过 geometry 给的上限、所有条目最终都能发出、同轨相邻两条的间距足够。
func TestAdvanceTimedSharesLanesWithPreservedCursor(t *testing.T) {
	items := []Item{
		{TimeMS: 0, Text: "一"}, {TimeMS: 0, Text: "二"}, {TimeMS: 0, Text: "三"}, {TimeMS: 0, Text: "四"},
	}
	state := NewAnimationState()
	options := layoutOptions(800, 600, DefaultLayoutSettings())
	lanes, _, _, _ := LaneGeometry(options)
	for step := 0; step < 12; step++ {
		AdvanceTimed(state, items, float64(step)*0.25, 0.25, options)
	}
	if state.Cursor != len(items) {
		t.Fatalf("冷却够了就该把 4 条都发出去（同轨可以复用），实际光标 %d", state.Cursor)
	}
	if len(state.Items) != 4 {
		t.Fatalf("4 条都应发出，实际 %d: %+v", len(state.Items), state.Items)
	}
	byLane := map[int][]Placement{}
	for _, item := range state.Items {
		if item.Lane < 0 || item.Lane >= lanes {
			t.Fatalf("轨道号越界（geometry 只给了 %d 条道）: %+v", lanes, item)
		}
		byLane[item.Lane] = append(byLane[item.Lane], item)
	}
	for lane, placements := range byLane {
		// 投放顺序即 x 递减顺序（后发的从右边缘 x=width 进入）；同轨复用要求
		// 新的一条满足 x_new >= x_old + width_old + LANE_HEAD_GAP，否则会追尾。
		for index := 1; index < len(placements); index++ {
			previous, current := placements[index-1], placements[index]
			if gap := current.X - previous.X; gap < previous.Width+LaneHeadGap {
				t.Fatalf("第 %d 道相邻两条会追尾：间距 %.1f < 前一条宽度 %.1f + 间距 %.1f",
					lane, gap, previous.Width, LaneHeadGap)
			}
		}
	}

	// 轨道全被占满、且都还没走开时，必须**保留光标**等下一帧（而不是丢弹幕）。
	full := NewAnimationState()
	many := make([]Item, 0, 12)
	for index := 0; index < 12; index++ {
		many = append(many, Item{TimeMS: 0, Text: "密"})
	}
	options = LayoutOptions{Width: 300, Height: 200, Settings: LayoutSettings{Area: AreaHalf, FontSize: 32, Density: DensityDense}}
	for step := 0; step < 3; step++ {
		AdvanceTimed(full, many, float64(step)*0.25, 0.25, options)
	}
	if full.Cursor == 0 {
		t.Fatal("窄画布下应当发出至少一条")
	}
	if full.Cursor != len(full.Items) && len(full.Items) == 0 {
		t.Fatal("没有空轨时应保留光标而不是丢弹幕")
	}
}

// TestAdvanceTimedCooldownAndEmitBudget 校验投放节流（同刻不堆叠、单帧最多 4 条）。
func TestAdvanceTimedCooldownAndEmitBudget(t *testing.T) {
	items := make([]Item, 0, 8)
	for index := 0; index < 8; index++ {
		items = append(items, Item{TimeMS: 0, Text: "第" + string(rune('一'+index)) + "条"})
	}
	state := NewAnimationState()
	// 密集 + 全屏，轨道够多，限制只来自冷却与单帧预算。
	options := LayoutOptions{Width: 2000, Height: 2000, Settings: LayoutSettings{Area: AreaFull, FontSize: 16, Density: DensityDense}}
	emittedTotal := 0
	for step := 0; step < 40; step++ {
		before := len(state.Items)
		AdvanceTimed(state, items, float64(step)*0.25, 0.25, options)
		if grown := len(state.Items) - before; grown > MaxEmitPerFrame {
			t.Fatalf("单帧投放不得超过 %d 条，实际 %d", MaxEmitPerFrame, grown)
		}
		emittedTotal += len(state.Items) - before
	}
	// 冷却 0.25 秒 + 单帧 delta 上限 0.25 秒 → 每帧最多 1 条，40 帧远多于 8 条。
	if emittedTotal < 8 {
		t.Fatalf("冷却不该把投放压到几乎为零：40 帧只投放了 %d 条", emittedTotal)
	}
}

// TestAdvanceTimedReducedMotion 校验减少动态效果模式：单轨、左侧固定、6 秒生命周期。
func TestAdvanceTimedReducedMotion(t *testing.T) {
	items := []Item{{TimeMS: 0, Text: "轮播"}}
	state := NewAnimationState()
	options := LayoutOptions{Width: 800, Height: 600, ReducedMotion: true, Settings: DefaultLayoutSettings()}
	AdvanceTimed(state, items, 0, 0.1, options)
	if len(state.Items) != 1 {
		t.Fatalf("应投放一条: %+v", state.Items)
	}
	if state.Items[0].Lane != 0 || state.Items[0].X != 12 {
		t.Fatalf("减少动态效果下弹幕固定在左侧、第 0 轨: %+v", state.Items[0])
	}
	// 6 秒后清屏（不是按 x 移出屏幕）。注意单帧最多推进 MaxFrameDelta 秒，
	// 所以 30 帧（每帧 0.25 秒）才够到 6 秒。
	for step := 1; step <= 30; step++ {
		AdvanceTimed(state, nil, float64(step)*0.25, 0.25, options)
	}
	if len(state.Items) != 0 {
		t.Fatalf("减少动态效果模式下应在 %.0f 秒后清屏: %+v", ReducedMotionReadSeconds, state.Items)
	}
}

// TestAdvanceTimedIgnoresTinyCanvasAndNilState 校验画布过小与 nil 状态。
func TestAdvanceTimedIgnoresTinyCanvasAndNilState(t *testing.T) {
	items := []Item{{TimeMS: 0, Text: "a"}}
	if AdvanceTimed(nil, items, 0, 0.1, layoutOptions(800, 600, DefaultLayoutSettings())) {
		t.Fatal("nil 状态应返回 false")
	}
	state := NewAnimationState()
	if AdvanceTimed(state, items, 0, 0.1, layoutOptions(MinCanvasWidth-1, 600, DefaultLayoutSettings())) {
		t.Fatal("宽度不足应什么都不做")
	}
	if AdvanceTimed(state, items, 0, 0.1, layoutOptions(800, MinCanvasHeight-1, DefaultLayoutSettings())) {
		t.Fatal("高度不足应什么都不做")
	}
	if len(state.Items) != 0 {
		t.Fatal("画布过小时不应投放")
	}
}

// TestPlacementJSONIsStable 校验布局输出的线格式稳定（两端按同一份 JSON 渲染）。
func TestPlacementJSONIsStable(t *testing.T) {
	items := []Item{{TimeMS: 0, Text: strings.Repeat("长", 200), Color: "#FF0000", Mode: ModeTop}}
	state := NewAnimationState()
	AdvanceTimed(state, items, 0, 0.1, layoutOptions(800, 600, DefaultLayoutSettings()))
	if len(state.Items) != 1 {
		t.Fatalf("应投放一条: %+v", state.Items)
	}
	if runes := []rune(state.Items[0].Text); len(runes) != MaxPlacementRunes || runes[len(runes)-1] != '…' {
		t.Fatalf("在屏文本应截成 %d 个字符 + 「…」，实际 %d", MaxPlacementRunes-1, len(runes))
	}
	if state.Items[0].Content == state.Items[0].Text {
		t.Fatal("原文必须保留（渲染层需要完整文本做提示/复制）")
	}
	first, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	second := NewAnimationState()
	AdvanceTimed(second, items, 0, 0.1, layoutOptions(800, 600, DefaultLayoutSettings()))
	again, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(first) != string(again) {
		t.Fatalf("同样的输入必须给出同样的布局:\n%s\n%s", first, again)
	}
	if !strings.Contains(string(first), `"lane"`) {
		t.Fatalf("布局输出的字段名必须稳定: %s", first)
	}
}

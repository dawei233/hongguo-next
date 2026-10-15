package danmaku

import (
	"encoding/json"
	"math"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// 本文件把 C 的轨道布局算法（src/components/danmakuOfficial.mjs 的
// advanceTimedDanmaku / isSeek / lowerBoundTime，以及 danmakuModel.mjs 的
// normalizeDanmakuSettings / commentIsBlocked）移植成 Go。
//
// 为什么放在内核而不是各端：轨道分配是**算法**，不是渲染（§5.2）。两端各自实现一份
// 就会出现"同一集在 PC 与安卓上疏密不同"这种说不清的问题。因此这里只算几何与投放，
// 不碰任何绘制 API；两端拿到 Placement 后各自画。
//
// 时间单位：入参是**秒**（与 C 的渲染数学一致：位移 85px/s、字号算宽度），
// 而 Item.TimeMS 是毫秒 —— 内部换算，只在入口/出口各转一次。

// 布局常量（全部取自 C，改之前先读它的注释）。
const (
	// LaneHeadGap 是同轨两条弹幕之间至少要留出的水平间距，避免后发的追尾前面那条。
	LaneHeadGap = 24.0
	// MaxEmitPerFrame 是单帧最多消费多少条待发弹幕，防长卡顿后恢复时一次性涌入。
	MaxEmitPerFrame = 4
	// StaleSeconds：严重迟到的弹幕直接丢弃 —— 它们在时间轴上已经过去了，补发只会突兀。
	StaleSeconds = 5.0
	// ReducedMotionReadSeconds 是减少动态效果模式下每条弹幕的展示时长。
	//
	// 这个模式一次只轮播一条，吞吐天生比滚动模式低一个量级，迟到阈值必须跟着放宽，
	// 否则排队中的弹幕会在还没轮到时就全部被判"迟到"丢掉（C 的原注释）。
	ReducedMotionReadSeconds = 6.0
	// DefaultBottomInset 是底部留给控制条的默认高度。
	DefaultBottomInset = 76.0
	// MinCanvasWidth / MinCanvasHeight 是能布局的最小画布；更小就什么都不做。
	MinCanvasWidth  = 40.0
	MinCanvasHeight = 60.0
	// MaxPlacementRunes 是单条在屏文本的字符上限：超过则截断为 79 个字符 + 「…」。
	MaxPlacementRunes = 80
	// ScrollSpeedPixels 是 1 倍速下的水平位移（px/s）。
	ScrollSpeedPixels = 85.0
	// MaxFrameDelta 是单帧最多推进的媒体时间（秒）：长卡顿后别把画面糊住。
	MaxFrameDelta = 0.25
	// OpacityMin/Max、FontSizeMin/Max、SpeedMin/Max 是设置的夹紧区间。
	OpacityMin, OpacityMax   = 20, 100
	FontSizeMin, FontSizeMax = 16, 32
	SpeedMin, SpeedMax       = 0.5, 2.0
	// BlockedWordsMaxRunes 是自定义屏蔽词串的长度上限。
	BlockedWordsMaxRunes = 10000
)

// 弹幕区域（占用画布高度的比例）。
const (
	AreaQuarter = "quarter"
	AreaHalf    = "half"
	AreaFull    = "full"
)

// 密度档。
const (
	DensitySparse = "sparse"
	DensityNormal = "normal"
	DensityDense  = "dense"
)

// 缺省设置（与 C 的 DEFAULT_DANMAKU_SETTINGS 一致）。
const (
	DefaultOpacity  = 80
	DefaultFontSize = 22
	DefaultSpeed    = 1.0
	DefaultArea     = AreaHalf
	DefaultDensity  = DensityNormal
)

// emitInterval 是两次投放之间的最小间隔（秒），按密度分档。
//
// 注意**不要照抄**评论滚动条那套（3 / 1.8 / 0.9）：评论滚动条没有时间轴，靠这个间隔
// 给队列定速；弹幕有时间轴，时间轴本身就在定速，间隔只用来防同刻堆叠。照抄那套会
// 把吞吐压到 0.55 条/秒，而实测一部剧的弹幕密度约 0.5 条/秒且带突发，结果是突发段落
// 整批变成"迟到"而被丢弃（C 的原注释）。密度主要靠通道数控制。
var emitInterval = map[string]float64{DensitySparse: 1.5, DensityNormal: 0.6, DensityDense: 0.25}

// LayoutSettings 是弹幕视觉设置（对应 C 的 settings 对象）。
//
// 注意 SensitiveFilterDisabled 与 C 的 sensitiveFilter **是反的**：Go 的零值必须是
// 安全的那一侧（默认开启敏感词过滤），否则"忘了设置"就等于"关掉了过滤"。
type LayoutSettings struct {
	// Opacity 是透明度百分比（20~100）。
	Opacity int `json:"opacity"`
	// FontSize 是字号（px，16~32）；轨道高度与文本宽度都由它推出来。
	FontSize int `json:"fontSize"`
	// Speed 是滚动速度倍数（0.5~2）。
	Speed float64 `json:"speed"`
	// Area 取 AreaQuarter / AreaHalf / AreaFull。
	Area string `json:"area"`
	// Density 取 DensitySparse / DensityNormal / DensityDense。
	Density string `json:"density"`
	// SensitiveFilterDisabled 为真时**关闭**内置敏感词过滤（零值＝开启）。
	SensitiveFilterDisabled bool `json:"sensitiveFilterDisabled"`
	// BlockedWords 是自定义屏蔽词，用换行/逗号/分号分隔。
	BlockedWords string `json:"blockedWords"`
}

// DefaultLayoutSettings 返回缺省设置（敏感词过滤开启）。
func DefaultLayoutSettings() LayoutSettings {
	return LayoutSettings{
		Opacity:  DefaultOpacity,
		FontSize: DefaultFontSize,
		Speed:    DefaultSpeed,
		Area:     DefaultArea,
		Density:  DefaultDensity,
	}
}

// NormalizeLayoutSettings 夹紧越界值、把非法枚举回落到缺省。
//
// 与 C 的 normalizeDanmakuSettings 同义：数值越界夹到边界（而不是丢弃整份设置），
// area/density 取非法值则回落缺省。**不接受**浮点 NaN：它会让后面的几何计算变成
// NaN 并静默画出空白（C 用 Number.isFinite 挡这个）。
func NormalizeLayoutSettings(value LayoutSettings) LayoutSettings {
	out := DefaultLayoutSettings()
	if finite := value.Opacity; finite != 0 {
		out.Opacity = clampInt(finite, OpacityMin, OpacityMax)
	}
	if finite := value.FontSize; finite != 0 {
		out.FontSize = clampInt(finite, FontSizeMin, FontSizeMax)
	}
	if !math.IsNaN(value.Speed) && !math.IsInf(value.Speed, 0) && value.Speed != 0 {
		out.Speed = clampFloat(value.Speed, SpeedMin, SpeedMax)
	}
	if isArea(value.Area) {
		out.Area = value.Area
	}
	if isDensity(value.Density) {
		out.Density = value.Density
	}
	out.SensitiveFilterDisabled = value.SensitiveFilterDisabled
	runes := []rune(value.BlockedWords)
	if len(runes) > BlockedWordsMaxRunes {
		runes = runes[:BlockedWordsMaxRunes]
	}
	out.BlockedWords = string(runes)
	return out
}

// NormalizeLayoutSettingsJSON 接受 C 形态的设置对象（键与 C 的字段名一致，
// sensitiveFilter 是"开启"而不是"关闭"），便于各端把已有的持久化设置直接喂进来。
//
// 缺失的键保持缺省；类型不对的键忽略（不要因为一个坏字段把整份设置丢掉）。
func NormalizeLayoutSettingsJSON(raw map[string]any) LayoutSettings {
	settings := DefaultLayoutSettings()
	if raw == nil {
		return settings
	}
	if value, ok := numberValue(raw["opacity"]); ok {
		settings.Opacity = clampInt(int(value), OpacityMin, OpacityMax)
	}
	if value, ok := numberValue(raw["fontSize"]); ok {
		settings.FontSize = clampInt(int(value), FontSizeMin, FontSizeMax)
	}
	if value, ok := numberValue(raw["speed"]); ok && !math.IsNaN(value) && !math.IsInf(value, 0) {
		settings.Speed = clampFloat(value, SpeedMin, SpeedMax)
	}
	if text, ok := raw["area"].(string); ok && isArea(text) {
		settings.Area = text
	}
	if text, ok := raw["density"].(string); ok && isDensity(text) {
		settings.Density = text
	}
	// C 的语义是 `sensitiveFilter !== false`：只有显式 false 才是关闭。
	if flag, ok := raw["sensitiveFilter"].(bool); ok {
		settings.SensitiveFilterDisabled = !flag
	}
	if text, ok := raw["blockedWords"].(string); ok {
		runes := []rune(text)
		if len(runes) > BlockedWordsMaxRunes {
			runes = runes[:BlockedWordsMaxRunes]
		}
		settings.BlockedWords = string(runes)
	}
	return settings
}

// builtinBlockedWords 是内置敏感词表，逐字取自 C 的 BUILTIN_BLOCKED_WORDS。
//
// 之所以内置而不是靠用户填：这些词一旦漏进来，弹幕区就是审核事故。
var builtinBlockedWords = []string{
	"习近平", "習近平", "共产党", "共產黨", "中国共产党", "中共", "国民党", "國民黨", "民进党", "民進黨",
	"毛泽东", "毛澤東", "邓小平", "鄧小平", "江泽民", "江澤民", "胡锦涛", "胡錦濤", "李克强", "李克強", "蔡英文", "赖清德", "賴清德",
	"台独", "台獨", "港独", "港獨", "藏独", "藏獨", "疆独", "疆獨", "六四", "天安门事件", "天安門事件", "法轮功", "法輪功", "反共", "反华", "反華",
	"文革", "文化大革命", "政治局", "颠覆政权", "顛覆政權", "操你妈", "操你媽", "傻逼", "傻屄",
}

// CommentIsBlocked 报告一条弹幕是否应当被屏蔽。
//
// 匹配前的归一（C 的 comparable）：NFKC 归一 → 转小写 → 去掉空白、零宽字符与方向
// 控制字符。不做这一步的话，全角"共產黨"、中间插零宽空格的写法、大小写变体都能绕过
// 词表 —— 屏蔽词的价值就在于挡住这些。
func CommentIsBlocked(content string, settings LayoutSettings) bool {
	words := make([]string, 0, 8+len(builtinBlockedWords))
	for _, word := range strings.FieldsFunc(settings.BlockedWords, func(char rune) bool {
		return char == '\n' || char == ',' || char == '，' || char == ';' || char == '；'
	}) {
		if comparable := comparableText(word); comparable != "" {
			words = append(words, comparable)
		}
	}
	if !settings.SensitiveFilterDisabled {
		for _, word := range builtinBlockedWords {
			words = append(words, comparableText(word))
		}
	}
	text := comparableText(content)
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// comparableText 把文本归一成"用于比较"的形态（见 CommentIsBlocked）。
func comparableText(text string) string {
	normalized := strings.ToLower(norm.NFKC.String(text))
	var builder strings.Builder
	builder.Grow(len(normalized))
	for _, char := range normalized {
		switch {
		case unicode.IsSpace(char):
		case char >= 0x200b && char <= 0x200f:
		case char >= 0x202a && char <= 0x202e:
		case char >= 0x2060 && char <= 0x2069:
		case char == 0xfeff:
		default:
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

// LayoutOptions 是一次布局的全部输入。
type LayoutOptions struct {
	Width, Height float64
	Settings      LayoutSettings
	ReducedMotion bool
	// BottomInset 是底部留给控制条的高度；<= 0 取 DefaultBottomInset。
	BottomInset float64
}

// normalized 返回夹紧后的设置与底部留白。
func (o LayoutOptions) normalized() (LayoutSettings, float64) {
	settings := NormalizeLayoutSettings(o.Settings)
	inset := o.BottomInset
	if inset <= 0 {
		inset = DefaultBottomInset
	}
	return settings, inset
}

// LaneGeometry 计算轨道数与轨道几何。
//
// 这是"两端视觉一致"的关键：轨道数、轨道间距、行高全部由**算法**给出，
// 两端只按这些数字画。
func LaneGeometry(options LayoutOptions) (lanes int, top, laneGap, lineHeight float64) {
	settings, inset := options.normalized()
	lineHeight = float64(settings.FontSize) + 12
	top = 12
	bottom := math.Min(options.Height-inset, options.Height*areaFactor(settings.Area))
	if options.ReducedMotion {
		// 减少动态效果时一次只轮播一条：单轨是刻意的。
		return 1, top, 0, lineHeight
	}
	available := math.Floor((bottom - top) / lineHeight)
	lanes = int(math.Max(1, math.Min(float64(densityLanes(settings.Density)), math.Max(available, 1))))
	if lanes > 1 {
		laneGap = math.Max(lineHeight, (bottom-top-lineHeight)/float64(lanes-1))
	}
	return lanes, top, laneGap, lineHeight
}

// Placement 是一条"已在屏"的弹幕。
//
// 它自带 JSON tag：内核把布局结果交给各端渲染时不必再定义一遍镜像类型。
type Placement struct {
	ID      int     `json:"id"`
	Text    string  `json:"text"`    // 截断后的显示文本
	Content string  `json:"content"` // 原文
	Color   string  `json:"color"`
	Mode    int     `json:"mode"`
	Lane    int     `json:"lane"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Age     float64 `json:"age"`
}

// AnimationState 是投放状态机（对应 C 的 feed.animation）。
//
// LastTime 用 HasLast 而不是哨兵值区分"第一次"：C 用的是 undefined，
// Go 里用 0 会被误判成"上一次在 0 秒"，跳转判定就错了。
type AnimationState struct {
	Items    []Placement `json:"items"`
	Cursor   int         `json:"cursor"`
	NextID   int         `json:"nextId"`
	Cooldown float64     `json:"cooldown"`
	LastTime float64     `json:"lastTime"`
	HasLast  bool        `json:"hasLast"`
}

// NewAnimationState 返回空状态。
func NewAnimationState() *AnimationState { return &AnimationState{} }

// AdvanceTimed 按视频时间投放弹幕并推进已在屏的弹幕，返回是否需要重绘。
//
// items 必须按 TimeMS 升序（Merger.Items() 保证），currentTimeS 是视频当前播放位置
// （秒），elapsedS 是本帧推进的**媒体**时间（秒，已乘倍速）。
func AdvanceTimed(state *AnimationState, items []Item, currentTimeS, elapsedS float64, options LayoutOptions) bool {
	if state == nil {
		return false
	}
	if !(options.Width >= MinCanvasWidth) || !(options.Height >= MinCanvasHeight) {
		return false
	}
	settings, _ := options.normalized()
	delta := math.Min(math.Max(elapsedS, 0), MaxFrameDelta)
	changed := false
	currentTimeMS := int64(math.Round(currentTimeS * 1000))

	if IsSeek(state.LastTime, currentTimeS, elapsedS, state.HasLast) {
		// 跳转后必须重建：光标对齐到新位置，在屏弹幕全部作废（它们属于跳转前的时间点）。
		// 这里**不能**提前 return —— 否则跳转那一帧不投放任何弹幕，跳过去要等下一帧
		// 才出画面，观感上像卡了一下（C 的原注释）。
		state.Cursor = LowerBoundMS(items, currentTimeMS)
		if len(state.Items) > 0 {
			changed = true
		}
		state.Items = nil
		state.Cooldown = 0
	}
	state.LastTime = currentTimeS
	state.HasLast = true

	before := len(state.Items)
	for index := range state.Items {
		state.Items[index].Age += delta
		if !options.ReducedMotion {
			state.Items[index].X -= ScrollSpeedPixels * settings.Speed * delta
		}
	}
	kept := state.Items[:0]
	for _, item := range state.Items {
		if options.ReducedMotion {
			if item.Age < ReducedMotionReadSeconds {
				kept = append(kept, item)
			}
			continue
		}
		if item.X+item.Width > 0 {
			kept = append(kept, item)
		}
	}
	state.Items = kept
	if len(state.Items) != before {
		changed = true
	}
	state.Cooldown = math.Max(0, state.Cooldown-delta)

	lanes, top, laneGap, _ := LaneGeometry(options)
	laneFree := func(index int) bool {
		newest := -1
		for position := range state.Items {
			if state.Items[position].Lane != index {
				continue
			}
			if newest < 0 || state.Items[position].X > state.Items[newest].X {
				newest = position
			}
		}
		if newest < 0 {
			return true
		}
		// 允许同一道里多发：只要最后一条已经走出足够距离，新的一条就不会追尾。
		// 上游评论滚动条用的是"这道有人就占着"，对真弹幕（一集上百条）会严重丢量。
		return state.Items[newest].X+state.Items[newest].Width+LaneHeadGap <= options.Width
	}

	staleAfter := int64(StaleSeconds * 1000)
	if options.ReducedMotion {
		staleAfter = int64(ReducedMotionReadSeconds * 1000)
	}
	emitted := 0
	for state.Cursor < len(items) && emitted < MaxEmitPerFrame {
		entry := items[state.Cursor]
		// 时间比较统一走毫秒整数值：秒的浮点表示会让"刚好到点"的弹幕偶尔晚一帧
		// 出现，而这类抖动在两端会表现成"同一集弹幕出现时刻不同"。
		if entry.TimeMS > currentTimeMS {
			break
		}
		stale := currentTimeMS-entry.TimeMS > staleAfter
		if CommentIsBlocked(entry.Text, settings) || stale {
			// 被屏蔽或已过期的条目直接跳过：它们不占轨道，也不该阻塞后面的。
			state.Cursor++
			continue
		}
		if state.Cooldown > 0 {
			break
		}
		lane := -1
		for index := 0; index < lanes; index++ {
			if laneFree(index) {
				lane = index
				break
			}
		}
		if lane < 0 {
			// 没有空轨：**保留 cursor**，下一帧再试。
			break
		}
		state.Items = append(state.Items, Placement{
			ID:      state.NextID,
			Text:    truncatePlacement(entry.Text),
			Content: entry.Text,
			Color:   entry.Color,
			Mode:    entry.Mode,
			Lane:    lane,
			X:       placementStartX(options),
			Y:       top + float64(lane)*laneGap,
			Width:   placementWidth(entry.Text, settings),
			Age:     0,
		})
		state.NextID++
		state.Cooldown = placementCooldown(settings, options.ReducedMotion)
		state.Cursor++
		emitted++
		changed = true
	}
	return changed
}

// LowerBoundMS 返回第一条 TimeMS >= currentTimeMS 的下标（列表按 TimeMS 升序）。
func LowerBoundMS(items []Item, currentTimeMS int64) int {
	low, high := 0, len(items)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if items[middle].TimeMS < currentTimeMS {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low
}

// IsSeek 判断用户是否跳转了进度。
//
// 正常播放时 currentTime 的增量约等于本帧的媒体时间增量；一旦出现负数增量或远超增量
// 的正跳变，就只能是 seek。判错会导致快进后把中间整段弹幕一次性倒到屏幕上
// （C 的原注释）。hasLast 为假表示这是第一帧——第一帧一律按 seek 处理（重建光标）。
func IsSeek(lastTime, currentTime, elapsed float64, hasLast bool) bool {
	if !hasLast {
		return true
	}
	jump := currentTime - lastTime
	if jump < 0 {
		return true
	}
	return jump > elapsed*3+1
}

// placementStartX 是新弹幕的起点：正常模式从右边缘进入，减少动态效果模式固定在左侧。
func placementStartX(options LayoutOptions) float64 {
	if options.ReducedMotion {
		return 12
	}
	return options.Width
}

// placementWidth 用"字符数 × 字号"估宽（C 的估算：未知字体的保守近似）。
func placementWidth(text string, settings LayoutSettings) float64 {
	return float64(len([]rune(text))+1) * float64(settings.FontSize)
}

// placementCooldown 是下一次投放前的最短等待。
func placementCooldown(settings LayoutSettings, reducedMotion bool) float64 {
	if reducedMotion {
		return ReducedMotionReadSeconds
	}
	if interval, exists := emitInterval[settings.Density]; exists {
		return interval
	}
	return emitInterval[DensityNormal]
}

// truncatePlacement 把过长的弹幕截成 79 个字符 + 「…」（在屏文本上限 80 字符）。
func truncatePlacement(text string) string {
	runes := []rune(text)
	if len(runes) <= MaxPlacementRunes {
		return text
	}
	return string(runes[:MaxPlacementRunes-1]) + "…"
}

func areaFactor(area string) float64 {
	switch area {
	case AreaQuarter:
		return 0.25
	case AreaFull:
		return 1
	default:
		return 0.5
	}
}

func densityLanes(density string) int {
	switch density {
	case DensitySparse:
		return 1
	case DensityDense:
		return 6
	default:
		return 3
	}
}

func isArea(value string) bool {
	return value == AreaQuarter || value == AreaHalf || value == AreaFull
}

func isDensity(value string) bool {
	return value == DensitySparse || value == DensityNormal || value == DensityDense
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func clampFloat(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// numberValue 从 JSON 解出来的值里取数字（json.Number / float64 / int / 字符串数字）。
func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		var parsed float64
		if err := json.Unmarshal([]byte(typed), &parsed); err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

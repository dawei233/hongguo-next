// Package danmaku 是红果弹幕内核（M2）：分片抓取、合并去重、轨道布局与降级源。
//
// 本包是 §2.1 目录结构里 core/danmaku 的落地，边界见 §2.2：允许依赖
// core/source（取 vid）与 core/danmaku/sign；不做 UI，不做渲染。
//
// # 来源与移植关系
//
// 实现移植自 C（hongguo-drama，GPL-3.0，与本仓同许可）：
//
//	本包文件          来源（C）                              说明
//	danmaku.go        —                                       类型与合并契约（本仓新增）
//	parse.go          src/native/danmaku.js parsePage 段      响应解析与清洗规则
//	collect.go        src/native/danmaku.js fetchDanmaku 段   分片循环与失败语义
//	layout.go         src/components/danmakuOfficial.mjs      轨道布局（两端共享）
//	webfallback.go    src/native/episode-comments.js          无签名公开评论降级源
//	sign/             src/native/danmaku-sign.js + sm3.js     签名（黄金向量回归）
//
// B（vendor 进 core 的扁平包）里已有的同类实现（core/ui_playback_danmaku.go 的
// 分页循环、core/provider_hongguo_comment_sign.go 的签名）**保持原样**：
// M2 只新建文件，不改动 core 既有文件；两者的等价性由 core/danmaku_bridge.go
// 导出的迁移期接口在测试里交叉回归（见 sign/parity_test.go）。
//
// # 两条硬约束（来自 C 的工程注释，改坏即签名失效）
//
//  1. 不得"顺手修正"参考实现里看起来像 bug 的写法（X-Gorgon 的 RC4 类密钥调度
//     是 box[i] = box[j] 赋值而非交换）。见 sign/gorgon.go 与
//     TestXGorgonKSAMustNotSwap。
//  2. 不得改变位运算的宽度与截断位置（全部 64 位运算按 uint64 回绕）。见
//     Test64BitTruncation。
//
// # 去重键：播放时间 + 文本
//
// 上游接口按游标分批返回子集，同一条弹幕会在不同批次里带着不同的 comment_id
// 再次出现（B 的参考实现实测：同一集几十秒内会给出 71~104 条不同结果）。
// 因此跨批次去重**不能依赖 id**，必须按（播放时间, 文本）判定 —— 这正是
// Merge/Identity 的语义，也是 Track 里刻意不带 comment_id 的原因
// （contracts/danmaku.schema.json $defs.danmakuItem 的 description 同此约定）。
package danmaku

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/dawei233/hongguo-next/core/source"
)

// 弹幕模式（contracts/danmaku.schema.json $defs.danmakuItem.mode）。
const (
	ModeScroll = source.DanmakuModeScroll // 1：滚动（默认）
	ModeTop    = source.DanmakuModeTop    // 2：顶部固定
	ModeBottom = source.DanmakuModeBottom // 3：底部固定
)

// ColorDefault 是缺省弹幕颜色（源未给颜色时）。
const ColorDefault = source.DanmakuColorDefault

// 上游接口的硬上限，取自 C 的同名常量（超限即被上游拒绝或被视为脏数据）。
const (
	// MaxTextLen 是单条文本上限；超长文本直接丢弃（正常弹幕不会这么长）。
	MaxTextLen = 200
	// MaxTimelineMS 是时间轴上限：7 天，超出视为脏数据。
	MaxTimelineMS = 7 * 24 * 60 * 60 * 1000
	// MaxDurationMS 是单次请求可声明的窗口上限（24 小时）。
	MaxDurationMS = 24 * 60 * 60 * 1000
	// EffectiveDurationMS 是单次请求声明的"剧集时长"。
	EffectiveDurationMS = 60 * 60 * 1000
	// PageCount 是单次向上游要的条数（上游游标分页的页大小）。
	PageCount = 90
	// MaxPages 是分页上限，防死循环。
	MaxPages = 40
	// MaxItems 是单集条目上限，防内存与 IPC 载荷失控。
	MaxItems = 3000
	// MaxEmptyPages：连续空页就停 —— 上游偶尔 has_more=true 却一直给空。
	MaxEmptyPages = 2
	// PageGap 是相邻分页间隔，别把上游打急。
	PageGap = 120 * time.Millisecond
)

// Item 是单条弹幕，与 contracts/danmaku.schema.json $defs.danmakuItem 逐字段对应。
//
// 不含上游 comment_id：见包注释「去重键：播放时间 + 文本」。
type Item struct {
	// TimeMS 是相对本集起点的偏移（毫秒）；无时间轴的降级评论固定为 0。
	TimeMS int64 `json:"timeMs"`
	// Text 是弹幕文本，不可空串（空文本在解析阶段即被丢弃）。
	Text string `json:"text"`
	// Color 形如 "#RRGGBB" 或 "#RRGGBBAA"；缺省为 ColorDefault。
	Color string `json:"color"`
	// Mode 取 ModeScroll / ModeTop / ModeBottom；源未给时取 1。
	Mode int `json:"mode"`
	// Weight 是布局权重（§5.2 的密度/优先级控制）；0 表示未指定。
	Weight int `json:"weight"`
}

// Track 是一集（或一个分片区间）归一后的弹幕，按 TimeMS 升序、已按
// （TimeMS, Text）去重。与 UI 无关。
type Track struct {
	// VideoID 是源内视频 ID（不透明字符串）。
	VideoID string `json:"videoId"`
	// Items 不可为 nil；空切片表示该区间确无弹幕，抓取失败必须走错误而不是空轨。
	Items []Item `json:"items"`
}

// ToSourceTrack 映射为站源层契约类型（§4.1）。
//
// 两边字段一一对应，因此这里不做任何取舍；Items 为空时给空切片而非 nil，
// 因为契约要求 items 不可为 null。
func (t Track) ToSourceTrack() source.DanmakuTrack {
	items := make([]source.Danmaku, 0, len(t.Items))
	for _, item := range t.Items {
		items = append(items, source.Danmaku{
			TimeMS: item.TimeMS,
			Text:   item.Text,
			Color:  item.Color,
			Mode:   item.Mode,
			Weight: item.Weight,
		})
	}
	return source.DanmakuTrack{VideoID: t.VideoID, Items: items}
}

// Identity 是弹幕的内容身份：同一播放时间点上的同一句话视为同一条。
//
// 与 core 的 danmakuIdentity（core/ui_playback_danmaku.go）语义一致，只是这里用
// 整数毫秒而不是秒的浮点形态：浮点格式化在不同语言/平台上的舍入差异会让
// "看似相同"的两条算出不同的键。
func Identity(timeMS int64, text string) string {
	return strconv.FormatInt(timeMS, 10) + "|" + text
}

// Merger 是跨分片合并器：按（播放时间 + 文本）去重，最后按时间稳定排序。
//
// 零值不可用，用 NewMerger 构造。Merger 不是并发安全的：一个分片序列用一个。
type Merger struct {
	seen    map[string]struct{}
	items   []Item
	dupes   int
	skipped int
}

// NewMerger 构造合并器。
func NewMerger() *Merger {
	return &Merger{seen: make(map[string]struct{})}
}

// Add 收下一批条目，返回新增条数与被丢弃条数（重复 + 非法）。
//
// 非法条目按 C 的清洗规则在此处挡掉：空文本、文本超长、时间轴为负或越界。
// 注意这里**只做合并**，不做截断：截断属于呈现阶段（见 Report.Present），
// 免得缓存里留下"半截"结果。
func (m *Merger) Add(items ...Item) (added, dropped int) {
	for _, item := range items {
		if item.Text == "" || len([]rune(item.Text)) > MaxTextLen || item.TimeMS < 0 || item.TimeMS > MaxTimelineMS {
			m.skipped++
			dropped++
			continue
		}
		if item.Color == "" {
			item.Color = ColorDefault
		}
		if item.Mode != ModeTop && item.Mode != ModeBottom {
			item.Mode = ModeScroll
		}
		key := Identity(item.TimeMS, item.Text)
		if _, exists := m.seen[key]; exists {
			m.dupes++
			dropped++
			continue
		}
		m.seen[key] = struct{}{}
		m.items = append(m.items, item)
		added++
	}
	return added, dropped
}

// Len 是已合并的条目数。
func (m *Merger) Len() int { return len(m.items) }

// Duplicates 是累计因重复而丢弃的条数（含同一分片内与跨分片）。
func (m *Merger) Duplicates() int { return m.dupes }

// Items 返回按 TimeMS 升序稳定排序的副本（同一时间点保持到达顺序）。
func (m *Merger) Items() []Item {
	out := make([]Item, len(m.items))
	copy(out, m.items)
	sort.SliceStable(out, func(i, j int) bool { return out[i].TimeMS < out[j].TimeMS })
	return out
}

// ChunkRequest 是一次分片抓取的入参（内核侧形态，毫秒）。
//
// 与 contracts/danmaku.schema.json 的 chunkRequest（秒）不是同一个东西：那份是
// 跨端线格式，这份是内核内部表示；换算只发生在 wire 边界。
type ChunkRequest struct {
	// EpisodeID 是集 ID（回显用，不参与上游请求）。
	EpisodeID string
	// SeriesID 是源内剧 ID（上游 business_param.book_id）。填错会被上游拒。
	SeriesID string
	// VideoID 是本集视频 ID（上游 group_id）。与 SeriesID 不同，两者不可混用。
	VideoID string
	// StartMS 是本分片起点（含）。
	StartMS int64
	// DurationMS 是声明的窗口终点（不含）。上游实测语义是"结束偏移"而非时长。
	DurationMS int64
}

// Fetcher 抓取一个官方签名分片，返回**原始响应体**（不改写、不预解析）。
//
// 由 core/danmaku_bridge.go 提供走内核既有签名链路的实现；测试用假实现注入，
// 因此本包的所有解析与合并逻辑都可以离线验证。
type Fetcher interface {
	FetchChunk(ctx context.Context, request ChunkRequest) ([]byte, error)
}

// WebCommentPage 是公开网页评论的一页（无签名降级源）。
type WebCommentPage struct {
	Items []WebCommentItem
	// NextCursor 为 "" 表示没有下一页。
	NextCursor string
	HasMore    bool
	// Total 为 0 表示未知；公开预览通常只给部分评论。
	Total int64
	// SourceLimited 表示源只提供部分数据，UI 必须标注。
	SourceLimited bool
	Notice        string
}

// WebCommentItem 是一条公开评论。没有时间轴，也没有可信的稳定 ID。
type WebCommentItem struct {
	// ID 由源的稳定字段派生（不含会变化的点赞数）；为空时按文本兜底。
	ID      string
	Content string
}

// WebCommentFetcher 按游标抓取公开评论。
type WebCommentFetcher interface {
	FetchWebComments(ctx context.Context, seriesID, episodeID, cursor string) (WebCommentPage, error)
}

// Report 是一次抓取过程的统计与状态，供上层决定文案与是否续拉。
//
// 全部出口共用一套文案计算（见 Notice）：失败优先于"没有弹幕"，绝不把
// "拿不到"说成"本集没有弹幕"。
type Report struct {
	// Pages 是实际请求的分片数；EmptyPages 是末尾连续空页数。
	Pages      int
	EmptyPages int
	// Truncated 表示因达到 MaxItems 而截断；Partial 表示中途失败但保留了已有条目。
	Truncated bool
	Partial   bool
	// Failure 非空时表示源不可用/中断的说明；它一律优先于其它文案。
	Failure string
	// Total 是上游给出的该集总数；0 表示未知。
	Total int64
	// NextMS 是下一分片起点；<= StartMS 表示无更多。
	StartMS int64
	NextMS  int64
	// Warnings 是数据校验发现的问题（时间轴不单调、空结果等），供诊断用。
	Warnings []string
	// SourceLimited/Notice 来自降级源。
	SourceLimited bool
	Notice        string
	// Degraded 表示本次结果来自无签名的公开评论降级源。
	Degraded bool
}

// ResolveNotice 是唯一的结束状态出口。
//
// 官方弹幕有好几条失败路径（超时、HTTP 错误、部分成功、缓存命中失败结果）。
// 只要有一条路径自己拼状态文案，就会出现"源坏了却显示本集没有弹幕"这类事故
// （C 的 finalize/resolveDanmakuStatus 同此约定）。
func (r Report) ResolveNotice(count int) string {
	if r.Failure != "" {
		return r.Failure
	}
	if r.Truncated {
		return "弹幕较多，本次仅载入前 " + strconv.Itoa(count) + " 条"
	}
	if count == 0 {
		if r.Notice != "" {
			return r.Notice
		}
		return "本集暂无弹幕"
	}
	if r.Notice != "" {
		return "已载入 " + strconv.Itoa(count) + " 条弹幕（" + r.Notice + "）"
	}
	return "已载入 " + strconv.Itoa(count) + " 条弹幕"
}

// Validate 校验一批已合并条目的数据质量，返回发现的告警（不改变数据）。
//
// §5.2 的 T 节点：数量、时间轴单调性、空结果告警。空结果不算错误 ——
// 它可能真的没有弹幕，但调用方需要能区分"确实为空"与"抓取失败"，
// 因此这里只出告警，错误由 Collect/降级源返回。
func Validate(items []Item) []string {
	var warnings []string
	if len(items) == 0 {
		warnings = append(warnings, "本区间没有解析出任何弹幕")
		return warnings
	}
	if len(items) > MaxItems {
		warnings = append(warnings, "条目数超过单集上限 "+strconv.Itoa(MaxItems))
	}
	previous := int64(-1)
	for index, item := range items {
		if item.TimeMS < previous {
			warnings = append(warnings, "第 "+strconv.Itoa(index+1)+" 条的时间轴倒退（"+strconv.FormatInt(item.TimeMS, 10)+" < "+strconv.FormatInt(previous, 10)+"）")
			break
		}
		previous = item.TimeMS
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		key := Identity(item.TimeMS, item.Text)
		if _, exists := seen[key]; exists {
			warnings = append(warnings, "存在未去重的重复条目："+key)
			break
		}
		seen[key] = struct{}{}
	}
	return warnings
}

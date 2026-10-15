// 本仓新增（M1 契约层）。
//
// 理由：§4.1 定义的跨源统一模型此前只存在于文档里；vendor 的 core 用扁平包内的
// nativeDrama / Chapter 等自有类型表达同类数据。本文件把 §4.1 的模型固化为
// 契约类型，字段与文档逐字段一致，JSON tag 与 contracts/bridge-api.schema.json、
// contracts/danmaku.schema.json 逐字段对应（含单位与可空性说明）。
package source

import "time"

// Series 是跨源统一的剧集模型（§4.1）。
type Series struct {
	// ID 是全局剧 ID，形如 "<sourceId>:<sourceKey>"（例 "hongguo:12345"）。
	// §4.4 的别名链（同一剧在多源上的 ID 互为别名）以此值为键。
	ID ID `json:"id"`
	// SourceID 是提供该条数据的站源 ID（13 个之一），不是源内剧 ID
	// ——源内剧 ID 是 ID 中冒号之后的部分。
	SourceID ID `json:"sourceId"`
	// Title 是剧名，不可空串（适配层以 core 的 DisplayTitle 兜底）。
	Title string `json:"title"`
	// Cover 是封面绝对 URL；"" 表示源未给出，调用方自行占位，不得伪造。
	Cover string `json:"cover"`
	// Year 是首播年（4 位）；0 表示未知。
	Year int `json:"year"`
	// Total 是总集数；未知为 0（不表示零集）。
	Total int `json:"total"`
	// Tag 是源原始分类/标签文案（未归一，供 UI 直显）；"" 表示源未给出。
	Tag string `json:"tag"`
	// Score 是源原始评分文案（保留原文，不解析成数值）；"" 表示无评分。
	Score string `json:"score"`
}

// Episode 是跨源统一的集模型（§4.1）。
type Episode struct {
	// SeriesID 是所属剧的全局 ID，与 Series.ID 同值。
	SeriesID ID `json:"seriesId"`
	// EpisodeID 是源内不透明 ID，格式 "<sourceId>:<...>"。
	// vendor 的 core 现状为 "<sourceId>:<seriesKey>:<chapterKey>"，其中 seriesKey
	// 可能为空、chapterKey 本身可能含冒号，因此除首个冒号外不得再行切分。
	EpisodeID string `json:"episodeId"`
	// Index 是集序号，1 起始（与 core 的「第%d集」及 localPlan(dramaID, index) 一致）；0 表示未知。
	Index int `json:"index"`
	// Title 是集标题；"" 表示源未给出。
	Title string `json:"title"`
	// DurationS 是时长（秒）；0 表示未知（core 的 Chapter 不携带时长）。
	DurationS int `json:"durationS"`
}

// Stream 描述「怎么播」，不含任何源特有字段（§4.1）。
//
// Resolve 不返回文件路径：由 core/media 统一决定 ffmpeg / 直连 / 代理，
// 而不是每个源各写一套下载器。
type Stream struct {
	// URL 是取流地址。不得是 core 内部协议标记（如 "hongguo-cenc://"，
	// 那是待解析的中间态，必须经 core 的 providerMedia 解析后才有真实地址）。
	URL string `json:"url"`
	// Encrypted 表示是否需要以 DecryptionKey 解密。
	Encrypted bool `json:"encrypted"`
	// DecryptionKey 是 AES-128 密钥的十六进制小写形态（16 字节 → 32 字符，
	// 与 core 的 hex.EncodeToString(CENCKey/HLSKey) 一致）。
	// 仅当 Encrypted 时非空：MP4 CENC 取 CENCKey，HLS AES-128 取 HLSKey。
	DecryptionKey string `json:"decryptionKey,omitempty"`
	// Headers 是取流时必须附带的请求头（至少含 Referer）。
	// nil 与空 map 等价，均表示无需附加头（Go 的 nil map 序列化为 null）。
	Headers map[string]string `json:"headers"`
	// Quality 是实际清晰度标签，取值同 Quality（"1080p" / "720p" / "auto"）；
	// 未知一律归一为 "auto"，不用空串表示未知。
	Quality string `json:"quality"`
	// Container 是封装形态：ContainerMP4 或 ContainerHLS。
	Container string `json:"container"`
	// NeedsSanitize 表示媒体层在 ffmpeg remux 后是否必须再做一次 MP4 净化（ADR-0004）。
	// 该结论取决于源的加密形态，因此必须由 Resolve 显式带出，不能藏在 provider 内部。
	NeedsSanitize bool `json:"needsSanitize"`
	// ExpiresAt 是地址失效时刻。零值（time.Time{}，序列化为
	// "0001-01-01T00:00:00Z"）表示无过期或未知，与契约里的 null 含义相同。
	ExpiresAt time.Time `json:"expiresAt"`
}

// DanmakuTrack 是内核归一后的弹幕，与 UI 无关（§4.1）。
type DanmakuTrack struct {
	// VideoID 是源内视频 ID（不透明字符串）。
	VideoID string `json:"videoId"`
	// Items 按 TimeMS 升序、已按 (TimeMS, Text) 去重；
	// 空数组表示该区间确无弹幕，抓取失败必须返回错误而不是空轨。
	Items []Danmaku `json:"items"`
}

// 弹幕模式（§4.1 Danmaku.Mode）。
const (
	DanmakuModeScroll = 1 // 滚动（默认）
	DanmakuModeTop    = 2 // 顶部固定
	DanmakuModeBottom = 3 // 底部固定
)

// DanmakuColorDefault 是缺省弹幕颜色（§4.1：缺省 "#FFFFFF"，不透明白）。
const DanmakuColorDefault = "#FFFFFF"

// Danmaku 是单条弹幕（§4.1）。
//
// 不含上游 comment_id：上游会给同一句话分配不同的 id，去重必须按
// (TimeMS, Text) 判定（见 core 的 danmakuIdentity 注释），暴露 id 只会诱导错误实现。
type Danmaku struct {
	// TimeMS 是相对本集起点的偏移（毫秒）；无时间轴的降级评论固定为 0。
	TimeMS int64 `json:"timeMs"`
	// Text 是弹幕文本，不可空串。
	Text string `json:"text"`
	// Color 形如 "#RRGGBBAA" 或 "#RRGGBB"；缺省为 DanmakuColorDefault。
	Color string `json:"color"`
	// Mode 取 DanmakuModeScroll / DanmakuModeTop / DanmakuModeBottom；源未给时取 1。
	Mode int `json:"mode"`
	// Weight 是布局权重，用于轨道分配的优先级/密度控制；0 表示未指定。
	Weight int `json:"weight"`
}

// Query 是搜索入参（§4.1）。
type Query struct {
	// Keyword 为 "" 表示不带关键字的浏览式列举。
	Keyword string `json:"keyword"`
	// Page 是页码，1 起始。
	Page int `json:"page"`
	// PageSize 是每页条数；0 表示由站源决定。
	PageSize int `json:"pageSize"`
	// Filters 是源特有的筛选条件（分类 ID、地区、年代等），键值均为字符串；
	// nil 与空 map 等价。
	Filters map[string]string `json:"filters"`
}

// Page 是分页结果（§4.1）。
type Page struct {
	// Items 为空数组表示本页确无结果。
	Items []Series `json:"items"`
	// Total 是命中总数；0 表示未知（多数站源不返回总数），不表示确为零。
	Total int `json:"total"`
	// HasMore 表示是否还有下一页；站源未给时由适配层按「本页是否满页」推断。
	HasMore bool `json:"hasMore"`
}

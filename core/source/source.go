// 本仓新增（M1 契约层 / 站源适配层）。
//
// 理由：docs/ARCHITECTURE.md §2.1 要求 core/source 只承载跨源统一模型与唯一扩展点
// Source 接口，而 vendor 自上游的 core 目前是扁平的单一 package core（123 个文件 /
// 24035 行）。本包不搬运、不重构既有 core 文件，只新增契约类型；既有
// nativeDrama / providerMedia / Chapter 到本包类型的映射放在 core/source/adapter。
//
// 字段与 §4.1 的 Go 模型逐字段一致；JSON 形态对应 contracts/bridge-api.schema.json。
// 本包只含类型与接口（外加 Caps 的位运算与线格式序列化），不含 IO、不含源实现。
//
// 两处 §4.1 未单列、但其接口签名必须用到的支撑类型也定义在这里，语义取自 §4.1：
// Quality（Resolve 的入参）与 Category（Categories 的返回元素）。
package source

import "context"

// ID 是站源 ID，或由站源 ID 与前缀构成的复合 ID（不透明字符串）。
//
// 单值形态为 §4.3 去重后的 11 个对外站源 ID 之一（"hongguo" | "huangguo" | ... | "hema"）。
// 复合形态见 Series.ID / Episode.EpisodeID：形如 "<sourceId>:<sourceKey>"。
type ID string

// §4.3 去重后的 11 个对外站源 ID（对外唯一 ID）。顺序为规范顺序：
// 默认可见 → 解锁后可见 → C 侧移植。
//
// 权威依据是 B 的站源注册表（`lib/models.dart` 的 `SourceSite.primaryValues` /
// `restrictedValues`），本表与它逐项对应；注释里的中文名与一句简介取自
// 注册表的 `name` / `description`。三处需要说明的合并与差异：
//   - Huangguo 一个 ID 覆盖 core 内部的**三个** facet：huangguoai / huangguo-video /
//     cloudfront（注册表的 `entryName` 依次为「AI」「视频」「旧版」，组名「黄果」）。
//     facet 差异只存在于 core 的 provider 常量与各源的 BaseURL 配置，对外不暴露。
//   - 「旧接口自动发现」**不是站源**：注册表里没有它的条目，它是 core 在 cloudfront
//     路径下兜底的回退机制，因此不占对外 ID。识别到 "legacy" 一律显式拒绝，
//     见 core/source/adapter 的 ErrFallbackOnly。
//   - Xifan / Hema 为 C 侧独有源，注册表里没有（B 端没有它们），本仓计划在 M3/M4
//     从 C 移植（§3.3）。
const (
	Hongguo     ID = "hongguo"     // 红果：短剧 · 漫剧 · AI 剧（默认可见）
	Hanxiaoquan ID = "hanxiaoquan" // 韩小圈：韩剧 · 韩国电影 · 综艺动漫（默认可见）
	Guipian     ID = "guipian"     // 鬼片：鬼片 · 电视剧 · 动漫（默认可见）
	Sorani      ID = "sorani"      // 青空：番剧 · 剧场动画 · 特摄（默认可见）
	Huangdou    ID = "huangdou"    // 黄豆：精选短剧（需解锁）
	Huangju     ID = "huangju"     // 剧果：热门 · 最新 · 分类短剧（需解锁）
	Yeguo       ID = "yeguo"       // 野果：分类短剧 · 在线搜索（需解锁）
	DSD         ID = "dsd"         // 帝果：分类视频 · 在线搜索（需解锁）
	Huangguo    ID = "huangguo"    // 黄果：视频 / AI / 旧版 三个 facet 的合并（需解锁）
	Xifan       ID = "xifan"       // 西饭：C 侧独有源，待移植（§3.3）
	Hema        ID = "hema"        // 河马：C 侧独有源，待移植（§3.3）
)

// orderedIDs 是 11 个对外站源 ID 的规范顺序（即上面常量表的顺序）。
//
// 它是「站源顺序」的唯一真相源：contracts/source-caps.schema.json 的
// $defs.sourceId.enum 与此逐项同序，由 core/source/source_caps_test.go 断言。
var orderedIDs = [...]ID{
	Hongguo, Hanxiaoquan, Guipian, Sorani,
	Huangdou, Huangju, Yeguo, DSD, Huangguo,
	Xifan, Hema,
}

// IDs 按规范顺序返回 11 个对外站源 ID 的副本。
func IDs() []ID {
	return append([]ID(nil), orderedIDs[:]...)
}

// displayNames 是 11 个对外站源 ID 的中文显示名（两端 UI 直显用，不允许为空）。
//
// 取自 B 的站源注册表 `SourceSite.name`；Huangguo 取组名「黄果」——它自己的三个
// facet 用 entryName「视频」「AI」「旧版」在组内区分，见 adapter 的 facet 展开。
var displayNames = map[ID]string{
	Hongguo:     "红果",
	Hanxiaoquan: "韩小圈",
	Guipian:     "鬼片",
	Sorani:      "青空",
	Huangdou:    "黄豆",
	Huangju:     "剧果",
	Yeguo:       "野果",
	DSD:         "帝果",
	Huangguo:    "黄果",
	Xifan:       "西饭",
	Hema:        "河马",
}

// DisplayName 返回站源的中文显示名；未知 ID（含复合 ID）返回空串。
//
// 空串表示「调用方自行决定展示文案」，与 §4.1 对可空展示字段的约定一致，
// 不表示「该源没有名字」。
func DisplayName(id ID) string {
	return displayNames[id]
}

// IsKnown 报告该 ID 是否是 11 个对外站源 ID 之一（复合 ID 与未知 ID 返回 false）。
func IsKnown(id ID) bool {
	_, ok := displayNames[id]
	return ok
}

// IsPrimary 报告该站源是否属注册表的 primaryValues：默认可见、无需解锁。
//
// 恰好是 4 个：hongguo / hanxiaoquan / guipian / sorani。
func IsPrimary(id ID) bool {
	switch id {
	case Hongguo, Hanxiaoquan, Guipian, Sorani:
		return true
	default:
		return false
	}
}

// RequiresUnlock 报告该站源是否属注册表的 restrictedValues（默认隐藏、解锁后可见）。
//
// 规则同注册表的 visibleValues：未解锁时只留 primaryValues，其余已知源都要解锁。
// C 侧独有的 Xifan / Hema 不在 B 的注册表里，但它们同样不在 primaryValues 中，
// 因此按同一规则要求解锁；未知 ID 一律返回 false —— 未知 ID 是应被拒绝的输入，
// 不是「需解锁的源」。
func RequiresUnlock(id ID) bool {
	return IsKnown(id) && !IsPrimary(id)
}

// Quality 是请求清晰度（§4.1 Resolve 的入参）。
//
// 线上形态为字符串标签；core 内部以像素高度（int）表达清晰度，
// 换算由 core/source/adapter 承担。
type Quality string

const (
	QualityAuto  Quality = "auto" // 交由播放侧按可用清晰度自选；也是未知清晰度的归一值
	Quality2160p Quality = "2160p"
	Quality1440p Quality = "1440p"
	Quality1080p Quality = "1080p"
	Quality720p  Quality = "720p"
	Quality480p  Quality = "480p"
	Quality360p  Quality = "360p"
)

// 封装形态（§4.1 Stream.Container 的取值）。
const (
	ContainerMP4 = "mp4"
	ContainerHLS = "hls"
)

// Category 是源内分类条目（§4.1 Categories 的返回元素）。
type Category struct {
	ID   string `json:"id"`   // 源内不透明分类 ID，用于回填 Query.Filters；不可空
	Name string `json:"name"` // 分类展示名；不可空
}

// Source 是唯一的站源扩展点（§4.1）。
//
// 实现必须无状态、可并发：所有缓存/登录态放在 core/storage 或 core/net 共享层，
// 不允许 provider 各自持有可变的会话状态。
//
// 可选能力一律用 Caps() 声明；不支持的能力必须返回错误，不得「假装成功」
// （例如未实现弹幕的源不得返回空弹幕轨冒充成功）。
type Source interface {
	ID() ID
	Name() string
	Caps() Caps

	Search(ctx context.Context, q Query) (Page, error)
	Categories(ctx context.Context) ([]Category, error)
	Rank(ctx context.Context, board string, page int) (Page, error)

	Detail(ctx context.Context, seriesID string) (Series, error)
	Episodes(ctx context.Context, seriesID string) ([]Episode, error)

	Resolve(ctx context.Context, episodeID string, q Quality) (Stream, error)

	// Danmaku 是可选能力，仅当 Caps().Has(CapDanmaku)（或 CapWebCommentFallback）时被调用。
	// startS 为区间起点（秒，含），durS 为区间终点（秒，不含）——与既有 core 的
	// startMs / durationMs 语义一致（core 的字段名 duration 实际是结束偏移）。
	Danmaku(ctx context.Context, episodeID string, startS, durS int64) (DanmakuTrack, error)
}

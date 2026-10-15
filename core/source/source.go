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
// 单值形态为 §4.3 去重后的 13 个站源 ID 之一（"hongguo" | "huangguo" | ... | "hema"）。
// 复合形态见 Series.ID / Episode.SeriesID：形如 "<sourceId>:<sourceKey>"。
type ID string

// §4.3 去重后的 13 个站源 ID（对外唯一 ID）。
//
// 注意两处合并/差异，均按 §4.3：
//   - huangguo 的 ai / video 两个 facet 在契约层合并为 Huangguo 一个 ID，
//     facet 差异只存在于 vendor 的 core 内部（core 的 sourceHuangguoAI /
//     sourceHuangguoVideo 两个常量）与各源自己的 BaseURL 配置；
//   - Legacy 是「旧接口自动发现」的 fallback 机制，不是真站源，但按 §4.3 仍占一个 ID；
//   - Xifan / Hema 为 C 侧独有源，本仓计划在 M3/M4 移植（§3.3）。
const (
	Hongguo     ID = "hongguo"
	Huangguo    ID = "huangguo"
	Guipian     ID = "guipian"
	Hanxiaoquan ID = "hanxiaoquan"
	Huangdou    ID = "huangdou"
	Huangju     ID = "huangju"
	Yeguo       ID = "yeguo"
	DSD         ID = "dsd"
	CloudFront  ID = "cloudfront"
	Sorani      ID = "sorani"
	Legacy      ID = "legacy"
	Xifan       ID = "xifan"
	Hema        ID = "hema"
)

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

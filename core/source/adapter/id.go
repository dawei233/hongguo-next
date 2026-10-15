// 本仓新增（M1 站源适配层）。
//
// 理由：vendor 的 core 用「源名字符串 + 主机名」表达站源身份，且把黄果拆成
// sourceHuangguoAI / sourceHuangguoVideo / sourceCloudFront 三个 facet；§4.3 要求对外
// 只暴露 11 个统一 ID（黄果的三个 facet 合并成一个）。本文件是「core 站源名 ⇄ §4.1
// 站源 ID」的唯一映射点，被 series.go / media.go / episode.go 复用。
//
// 本包只做类型与 ID 映射，不含网络与 IO，也不改动 core 的任何既有行为与导出符号。
package adapter

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/dawei233/hongguo-next/core/source"
)

var (
	// ErrUnknownSource：既不是 §4.3 的 11 个站源 ID，也不是 vendor 的 core 认识的站源名/主机名。
	ErrUnknownSource = errors.New("未知站源")
	// ErrInvalidID：ID 形态不合法（空串、缺冒号、必填段为空等）。
	ErrInvalidID = errors.New("站源 ID 形态无效")
	// ErrNotImplementedInCore：该 §4.1 站源 ID 在本仓 vendored 的 core 里还没有 provider 实现。
	ErrNotImplementedInCore = errors.New("本仓 core 尚无该站源的实现")
	// ErrFallbackOnly：该名字是「旧接口自动发现」的回退机制而非站源，没有对外 ID。
	ErrFallbackOnly = errors.New("该站源名是回退机制而非站源")
)

// fallbackSourceName 是 B 的注册表之外、core 实现里的「旧接口自动发现」机制名。
//
// 它不是站源：注册表（lib/models.dart 的 SourceSite）里没有它的条目，core 里也没有
// 以它为前缀的数据——它探测到旧接口后产出的剧/集一律以黄果旧版 facet（cloudfront）为
// 源（见 core/provider_legacy_app.go）。因此它不占对外 ID，只作为输入黑名单保留：
// 老配置或老持久化里可能出现 "legacy"，必须得到明确拒绝，而不是被当成未知源。
const fallbackSourceName = "legacy"

// huangguoFacets 是黄果（Huangguo）在 core 内部的三个 facet，顺序即优先级（首个为主 facet）。
//
// 它是 §4.3「黄果的三个 facet 合并为一个 source」的反向映射：契约层只认 huangguo，
// 实现层仍是三个 provider 路径。对应的注册表 entryName（组名「黄果」）见 facetEntryNames。
var huangguoFacets = []string{"huangguoai", "huangguo-video", "cloudfront"}

// facetEntryNames 是注册表的 `SourceSite.entryName`：同一组内区分各 facet 的中文入口名。
//
// 只有黄果组有 facet；其中 cloudfront 在组内读作「旧版」（黄果旧版），不是独立站源名。
var facetEntryNames = map[string]string{
	"huangguoai":     "AI",
	"huangguo-video": "视频",
	"cloudfront":     "旧版",
}

// FacetEntryName 返回黄果组内某个 facet 的中文入口名（注册表的 entryName）；
// 传入非 facet 名返回空串。组名用 source.DisplayName(source.Huangguo)（「黄果」）。
func FacetEntryName(coreSourceName string) string {
	return facetEntryNames[strings.ToLower(strings.TrimSpace(coreSourceName))]
}

// coreSources 是 11 个站源 ID 到 core provider 源名的展开（顺序即优先级，首个为主 facet）。
//
// 只有 Huangguo 需要展开：它覆盖 core 的三个 facet。其余源与 core 的 provider 源名一一对应。
var coreSources = map[source.ID][]string{
	source.Hongguo:     {"hongguo"},
	source.Hanxiaoquan: {"hanxiaoquan"},
	source.Guipian:     {"guipian"},
	source.Sorani:      {"sorani"},
	source.Huangdou:    {"huangdou"},
	source.Huangju:     {"huangju"},
	source.Yeguo:       {"yeguo"},
	source.DSD:         {"dsd"},
	source.Huangguo:    huangguoFacets,
	// Xifan / Hema 在 core 里没有 provider 源名，见 CoreSources 的拒绝路径。
}

// coreSourceAliases 把 core 里出现过的站源名与主机名映射到 §4.1 的站源 ID。
//
// 本表是 core/provider_huangguo.go 的 canonicalProviderSource 与
// core/provider_media.go 的 providerSourceForURL 的只读复刻：两个函数均未导出，
// 本包无法调用，而本次里程碑不允许改动 core 的导出面（bridge 的 2 个 C 符号 ABI 冻结）。
// M2/M3 把 core 拆成分包时它俩会以导出形态出现，届时删除本表直接调用，避免两处漂移。
var coreSourceAliases = map[string]source.ID{
	// huangguo：三个 facet（ai / video / 旧版 cloudfront）在契约层都归一到同一个 ID。
	// 裸 "huangguo" 亦归一到 ai facet —— 与 core 的 canonicalProviderSource 一致。
	"huangguo":       source.Huangguo,
	"huangguoai":     source.Huangguo,
	"huangguoai.com": source.Huangguo,
	"huangguo-video": source.Huangguo,
	"huangguo.video": source.Huangguo,
	"cloudfront":     source.Huangguo,

	"hongguo":               source.Hongguo,
	"hongguoduanju.com":     source.Hongguo,
	"www.hongguoduanju.com": source.Hongguo,

	"huangdou":     source.Huangdou,
	"tideember.cc": source.Huangdou,
	"xqjurgek.top": source.Huangdou,

	"huangju":         source.Huangju,
	"huangju.net":     source.Huangju,
	"www.huangju.net": source.Huangju,
	"api.huangju.net": source.Huangju,

	"yeguo":               source.Yeguo,
	"ygdj7.com":           source.Yeguo,
	"www.ygdj7.com":       source.Yeguo,
	"yeguodj.com":         source.Yeguo,
	"www.yeguodj.com":     source.Yeguo,
	"analyze.buxefaex.cc": source.Yeguo,
	"delta.ygrwdsgt.cc":   source.Yeguo,

	"dsd":            source.DSD,
	"dsd.com.se":     source.DSD,
	"www.dsd.com.se": source.DSD,

	"sorani":         source.Sorani,
	"sorani.net":     source.Sorani,
	"www.sorani.net": source.Sorani,
	"sorani.cc":      source.Sorani,
	"api.sorani.cc":  source.Sorani,

	"guipian":           source.Guipian,
	"guipianwu.com":     source.Guipian,
	"www.guipianwu.com": source.Guipian,

	"hanxiaoquan":      source.Hanxiaoquan,
	"jennyhow.com":     source.Hanxiaoquan,
	"www.jennyhow.com": source.Hanxiaoquan,

	// 两个 C 侧独有源：core 当前没有对应 provider，但 ID 本身是契约内的合法站源 ID。
	// 「旧接口自动发现」（legacy）刻意不在此表内：它不是站源、没有对外 ID，
	// 命中的输入走 SourceID 的 ErrFallbackOnly 拒绝路径。
	"xifan": source.Xifan,
	"hema":  source.Hema,
}

// hostSourceSuffixes 复刻 providerSourceForURL 的后缀规则：huangguo 的镜像域与 yeguo 的跳板域。
var hostSourceSuffixes = []struct {
	suffix string
	id     source.ID
}{
	{".ediayikma.cc", source.Huangguo},
	{".agdkczeyx.cc", source.Huangguo},
	{".buxefaex.cc", source.Yeguo},
	{".fzchosdi.cc", source.Yeguo},
}

// SourceIDs 按 §4.3 的规范顺序返回 11 个对外站源 ID（副本；顺序的唯一真相源是 source.IDs）。
func SourceIDs() []source.ID {
	return source.IDs()
}

// SourceID 把 core 侧的站源名/facet 名/主机名归一为 §4.1 的站源 ID。
//
// 传入 URL 时自动改走 SourceIDForURL。两类拒绝路径刻意分开：
//   - "legacy"（旧接口自动发现）返回 ErrFallbackOnly：它是回退机制而非站源，
//     当成站源会凭空造出一个没有数据的源；
//   - 其余无法识别时返回包装了 ErrUnknownSource 的错误——core 的同名逻辑用空串表示
//     未知，这里刻意改成显式错误：空串会被下游当成「有效但为空」，是跨语言边界上
//     最容易漏掉的失败形态。
func SourceID(name string) (source.ID, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("%w: 站源名为空", ErrInvalidID)
	}
	if strings.Contains(trimmed, "://") {
		return SourceIDForURL(trimmed)
	}
	lowered := strings.ToLower(trimmed)
	if lowered == fallbackSourceName {
		return "", fmt.Errorf("%w: %q（旧接口自动发现由 core 的 provider_legacy_app.go 以黄果旧版 facet 兜底；它不是站源、没有对外 ID）", ErrFallbackOnly, trimmed)
	}
	if id, ok := coreSourceAliases[lowered]; ok {
		return id, nil
	}
	if isHostLike(lowered) {
		if id, ok := sourceForHost(lowered); ok {
			return id, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownSource, trimmed)
}

// SourceIDForURL 复刻 core 的 providerSourceForURL：按主机名判定该 URL 属于哪个站源。
func SourceIDForURL(rawURL string) (source.ID, error) {
	trimmed := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %q 不是合法 URL: %v", ErrInvalidID, trimmed, err)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", fmt.Errorf("%w: %q 缺少主机名", ErrInvalidID, trimmed)
	}
	if id, ok := sourceForHost(host); ok {
		return id, nil
	}
	return "", fmt.Errorf("%w: 主机 %q 未匹配任何已知站源", ErrUnknownSource, host)
}

func sourceForHost(host string) (source.ID, bool) {
	if id, ok := coreSourceAliases[host]; ok {
		return id, true
	}
	for _, rule := range hostSourceSuffixes {
		if strings.HasSuffix(host, rule.suffix) {
			return rule.id, true
		}
	}
	return "", false
}

func isHostLike(value string) bool {
	return strings.Contains(value, ".") &&
		!strings.ContainsAny(value, " \t/\\") &&
		!strings.HasPrefix(value, ".") &&
		!strings.HasSuffix(value, ".")
}

// CoreSources 返回该 §4.1 站源 ID 在本仓 core 里对应的 provider 源名，按优先级排序。
//
// 两处刻意的差异（均按 §4.3，不修改 core 行为）：
//   - Huangguo 返回三个 facet（huangguoai、huangguo-video、cloudfront），调用方需自行
//     决定用哪个；组内中文入口名见 FacetEntryName；
//   - Xifan / Hema 返回 ErrNotImplementedInCore：它们是 C 侧独有源，§3.3 计划在 M3/M4 移植。
//
// "legacy" 不是合法入参（不占对外 ID），但仍显式拒绝而非报未知源：老配置/老持久化
// 里可能出现它，调用方需要能区分「回退机制」与「拼错的源名」。
func CoreSources(id source.ID) ([]string, error) {
	if names, ok := coreSources[id]; ok {
		return append([]string(nil), names...), nil
	}
	// 回退机制名按大小写不敏感判定（它只用于「解释为什么拒绝」），其余 ID 一律精确匹配。
	if strings.EqualFold(string(id), fallbackSourceName) {
		return nil, fmt.Errorf("%w: %s（旧接口自动发现，core 的 provider_legacy_app.go 以黄果旧版 facet 兜底）", ErrFallbackOnly, id)
	}
	switch id {
	case source.Xifan, source.Hema:
		return nil, fmt.Errorf("%w: %s（§3.3 计划 M3/M4 从 C 移植）", ErrNotImplementedInCore, id)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownSource, id)
	}
}

// PrimaryCoreSource 返回主 provider 源名（CoreSources 的首项）。
//
// 对 Huangguo 取 "huangguoai"：这与 core 自身把裸 "huangguo" 归一为 huangguoai 的
// 行为一致（core 的 canonicalProviderSource）。需要精确 facet 时用 CoreSources 展开。
func PrimaryCoreSource(id source.ID) (string, error) {
	names, err := CoreSources(id)
	if err != nil {
		return "", err
	}
	return names[0], nil
}

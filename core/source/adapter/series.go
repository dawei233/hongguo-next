// 本仓新增（M1 站源适配层）。
//
// 理由：§4.1 的 Series / Page 是跨源统一模型，而 vendor 的 core 有两条等价来源：
// 已导出的 core.Drama（源返回的原始剧数据）与未导出的 core.nativeDrama（内核归一后的
// 剧数据，随 nativeInput.Drama / 目录结果 JSON 上线）。本文件把两者映射到契约类型。
//
// core.nativeDrama 未导出、本包无法命名，因此镜像为 NativeDrama 并额外提供
// 「any + JSON 形态」入口：core 内部（M2 接线时）可以把 nativeDrama 值直接传进来，
// 跨包传值合法、只是不能命名其类型。本包不改动 core 的任何导出符号。
package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dawei233/hongguo-next/core"
	"github.com/dawei233/hongguo-next/core/source"
)

// ErrInvalidSeries：剧数据不合法（既无可用 ID 也无站源前缀）。
var ErrInvalidSeries = errors.New("剧数据无效")

// displayTitleFallback 与 core 的 Drama.DisplayTitle 兜底文案一致。
const displayTitleFallback = "短剧"

// NativeDrama 是 core.nativeDrama 的线格式镜像（JSON tag 逐字段一致）。
//
// 只读镜像：core 的该类型未导出，跨包不能命名，也无法在包外做编译期断言；
// 字段名与 tag 必须与 core/app_runtime.go 的 nativeDrama 保持同步，目前由
// series_test.go 的黄金 JSON 夹具兜底。M2 接线时应在 core 内补一条
// 「core.nativeDrama 可无损反序列化进本类型」的断言；本次里程碑不改动 core 目录。
type NativeDrama struct {
	MetadataSchema int      `json:"metadataSchema"`
	ID             string   `json:"id"`
	Source         string   `json:"source"`
	SourceID       string   `json:"sourceId"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Cover          string   `json:"cover"`
	Episodes       int      `json:"episodes"`
	Category       string   `json:"category"`
	VIP            *bool    `json:"vip"`
	Heat           string   `json:"heat,omitempty"`
	Views          string   `json:"views,omitempty"`
	OnlineDate     string   `json:"onlineDate,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	ReleaseStatus  string   `json:"releaseStatus,omitempty"`
}

// NativeCatalogPage 是 core 目录结果（nativeCatalogResult）的线格式镜像。
type NativeCatalogPage struct {
	Items       []NativeDrama `json:"items"`
	HasMore     bool          `json:"hasMore"`
	Page        int           `json:"page"`
	Warning     string        `json:"warning,omitempty"`
	LocalSearch bool          `json:"localSearch"`
	Fresh       bool          `json:"fresh"`
}

// yearPattern 只取 2000–2100 的四位年份，越界视为未知（与 core 的 providerReleaseDate 边界一致）。
var yearPattern = regexp.MustCompile(`\b(20[0-9]{2})\b`)

// SeriesFromDrama 把 core.Drama 映射为契约 Series。
//
// 字段取舍与 core 的 nativeNormalize（core/app_runtime.go）保持一致，
// 以免同一部剧在「直连 Drama」与「经内核归一」两条路径上得到不同结果：
//   - 封面按 core 的候选顺序取第一个可用地址，并解出内核 /api/ui/image 代理形态里的原图地址；
//   - 集数取 TotalEpisode/EpisodeCount/ChapterCount/Total/Episodes 的最大值；
//   - 标签取 CategoryName/TypeName/SortName/Category 的第一个非空值，再退回 ChannelName。
func SeriesFromDrama(drama core.Drama) (source.Series, error) {
	id, sourceKey, err := seriesIdentity(drama.ID, drama.Source, drama.SourceID)
	if err != nil {
		return source.Series{}, err
	}
	seriesID, err := SeriesID(id, sourceKey)
	if err != nil {
		return source.Series{}, err
	}
	title := strings.TrimSpace(drama.DisplayTitle())
	if title == "" {
		title = displayTitleFallback
	}
	cover := ""
	for _, candidate := range []any{
		drama.Cover, drama.CoverURL, drama.CoverURLSnake, drama.Image, drama.ImageURL,
		drama.ImageURLSnake, drama.Img, drama.Pic, drama.Picture, drama.Poster,
		drama.Thumb, drama.Thumbnail,
	} {
		if resolved := contractCoverURL(textFromAny(candidate)); resolved != "" {
			cover = resolved
			break
		}
	}
	total := 0
	for _, candidate := range []any{
		drama.TotalEpisode, drama.TotalEpisodeSnake, drama.EpisodeCount, drama.EpisodeCountSnake,
		drama.ChapterCount, drama.ChapterCountSnake, drama.Total, drama.Episodes,
	} {
		if count, err := strconv.Atoi(textFromAny(candidate)); err == nil && count > total {
			total = count
		}
	}
	return source.Series{
		ID:       seriesID,
		SourceID: id,
		Title:    title,
		Cover:    cover,
		Year:     yearFromText(drama.OnlineDate),
		Total:    total,
		Tag:      dramaTag(drama),
		Score:    strings.TrimSpace(drama.Score),
	}, nil
}

// SeriesFromNativeDrama 把镜像形态的内核剧数据映射为契约 Series。
func SeriesFromNativeDrama(drama NativeDrama) (source.Series, error) {
	id, sourceKey, err := seriesIdentity(drama.ID, drama.Source, drama.SourceID)
	if err != nil {
		return source.Series{}, err
	}
	seriesID, err := SeriesID(id, sourceKey)
	if err != nil {
		return source.Series{}, err
	}
	title := strings.TrimSpace(drama.Title)
	if title == "" {
		title = displayTitleFallback
	}
	return source.Series{
		ID:       seriesID,
		SourceID: id,
		Title:    title,
		Cover:    contractCoverURL(drama.Cover),
		Year:     yearFromText(drama.OnlineDate),
		Total:    drama.Episodes,
		Tag:      strings.TrimSpace(drama.Category),
		// core.nativeDrama 不携带评分：镜像路径的 Score 恒为 ""（未知），
		// 需要评分时走 SeriesFromDrama。
		Score: "",
	}, nil
}

// SeriesFromNativeDramaValue 接受任意「内核剧数据」值（含 core 内部把
// core.nativeDrama 值直接传进来的情形），按 JSON 形态解码后映射。
//
// 这是本包与未导出类型对接的唯一入口：跨包传值合法，命名类型不合法。
func SeriesFromNativeDramaValue(value any) (source.Series, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return source.Series{}, fmt.Errorf("%w: 无法序列化剧数据: %v", ErrInvalidSeries, err)
	}
	var drama NativeDrama
	if err := json.Unmarshal(body, &drama); err != nil {
		return source.Series{}, fmt.Errorf("%w: 剧数据不是对象形态: %v", ErrInvalidSeries, err)
	}
	return SeriesFromNativeDrama(drama)
}

// PageFromNativeCatalog 把内核目录结果映射为契约 Page。
//
// Total 恒为 0：core 的目录结果只带 hasMore，不带总数，而契约用 0 表示未知
// ——不猜一个「看起来合理」的数。
func PageFromNativeCatalog(catalog NativeCatalogPage) (source.Page, error) {
	items := make([]source.Series, 0, len(catalog.Items))
	for offset, drama := range catalog.Items {
		series, err := SeriesFromNativeDrama(drama)
		if err != nil {
			return source.Page{}, fmt.Errorf("目录第 %d 项: %w", offset+1, err)
		}
		items = append(items, series)
	}
	return source.Page{Items: items, Total: 0, HasMore: catalog.HasMore}, nil
}

// PageFromNativeCatalogValue 是 PageFromNativeCatalog 的 any/JSON 形态入口。
func PageFromNativeCatalogValue(value any) (source.Page, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return source.Page{}, fmt.Errorf("%w: 无法序列化目录结果: %v", ErrInvalidSeries, err)
	}
	var catalog NativeCatalogPage
	if err := json.Unmarshal(body, &catalog); err != nil {
		return source.Page{}, fmt.Errorf("%w: 目录结果不是对象形态: %v", ErrInvalidSeries, err)
	}
	return PageFromNativeCatalog(catalog)
}

// seriesIdentity 推导 (站源 ID, 源内剧 ID)。
//
// 优先用 core 的剧 ID（"<facet>:<sourceKey>"，信息最全），退化时才用
// Source + SourceID 两个独立字段，且两者必须自洽。返回的是站源 ID 本身
// （facet 已归一），不是复合剧 ID。
func seriesIdentity(coreDramaID, coreSource, coreSourceID string) (source.ID, string, error) {
	trimmedID := strings.TrimSpace(coreDramaID)
	if strings.Contains(trimmedID, ":") {
		id, key, err := SplitSeriesID(trimmedID)
		if err != nil {
			return "", "", fmt.Errorf("%w: 剧 ID %q: %v", ErrInvalidSeries, coreDramaID, err)
		}
		return id, key, nil
	}
	key := strings.TrimSpace(coreSourceID)
	if key == "" && trimmedID != "" {
		key = trimmedID
	}
	if key == "" {
		return "", "", fmt.Errorf("%w: 剧 ID %q 与源内 ID 均为空", ErrInvalidSeries, coreDramaID)
	}
	id, err := SourceID(coreSource)
	if err != nil {
		return "", "", fmt.Errorf("%w: 剧 ID %q 缺少可解析的站源: %v", ErrInvalidSeries, coreDramaID, err)
	}
	return id, key, nil
}

// contractCoverURL 把 core 的封面值归一为契约要求的「绝对 URL 或空串」。
//
// 复刻 core 的 coverPathFromAny 语义（只读复刻）：协议相对地址补 https:，
// 内核自己的图片代理形态 "/api/ui/image?url=..." 解出其中的原图地址。
// 归一不出来的一律返回 ""：契约允许空串表示未知，但不允许把相对路径伪装成封面地址。
func contractCoverURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	}
	if strings.HasPrefix(value, "/api/ui/image") {
		if parsed, err := url.Parse(value); err == nil {
			if inner := strings.TrimSpace(parsed.Query().Get("url")); inner != "" {
				value = inner
			}
		}
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return ""
}

// yearFromText 从上线日期里取年份；RFC3339 先按时间解析，其余取首个 2000–2099 的四位年。
func yearFromText(raw string) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}
	if stamp, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return stamp.Year()
	}
	match := yearPattern.FindString(value)
	if match == "" {
		return 0
	}
	year, err := strconv.Atoi(match)
	if err != nil {
		return 0
	}
	return year
}

// dramaTag 复刻 core 的 nativeDramaCategory 取值顺序。
func dramaTag(drama core.Drama) string {
	for _, value := range []string{
		drama.CategoryName, drama.CategoryNameSnake, drama.TypeName, drama.TypeNameSnake,
		drama.SortName, drama.SortNameSnake, drama.Category,
	} {
		if tag := strings.TrimSpace(value); tag != "" {
			return tag
		}
	}
	switch strings.TrimSpace(drama.ChannelName) {
	case "真人剧", "短剧", "漫剧", "AI 剧", "AI剧", "AI 短剧", "AI 漫剧", "AI 换脸", "AI 魔改":
		return strings.TrimSpace(drama.ChannelName)
	}
	return ""
}

// textFromAny 复刻 core 的 nativeText（只读复刻）：把源返回的任意标量/容器取成文本。
func textFromAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"url", "src", "cover", "image", "pic"} {
			if text := textFromAny(typed[key]); text != "" {
				return text
			}
		}
	case []any:
		for _, entry := range typed {
			if text := textFromAny(entry); text != "" {
				return text
			}
		}
	case json.Number:
		return typed.String()
	case float64:
		return strconv.Itoa(int(typed))
	case float32:
		return strconv.Itoa(int(typed))
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	}
	return ""
}

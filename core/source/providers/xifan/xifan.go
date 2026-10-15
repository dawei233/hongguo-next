// 本仓新增（M3 站源 provider）。
//
// 本文件是 C `refs/hongguo-drama/src/native/xifan.js`（135 行）的 Go 逐函数转写
// （ARCHITECTURE §3.3 #24）。映射关系（JS → 本文件）：
//
//	const API / ID          (3-4)     → DefaultBaseURL / idPattern
//	plainText               (7-9)     → plainText
//	parseId                 (11-17)   → parseID
//	httpsUrl                (19-24)   → httpsURL
//	request                 (26-40)   → (*Provider).request
//	contents                (42-44)   → contents
//	genres                  (46-60)   → (*Provider).categories
//	normalizeList           (62-73)   → normalizeSeries
//	browseList              (75-86)   → Search（Keyword 为空时）/ Categories
//	search                  (88-95)   → Search（Keyword 非空时）
//	detail                  (97-104)  → (*Provider).detail
//	fetchEpisodeList        (106-117) → Episodes
//	fetchPlayUrlSingle      (119-133) → Resolve
//
// # 移植时保留的硬性行为（每一条都来自 C 的自检脚本 scripts/check-xifan.cjs）
//
//   - 端点常量与每请求都附带的 `version=2001001&androidVersionCode=28`；
//   - `ret !== 0 || !result` 一律判失败（不解析部分结果）；
//   - 分类 slug 形如 `<categoryId>-<oppoCategory 的 UTF-8 十六进制>`，
//     因此「同分类号不同名称」不会碰撞（自检脚本专门断言过这一点）；
//   - 总页码不虚构：来源不返回总页数，契约里 Total/HasMore 按本页是否满页推断，
//     绝不编造 totalPages；
//   - **分集列表不泄漏任何媒体地址**（自检脚本断言 `!JSON.stringify(episodes).includes('playUrl')`），
//     本仓同样只在 Resolve 时取地址；
//   - 每次 Resolve 都重新读一次 `getDuanjuInfo` 判定 unlock，**不缓存解锁状态**
//     （「原先可用地址也必须重新验锁」）；`unlock` 必须严格为 true 才算解锁，
//     缺失/false 一律锁定；
//   - 播放地址必须是 https、且不带用户名密码；http/file 一律拒绝，
//     且**不得**回退到其它字段；
//   - 身份非法（ID 不匹配正则、或分集不属于该剧）必须在**发起网络请求之前**失败。
//
// 与 JS 的接口差异：JS 返回 `{success:false,error}` 形态的错误对象，本仓按 §4.1 的
// Source 接口返回 error（错误文案保持原样，便于与 C 侧日志对照）。
package xifan

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dawei233/hongguo-next/core/source"
	"github.com/dawei233/hongguo-next/core/source/providers/internal/httpx"
)

// DefaultBaseURL 是 xifan.js:3 的 API 常量。
const DefaultBaseURL = "https://xifan-api-cn.youlishipin.com"

// 与 JS 逐项对应：每页 30 条（browseList:82 的 offset=(pg-1)*30）、
// 分类缓存 5 分钟（genres:58）、单页请求上限（axios maxContentLength）。
const (
	pageSize         = 30
	categoryCacheTTL = 5 * time.Minute
	defaultUserAgent = "Mozilla/5.0"
)

// idPattern 对应 xifan.js:4 的 `^[a-zA-Z0-9_-]{1,200}$`。
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,200}$`)

// tagPattern 对应 plainText 里的 `replace(/<[^>]*>/g, ”)`。
var tagPattern = regexp.MustCompile(`<[^>]*>`)

// ErrUnsupported 用于未实现的能力位（§4.1：不支持的能力必须返回错误，不得假装成功）。
var ErrUnsupported = errors.New("该站源不支持此能力")

// ErrInvalidID / ErrLocked / ErrNotAvailable 是 Resolve 的稳定错误分类。
var (
	ErrInvalidID    = errors.New("西饭剧集编号无效")
	ErrLocked       = errors.New("该集当前锁定，请在西饭官方 App 确认可看范围")
	ErrNotAvailable = errors.New("西饭未提供有效播放地址")
)

// Provider 是西饭站源（无状态、可并发，§4.1）。
type Provider struct {
	// BaseURL 默认 DefaultBaseURL；测试里指向 httptest。
	BaseURL string
	// HTTP 为 nil 时用默认客户端（10 秒超时、不跟随重定向、8MB 上限）。
	HTTP *httpx.Client
	// Now 为 nil 时用 time.Now（分类缓存）。
	Now func() time.Time

	cachedCats []source.Category
	cachedAt   time.Time
	cacheMu    sync.Mutex
}

// New 造一个西饭 provider（BaseURL 为空时用 DefaultBaseURL）。
func New(baseURL string) *Provider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Provider{BaseURL: strings.TrimRight(baseURL, "/")}
}

var _ source.Source = (*Provider)(nil)

// ID 实现 source.Source。
func (p *Provider) ID() source.ID { return source.Xifan }

// Name 实现 source.Source。
func (p *Provider) Name() string { return "西饭" }

// Caps 实现 source.Source。
//
// 只声明实测确认的能力：搜索、分类浏览、返回可直链播放的地址。
// 不声明 CapHLS —— 播放地址是否可能为 m3u8 尚未实测（M3 待办），
//
//	宁可不声明（宁缺勿滥）也不要让调用方以为拿到了 HLS 能力。
func (p *Provider) Caps() source.Caps {
	return source.CapSearch | source.CapCategory | source.CapDirectPlay
}

func (p *Provider) client() *httpx.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	// 头与 xifan.js:32 一致：只有 UA 与 Accept（不带任何 cookie/token）。
	p.HTTP = httpx.New(p.BaseURL, map[string]string{"Accept": "application/json"})
	p.HTTP.UserAgent = defaultUserAgent
	return p.HTTP
}

func (p *Provider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// ── JS 侧纯函数 ────────────────────────────────────────────────────────────

// plainText 对应 xifan.js:7-9：剥 HTML 标签并裁空白。
func plainText(value string) string {
	return strings.TrimSpace(tagPattern.ReplaceAllString(value, ""))
}

// identity 是 parseId 的产物。
type identity struct {
	Source    string
	DuanjuID  string
	EpisodeID string
	SeriesID  string
}

// parseID 对应 xifan.js:11-17。
//
// 剧 ID：`xifan:<source>:<duanjuId>`（3 段）；分集 ID：再加 `:<episodeId>`（4 段）。
// 每段都必须匹配 idPattern —— 这一条同时挡住了路径穿越（如 `../outside`）。
func parseID(value string, episode bool) (identity, error) {
	parts := strings.Split(value, ":")
	want := 3
	if episode {
		want = 4
	}
	if len(parts) != want || parts[0] != "xifan" {
		return identity{}, ErrInvalidID
	}
	for _, part := range parts[1:] {
		if !idPattern.MatchString(part) {
			return identity{}, ErrInvalidID
		}
	}
	out := identity{Source: parts[1], DuanjuID: parts[2], SeriesID: strings.Join(parts[:3], ":")}
	if episode {
		out.EpisodeID = parts[3]
	}
	return out, nil
}

// httpsURL 对应 xifan.js:19-24：只接受 https、且不带用户名/密码的地址。
func httpsURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

// ── 取数与解析 ────────────────────────────────────────────────────────────

// request 对应 xifan.js:26-40：GET + 固定附带 version/androidVersionCode，
// 且要求 `ret === 0 && typeof result === 'object'`。
func (p *Provider) request(ctx context.Context, apiPath string, params url.Values) (json.RawMessage, error) {
	query := url.Values{}
	for name, values := range params {
		for _, value := range values {
			query.Add(name, value)
		}
	}
	// xifan.js:31 的两个固定参数。
	query.Set("version", "2001001")
	query.Set("androidVersionCode", "28")

	body, err := p.client().Get(ctx, apiPath, query)
	if err != nil {
		if errors.Is(err, httpx.ErrTimeout) {
			return nil, errors.New("西饭加载超时，请重试")
		}
		return nil, errors.New("西饭来源暂不可用，请稍后重试")
	}
	var envelope struct {
		Ret    *int            `json:"ret"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, errors.New("西饭来源暂不可用，请稍后重试")
	}
	if envelope.Ret == nil || *envelope.Ret != 0 || len(envelope.Result) == 0 {
		return nil, errors.New("西饭来源暂不可用，请稍后重试")
	}
	trimmed := strings.TrimSpace(string(envelope.Result))
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil, errors.New("西饭来源暂不可用，请稍后重试")
	}
	return envelope.Result, nil
}

// contents 对应 xifan.js:42-44：`elements[].contents[]` 拉平。
type contentItem struct {
	CategoryItem struct {
		CategoryID    httpx.FlexInt  `json:"categoryId"`
		OppoCategory  string         `json:"oppoCategory"`
		Version       httpx.FlexInt  `json:"version"`
		SubCategories []categoryItem `json:"subCategories"`
	} `json:"categoryItemVo"`
	Duanju struct {
		Source     string           `json:"source"`
		DuanjuID   httpx.FlexString `json:"duanjuId"`
		Title      httpx.FlexString `json:"title"`
		CoverImage httpx.FlexString `json:"coverImageUrl"`
		Total      httpx.FlexInt    `json:"total"`
		Categories []string         `json:"categories"`
	} `json:"duanjuVo"`
}

type categoryItem struct {
	CategoryID   httpx.FlexInt `json:"categoryId"`
	OppoCategory string        `json:"oppoCategory"`
	Version      httpx.FlexInt `json:"version"`
}

type resultEnvelope struct {
	Elements []struct {
		Contents []json.RawMessage `json:"contents"`
	} `json:"elements"`
	HasMore bool `json:"hasMore"`
}

func contentsOf(raw json.RawMessage) ([]contentItem, bool, error) {
	var envelope resultEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, false, err
	}
	var items []contentItem
	for _, element := range envelope.Elements {
		for _, content := range element.Contents {
			var item contentItem
			if err := json.Unmarshal(content, &item); err != nil {
				// 与 JS 一致：contents 里若不是对象，各字段取值皆为 undefined，
				// 后续的 ID/标题校验必然把它滤掉，这里直接跳过等价。
				continue
			}
			items = append(items, item)
		}
	}
	return items, envelope.HasMore, nil
}

// categories 对应 xifan.js:46-60（含 5 分钟缓存）。
func (p *Provider) categories(ctx context.Context, forceRefresh bool) ([]source.Category, error) {
	p.cacheMu.Lock()
	if !forceRefresh && len(p.cachedCats) > 0 && p.cachedAt.Add(categoryCacheTTL).After(p.now()) {
		cached := append([]source.Category(nil), p.cachedCats...)
		p.cacheMu.Unlock()
		return cached, nil
	}
	p.cacheMu.Unlock()

	raw, err := p.request(ctx, "/xifan/drama/portalPage", url.Values{"reqType": {"duanjuCategory"}})
	if err != nil {
		return nil, err
	}
	items, _, err := contentsOf(raw)
	if err != nil {
		return nil, errors.New("西饭来源暂不可用，请稍后重试")
	}
	seen := map[string]bool{}
	var out []source.Category
	for _, item := range items {
		candidates := item.CategoryItem.SubCategories
		// JS：`category ? (Array.isArray(subCategories) ? subCategories : [category]) : []`
		if item.CategoryItem.CategoryID != 0 || item.CategoryItem.OppoCategory != "" {
			if len(candidates) == 0 {
				candidates = []categoryItem{{CategoryID: item.CategoryItem.CategoryID,
					OppoCategory: item.CategoryItem.OppoCategory, Version: item.CategoryItem.Version}}
			}
		}
		for _, candidate := range candidates {
			label := plainText(candidate.OppoCategory)
			if candidate.CategoryID <= 0 || label == "" {
				continue
			}
			slug := categorySlug(candidate.CategoryID.Int(), label)
			if seen[slug] {
				continue
			}
			seen[slug] = true
			out = append(out, source.Category{ID: slug, Name: label})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("西饭没有返回可用分类，请重试")
	}
	p.cacheMu.Lock()
	p.cachedCats, p.cachedAt = out, p.now()
	p.cacheMu.Unlock()
	return out, nil
}

// categorySlug 对应 xifan.js:54：`${categoryId}-${Buffer.from(name).toString('hex')}`。
func categorySlug(categoryID int, label string) string {
	return strconv.Itoa(categoryID) + "-" + hex.EncodeToString([]byte(label))
}

// normalizeSeries 对应 xifan.js:62-73 的 normalizeList（只取契约需要的字段）。
//
// 与 JS 的差异：JS 的返回体是给旧 UI 用的 `{success, results, page, total, totalPages,
// hasMore, nextPage, genres, pageTitle, sourceUrl}`；本仓按 §4.1 只产出 Page
// （Total/HasMore），**不虚构总页数**（JS 自己也把 totalPages 固定为 0）。
func normalizeSeries(items []contentItem, hasMore bool) source.Page {
	seen := map[string]bool{}
	page := source.Page{Items: []source.Series{}}
	for _, item := range items {
		drama := item.Duanju
		if !idPattern.MatchString(drama.Source) || !idPattern.MatchString(drama.DuanjuID.String()) {
			continue
		}
		title := plainText(drama.Title.String())
		if title == "" {
			continue
		}
		seriesID := source.ID("xifan:" + drama.Source + ":" + drama.DuanjuID.String())
		if seen[string(seriesID)] {
			continue
		}
		seen[string(seriesID)] = true
		page.Items = append(page.Items, source.Series{
			ID:       seriesID,
			SourceID: source.Xifan,
			Title:    title,
			Cover:    httpsURL(drama.CoverImage.String()),
			Total:    drama.Total.Int(),
			Tag:      firstTags(drama.Categories),
		})
	}
	// JS：`hasMore = result.hasMore === true && results.length > 0`
	page.HasMore = hasMore && len(page.Items) > 0
	return page
}

// firstTags 对应 JS 的 `categories.map(plainText).filter(Boolean).slice(0, 4)`。
func firstTags(values []string) string {
	out := make([]string, 0, 4)
	for _, value := range values {
		text := plainText(value)
		if text == "" {
			continue
		}
		out = append(out, text)
		if len(out) == 4 {
			break
		}
	}
	return strings.Join(out, "/")
}

// Search 实现 source.Source：Keyword 为空时等价于 JS 的 browseList（分类浏览），
// 非空时等价于 JS 的 search。
//
// 两者都走本函数是因为 §4.1 只有一个 Search 入口；
// 分类选择经 Query.Filters["genre"]（值为 Categories 返回的 slug）传入。
func (p *Provider) Search(ctx context.Context, q source.Query) (source.Page, error) {
	page := q.Page
	if page < 1 {
		page = 1
	}
	keyword := strings.TrimSpace(q.Keyword)
	if keyword != "" {
		raw, err := p.request(ctx, "/xifan/search/getSearchList",
			url.Values{"keyword": {keyword}, "pageIndex": {strconv.Itoa(page)}})
		if err != nil {
			return source.Page{}, err
		}
		items, hasMore, err := contentsOf(raw)
		if err != nil {
			return source.Page{}, errors.New("西饭来源暂不可用，请稍后重试")
		}
		return normalizeSeries(items, hasMore), nil
	}
	return p.browse(ctx, q.Filters["genre"], page)
}

// browse 对应 xifan.js:75-86。
func (p *Provider) browse(ctx context.Context, genre string, page int) (source.Page, error) {
	categories, err := p.categories(ctx, false)
	if err != nil {
		return source.Page{}, err
	}
	selected, ok := selectCategory(categories, genre)
	if !ok {
		return source.Page{}, errors.New("西饭题材无效，请重新选择")
	}
	categoryID, label := splitSlug(selected)
	if categoryID <= 0 {
		return source.Page{}, errors.New("西饭题材无效，请重新选择")
	}
	raw, err := p.request(ctx, "/xifan/drama/portalPage", url.Values{
		"reqType":         {"aggregationPage"},
		"offset":          {strconv.Itoa((page - 1) * pageSize)},
		"categoryId":      {strconv.Itoa(categoryID)},
		"categoryNames":   {label},
		"categoryVersion": {"1"},
	})
	if err != nil {
		return source.Page{}, err
	}
	items, hasMore, err := contentsOf(raw)
	if err != nil {
		return source.Page{}, errors.New("西饭来源暂不可用，请稍后重试")
	}
	return normalizeSeries(items, hasMore), nil
}

// selectCategory 对应 JS：`genre ? items.find(slug===genre) : items.find(label==='都市') || items[0]`。
func selectCategory(categories []source.Category, genre string) (source.Category, bool) {
	if strings.TrimSpace(genre) != "" {
		for _, item := range categories {
			if item.ID == genre {
				return item, true
			}
		}
		return source.Category{}, false
	}
	for _, item := range categories {
		if item.Name == "都市" {
			return item, true
		}
	}
	if len(categories) == 0 {
		return source.Category{}, false
	}
	return categories[0], true
}

// splitSlug 反解 `<categoryId>-<hex(label)>`。
func splitSlug(category source.Category) (int, string) {
	index := strings.Index(category.ID, "-")
	if index <= 0 {
		return 0, ""
	}
	categoryID, err := strconv.Atoi(category.ID[:index])
	if err != nil {
		return 0, ""
	}
	decoded, err := hex.DecodeString(category.ID[index+1:])
	if err != nil {
		return categoryID, category.Name
	}
	return categoryID, string(decoded)
}

// Categories 实现 source.Source。
func (p *Provider) Categories(ctx context.Context) ([]source.Category, error) {
	return p.categories(ctx, false)
}

// Rank 实现 source.Source：西饭没有榜单接口（JS 里也没有），按 §4.1 明确报不支持。
func (p *Provider) Rank(context.Context, string, int) (source.Page, error) {
	return source.Page{}, fmt.Errorf("%w: 西饭没有榜单", ErrUnsupported)
}

// Danmaku 实现 source.Source：西饭不提供弹幕，且没有可用的无签名评论降级源。
func (p *Provider) Danmaku(context.Context, string, int64, int64) (source.DanmakuTrack, error) {
	return source.DanmakuTrack{}, fmt.Errorf("%w: 西饭没有弹幕", ErrUnsupported)
}

// Detail 实现 source.Source。
func (p *Provider) Detail(ctx context.Context, seriesID string) (source.Series, error) {
	parsed, err := parseID(seriesID, false)
	if err != nil {
		return source.Series{}, err
	}
	result, err := p.detail(ctx, parsed)
	if err != nil {
		return source.Series{}, err
	}
	// Total：优先用上游声明的总集数；没有就退回「实际返回的分集数」
	// （两者都不是虚构 —— 契约里 0 表示未知，故优先取更明确的那个）。
	total := result.Total.Int()
	if total <= 0 {
		total = len(result.Episodes)
	}
	return source.Series{
		ID:       source.ID(parsed.SeriesID),
		SourceID: source.Xifan,
		Title:    firstNonEmpty(plainText(result.Title.String()), "短剧"),
		Cover:    httpsURL(result.CoverImage.String()),
		Total:    total,
	}, nil
}

// Episodes 实现 source.Source。
//
// 契约不含「锁定」字段：锁定状态每次 Resolve 时重新判定（对应 JS 的
// 「Always re-read access state」），因此这里只给出集号与标题，
// 也**不会**泄漏任何媒体地址。
func (p *Provider) Episodes(ctx context.Context, seriesID string) ([]source.Episode, error) {
	parsed, err := parseID(seriesID, false)
	if err != nil {
		return nil, err
	}
	result, err := p.detail(ctx, parsed)
	if err != nil {
		return nil, err
	}
	episodes := make([]source.Episode, 0, len(result.Episodes))
	for _, episode := range result.Episodes {
		if !idPattern.MatchString(episode.EpisodeID.String()) {
			continue
		}
		episodes = append(episodes, source.Episode{
			SeriesID:  source.ID(parsed.SeriesID),
			EpisodeID: parsed.SeriesID + ":" + episode.EpisodeID.String(),
			Index:     episode.Index.Int(),
			Title:     plainText(episode.Title.String()),
		})
	}
	sortEpisodes(episodes)
	if len(episodes) == 0 {
		return nil, errors.New("西饭分集列表为空")
	}
	return episodes, nil
}

// Resolve 实现 source.Source（对应 xifan.js:119-133 的 fetchPlayUrlSingle）。
func (p *Provider) Resolve(ctx context.Context, episodeID string, quality source.Quality) (source.Stream, error) {
	parsed, err := parseID(episodeID, true)
	if err != nil {
		return source.Stream{}, err
	}
	result, err := p.detail(ctx, parsed)
	if err != nil {
		return source.Stream{}, err
	}
	var episode *detailEpisode
	for index := range result.Episodes {
		if result.Episodes[index].EpisodeID.String() == parsed.EpisodeID {
			episode = &result.Episodes[index]
			break
		}
	}
	if episode == nil {
		return source.Stream{}, errors.New("西饭未找到该分集")
	}
	// unlock 必须严格为 true（JS：`episode.unlock !== true`）。
	if !episode.Unlock.Bool() {
		return source.Stream{}, fmt.Errorf("%w（第 %d 集）", ErrLocked, episode.Index.Int())
	}
	address := httpsURL(episode.PlayURL.String())
	if address == "" {
		return source.Stream{}, ErrNotAvailable
	}
	return source.Stream{
		URL:       address,
		Encrypted: false,
		Headers:   nil,
		Quality:   string(source.QualityAuto),
		Container: containerFor(address),
		// 西饭给的是明文直链（无 CENC、无 HLS 密钥），因此无需净化。
		NeedsSanitize: false,
	}, nil
}

// ── detail ────────────────────────────────────────────────────────────────

type detailEpisode struct {
	EpisodeID httpx.FlexString `json:"episodeId"`
	Index     httpx.FlexInt    `json:"index"`
	Title     httpx.FlexString `json:"title"`
	// Unlock 用 StrictBool 复刻 JS 的 `episode.unlock !== true`（非布尔一律按锁定处理）。
	Unlock  httpx.StrictBool `json:"unlock"`
	PlayURL httpx.FlexString `json:"playUrl"`
}

type detailResult struct {
	Source     string           `json:"source"`
	DuanjuID   httpx.FlexString `json:"duanjuId"`
	Title      httpx.FlexString `json:"title"`
	CoverImage httpx.FlexString `json:"coverImageUrl"`
	Total      httpx.FlexInt    `json:"total"`
	Episodes   []detailEpisode  `json:"episodeList"`
}

// detail 对应 xifan.js:97-104：拿到结果后必须校验「返回的剧与请求一致」。
func (p *Provider) detail(ctx context.Context, parsed identity) (detailResult, error) {
	raw, err := p.request(ctx, "/xifan/drama/getDuanjuInfo",
		url.Values{"source": {parsed.Source}, "duanjuId": {parsed.DuanjuID}})
	if err != nil {
		return detailResult{}, err
	}
	var result detailResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return detailResult{}, errors.New("西饭来源暂不可用，请稍后重试")
	}
	// JS：`result.source !== source || String(result.duanjuId) !== duanjuId || !Array.isArray(episodeList)`
	if result.Source != parsed.Source || result.DuanjuID.String() != parsed.DuanjuID || result.Episodes == nil {
		return detailResult{}, errors.New("西饭返回的剧集与请求不一致")
	}
	return result, nil
}

func sortEpisodes(episodes []source.Episode) {
	for outer := 1; outer < len(episodes); outer++ {
		for inner := outer; inner > 0 && episodes[inner].Index < episodes[inner-1].Index; inner-- {
			episodes[inner], episodes[inner-1] = episodes[inner-1], episodes[inner]
		}
	}
}

// containerFor 判定封装形态（契约 Stream.Container）。
//
// 与 core/source/adapter 的同一判据一致：地址以 .m3u8 结尾即 HLS，否则按 MP4 处理。
func containerFor(address string) string {
	lowered := strings.ToLower(address)
	if index := strings.Index(lowered, "?"); index >= 0 {
		lowered = lowered[:index]
	}
	if strings.HasSuffix(lowered, ".m3u8") {
		return source.ContainerHLS
	}
	return source.ContainerMP4
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

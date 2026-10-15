// 本仓新增（M3 站源 provider）。
//
// 本文件是 C `refs/hongguo-drama/src/native/hema.js`（167 行）的 Go 逐函数转写
// （ARCHITECTURE §3.3 #24）。映射关系（JS → 本文件）：
//
//	const ORIGIN / ID       (3-4)     → DefaultOrigin / idPattern
//	plainText               (7-9)     → plainText
//	httpsUrl                (11-16)   → httpsURL
//	parseId                 (18-24)   → parseID
//	request                 (26-42)   → (*Provider).request
//	normalizeBooks          (44-53)   → normalizeSeries
//	pageData                (55-62)   → pageData（__NEXT_DATA__ 提取）
//	browseList              (64-100)  → Search（Keyword 为空）/ Categories
//	search                  (102-115) → Search（Keyword 非空）
//	detail                  (117-136) → (*Provider).detail
//	fetchEpisodeList        (138-149) → Episodes
//	fetchPlayUrlSingle      (151-165) → Resolve
//
// # 移植时保留的硬性行为（每一条都来自 C 的自检脚本 scripts/check-hema.cjs
// 与真实页面快照 scripts/fixtures/hema-browse/*.html）
//
//   - 站点身份头：UA / Accept / Content-Type / pname / Origin / Referer 六项；
//     且**不得**出现 cookie/authorization/session/token（自检脚本逐项断言）；
//   - `isCharge !== '0'` 是**严格字符串**比较：实测上游给的是字符串 '0'/'1'，
//     但 null/undefined/false/”/'false'/'00' 一律判为收费（自检脚本逐个覆盖过），
//     因此这里用 FlexString 复刻 JS 的宽松取值 + 严格比较；
//   - **收费集不得泄漏媒体地址**，播放地址优先 `chapterVideoVo.mp4720p`，
//     退回 `chapterVideoVo.mp4`，**绝不**回退到 `encryptUrl`；
//   - 播放地址必须 https 且不带用户名密码；http/file 一律拒绝；
//   - 每次 Resolve 都重新读详情判权限（「never reuse a cached free/paid decision」）；
//   - 身份非法（ID 不匹配正则 / 分集不属于该剧）必须在**发起网络请求之前**失败；
//   - 分集列表校验：chapterId 必须匹配 ID 正则、**不得重复**、且若带 bookId 必须与
//     请求的 bookId 一致 —— 任一条不满足即整源失败（防止串集）；
//   - 详情页/目录页都从 `__NEXT_DATA__` 脚本块取 JSON，解析失败即报错；
//   - 错误信息不得泄漏上游响应内容（自检脚本用「私密响应不得泄漏」断言过）。
//
// 与 JS 的接口差异同 xifan.go：JS 返回 `{success:false,error}` 对象，本仓按 §4.1
// 返回 error，文案保持一致。
package hema

import (
	"context"
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

// DefaultOrigin 是 hema.js:3 的站点常量。
const DefaultOrigin = "https://www.kuaikaw.cn"

const (
	categoryCacheTTL = 5 * time.Minute
	defaultUserAgent = "Mozilla/5.0"
	// searchPath 是 hema.js:107 的固定搜索端点（6007 是站点频道号）。
	searchPath = "/seo/video/6007"
)

// idPattern 对应 hema.js:4 的 `^\d{1,30}$`。
var idPattern = regexp.MustCompile(`^\d{1,30}$`)

// slugPattern 对应 hema.js:69 的 `^\d{1,30}(?:-\d{1,30}){0,15}$`。
var slugPattern = regexp.MustCompile(`^\d{1,30}(?:-\d{1,30}){0,15}$`)

// tagPattern 对应 plainText 的 `replace(/<[^>]*>/g, ”)`。
var tagPattern = regexp.MustCompile(`<[^>]*>`)

// nextDataPattern 对应 hema.js:56/120 的 `__NEXT_DATA__` 脚本块提取。
var nextDataPattern = regexp.MustCompile(`(?is)<script[^>]*id=["']__NEXT_DATA__["'][^>]*>(.*?)</script>`)

// ErrUnsupported / 各类稳定错误分类。
var (
	ErrUnsupported  = errors.New("该站源不支持此能力")
	ErrInvalidID    = errors.New("河马剧集编号无效")
	ErrLocked       = errors.New("该集当前锁定，请在河马官方平台确认可看范围")
	ErrNotAvailable = errors.New("河马未提供有效 HTTPS 播放地址")
	ErrUnavailable  = errors.New("河马来源暂不可用，请稍后重试")
	ErrTimeout      = errors.New("河马加载超时，请重试")
	ErrBadData      = errors.New("河马目录数据异常，请稍后重试")
	ErrMismatch     = errors.New("河马返回的剧集与请求不一致")
	ErrInvalidPage  = errors.New("河马题材或页码无效，请重新选择")
)

// Provider 是河马站源（无状态、可并发，§4.1）。
type Provider struct {
	// Origin 默认 DefaultOrigin；测试里指向 httptest。
	Origin string
	// HTTP 为 nil 时用默认客户端。
	HTTP *httpx.Client
	// Now 为 nil 时用 time.Now（分类缓存）。
	Now func() time.Time

	cachedCats []source.Category
	cachedAt   time.Time
	cacheMu    sync.Mutex
}

// New 造一个河马 provider（origin 为空时用 DefaultOrigin）。
func New(origin string) *Provider {
	if strings.TrimSpace(origin) == "" {
		origin = DefaultOrigin
	}
	return &Provider{Origin: strings.TrimRight(origin, "/")}
}

var _ source.Source = (*Provider)(nil)

// ID 实现 source.Source。
func (p *Provider) ID() source.ID { return source.Hema }

// Name 实现 source.Source。
func (p *Provider) Name() string { return "河马" }

// Caps 实现 source.Source：搜索、分类浏览、直链播放（同 xifan 的理由，不声明 HLS）。
func (p *Provider) Caps() source.Caps {
	return source.CapSearch | source.CapCategory | source.CapDirectPlay
}

func (p *Provider) client() *httpx.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	// 头与 hema.js:30-31 一致；pname/Origin/Referer 是站点要求的身份三件套。
	p.HTTP = httpx.New(p.Origin, map[string]string{
		"Accept":  "text/html",
		"pname":   hostOf(p.Origin),
		"Origin":  p.Origin,
		"Referer": p.Origin + "/",
	})
	p.HTTP.UserAgent = defaultUserAgent
	return p.HTTP
}

// hostOf 取 origin 的 host（pname 用站点域名，实测为 www.kuaikaw.cn）。
func hostOf(origin string) string {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	}
	return parsed.Host
}

func (p *Provider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// ── JS 侧纯函数 ────────────────────────────────────────────────────────────

// plainText 对应 hema.js:7-9。
func plainText(value string) string {
	return strings.TrimSpace(tagPattern.ReplaceAllString(value, ""))
}

// identity 是 parseId 的产物。
type identity struct {
	BookID    string
	ChapterID string
	SeriesID  string
}

// parseID 对应 hema.js:18-24：剧 ID 两段、分集 ID 三段，每段必须是纯数字。
func parseID(value string, episode bool) (identity, error) {
	parts := strings.Split(value, ":")
	want := 2
	if episode {
		want = 3
	}
	if len(parts) != want || parts[0] != "hema" {
		return identity{}, ErrInvalidID
	}
	for _, part := range parts[1:] {
		if !idPattern.MatchString(part) {
			return identity{}, ErrInvalidID
		}
	}
	out := identity{BookID: parts[1], SeriesID: "hema:" + parts[1]}
	if episode {
		out.ChapterID = parts[2]
	}
	return out, nil
}

// httpsURL 对应 hema.js:11-16。
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

// ── 取数 ──────────────────────────────────────────────────────────────────

// request 对应 hema.js:26-42：有 body 走 POST（Accept: application/json），
// 无 body 走 GET（Accept: text/html）。
func (p *Provider) request(ctx context.Context, apiPath string, payload any) ([]byte, error) {
	client := p.client()
	var (
		body []byte
		err  error
	)
	if payload != nil {
		body, err = client.Post(ctx, apiPath, payload)
	} else {
		body, err = client.Get(ctx, apiPath, nil)
	}
	if err != nil {
		if errors.Is(err, httpx.ErrTimeout) {
			return nil, ErrTimeout
		}
		return nil, ErrUnavailable
	}
	return body, nil
}

// pageData 对应 hema.js:55-62：从 HTML 里取出 __NEXT_DATA__ 的 props.pageProps。
func pageData(html []byte) (json.RawMessage, error) {
	match := nextDataPattern.FindSubmatch(html)
	if match == nil {
		return nil, ErrBadData
	}
	var document struct {
		Props struct {
			PageProps json.RawMessage `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(match[1], &document); err != nil {
		return nil, ErrBadData
	}
	trimmed := strings.TrimSpace(string(document.Props.PageProps))
	if !strings.HasPrefix(trimmed, "{") {
		return nil, ErrBadData
	}
	return document.Props.PageProps, nil
}

// ── 列表 ──────────────────────────────────────────────────────────────────

type bookItem struct {
	BookID          httpx.FlexString `json:"bookId"`
	BookName        httpx.FlexString `json:"bookName"`
	TotalChapterNum httpx.FlexInt    `json:"totalChapterNum"`
	CoverWap        httpx.FlexString `json:"coverWap"`
	BookTypeThree   []struct {
		Name httpx.FlexString `json:"name"`
	} `json:"bookTypeThree"`
}

type genreItem struct {
	TypeThreeID  httpx.FlexString `json:"typeThreeId"`
	Name         httpx.FlexString `json:"name"`
	TypeThreeIDs []httpx.FlexInt  `json:"typeThreeIds"`
}

// normalizeSeries 对应 hema.js:44-53。
func normalizeSeries(books []bookItem) []source.Series {
	seen := map[string]bool{}
	out := make([]source.Series, 0, len(books))
	for _, book := range books {
		if !idPattern.MatchString(book.BookID.String()) {
			continue
		}
		title := plainText(book.BookName.String())
		if title == "" {
			continue
		}
		seriesID := "hema:" + book.BookID.String()
		if seen[seriesID] {
			continue
		}
		seen[seriesID] = true
		out = append(out, source.Series{
			ID:       source.ID(seriesID),
			SourceID: source.Hema,
			Title:    title,
			Cover:    httpsURL(book.CoverWap.String()),
			Total:    book.TotalChapterNum.Int(),
			Tag:      bookTags(book),
		})
	}
	return out
}

// bookTags 对应 JS 的 `bookTypeThree.map(tag => plainText(tag?.name)).filter(Boolean).slice(0,4)`。
func bookTags(book bookItem) string {
	out := make([]string, 0, 4)
	for _, tag := range book.BookTypeThree {
		text := plainText(tag.Name.String())
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

// categories 对应 hema.js:73-85（含 5 分钟缓存）：
// 取 `/browse` 的页面数据，按 typeThreeIds 生成 slug（"-" 连接）。
func (p *Provider) categories(ctx context.Context, forceRefresh bool) ([]source.Category, error) {
	p.cacheMu.Lock()
	if !forceRefresh && len(p.cachedCats) > 0 && p.cachedAt.Add(categoryCacheTTL).After(p.now()) {
		cached := append([]source.Category(nil), p.cachedCats...)
		p.cacheMu.Unlock()
		return cached, nil
	}
	p.cacheMu.Unlock()

	body, err := p.request(ctx, "/browse", nil)
	if err != nil {
		return nil, err
	}
	raw, err := pageData(body)
	if err != nil {
		return nil, err
	}
	var directory struct {
		TypeThreeList []genreItem `json:"typeThreeList"`
	}
	if err := json.Unmarshal(raw, &directory); err != nil || directory.TypeThreeList == nil {
		return nil, errors.New("河马没有返回可用题材，请重试")
	}
	seen := map[string]bool{}
	var out []source.Category
	for _, item := range directory.TypeThreeList {
		if len(item.TypeThreeIDs) == 0 || len(item.TypeThreeIDs) > 16 {
			continue
		}
		parts := make([]string, 0, len(item.TypeThreeIDs))
		valid := true
		for _, id := range item.TypeThreeIDs {
			if !idPattern.MatchString(strconv.Itoa(id.Int())) {
				valid = false
				break
			}
			parts = append(parts, strconv.Itoa(id.Int()))
		}
		if !valid {
			continue
		}
		label := plainText(item.Name.String())
		if label == "" {
			continue
		}
		slug := strings.Join(parts, "-")
		// JS：`if (item.slug === '0' || seen.has(item.slug)) return false`
		if slug == "0" || seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, source.Category{ID: slug, Name: label})
	}
	if len(out) == 0 {
		return nil, errors.New("河马没有返回可用题材，请重试")
	}
	p.cacheMu.Lock()
	p.cachedCats, p.cachedAt = out, p.now()
	p.cacheMu.Unlock()
	return out, nil
}

// Search 实现 source.Source：Keyword 为空 → 目录浏览（browseList），
// 非空 → 站内搜索（POST /seo/video/6007）。
func (p *Provider) Search(ctx context.Context, q source.Query) (source.Page, error) {
	page := q.Page
	if page < 1 {
		page = 1
	}
	if keyword := strings.TrimSpace(q.Keyword); keyword != "" {
		return p.search(ctx, keyword, page)
	}
	return p.browse(ctx, q.Filters["genre"], page)
}

// search 对应 hema.js:102-115。
func (p *Provider) search(ctx context.Context, keyword string, page int) (source.Page, error) {
	body, err := p.request(ctx, searchPath, map[string]any{
		"sourceType": 1, "keyword": keyword, "index": page})
	if err != nil {
		return source.Page{}, err
	}
	var envelope struct {
		RetCode *int `json:"retCode"`
		Data    *struct {
			IsMore    httpx.FlexInt `json:"isMore"`
			TotalSize httpx.FlexInt `json:"totalSize"`
			BookList  []bookItem    `json:"bookList"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return source.Page{}, ErrBadData
	}
	if envelope.RetCode == nil || *envelope.RetCode != 0 || envelope.Data == nil || envelope.Data.BookList == nil {
		return source.Page{}, errors.New("河马搜索数据异常，请稍后重试")
	}
	items := normalizeSeries(envelope.Data.BookList)
	// JS：`hasMore = data.isMore === 1 && results.length > 0`
	return source.Page{Items: items, Total: envelope.Data.TotalSize.Int(),
		HasMore: envelope.Data.IsMore.Int() == 1 && len(items) > 0}, nil
}

// browse 对应 hema.js:64-100。
func (p *Provider) browse(ctx context.Context, genre string, page int) (source.Page, error) {
	slug := "0"
	if strings.TrimSpace(genre) != "" {
		slug = strings.TrimSpace(genre)
	}
	// JS：`!Number.isSafeInteger(pg) || pg < 1 || !slugPattern.test(slug)`（pg 已在调用方归一）
	if page < 1 || !slugPattern.MatchString(slug) {
		return source.Page{}, ErrInvalidPage
	}
	categories, err := p.categories(ctx, false)
	if err != nil {
		return source.Page{}, err
	}
	if slug != "0" {
		found := false
		for _, item := range categories {
			if item.ID == slug {
				found = true
				break
			}
		}
		if !found {
			return source.Page{}, errors.New("河马题材无效，请重新选择")
		}
	}
	// JS：`slug === '0' && pg === 1 ? '/browse' : `/browse/${slug}${pg > 1 ? `/${pg}` : ''}``
	apiPath := "/browse"
	if slug != "0" || page > 1 {
		apiPath = "/browse/" + slug
		if page > 1 {
			apiPath += "/" + strconv.Itoa(page)
		}
	}
	body, err := p.request(ctx, apiPath, nil)
	if err != nil {
		return source.Page{}, err
	}
	raw, err := pageData(body)
	if err != nil {
		return source.Page{}, err
	}
	var directory struct {
		Page        httpx.FlexInt    `json:"page"`
		TypeThreeID httpx.FlexString `json:"typeThreeId"`
		BookList    []bookItem       `json:"bookList"`
	}
	if err := json.Unmarshal(raw, &directory); err != nil || directory.BookList == nil {
		return source.Page{}, errors.New("河马返回的目录与请求不一致，请重试")
	}
	// JS 的严格一致性校验（"河马返回的目录与请求不一致"）：
	// 注意 typeThreeId 为 ""（全部）时按 '0' 处理。
	presented := directory.TypeThreeID.String()
	if presented == "" {
		presented = "0"
	}
	if directory.Page.Int() != page || presented != slug {
		return source.Page{}, errors.New("河马返回的目录与请求不一致，请重试")
	}
	items := normalizeSeries(directory.BookList)
	// JS：用「下一页链接」是否存在于 HTML 里判 hasMore（站点总页数未经验证）。
	nextLink := regexp.MustCompile(`<a\b[^>]*\shref=["']/browse/` + regexp.QuoteMeta(slug) + `/` +
		strconv.Itoa(page+1) + `["']`)
	return source.Page{Items: items, HasMore: len(items) > 0 && nextLink.Match(body)}, nil
}

// Categories 实现 source.Source。
func (p *Provider) Categories(ctx context.Context) ([]source.Category, error) {
	return p.categories(ctx, false)
}

// Rank 实现 source.Source：河马没有榜单接口。
func (p *Provider) Rank(context.Context, string, int) (source.Page, error) {
	return source.Page{}, fmt.Errorf("%w: 河马没有榜单", ErrUnsupported)
}

// Danmaku 实现 source.Source：河马不提供弹幕。
func (p *Provider) Danmaku(context.Context, string, int64, int64) (source.DanmakuTrack, error) {
	return source.DanmakuTrack{}, fmt.Errorf("%w: 河马没有弹幕", ErrUnsupported)
}

// ── 详情与播放 ────────────────────────────────────────────────────────────

type chapterItem struct {
	ChapterID    httpx.FlexString `json:"chapterId"`
	ChapterName  httpx.FlexString `json:"chapterName"`
	ChapterIndex httpx.FlexInt    `json:"chapterIndex"`
	// IsCharge 用 FlexString 复刻 JS 的宽松取值 + 严格比较 `!== '0'`。
	IsCharge httpx.FlexString `json:"isCharge"`
	BookID   httpx.FlexString `json:"bookId"`
	Video    struct {
		MP4720P    string `json:"mp4720p"`
		MP4        string `json:"mp4"`
		EncryptURL string `json:"encryptUrl"`
	} `json:"chapterVideoVo"`
}

type detailResult struct {
	Book struct {
		BookID          httpx.FlexString `json:"bookId"`
		BookName        httpx.FlexString `json:"bookName"`
		TotalChapterNum httpx.FlexInt    `json:"totalChapterNum"`
		CoverWap        httpx.FlexString `json:"coverWap"`
	} `json:"bookInfoVo"`
	Chapters []chapterItem `json:"chapterList"`
}

// detail 对应 hema.js:117-136（含逐集校验：ID 合法、不重复、bookId 一致）。
func (p *Provider) detail(ctx context.Context, parsed identity) (detailResult, error) {
	body, err := p.request(ctx, "/drama/"+parsed.BookID, nil)
	if err != nil {
		return detailResult{}, err
	}
	raw, err := pageData(body)
	if err != nil {
		return detailResult{}, errors.New("河马详情数据异常，请稍后重试")
	}
	var result detailResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return detailResult{}, errors.New("河马详情数据异常，请稍后重试")
	}
	if result.Book.BookID.String() != parsed.BookID || result.Chapters == nil {
		return detailResult{}, ErrMismatch
	}
	seen := map[string]bool{}
	for _, chapter := range result.Chapters {
		id := chapter.ChapterID.String()
		if !idPattern.MatchString(id) || seen[id] {
			return detailResult{}, errors.New("河马分集数据异常，请稍后重试")
		}
		if chapter.BookID.String() != "" && chapter.BookID.String() != parsed.BookID {
			return detailResult{}, errors.New("河马分集数据异常，请稍后重试")
		}
		seen[id] = true
	}
	return result, nil
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
	total := result.Book.TotalChapterNum.Int()
	if total <= 0 {
		total = len(result.Chapters)
	}
	return source.Series{
		ID:       source.ID(parsed.SeriesID),
		SourceID: source.Hema,
		Title:    firstNonEmpty(plainText(result.Book.BookName.String()), "短剧"),
		Cover:    httpsURL(result.Book.CoverWap.String()),
		Total:    total,
	}, nil
}

// Episodes 实现 source.Source：只给集号与标题，不含锁定状态与媒体地址。
func (p *Provider) Episodes(ctx context.Context, seriesID string) ([]source.Episode, error) {
	parsed, err := parseID(seriesID, false)
	if err != nil {
		return nil, err
	}
	result, err := p.detail(ctx, parsed)
	if err != nil {
		return nil, err
	}
	episodes := make([]source.Episode, 0, len(result.Chapters))
	for position, chapter := range result.Chapters {
		// JS：`vid_index: Number(episode.chapterIndex) || index + 1`
		index := chapter.ChapterIndex.Int()
		if index == 0 {
			index = position + 1
		}
		episodes = append(episodes, source.Episode{
			SeriesID:  source.ID(parsed.SeriesID),
			EpisodeID: parsed.SeriesID + ":" + chapter.ChapterID.String(),
			Index:     index,
			Title:     plainText(chapter.ChapterName.String()),
		})
	}
	sortEpisodes(episodes)
	if len(episodes) == 0 {
		return nil, errors.New("河马分集列表为空")
	}
	return episodes, nil
}

// Resolve 实现 source.Source（对应 hema.js:151-165）。
func (p *Provider) Resolve(ctx context.Context, episodeID string, quality source.Quality) (source.Stream, error) {
	parsed, err := parseID(episodeID, true)
	if err != nil {
		return source.Stream{}, err
	}
	result, err := p.detail(ctx, parsed)
	if err != nil {
		return source.Stream{}, err
	}
	var chapter *chapterItem
	for index := range result.Chapters {
		if result.Chapters[index].ChapterID.String() == parsed.ChapterID {
			chapter = &result.Chapters[index]
			break
		}
	}
	if chapter == nil {
		return source.Stream{}, errors.New("河马未找到该分集")
	}
	// JS：`episode.isCharge !== '0'` → 锁定（严格字符串比较）。
	if chapter.IsCharge.String() != "0" {
		return source.Stream{}, fmt.Errorf("%w（第 %d 集）", ErrLocked, chapter.ChapterIndex.Int())
	}
	// 优先 720p，退回 mp4；**不回退** encryptUrl（自检脚本专门断言过）。
	address := httpsURL(chapter.Video.MP4720P)
	qualityLabel := source.Quality720p
	if address == "" {
		address = httpsURL(chapter.Video.MP4)
		qualityLabel = source.QualityAuto
	}
	if address == "" {
		return source.Stream{}, ErrNotAvailable
	}
	return source.Stream{
		URL:           address,
		Encrypted:     false,
		Headers:       nil,
		Quality:       string(qualityLabel),
		Container:     containerFor(address),
		NeedsSanitize: false,
	}, nil
}

func sortEpisodes(episodes []source.Episode) {
	for outer := 1; outer < len(episodes); outer++ {
		for inner := outer; inner > 0 && episodes[inner].Index < episodes[inner-1].Index; inner-- {
			episodes[inner], episodes[inner-1] = episodes[inner-1], episodes[inner]
		}
	}
}

// containerFor 判定封装形态（与 adapter 的判据一致）。
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

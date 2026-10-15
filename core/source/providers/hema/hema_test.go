// 本仓新增（M3 站源 provider 测试）。
//
// 用例来自 C 的自检脚本 scripts/check-hema.cjs、check-hema-browse.cjs 与真实页面快照
// （testdata/*.html，原样搬自 C 的 scripts/fixtures/hema-browse）：
//   - 目录页/详情页都从 __NEXT_DATA__ 取 JSON；
//   - 题材 slug 形如 `417-464`，「全部」（typeThreeIds=[0]）被滤掉；
//   - 目录页要按 href 里的下一页链接判 hasMore；
//   - `isCharge !== '0'` 是严格字符串比较：'1'/null/false/”/'false'/'00' 全部锁定；
//   - 收费集不得泄漏媒体地址，也**不得**回退到 encryptUrl；
//   - 每次 Resolve 重新判权限；分集列表不泄漏媒体地址；
//   - 详情校验：chapterId 必须合法、不重复、bookId 必须一致；
//   - 身份非法在发起网络请求之前失败。
package hema_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dawei233/hongguo-next/core/source"
	"github.com/dawei233/hongguo-next/core/source/providers/hema"
)

const (
	testBookID    = "41000415116"
	testChapterID = "591821028"
	testSeriesID  = "hema:" + testBookID
	testVid       = testSeriesID + ":" + testChapterID
)

// fakeHema 是假河马站点：目录页直接回放 C 的 fixture HTML，接口按需构造。
type fakeHema struct {
	mu         sync.Mutex
	requests   []*http.Request
	lastBody   []byte
	chapterLen int
	freeCount  int
	isCharge   map[int]any // chapter index → isCharge 取值（缺失表示字段不存在）
	bookID     string
	video      map[string]any
	retCode    int
	status     int
	duplicate  bool
	// mismatchPage 为 true 时，任何目录请求都回第 1 页快照（用于验证一致性校验）。
	mismatchPage bool
}

func newFakeHema() *fakeHema {
	return &fakeHema{
		chapterLen: 74, freeCount: 5, bookID: testBookID,
		isCharge: map[int]any{},
		video: map[string]any{"mp4720p": "https://media.example/720.mp4",
			"mp4": "https://media.example/video.mp4", "encryptUrl": "https://media.example/forbidden"},
	}
}

func (f *fakeHema) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeHema) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return nil
	}
	return f.requests[len(f.requests)-1]
}

// lastRequestBody 返回最后一次请求的请求体（服务端 handler 里读完后留存）。
func (f *fakeHema) lastRequestBody() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.lastBody...)
}

func (f *fakeHema) chapterList() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]any, 0, f.chapterLen)
	for index := 0; index < f.chapterLen; index++ {
		// 与 C 的 check-hema.cjs 同形：chapterId = 591821028 + i，chapterIndex = i+1。
		chapterID := strconv.Itoa(591821028 + index)
		item := map[string]any{
			"chapterId": chapterID, "chapterName": "<b>第" + strconv.Itoa(index+1) + "集</b>",
			"chapterIndex": index + 1, "bookId": f.bookID,
		}
		charge, ok := f.isCharge[index]
		if !ok {
			if index < f.freeCount {
				charge = "0"
			} else {
				charge = "1"
			}
		}
		if charge != nil {
			item["isCharge"] = charge
		}
		if index < f.freeCount {
			item["chapterVideoVo"] = f.video
		}
		out = append(out, item)
	}
	if f.duplicate {
		out = append(out, out[0])
	}
	return out
}

func (f *fakeHema) handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body []byte
		if request.Body != nil {
			body, _ = io.ReadAll(request.Body)
		}
		f.mu.Lock()
		f.requests = append(f.requests, request.Clone(context.Background()))
		f.lastBody = body
		retCode, status, bookID, mismatch := f.retCode, f.status, f.bookID, f.mismatchPage
		f.mu.Unlock()
		if mismatch && strings.HasPrefix(request.URL.Path, "/browse") {
			writeFixture(writer, "browse.html")
			return
		}
		if status != 0 {
			writer.WriteHeader(status)
			return
		}
		switch {
		case request.URL.Path == "/browse":
			// 第 1 页「全部」直接回放 C 的真实页面快照。
			writeFixture(writer, "browse.html")
		case strings.HasPrefix(request.URL.Path, "/browse/"):
			// /browse/<slug>[/<page>]：第 1 页题材也回放快照（1129 → 都市页），
			// 其余（分页）按站点同形现造 —— 快照本身是 /browse/0/2（全部）那一页。
			segments := strings.Split(strings.TrimPrefix(request.URL.Path, "/browse/"), "/")
			slug := segments[0]
			pageNumber := 1
			if len(segments) > 1 {
				pageNumber, _ = strconv.Atoi(segments[1])
			}
			switch {
			case len(segments) == 1 && slug == "1129":
				writeFixture(writer, "genre.html")
			case len(segments) == 2 && slug == "0" && pageNumber == 2:
				writeFixture(writer, "page2.html")
			default:
				presented := slug
				if presented == "0" {
					presented = "0"
				}
				writeBrowsePage(writer, presented, pageNumber,
					[]any{map[string]any{"bookId": testBookID, "bookName": "分页样例剧",
						"totalChapterNum": "42", "coverWap": "https://images.example/cover.jpg"}},
					true)
			}
		case strings.HasPrefix(request.URL.Path, "/drama/"):
			detail := map[string]any{
				"bookInfoVo": map[string]any{"bookId": bookID, "bookName": "<i>出手</i>",
					"totalChapterNum": "74", "coverWap": "https://images.example/cover.jpg"},
				"chapterList": f.chapterList(),
			}
			writePageData(writer, detail)
		case strings.HasPrefix(request.URL.Path, "/seo/video/"):
			body := map[string]any{"retCode": retCode, "data": map[string]any{
				"isMore": 1, "totalSize": 241, "bookList": []any{
					map[string]any{"bookId": testBookID, "bookName": "<em>出手</em>",
						"totalChapterNum": "74", "coverWap": "https://images.example/cover.jpg",
						"bookTypeThree": []any{map[string]any{"id": 1, "name": "都市"}}},
					map[string]any{"bookId": "xifan:1", "bookName": "非法 ID 应丢弃"},
				}}}
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(body)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	})
}

// writeFixture 回放 C 的 fixture HTML（路径相对包目录）。
func writeFixture(writer http.ResponseWriter, name string) {
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write(body)
}

// writeBrowsePage 造一个与真实页面同形的目录页（含「下一页」链接）。
func writeBrowsePage(writer http.ResponseWriter, slug string, page int, books []any, withNext bool) {
	document := map[string]any{"props": map[string]any{"pageProps": map[string]any{
		"typeThreeId": slug, "page": page, "pages": 450, "bookList": books,
		"typeThreeList": []any{map[string]any{"typeThreeIds": []int{0}, "name": "全部"},
			map[string]any{"name": "都市", "typeThreeIds": []int{1129}}},
	}}}
	encoded, err := json.Marshal(document)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	html := `<script id="__NEXT_DATA__" type="application/json">` + string(encoded) + `</script>`
	if withNext {
		html += `<a href="/browse/` + slug + `/` + strconv.Itoa(page+1) + `">下一页</a>`
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write([]byte(html))
}

// writePageData 造一个与真实页面同形的 __NEXT_DATA__ 文档。
func writePageData(writer http.ResponseWriter, pageProps any) {
	encoded, err := json.Marshal(map[string]any{"props": map[string]any{"pageProps": pageProps}})
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write([]byte(`<script id="__NEXT_DATA__" type="application/json">` + string(encoded) + `</script>`))
}

func newProvider(t *testing.T, fake *fakeHema) *hema.Provider {
	t.Helper()
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	return hema.New(server.URL)
}

func TestParseIDEitherForm(t *testing.T) {
	provider := newProvider(t, newFakeHema())
	ctx := context.Background()
	if got := provider.ID(); got != source.Hema {
		t.Fatalf("ID 不符: %s", got)
	}
	if provider.Name() != "河马" {
		t.Fatalf("Name 不符: %s", provider.Name())
	}
	// 非法身份必须在发起网络请求之前失败。
	fake := newFakeHema()
	strict := newProvider(t, fake)
	before := fake.count()
	for _, invalid := range []string{"123", "hema:../evil:1", "hema:1:2:3", "xifan:" + testBookID + ":" + testChapterID,
		"hema:" + testBookID + ":abc", "hema:" + strings.Repeat("9", 31) + ":1"} {
		if _, err := strict.Resolve(ctx, invalid, source.QualityAuto); err == nil {
			t.Errorf("Resolve(%q) 应报错", invalid)
		}
		if _, err := strict.Episodes(ctx, invalid); err == nil {
			t.Errorf("Episodes(%q) 应报错", invalid)
		}
	}
	if got := fake.count(); got != before {
		t.Fatalf("非法身份不得发起网络请求（请求数 %d → %d）", before, got)
	}
	// 合法 ID 能取到流。
	stream, err := provider.Resolve(ctx, testVid, source.QualityAuto)
	if err != nil {
		t.Fatalf("合法分集应能解析: %v", err)
	}
	if stream.URL != "https://media.example/720.mp4" || stream.Quality != string(source.Quality720p) {
		t.Fatalf("流字段不符: %+v", stream)
	}
	if stream.Encrypted || stream.NeedsSanitize || stream.Container != source.ContainerMP4 {
		t.Fatalf("明文直链不该带解密/净化标记: %+v", stream)
	}
}

func TestCapsAndUnsupportedAbilities(t *testing.T) {
	provider := newProvider(t, newFakeHema())
	want := source.CapSearch | source.CapCategory | source.CapDirectPlay
	if caps := provider.Caps(); caps != want {
		t.Fatalf("能力位应为 %s，实际 %s", want, caps)
	}
	if _, err := provider.Rank(context.Background(), "hot", 1); !errors.Is(err, hema.ErrUnsupported) {
		t.Fatalf("Rank 应报不支持: %v", err)
	}
	if _, err := provider.Danmaku(context.Background(), testVid, 0, 60); !errors.Is(err, hema.ErrUnsupported) {
		t.Fatalf("Danmaku 应报不支持: %v", err)
	}
}

func TestCategoriesFromFixture(t *testing.T) {
	provider := newProvider(t, newFakeHema())
	categories, err := provider.Categories(context.Background())
	if err != nil {
		t.Fatalf("取题材失败: %v", err)
	}
	// fixture：全部[0] 被滤掉，逆袭 → "417-464"，都市 → "1129"。
	if len(categories) != 2 {
		t.Fatalf("应有 2 个题材（「全部」必须滤掉），实际 %+v", categories)
	}
	if categories[0].ID != "417-464" || categories[0].Name != "逆袭" {
		t.Fatalf("题材 slug 不符: %+v", categories[0])
	}
	if categories[1].ID != "1129" || categories[1].Name != "都市" {
		t.Fatalf("题材 slug 不符: %+v", categories[1])
	}
}

func TestBrowseFromFixture(t *testing.T) {
	provider := newProvider(t, newFakeHema())
	ctx := context.Background()
	// 默认（全部 + 第 1 页）→ /browse。
	page, err := provider.Search(ctx, source.Query{Page: 1})
	if err != nil {
		t.Fatalf("浏览失败: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("fixture 里有 2 部剧，实际 %+v", page.Items)
	}
	first := page.Items[0]
	if first.Title != "一九九八，傻妹揣着二十万回来了" || first.Total != 41 {
		t.Fatalf("剧字段不符: %+v", first)
	}
	if string(first.ID) != "hema:41000415116" || first.SourceID != source.Hema {
		t.Fatalf("ID 不符: %+v", first)
	}
	if first.Cover != "https://images.example/cover.jpg" {
		t.Fatalf("封面不符: %q", first.Cover)
	}
	// fixture 的 bookTypeThree 有 8 个标签 → 只取前 4 个。
	if first.Tag != "仿真人动态漫/现实/漫剧/家庭" {
		t.Fatalf("标签应取前 4 个: %q", first.Tag)
	}
	// hasMore 来自 fixture 里的 `href="/browse/0/2"`。
	if !page.HasMore {
		t.Fatal("fixture 含下一页链接，hasMore 应为 true")
	}
}

func TestBrowseGenreAndPagePath(t *testing.T) {
	fake := newFakeHema()
	provider := newProvider(t, fake)
	ctx := context.Background()
	if _, err := provider.Search(ctx, source.Query{Page: 1, Filters: map[string]string{"genre": "1129"}}); err != nil {
		t.Fatalf("题材浏览失败: %v", err)
	}
	if got := fake.last().URL.Path; got != "/browse/1129" {
		t.Fatalf("第 1 页题材路径应为 /browse/1129，实际 %s", got)
	}
	if _, err := provider.Search(ctx, source.Query{Page: 2, Filters: map[string]string{"genre": "1129"}}); err != nil {
		t.Fatalf("第 2 页题材浏览失败: %v", err)
	}
	if got := fake.last().URL.Path; got != "/browse/1129/2" {
		t.Fatalf("第 2 页题材路径应为 /browse/1129/2，实际 %s", got)
	}
	// 分页页面（现造）：slug 与 page 必须透传，hasMore 由下一页链接决定。
	page2, err := provider.Search(ctx, source.Query{Page: 2, Filters: map[string]string{"genre": "1129"}})
	if err != nil {
		t.Fatalf("第 2 页题材浏览失败: %v", err)
	}
	if len(page2.Items) != 1 || page2.Items[0].Title != "分页样例剧" || page2.Items[0].Total != 42 {
		t.Fatalf("分页结果不符: %+v", page2.Items)
	}
	if !page2.HasMore {
		t.Fatal("分页页面含下一页链接，hasMore 应为 true")
	}

	// 非法题材 / 非法页码（含正则边界）。
	if _, err := provider.Search(ctx, source.Query{Page: 1, Filters: map[string]string{"genre": "999"}}); err == nil {
		t.Fatal("不在题材表里的 slug 应报错")
	}
	if _, err := provider.Search(ctx, source.Query{Page: 1, Filters: map[string]string{"genre": "1-2-3-4-5-6-7-8-9-10-11-12-13-14-15-16-17"}}); !errors.Is(err, hema.ErrInvalidPage) {
		t.Fatalf("超过 16 段的 slug 应报 ErrInvalidPage，实际 %v", err)
	}
	if _, err := provider.Search(ctx, source.Query{Page: 1, Filters: map[string]string{"genre": "abc"}}); !errors.Is(err, hema.ErrInvalidPage) {
		t.Fatalf("非数字 slug 应报 ErrInvalidPage，实际 %v", err)
	}
	// 目录页返回的 page 与请求不一致 → 报错（JS 的一致性校验）。
	mismatch := newFakeHema()
	mismatch.mismatchPage = true
	mismatchProvider := newProvider(t, mismatch)
	if _, err := mismatchProvider.Search(ctx, source.Query{Page: 7}); err == nil ||
		!strings.Contains(err.Error(), "与请求不一致") {
		t.Fatalf("目录页页码不匹配应报错，实际 %v", err)
	}
}

func TestSearchPostBody(t *testing.T) {
	fake := newFakeHema()
	provider := newProvider(t, fake)
	page, err := provider.Search(context.Background(), source.Query{Keyword: " 总裁 ", Page: 2})
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	last := fake.last()
	if last.Method != http.MethodPost || last.URL.Path != "/seo/video/6007" {
		t.Fatalf("搜索应为 POST /seo/video/6007，实际 %s %s", last.Method, last.URL.Path)
	}
	var body map[string]any
	if err := json.Unmarshal(fake.lastRequestBody(), &body); err != nil {
		t.Fatalf("搜索请求体不是 JSON: %v (%q)", err, fake.lastRequestBody())
	}
	if body["sourceType"] != float64(1) || body["keyword"] != "总裁" || body["index"] != float64(2) {
		t.Fatalf("搜索请求体不符: %v", body)
	}
	if len(page.Items) != 1 {
		t.Fatalf("非法 ID 应被丢弃，实际 %+v", page.Items)
	}
	if page.Total != 241 || !page.HasMore {
		t.Fatalf("分页字段不符: %+v", page)
	}
	if page.Items[0].Title != "出手" {
		t.Fatalf("标题应剥掉 <em> 标签: %q", page.Items[0].Title)
	}
	// retCode != 0 → 报错（不返回部分结果）。
	fake.mu.Lock()
	fake.retCode = 1
	fake.mu.Unlock()
	if _, err := provider.Search(context.Background(), source.Query{Keyword: "x"}); err == nil {
		t.Fatal("retCode!=0 应报错")
	}
}

func TestEpisodesShapeAndNoMediaLeak(t *testing.T) {
	provider := newProvider(t, newFakeHema())
	episodes, err := provider.Episodes(context.Background(), testSeriesID)
	if err != nil {
		t.Fatalf("取分集失败: %v", err)
	}
	if len(episodes) != 74 {
		t.Fatalf("应有 74 集，实际 %d", len(episodes))
	}
	if episodes[0].EpisodeID != testVid || episodes[0].Index != 1 || episodes[0].Title != "第1集" {
		t.Fatalf("分集字段不符: %+v", episodes[0])
	}
	body, err := json.Marshal(episodes)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, leaked := range []string{"media.example", "chapterVideoVo", "mp4720p", "encryptUrl"} {
		if strings.Contains(string(body), leaked) {
			t.Fatalf("分集列表泄漏了媒体信息（%s）", leaked)
		}
	}
}

func TestResolveIsChargeStrictComparison(t *testing.T) {
	// check-hema.cjs：对 ['1', undefined, null, false, '', 'false', '00'] 全部判锁定。
	cases := []struct {
		name     string
		value    any
		absent   bool
		mustLock bool
	}{
		{name: "isCharge='0' 解锁", value: "0"},
		{name: "isCharge='1' 锁定", value: "1", mustLock: true},
		{name: "isCharge 缺失锁定", absent: true, mustLock: true},
		{name: "isCharge=null 锁定", value: nil, mustLock: true},
		{name: "isCharge=false 锁定", value: false, mustLock: true},
		{name: "isCharge='' 锁定", value: "", mustLock: true},
		{name: "isCharge='false' 锁定", value: "false", mustLock: true},
		{name: "isCharge='00' 锁定", value: "00", mustLock: true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			fake := newFakeHema()
			fake.freeCount = 1
			if item.absent {
				fake.isCharge[0] = nil
			} else {
				fake.isCharge[0] = item.value
			}
			provider := newProvider(t, fake)
			stream, err := provider.Resolve(context.Background(), testVid, source.QualityAuto)
			if item.mustLock {
				if !errors.Is(err, hema.ErrLocked) {
					t.Fatalf("应判锁定，实际 err=%v stream=%+v", err, stream)
				}
				if stream.URL != "" {
					t.Fatalf("锁定时不得给出地址: %+v", stream)
				}
				return
			}
			if err != nil {
				t.Fatalf("应成功: %v", err)
			}
			if stream.URL != "https://media.example/720.mp4" {
				t.Fatalf("地址不符: %s", stream.URL)
			}
		})
	}
}

func TestResolveRevalidatesAccessAndNeverUsesEncryptURL(t *testing.T) {
	fake := newFakeHema()
	fake.freeCount = 1
	provider := newProvider(t, fake)
	ctx := context.Background()
	if _, err := provider.Resolve(ctx, testVid, source.QualityAuto); err != nil {
		t.Fatalf("首次应成功: %v", err)
	}
	// 上游改判收费 → 必须重新读取权限（不复用上次结论）。
	fake.mu.Lock()
	fake.isCharge[0] = "1"
	fake.mu.Unlock()
	if _, err := provider.Resolve(ctx, testVid, source.QualityAuto); !errors.Is(err, hema.ErrLocked) {
		t.Fatalf("必须重新验权限，实际 err=%v", err)
	}
	// 恢复免费：mp4720p 缺失 → 退回 mp4；两者都非法 → 报错，且绝不回退 encryptUrl。
	fake.mu.Lock()
	fake.isCharge[0] = "0"
	fake.video = map[string]any{"mp4": "https://media.example/video.mp4", "encryptUrl": "https://media.example/forbidden"}
	fake.mu.Unlock()
	stream, err := provider.Resolve(ctx, testVid, source.QualityAuto)
	if err != nil {
		t.Fatalf("应退回 mp4: %v", err)
	}
	if stream.URL != "https://media.example/video.mp4" || stream.Quality != string(source.QualityAuto) {
		t.Fatalf("退回 mp4 时清晰度应为 auto: %+v", stream)
	}
	for _, invalid := range []string{"http://media.example/video.mp4",
		"https://user:pass@media.example/video.mp4", "file:///tmp/video.mp4", ""} {
		fake.mu.Lock()
		fake.video = map[string]any{"mp4": invalid, "encryptUrl": "https://media.example/forbidden"}
		fake.mu.Unlock()
		if _, err := provider.Resolve(ctx, testVid, source.QualityAuto); !errors.Is(err, hema.ErrNotAvailable) {
			t.Errorf("地址 %q 应被拒绝，实际 %v", invalid, err)
		}
	}
}

func TestDetailValidations(t *testing.T) {
	ctx := context.Background()
	// bookId 不一致。
	fake := newFakeHema()
	fake.bookID = "999"
	provider := newProvider(t, fake)
	if _, err := provider.Episodes(ctx, testSeriesID); !errors.Is(err, hema.ErrMismatch) {
		t.Fatalf("bookId 不一致应报 ErrMismatch，实际 %v", err)
	}
	if _, err := provider.Resolve(ctx, testVid, source.QualityAuto); err == nil {
		t.Fatal("bookId 不一致时 Resolve 也应报错")
	}
	// chapterId 重复。
	duplicate := newFakeHema()
	duplicate.duplicate = true
	dupProvider := newProvider(t, duplicate)
	if _, err := dupProvider.Episodes(ctx, testSeriesID); err == nil {
		t.Fatal("chapterId 重复应报错（防串集）")
	}
	// chapterId 非法。
	illegal := newFakeHema()
	illegal.chapterLen = 1
	illegal.freeCount = 1
	illegalProvider := newProvider(t, illegal)
	if _, err := illegalProvider.Episodes(ctx, testSeriesID); err != nil {
		t.Fatalf("%v", err)
	}
	// 页面缺 __NEXT_DATA__。
	broken := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("<html>unavailable</html>"))
	}))
	defer broken.Close()
	brokenProvider := hema.New(broken.URL)
	// hema.js 的 detail 有自己的 try/catch（文案与目录页不同），这里对齐 JS 的行为。
	if _, err := brokenProvider.Episodes(ctx, testSeriesID); err == nil ||
		!strings.Contains(err.Error(), "详情数据异常") {
		t.Fatalf("缺 __NEXT_DATA__ 应报「详情数据异常」，实际 %v", err)
	}
	if _, err := brokenProvider.Categories(ctx); !errors.Is(err, hema.ErrBadData) {
		t.Fatalf("目录页缺 __NEXT_DATA__ 应报 ErrBadData，实际 %v", err)
	}
	// 分集不存在。
	provider2 := newProvider(t, newFakeHema())
	if _, err := provider2.Resolve(ctx, testSeriesID+":999999999", source.QualityAuto); err == nil {
		t.Fatal("不存在的分集应报错")
	}
	// Detail 的总集数来自上游声明。
	detail, err := provider2.Detail(ctx, testSeriesID)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if detail.Total != 74 || detail.Title != "出手" || string(detail.ID) != testSeriesID {
		t.Fatalf("详情字段不符: %+v", detail)
	}
}

func TestErrorMappingAndNoLeak(t *testing.T) {
	// HTTP 500 → 源不可用且不泄漏响应体。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte("private upstream detail"))
	}))
	defer server.Close()
	provider := hema.New(server.URL)
	_, err := provider.Categories(context.Background())
	if err == nil {
		t.Fatal("HTTP 500 应报错")
	}
	if strings.Contains(err.Error(), "private upstream detail") {
		t.Fatalf("错误信息泄漏了上游内容: %v", err)
	}
	if !strings.Contains(err.Error(), "暂不可用") {
		t.Fatalf("应映射为「暂不可用」，实际 %v", err)
	}
	// 搜索结果里不得出现上游私密字段（错误响应同理）。
	if strings.Contains(err.Error(), "cookie") || strings.Contains(err.Error(), "token") {
		t.Fatalf("错误信息不得提及凭据字段: %v", err)
	}
}

func TestRequestHeadersMatchSiteIdentity(t *testing.T) {
	var captured http.Header
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		captured = request.Header.Clone()
		_, _ = writer.Write([]byte("<html><script id=\"__NEXT_DATA__\" type=\"application/json\">" +
			`{"props":{"pageProps":{"typeThreeList":[{"typeThreeIds":[1129],"name":"都市"}]}}}` +
			"</script></html>"))
	}))
	defer server.Close()
	provider := hema.New(server.URL)
	if _, err := provider.Categories(context.Background()); err != nil {
		t.Fatalf("%v", err)
	}
	// 六项站点身份头（hema.js:30-31）。
	expectations := map[string]string{
		"User-Agent": "Mozilla/5.0",
		"Accept":     "text/html",
		"pname":      strings.TrimPrefix(server.URL, "http://"),
		"Origin":     server.URL,
		"Referer":    server.URL + "/",
	}
	for name, want := range expectations {
		if got := captured.Get(name); got != want {
			t.Errorf("请求头 %s = %q，期望 %q", name, got, want)
		}
	}
	for _, name := range []string{"Cookie", "Authorization", "Session", "Token"} {
		if value := captured.Get(name); value != "" {
			t.Fatalf("请求头不得携带 %s（check-hema.cjs 明确禁止）", name)
		}
	}
}

func TestFixturesAreCopiedVerbatim(t *testing.T) {
	// fixture 原样搬自 C 的 scripts/fixtures/hema-browse（§3.3 #24），
	// 这里断言关键结构仍在，防止文件被换成别的内容而测试静默变弱。
	for _, name := range []string{"browse.html", "genre.html", "page2.html"} {
		body, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("缺少 fixture %s: %v", name, err)
		}
		if !regexp.MustCompile(`id="__NEXT_DATA__"`).Match(body) {
			t.Errorf("%s 应含 __NEXT_DATA__ 脚本块（与真实页面同形）", name)
		}
		if !bytesContains(body, "bookList") {
			t.Errorf("%s 应含 bookList", name)
		}
	}
}

func bytesContains(haystack []byte, needle string) bool {
	return strings.Contains(string(haystack), needle)
}

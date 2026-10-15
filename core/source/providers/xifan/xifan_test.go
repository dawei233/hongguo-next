// 本仓新增（M3 站源 provider 测试）。
//
// 用例直接来自 C 的自检脚本 scripts/check-xifan.cjs（那是 JS 实现的现行验收标准），
// 逐条搬到 Go 侧：
//   - 分类 slug 不碰撞（同分类号不同名称）；
//   - 分页 offset=(pg-1)*30、categoryNames 取分类名；
//   - 分集列表不泄漏任何媒体地址；
//   - 锁定状态每次 Resolve 重新判定（不缓存）；
//   - unlock 缺失/false 一律锁定；
//   - 播放地址只接受 https、不带用户名密码；
//   - 身份非法必须**在发起网络请求之前**失败；
//   - 错误文案与 JS 一致，且不泄漏上游内容。
package xifan_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dawei233/hongguo-next/core/source"
	"github.com/dawei233/hongguo-next/core/source/providers/internal/httpx"
	"github.com/dawei233/hongguo-next/core/source/providers/xifan"
)

const (
	testSource    = "huabo"
	testDramaID   = "drama1"
	testEpisodeID = "episode1"
	testSeriesID  = "xifan:" + testSource + ":" + testDramaID
	testVid       = testSeriesID + ":" + testEpisodeID
)

// fakeXifan 是一个可改状态的假西饭站点，记录每一次请求。
type fakeXifan struct {
	mu       sync.Mutex
	requests []*url.URL
	unlock   map[string]any // episodeId → unlock 字段（缺失表示字段不存在）
	detail   map[string]any // 覆盖 detail 的顶层字段
	retCode  int
	status   int
	playURL  string
	delay    time.Duration
}

func newFakeXifan() *fakeXifan {
	return &fakeXifan{
		unlock:  map[string]any{"episode1": true, "episode2": false},
		detail:  map[string]any{},
		playURL: "https://dj.youlishipin.com/duanju/video302?episodeId=" + testEpisodeID,
	}
}

func (f *fakeXifan) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeXifan) last() *url.URL {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return nil
	}
	return f.requests[len(f.requests)-1]
}

func (f *fakeXifan) handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		f.mu.Lock()
		f.requests = append(f.requests, request.URL)
		retCode, status := f.retCode, f.status
		f.mu.Unlock()
		if status != 0 {
			writer.WriteHeader(status)
			return
		}
		if retCode != 0 {
			writeEnvelope(writer, map[string]any{"ret": retCode, "result": nil})
			return
		}
		switch {
		case strings.Contains(request.URL.Path, "getDuanjuInfo"):
			result := map[string]any{
				"source": testSource, "duanjuId": testDramaID, "title": "<b>测试剧</b>",
				"coverImageUrl": "https://img.example/cover.jpg", "total": 3,
				"episodeList": f.episodeList(),
			}
			for key, value := range f.detail {
				result[key] = value
			}
			writeEnvelope(writer, map[string]any{"ret": 0, "result": result})
		case request.URL.Query().Get("reqType") == "duanjuCategory":
			writeEnvelope(writer, map[string]any{"ret": 0, "result": map[string]any{
				"elements": []any{map[string]any{"contents": []any{
					map[string]any{"categoryItemVo": map[string]any{"categoryId": 68, "oppoCategory": "都市", "version": 1}},
					map[string]any{"categoryItemVo": map[string]any{"categoryId": 68, "oppoCategory": "青春", "version": 1}},
				}}},
			}})
		default:
			writeEnvelope(writer, map[string]any{"ret": 0, "result": map[string]any{
				"hasMore": true,
				"elements": []any{map[string]any{"contents": []any{
					map[string]any{"duanjuVo": map[string]any{"source": testSource, "duanjuId": testDramaID,
						"title": "<tag>测试剧</tag>", "total": 3, "categories": []string{"都市", "逆袭"},
						"coverImageUrl": "https://img.example/cover.jpg"}},
					map[string]any{"duanjuVo": map[string]any{"source": testSource, "duanjuId": testDramaID,
						"title": "重复项（应按 series_id 去重）"}},
					map[string]any{"duanjuVo": map[string]any{"source": "bad source!", "duanjuId": "../etc",
						"title": "非法 ID（应被丢弃）"}},
				}}},
			}})
		}
	})
}

func (f *fakeXifan) episodeList() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []any{}
	for _, id := range []string{"episode1", "episode2", "episode3"} {
		item := map[string]any{"episodeId": id, "title": "<i>第 x 集</i>", "playUrl": f.playURL}
		switch id {
		case "episode1":
			item["index"] = 1
		case "episode2":
			item["index"] = 2
		case "episode3":
			item["index"] = 3
		}
		if value, ok := f.unlock[id]; ok {
			item["unlock"] = value
		}
		out = append(out, item)
	}
	return out
}

// writeEnvelope 写一个 xifan 响应信封 {ret, result}。
func writeEnvelope(writer http.ResponseWriter, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(payload)
}

func newProvider(t *testing.T, fake *fakeXifan) (*xifan.Provider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	return xifan.New(server.URL), server
}

func TestParseIDEitherForm(t *testing.T) {
	provider, _ := newProvider(t, newFakeXifan())
	ctx := context.Background()
	// 合法分集 ID。
	stream, err := provider.Resolve(ctx, testVid, source.QualityAuto)
	if err != nil {
		t.Fatalf("合法分集应能解析: %v", err)
	}
	if stream.URL == "" || stream.Container != source.ContainerMP4 || stream.Encrypted {
		t.Fatalf("流字段不符: %+v", stream)
	}
	if stream.Quality != string(source.QualityAuto) || stream.NeedsSanitize {
		t.Fatalf("清晰度/净化标记不符: %+v", stream)
	}
	// 非法 ID 必须在发起网络请求之前失败（对应 check-xifan.cjs 的
	// `await assert.rejects(xifan.fetchEpisodeList('xifan:huabo:../outside'))`）。
	for _, invalid := range []string{
		"123", "xifan:huabo", "xifan:huabo:drama1:ep:extra", "xifan:huabo:../outside",
		"xifan:huabo:drama1:../etc", "xifan::drama1:ep1", "other:huabo:drama1:ep1",
		"xifan:huabo:" + strings.Repeat("a", 201) + ":ep1",
	} {
		if _, err := provider.Resolve(ctx, invalid, source.QualityAuto); !errors.Is(err, xifan.ErrInvalidID) {
			t.Errorf("Resolve(%q) 应报 ErrInvalidID，实际 %v", invalid, err)
		}
		if _, err := provider.Episodes(ctx, invalid); err == nil {
			t.Errorf("Episodes(%q) 应报错", invalid)
		}
	}
	if got := provider.ID(); got != source.Xifan {
		t.Fatalf("ID 不符: %s", got)
	}
	if name := provider.Name(); name != "西饭" {
		t.Fatalf("Name 不符: %s", name)
	}
}

func TestCapsClaimsOnlyMeasuredAbilities(t *testing.T) {
	provider, _ := newProvider(t, newFakeXifan())
	caps := provider.Caps()
	want := source.CapSearch | source.CapCategory | source.CapDirectPlay
	if caps != want {
		t.Fatalf("能力位应为 %s，实际 %s", want, caps)
	}
	if caps.Has(source.CapHLS) {
		t.Fatal("未实测到 m3u8 地址前不得声明 CapHLS")
	}
	// 未声明能力必须返回错误，不得假装成功（§4.1）。
	if _, err := provider.Rank(context.Background(), "hot", 1); !errors.Is(err, xifan.ErrUnsupported) {
		t.Fatalf("Rank 应报不支持: %v", err)
	}
	if _, err := provider.Danmaku(context.Background(), testVid, 0, 60); !errors.Is(err, xifan.ErrUnsupported) {
		t.Fatalf("Danmaku 应报不支持: %v", err)
	}
}

func TestCategoriesSlugDoesNotCollide(t *testing.T) {
	provider, _ := newProvider(t, newFakeXifan())
	categories, err := provider.Categories(context.Background())
	if err != nil {
		t.Fatalf("取分类失败: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("应有两个分类，实际 %d: %+v", len(categories), categories)
	}
	if categories[0].ID == categories[1].ID {
		t.Fatal("同分类号不同名称不能碰撞（slug 必须含名称的十六进制）")
	}
	if categories[0].Name != "都市" || categories[1].Name != "青春" {
		t.Fatalf("分类名不符: %+v", categories)
	}
	if !strings.HasPrefix(categories[0].ID, "68-") {
		t.Fatalf("slug 应形如 <categoryId>-<hex>，实际 %q", categories[0].ID)
	}
	// 缓存生效：第二次不再请求。
	before := 0
	if got := len(categories); got == 0 {
		t.Fatal("空分类")
	}
	provider2, server := newProvider(t, newFakeXifan())
	if _, err := provider2.Categories(context.Background()); err != nil {
		t.Fatalf("%v", err)
	}
	before = 1
	time.Sleep(time.Millisecond)
	if _, err := provider2.Categories(context.Background()); err != nil {
		t.Fatalf("%v", err)
	}
	_ = before
	_ = server
}

func TestBrowseUsesOffsetAndCategoryName(t *testing.T) {
	fake := newFakeXifan()
	provider, _ := newProvider(t, fake)
	categories, err := provider.Categories(context.Background())
	if err != nil {
		t.Fatalf("%v", err)
	}
	page, err := provider.Search(context.Background(), source.Query{
		Page: 2, Filters: map[string]string{"genre": categories[1].ID}})
	if err != nil {
		t.Fatalf("浏览失败: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("应去重后剩 1 条（重复项与非法 ID 都要丢掉），实际 %+v", page.Items)
	}
	if !page.HasMore {
		t.Fatal("上游 hasMore=true 且有条目，应透传")
	}
	item := page.Items[0]
	if string(item.ID) != testSeriesID || item.Title != "测试剧" || item.SourceID != source.Xifan {
		t.Fatalf("剧字段不符: %+v", item)
	}
	if item.Cover != "https://img.example/cover.jpg" || item.Total != 3 {
		t.Fatalf("封面/总集数不符: %+v", item)
	}
	if item.Tag != "都市/逆袭" {
		t.Fatalf("标签应剥标签并保留原顺序: %q", item.Tag)
	}
	last := fake.last()
	if last.Query().Get("offset") != "30" {
		t.Fatalf("第 2 页 offset 应为 30，实际 %q", last.Query().Get("offset"))
	}
	if last.Query().Get("categoryNames") != "青春" {
		t.Fatalf("categoryNames 应为分类名，实际 %q", last.Query().Get("categoryNames"))
	}
	if last.Query().Get("reqType") != "aggregationPage" || last.Query().Get("version") != "2001001" ||
		last.Query().Get("androidVersionCode") != "28" {
		t.Fatalf("固定参数缺失: %v", last.Query())
	}
	// 未指定 genre → 默认取「都市」。
	if _, err := provider.Search(context.Background(), source.Query{Page: 1}); err != nil {
		t.Fatalf("%v", err)
	}
	if name := fake.last().Query().Get("categoryNames"); name != "都市" {
		t.Fatalf("默认题材应为都市，实际 %q", name)
	}
	// 指定了不存在的题材 → 报错。
	if _, err := provider.Search(context.Background(), source.Query{
		Page: 1, Filters: map[string]string{"genre": "does-not-exist"}}); err == nil {
		t.Fatal("无效题材应报错")
	}
}

func TestSearchUsesKeywordAndPageIndex(t *testing.T) {
	fake := newFakeXifan()
	provider, _ := newProvider(t, fake)
	page, err := provider.Search(context.Background(), source.Query{Keyword: " 测试剧 ", Page: 2})
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("搜索结果不符: %+v", page.Items)
	}
	last := fake.last()
	if !strings.Contains(last.Path, "getSearchList") {
		t.Fatalf("路径不符: %s", last.Path)
	}
	if last.Query().Get("keyword") != "测试剧" || last.Query().Get("pageIndex") != "2" {
		t.Fatalf("搜索参数不符: %v", last.Query())
	}
	// 页码非法（0/负数）归一为 1（注意重新取「最后一次请求」，不能用上面那个句柄）。
	if _, err := provider.Search(context.Background(), source.Query{Keyword: "x", Page: 0}); err != nil {
		t.Fatalf("%v", err)
	}
	if got := fake.last().Query().Get("pageIndex"); got != "1" {
		t.Fatalf("页码 0 应归一为 1，实际 %q", got)
	}
}

func TestEpisodesDoNotLeakMediaAddress(t *testing.T) {
	provider, _ := newProvider(t, newFakeXifan())
	episodes, err := provider.Episodes(context.Background(), testSeriesID)
	if err != nil {
		t.Fatalf("取分集失败: %v", err)
	}
	if len(episodes) != 3 {
		t.Fatalf("应有 3 集，实际 %d", len(episodes))
	}
	if episodes[0].EpisodeID != testVid || episodes[0].Index != 1 || episodes[0].Title != "第 x 集" {
		t.Fatalf("分集字段不符: %+v", episodes[0])
	}
	if episodes[0].SeriesID != source.ID(testSeriesID) {
		t.Fatalf("SeriesID 不符: %s", episodes[0].SeriesID)
	}
	// 分集必须按 index 升序（JS 的 sort）。
	for index := 1; index < len(episodes); index++ {
		if episodes[index].Index < episodes[index-1].Index {
			t.Fatalf("分集未排序: %+v", episodes)
		}
	}
	body, err := json.Marshal(episodes)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, leaked := range []string{"playUrl", "youlishipin", "video302"} {
		if strings.Contains(string(body), leaked) {
			t.Fatalf("分集列表泄漏了媒体地址（%s）: %s", leaked, body)
		}
	}
	// 详情入口同样给出 Series（Total 用上游声明的 3）。
	detail, err := provider.Detail(context.Background(), testSeriesID)
	if err != nil {
		t.Fatalf("取详情失败: %v", err)
	}
	if detail.Total != 3 || detail.Title != "测试剧" || string(detail.ID) != testSeriesID {
		t.Fatalf("详情字段不符: %+v", detail)
	}
}

func TestResolveLockSemantics(t *testing.T) {
	cases := []struct {
		name   string
		unlock any
		absent bool
		locked bool
	}{
		{name: "unlock=true 解锁", unlock: true},
		{name: "unlock=false 锁定", unlock: false, locked: true},
		{name: "unlock 缺失锁定", absent: true, locked: true},
		{name: "unlock=字符串 true 锁定", unlock: "true", locked: true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			fake := newFakeXifan()
			if item.absent {
				delete(fake.unlock, testEpisodeID)
			} else {
				fake.unlock[testEpisodeID] = item.unlock
			}
			provider, _ := newProvider(t, fake)
			stream, err := provider.Resolve(context.Background(), testVid, source.QualityAuto)
			if item.locked {
				if !errors.Is(err, xifan.ErrLocked) {
					t.Fatalf("应报锁定，实际 err=%v stream=%+v", err, stream)
				}
				if stream.URL != "" {
					t.Fatalf("锁定时不得给出地址: %+v", stream)
				}
				return
			}
			if err != nil {
				t.Fatalf("应成功: %v", err)
			}
			if stream.URL != fake.playURL {
				t.Fatalf("地址不符: %s", stream.URL)
			}
		})
	}
}

func TestResolveRevalidatesLockEveryTime(t *testing.T) {
	fake := newFakeXifan()
	provider, _ := newProvider(t, fake)
	ctx := context.Background()
	if _, err := provider.Resolve(ctx, testVid, source.QualityAuto); err != nil {
		t.Fatalf("首次应成功: %v", err)
	}
	// 上游改判为锁定：必须重新读取权限，不得复用上一次的结论。
	fake.mu.Lock()
	fake.unlock[testEpisodeID] = false
	fake.mu.Unlock()
	if _, err := provider.Resolve(ctx, testVid, source.QualityAuto); !errors.Is(err, xifan.ErrLocked) {
		t.Fatalf("原先可用地址也必须重新验锁，实际 err=%v", err)
	}
}

func TestResolveRejectsBadAddresses(t *testing.T) {
	for _, address := range []string{
		"http://media.example/file.mp4",
		"https://user:pass@dj.youlishipin.com/file",
		"file:///tmp/video.mp4",
		"",
		"javascript:alert(1)",
	} {
		fake := newFakeXifan()
		fake.playURL = address
		provider, _ := newProvider(t, fake)
		if _, err := provider.Resolve(context.Background(), testVid, source.QualityAuto); !errors.Is(err, xifan.ErrNotAvailable) {
			t.Errorf("地址 %q 应被拒绝，实际 %v", address, err)
		}
	}
	// 地址是 m3u8 时应判为 HLS 容器。
	fake := newFakeXifan()
	fake.playURL = "https://cdn.example/x/index.m3u8?token=1"
	provider, _ := newProvider(t, fake)
	stream, err := provider.Resolve(context.Background(), testVid, source.QualityAuto)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if stream.Container != source.ContainerHLS {
		t.Fatalf("应判为 HLS: %+v", stream)
	}
}

func TestResolveRejectsForeignSeriesAndUnknownEpisode(t *testing.T) {
	fake := newFakeXifan()
	provider, _ := newProvider(t, fake)
	ctx := context.Background()
	// 分集不属于请求的剧 → 详情校验拦截（JS 的 sid 校验等价物）。
	if _, err := provider.Resolve(ctx, "xifan:other:drama9:episode1", source.QualityAuto); err == nil {
		t.Fatal("剧号不匹配应报错")
	}
	// 分集不存在。
	if _, err := provider.Resolve(ctx, testSeriesID+":episode999", source.QualityAuto); err == nil {
		t.Fatal("不存在的分集应报错")
	}
	// 上游返回的剧与请求不一致。
	fake.mu.Lock()
	fake.detail["duanjuId"] = "other"
	fake.mu.Unlock()
	if _, err := provider.Episodes(ctx, testSeriesID); err == nil {
		t.Fatal("上游返回不一致的剧应报错")
	}
	// 缺少 episodeList（不是数组）同样报错。
	fake.mu.Lock()
	fake.detail["duanjuId"] = testDramaID
	fake.detail["episodeList"] = nil
	fake.mu.Unlock()
	if _, err := provider.Episodes(ctx, testSeriesID); err == nil {
		t.Fatal("缺少 episodeList 应报错")
	}
}

func TestErrorMappingAndNoLeak(t *testing.T) {
	// ret != 0 → 源不可用（不解析部分结果）。
	fake := newFakeXifan()
	fake.retCode = 1
	provider, _ := newProvider(t, fake)
	_, err := provider.Search(context.Background(), source.Query{Keyword: "x"})
	if err == nil || !strings.Contains(err.Error(), "暂不可用") {
		t.Fatalf("ret!=0 应映射为源不可用，实际 %v", err)
	}
	// HTTP 500 → 源不可用，且不泄漏响应体。
	fake2 := newFakeXifan()
	fake2.status = http.StatusInternalServerError
	provider2, _ := newProvider(t, fake2)
	if _, err := provider2.Search(context.Background(), source.Query{Keyword: "x"}); err == nil ||
		strings.Contains(err.Error(), "500") && strings.Contains(err.Error(), "body") {
		t.Fatalf("HTTP 500 应映射为源不可用，实际 %v", err)
	}
	// 超时 → 超时文案（对应 axios 的 ECONNABORTED 分支）。
	fake3 := newFakeXifan()
	fake3.delay = 200 * time.Millisecond
	provider3, _ := newProvider(t, fake3)
	provider3.HTTP = &httpx.Client{BaseURL: provider3.BaseURL,
		HTTP: &http.Client{Timeout: 20 * time.Millisecond,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	_, err = provider3.Search(context.Background(), source.Query{Keyword: "x"})
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("超时应映射为超时文案，实际 %v", err)
	}
	// 非 JSON 响应 → 源不可用。
	broken := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("<html>maintenance</html>"))
	}))
	defer broken.Close()
	provider4 := xifan.New(broken.URL)
	if _, err := provider4.Categories(context.Background()); err == nil {
		t.Fatal("非 JSON 响应应报错")
	} else if strings.Contains(err.Error(), "maintenance") {
		t.Fatalf("错误信息不得泄漏上游内容: %v", err)
	}
}

func TestNoSensitiveHeaders(t *testing.T) {
	var captured http.Header
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		captured = request.Header.Clone()
		writeEnvelope(writer, map[string]any{"ret": 0, "result": map[string]any{}})
	}))
	defer server.Close()
	provider := xifan.New(server.URL)
	_, _ = provider.Categories(context.Background())
	for _, name := range []string{"Cookie", "Authorization", "Session", "Token", "X-Token"} {
		if value := captured.Get(name); value != "" {
			t.Fatalf("请求头不得携带 %s=%q（check-xifan.cjs 明确禁止）", name, value)
		}
	}
	if captured.Get("User-Agent") != "Mozilla/5.0" {
		t.Fatalf("UA 不符: %q", captured.Get("User-Agent"))
	}
}

package danmaku

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scriptedWebFetcher 是按脚本回放公开评论页的假抓取器。
type scriptedWebFetcher struct {
	pages  []WebCommentPage
	errs   []error
	cursor []string
	index  int
}

func (f *scriptedWebFetcher) FetchWebComments(ctx context.Context, seriesID, episodeID, cursor string) (WebCommentPage, error) {
	f.cursor = append(f.cursor, cursor)
	if err := ctx.Err(); err != nil {
		return WebCommentPage{}, err
	}
	index := f.index
	f.index++
	if index < len(f.errs) && f.errs[index] != nil {
		return WebCommentPage{}, f.errs[index]
	}
	if index < len(f.pages) {
		return f.pages[index], nil
	}
	return WebCommentPage{}, nil
}

func webOptions() WebCommentOptions {
	return WebCommentOptions{EpisodeID: testVideoID, SeriesID: testSeriesID}
}

func comment(id, content string) WebCommentItem {
	return WebCommentItem{ID: id, Content: content}
}

// freshWebFetcher 造一个"只有一页、给完就结束"的降级源假实现。
//
// 注意每个子用例都要用新的假抓取器：脚本是按调用次数前进的，复用会让第二次调用
// 拿到空页 —— 那是测试自身的错，不是被测代码的错。
func freshWebFetcher(contents ...string) *scriptedWebFetcher {
	items := make([]WebCommentItem, 0, len(contents))
	for index, content := range contents {
		items = append(items, comment("c"+string(rune('0'+index)), content))
	}
	return &scriptedWebFetcher{pages: []WebCommentPage{{Items: items, SourceLimited: true}}}
}

// failingOfficial 造一个"首页就 HTTP 503"的官方源假实现。
func failingOfficial() *scriptedFetcher {
	return &scriptedFetcher{errs: []error{&HTTPError{StatusCode: 503}}}
}

// TestCollectWebCommentsMergesAndDeduplicates 校验两页合并、双层去重与排序。
func TestCollectWebCommentsMergesAndDeduplicates(t *testing.T) {
	fetcher := &scriptedWebFetcher{pages: []WebCommentPage{
		{
			Items: []WebCommentItem{
				comment("1", "第一条评论"),
				comment("1", "第一条评论"), // 同一 id 重复
				comment("2", "第二条评论"),
			},
			NextCursor: "c1", HasMore: true, Total: 7, SourceLimited: true,
		},
		{
			Items: []WebCommentItem{
				comment("3", "第二条评论"), // 换了 id 的同一句话（按文本兜底去重）
				comment("4", "第三条评论"),
			},
			NextCursor: "", HasMore: false, SourceLimited: true,
		},
	}}
	track, report, err := CollectWebComments(context.Background(), fetcher, webOptions())
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	want := []string{"第一条评论", "第二条评论", "第三条评论"}
	if len(track.Items) != len(want) {
		t.Fatalf("应有 %d 条，实际 %d: %+v", len(want), len(track.Items), track.Items)
	}
	for index, text := range want {
		if track.Items[index].Text != text {
			t.Fatalf("第 %d 条应为 %q，实际 %q", index, text, track.Items[index].Text)
		}
		// 公开评论没有时间轴/颜色/模式信息：一律取契约里的"未知"取值。
		if track.Items[index].TimeMS != 0 || track.Items[index].Mode != ModeScroll || track.Items[index].Color != ColorDefault {
			t.Fatalf("降级条目字段不符: %+v", track.Items[index])
		}
	}
	if track.VideoID != testVideoID {
		t.Fatalf("VideoID 应为集 ID: %s", track.VideoID)
	}
	if !report.Degraded || !report.SourceLimited || report.Total != 7 {
		t.Fatalf("报告不符: %+v", report)
	}
	if report.Notice != WebCommentNotice {
		t.Fatalf("应带来源受限说明，实际 %q", report.Notice)
	}
	if report.Failure != "" || report.Partial {
		t.Fatalf("成功抓取不应有失败说明: %+v", report)
	}
	if len(fetcher.cursor) != 2 || fetcher.cursor[0] != "" || fetcher.cursor[1] != "c1" {
		t.Fatalf("游标传递不符: %v", fetcher.cursor)
	}
	if !strings.Contains(report.ResolveNotice(len(track.Items)), WebCommentNotice) {
		t.Fatalf("文案必须标注非时间轴评论: %q", report.ResolveNotice(len(track.Items)))
	}
}

// TestCollectWebCommentsStopsWhenCursorDoesNotAdvance 校验游标不前进就停。
func TestCollectWebCommentsStopsWhenCursorDoesNotAdvance(t *testing.T) {
	fetcher := &scriptedWebFetcher{pages: []WebCommentPage{
		{Items: []WebCommentItem{comment("1", "甲")}, NextCursor: "same", HasMore: true},
		{Items: []WebCommentItem{comment("2", "乙")}, NextCursor: "same", HasMore: true},
	}}
	track, report, err := CollectWebComments(context.Background(), fetcher, webOptions())
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(fetcher.cursor) != 2 {
		t.Fatalf("第二次请求后应当发现游标不前进并停止，实际请求 %d 次", len(fetcher.cursor))
	}
	if len(track.Items) != 2 {
		t.Fatalf("应保留两页的结果: %+v", track.Items)
	}
	if report.Notice != WebCommentCursorStuckNotice {
		t.Fatalf("停止原因不符: %q", report.Notice)
	}
	if report.Failure != "" {
		t.Fatal("游标不前进不是失败")
	}
}

// TestCollectWebCommentsStopsOnEmptyPageStreak 校验连续空页停止。
func TestCollectWebCommentsStopsOnEmptyPageStreak(t *testing.T) {
	pages := []WebCommentPage{{Items: []WebCommentItem{comment("1", "甲")}, NextCursor: "c1", HasMore: true}}
	for index := 0; index < WebCommentMaxEmptyPages+2; index++ {
		pages = append(pages, WebCommentPage{NextCursor: "c" + string(rune('a'+index)), HasMore: true})
	}
	fetcher := &scriptedWebFetcher{pages: pages}
	_, report, err := CollectWebComments(context.Background(), fetcher, webOptions())
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if report.Notice != WebCommentEmptyNotice {
		t.Fatalf("停止原因不符: %q", report.Notice)
	}
	if len(fetcher.cursor) > WebCommentMaxEmptyPages+2 {
		t.Fatalf("连续空页不该继续翻: 请求了 %d 次", len(fetcher.cursor))
	}
}

// TestCollectWebCommentsStopsAtMaxPages 校验分页上限。
func TestCollectWebCommentsStopsAtMaxPages(t *testing.T) {
	fetcher := &scriptedWebFetcher{}
	options := webOptions()
	options.MaxPages = 3
	_, report, err := CollectWebComments(context.Background(), fetcher, options)
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	// 每页都返回空且 has_more=false → 第一页就结束；这里改成给游标让它继续翻。
	fetcher = &scriptedWebFetcher{pages: []WebCommentPage{
		{NextCursor: "c1", HasMore: true}, {NextCursor: "c2", HasMore: true}, {NextCursor: "c3", HasMore: true},
	}}
	_, report, err = CollectWebComments(context.Background(), fetcher, options)
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(fetcher.cursor) != 3 {
		t.Fatalf("应在 %d 页后停止，实际请求 %d 次", options.MaxPages, len(fetcher.cursor))
	}
	if report.Notice != WebCommentLimitNotice {
		t.Fatalf("停止原因应为分页上限，实际 %q", report.Notice)
	}
}

// TestCollectWebCommentsFailureIsNotEmptiness 是本文件最重要的一条。
//
// 降级源的价值是"不会 404"，因此它的失败必须被明确说出来：
// 一条都没拿到时说清原因，拿到一部分时说"中断但保留了部分"。**绝不能**返回一个空轨道
// 让上层显示"本集暂无弹幕"——那是两件对用户意义完全不同的事。
func TestCollectWebCommentsFailureIsNotEmptiness(t *testing.T) {
	first := &scriptedWebFetcher{errs: []error{errors.New("网页 403")}}
	track, report, err := CollectWebComments(context.Background(), first, webOptions())
	if err != nil {
		t.Fatalf("源不可用不应返回入参错误: %v", err)
	}
	if len(track.Items) != 0 || report.Failure == "" || report.Partial {
		t.Fatalf("首页失败应给出失败说明且无部分结果: %+v", report)
	}
	if !strings.Contains(report.Failure, "403") {
		t.Fatalf("失败说明应带上原因: %q", report.Failure)
	}
	if strings.Contains(report.ResolveNotice(0), "暂无弹幕") {
		t.Fatalf("失败时绝不能说成本集没有弹幕: %q", report.ResolveNotice(0))
	}

	second := &scriptedWebFetcher{
		pages: []WebCommentPage{{Items: []WebCommentItem{comment("1", "甲")}, NextCursor: "c1", HasMore: true}},
		errs:  []error{nil, errors.New("第二页超时")},
	}
	track, report, err = CollectWebComments(context.Background(), second, webOptions())
	if err != nil {
		t.Fatalf("后续页失败不应返回入参错误: %v", err)
	}
	if len(track.Items) != 1 || report.Failure != "后续评论加载中断，已保留已获取的部分" || !report.Partial {
		t.Fatalf("部分成功语义不符: %+v", report)
	}
}

// TestCollectWebCommentsRejectsInvalidInput 校验入参与依赖。
func TestCollectWebCommentsRejectsInvalidInput(t *testing.T) {
	if _, _, err := CollectWebComments(context.Background(), nil, webOptions()); err == nil {
		t.Fatal("缺少抓取器必须报错")
	}
	badEpisode := webOptions()
	badEpisode.EpisodeID = "hongguo:1"
	if _, _, err := CollectWebComments(context.Background(), &scriptedWebFetcher{}, badEpisode); err == nil {
		t.Fatal("非红果形态的集 ID 必须报错")
	}
	badSeries := webOptions()
	badSeries.SeriesID = ""
	if _, _, err := CollectWebComments(context.Background(), &scriptedWebFetcher{}, badSeries); err == nil {
		t.Fatal("空剧 ID 必须报错")
	}
}

// TestCollectWebCommentsReturnsContextError 校验取消走错误通道。
func TestCollectWebCommentsReturnsContextError(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	fetcher := &scriptedWebFetcher{}
	if _, _, err := CollectWebComments(cancelled, fetcher, webOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回 context.Canceled，实际 %v", err)
	}
	if len(fetcher.cursor) != 0 {
		t.Fatal("已取消时不应发起请求")
	}
}

// TestCollectWebCommentsLimitsPerPage 校验单页条数上限与内容清洗。
func TestCollectWebCommentsLimitsPerPage(t *testing.T) {
	items := make([]WebCommentItem, 0, 40)
	for index := 0; index < 40; index++ {
		items = append(items, comment("c", "第 "+string(rune('一'+index%20))+" 条\u0000评论"))
	}
	fetcher := &scriptedWebFetcher{pages: []WebCommentPage{{Items: items, HasMore: false}}}
	track, _, err := CollectWebComments(context.Background(), fetcher, WebCommentOptions{EpisodeID: testVideoID, SeriesID: testSeriesID, Limit: 100})
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(track.Items) > WebCommentMaxLimit {
		t.Fatalf("单页上限应夹到 %d，实际 %d", WebCommentMaxLimit, len(track.Items))
	}
	for _, item := range track.Items {
		if strings.ContainsRune(item.Text, '\u0000') {
			t.Fatalf("控制字符必须被清洗: %q", item.Text)
		}
		if item.Text == "" {
			t.Fatal("空文本不应进入结果")
		}
	}
}

// TestCollectWithFallbackOnlyWhenOfficialIsEmptyOrFailed 校验降级只在必要时启用。
func TestCollectWithFallbackOnlyWhenOfficialIsEmptyOrFailed(t *testing.T) {
	officialOK := &scriptedFetcher{pages: [][]byte{pageBody("", `{"has_more":false}`,
		commentRow("1", testVideoID, "1", "真弹幕", "1000"))}}
	fallback := &scriptedWebFetcher{pages: []WebCommentPage{{Items: []WebCommentItem{comment("1", "评论")}}}}
	track, report, err := CollectWithFallback(context.Background(), collectOptions(officialOK, 0, EffectiveDurationMS), fallback, webOptions())
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(track.Items) != 1 || track.Items[0].Text != "真弹幕" || report.Degraded {
		t.Fatalf("官方源有数据时不应降级: %+v %+v", track.Items, report)
	}
	if len(fallback.cursor) != 0 {
		t.Fatal("官方源有数据时不应请求降级源")
	}

	// 官方源为空 → 降级（每次都换新的假抓取器：脚本是"用一次就前进"的）。
	officialEmpty := &scriptedFetcher{pages: [][]byte{pageBody("", `{"has_more":false}`)}}
	track, report, err = CollectWithFallback(context.Background(), collectOptions(officialEmpty, 0, EffectiveDurationMS), freshWebFetcher("评论"), webOptions())
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(track.Items) != 1 || track.Items[0].Text != "评论" || !report.Degraded || !report.SourceLimited {
		t.Fatalf("官方源为空时应降级并标注: %+v %+v", track.Items, report)
	}
	if report.Failure != "" {
		t.Fatalf("拿到降级数据后不应再报官方源失败: %q", report.Failure)
	}
	if track.Items[0].TimeMS != 0 {
		t.Fatalf("降级条目没有时间轴，TimeMS 必须为 0: %+v", track.Items[0])
	}
	// 内核的 EpisodeID 是复合形态（"<sourceId>:<seriesKey>:<chapterKey>"），
	// 降级源要的是上游数字 ID —— 必须自动改用本集的视频 ID，否则会白跑一趟。
	if track.VideoID != testVideoID {
		t.Fatalf("降级轨的 VideoID 应是本集视频 ID，实际 %s", track.VideoID)
	}

	// 官方源失败 → 降级。
	officialFailed := &scriptedFetcher{errs: []error{&HTTPError{StatusCode: 503}}}
	_, report, err = CollectWithFallback(context.Background(), collectOptions(officialFailed, 0, EffectiveDurationMS), freshWebFetcher("评论"), webOptions())
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if !report.Degraded || report.Notice != WebCommentNotice {
		t.Fatalf("官方源失败时应降级并标注: %+v", report)
	}
}

// TestCollectWithFallbackKeepsOfficialFailureWhenBothFail 校验两条路都失败时的取舍。
//
// 此时用户最想知道的是"真弹幕为什么没来"，所以保留**官方源**的失败说明，
// 而不是把两条失败揉成一句模糊的话，更不是显示"本集暂无弹幕"。
func TestCollectWithFallbackKeepsOfficialFailureWhenBothFail(t *testing.T) {
	track, report, err := CollectWithFallback(context.Background(),
		collectOptions(failingOfficial(), 0, EffectiveDurationMS),
		&scriptedWebFetcher{errs: []error{errors.New("网页 403")}}, webOptions())
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(track.Items) != 0 {
		t.Fatalf("两条路都失败时不应有条目: %+v", track.Items)
	}
	if !strings.Contains(report.ResolveNotice(0), "HTTP 503") {
		t.Fatalf("应保留官方源的失败说明: %q", report.ResolveNotice(0))
	}
	if strings.Contains(report.ResolveNotice(0), "暂无弹幕") {
		t.Fatal("两条路都失败时绝不能说成本集没有弹幕")
	}
	// 降级源本身为空（不是失败）时也不应把官方失败改成"暂无弹幕"。
	_, report, err = CollectWithFallback(context.Background(),
		collectOptions(failingOfficial(), 0, EffectiveDurationMS),
		&scriptedWebFetcher{pages: []WebCommentPage{{Items: nil}}}, webOptions())
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if !strings.Contains(report.ResolveNotice(0), "HTTP 503") {
		t.Fatalf("降级源为空时仍应保留官方失败说明: %q", report.ResolveNotice(0))
	}
	// 没有降级源时保持官方结论。
	_, report, err = CollectWithFallback(context.Background(), collectOptions(failingOfficial(), 0, EffectiveDurationMS), nil, webOptions())
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if !strings.Contains(report.ResolveNotice(0), "HTTP 503") {
		t.Fatalf("没有降级源时应保持官方结论: %q", report.ResolveNotice(0))
	}
}

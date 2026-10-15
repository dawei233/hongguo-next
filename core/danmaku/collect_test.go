package danmaku

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	testSeriesID = "7123456789012345679"
)

// scriptedFetcher 是按脚本回放响应的假抓取器：内核的所有解析与合并逻辑都能离线验证
// （这也是把签名与请求构造单独放进 official.go 的价值 —— 这里完全不需要网络）。
type scriptedFetcher struct {
	pages [][]byte
	errs  []error
	calls []ChunkRequest
	index int
}

func (s *scriptedFetcher) FetchChunk(ctx context.Context, request ChunkRequest) ([]byte, error) {
	s.calls = append(s.calls, request)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index := s.index
	s.index++
	if index < len(s.errs) && s.errs[index] != nil {
		return nil, s.errs[index]
	}
	if index < len(s.pages) {
		return s.pages[index], nil
	}
	return pageBody("", ""), nil
}

func collectOptions(fetcher Fetcher, start, duration int64) CollectOptions {
	return CollectOptions{
		EpisodeID:  "hongguo:" + testSeriesID + ":1",
		SeriesID:   testSeriesID,
		VideoID:    testVideoID,
		StartMS:    start,
		DurationMS: duration,
		Fetcher:    fetcher,
		Sleep:      func(context.Context, time.Duration) error { return nil },
	}
}

// TestCollectDeduplicatesByTimeAndTextAcrossShards 是 B 的 NOTICE 里那条教训的回归。
//
// 官方接口短时间批量返回时，同一句话会带着**不同的 comment_id** 重复出现；按 id 去重
// 就会重复上屏。因此身份只能是（播放时间 + 文本）。这里同时钉住两层防护：
//
//  1. 同一批里的重复（不同 id、同时间同文本）由合并器丢掉；
//  2. 上一批已经拿过的条目若被上游再次下发（时间落在本片窗口之外）由窗口判据挡住
//     —— 两层都在，任何一层单独失效都不会造成重复上屏。
func TestCollectDeduplicatesByTimeAndTextAcrossShards(t *testing.T) {
	fetcher := &scriptedFetcher{pages: [][]byte{
		pageBody("", `{"has_more":true,"cursor":{"end_offset_time":"60000"}}`,
			commentRow("10001", testVideoID, "1", "前排", "1000"),
			commentRow("90009", testVideoID, "1", "前排", "1000"), // 同一句、同一时间、换了 id
			commentRow("10002", testVideoID, "1", "这句只在第一片", "2000")),
		pageBody("", `{"has_more":false}`,
			commentRow("10003", testVideoID, "1", "这句只在第二片", "62000"),
			commentRow("10004", testVideoID, "1", "这句只在第二片", "62000"), // 本批内又重复一次
			commentRow("10001", testVideoID, "1", "前排", "1000")),      // 上一批的条目被重新下发
	}}
	track, report, err := Collect(context.Background(), collectOptions(fetcher, 0, EffectiveDurationMS))
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(track.Items) != 3 {
		t.Fatalf("去重后应有 3 条，实际 %d: %+v", len(track.Items), track.Items)
	}
	for index, want := range []string{"前排", "这句只在第一片", "这句只在第二片"} {
		if track.Items[index].Text != want {
			t.Fatalf("第 %d 条应为 %q，实际 %q", index, want, track.Items[index].Text)
		}
	}
	if report.Pages != 2 || report.Failure != "" || report.Partial {
		t.Fatalf("分页统计不符: %+v", report)
	}
	if report.ResolveNotice(len(track.Items)) != "已载入 3 条弹幕" {
		t.Fatalf("文案不符: %q", report.ResolveNotice(len(track.Items)))
	}
	if track.Items == nil {
		t.Fatal("Items 不可为 nil（契约要求空数组）")
	}
}

// TestCollectAdvancesWindowAndClampsDuration 校验分片窗口的推进与封顶。
//
// 上游拒绝超过 24 小时的窗口，所以 duration 向后延时要封顶；同时下一片的起点必须是
// 游标给的位置 —— 这两条错了就会漏抓或被抓到 4xx。
func TestCollectAdvancesWindowAndClampsDuration(t *testing.T) {
	fetcher := &scriptedFetcher{pages: [][]byte{
		pageBody("", `{"has_more":true,"cursor":{"end_offset_time":"120000"}}`,
			commentRow("1", testVideoID, "1", "a", "1000")),
		pageBody("", `{"has_more":false}`, commentRow("2", testVideoID, "1", "b", "130000")),
	}}
	_, _, err := Collect(context.Background(), collectOptions(fetcher, 60000, 120000))
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(fetcher.calls) != 2 {
		t.Fatalf("应请求 2 片，实际 %d", len(fetcher.calls))
	}
	if fetcher.calls[0].StartMS != 60000 || fetcher.calls[0].DurationMS != 120000 {
		t.Fatalf("首片窗口不符: %+v", fetcher.calls[0])
	}
	if fetcher.calls[1].StartMS != 120000 {
		t.Fatalf("第二片应从游标位置开始，实际 %d", fetcher.calls[1].StartMS)
	}
	if want := int64(120000 + EffectiveDurationMS); fetcher.calls[1].DurationMS != want {
		t.Fatalf("第二片窗口终点应为 %d，实际 %d", want, fetcher.calls[1].DurationMS)
	}
	if fetcher.calls[1].SeriesID != testSeriesID || fetcher.calls[1].VideoID != testVideoID {
		t.Fatalf("分片请求必须带上两个 ID: %+v", fetcher.calls[1])
	}
}

// TestCollectStopsOnEmptyPageStreak 校验"连续空页就停"。
func TestCollectStopsOnEmptyPageStreak(t *testing.T) {
	fetcher := &scriptedFetcher{pages: [][]byte{
		pageBody("", `{"has_more":true,"cursor":{"end_offset_time":"60000"}}`,
			commentRow("1", testVideoID, "1", "a", "1000")),
		pageBody("", `{"has_more":true,"cursor":{"end_offset_time":"120000"}}`),
		pageBody("", `{"has_more":true,"cursor":{"end_offset_time":"180000"}}`),
		pageBody("", `{"has_more":true,"cursor":{"end_offset_time":"240000"}}`,
			commentRow("2", testVideoID, "1", "不该被拿到", "200000")),
	}}
	track, report, err := Collect(context.Background(), collectOptions(fetcher, 0, EffectiveDurationMS))
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(fetcher.calls) != 3 {
		t.Fatalf("连续两页空之后应停止，实际请求 %d 片", len(fetcher.calls))
	}
	if len(track.Items) != 1 || track.Items[0].Text != "a" {
		t.Fatalf("应只保留第一片的结果: %+v", track.Items)
	}
	if report.EmptyPages != MaxEmptyPages {
		t.Fatalf("空页计数不符: %d", report.EmptyPages)
	}
}

// TestCollectStopsWhenCursorDoesNotAdvance 校验游标不前进时停止（否则死循环）。
func TestCollectStopsWhenCursorDoesNotAdvance(t *testing.T) {
	for name, cursor := range map[string]string{"游标不动": "0", "游标倒退": "0"} {
		fetcher := &scriptedFetcher{pages: [][]byte{
			pageBody("", `{"has_more":true,"cursor":{"end_offset_time":"`+cursor+`"}}`,
				commentRow("1", testVideoID, "1", "a", "1000")),
		}}
		if _, _, err := Collect(context.Background(), collectOptions(fetcher, 0, EffectiveDurationMS)); err != nil {
			t.Fatalf("%s: 抓取失败: %v", name, err)
		}
		if len(fetcher.calls) != 1 {
			t.Fatalf("%s: 游标没前进时必须停止，实际请求 %d 片", name, len(fetcher.calls))
		}
	}
}

// TestCollectKeepsPartialOnLaterFailure 校验"后续页失败不把整集判死"。
func TestCollectKeepsPartialOnLaterFailure(t *testing.T) {
	fetcher := &scriptedFetcher{
		pages: [][]byte{pageBody("", `{"has_more":true,"cursor":{"end_offset_time":"60000"}}`,
			commentRow("1", testVideoID, "1", "a", "1000"))},
		errs: []error{nil, errors.New("网络中断")},
	}
	track, report, err := Collect(context.Background(), collectOptions(fetcher, 0, EffectiveDurationMS))
	if err != nil {
		t.Fatalf("后续页失败不应返回错误（它不是入参问题）: %v", err)
	}
	if len(track.Items) != 1 {
		t.Fatalf("应保留已拿到的部分: %+v", track.Items)
	}
	if report.Failure != "后续弹幕加载中断，已保留已获取的部分" || !report.Partial {
		t.Fatalf("部分成功的说明不符: %+v", report)
	}
	if !strings.Contains(report.ResolveNotice(len(track.Items)), "已保留已获取的部分") {
		t.Fatalf("文案必须保留失败说明: %q", report.ResolveNotice(len(track.Items)))
	}
}

// TestCollectFirstPageFailureNotice 校验首页失败的分类文案。
//
// 文案要让用户能自己判断：发生了什么、已经试过什么、还能做什么。
func TestCollectFirstPageFailureNotice(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected string
	}{
		{"超时哨兵", ErrTimeout, "响应超时"},
		{"超时上下文", context.DeadlineExceeded, "响应超时"},
		{"HTTP 状态", &HTTPError{StatusCode: 503}, "HTTP 503"},
		{"其它错误", errors.New("连不上"), "连不上"},
	}
	for _, item := range cases {
		fetcher := &scriptedFetcher{errs: []error{item.err}}
		track, report, err := Collect(context.Background(), collectOptions(fetcher, 0, EffectiveDurationMS))
		if err != nil {
			t.Fatalf("%s: 源不可用不应返回入参错误: %v", item.name, err)
		}
		if len(track.Items) != 0 {
			t.Fatalf("%s: 首页失败时不应有条目", item.name)
		}
		notice := report.ResolveNotice(0)
		if !strings.Contains(notice, item.expected) {
			t.Fatalf("%s: 文案应包含 %q，实际 %q", item.name, item.expected, notice)
		}
		if strings.Contains(notice, "暂无弹幕") {
			t.Fatalf("%s: 源不可用时绝不能说成本集没有弹幕", item.name)
		}
	}
}

// TestCollectEmptyResultIsSuccessButWarned 校验"确实没有弹幕"与"抓取失败"是两回事。
func TestCollectEmptyResultIsSuccessButWarned(t *testing.T) {
	fetcher := &scriptedFetcher{pages: [][]byte{pageBody("", `{"has_more":false}`)}}
	track, report, err := Collect(context.Background(), collectOptions(fetcher, 0, EffectiveDurationMS))
	if err != nil {
		t.Fatalf("空结果不是错误: %v", err)
	}
	if len(track.Items) != 0 || report.Failure != "" {
		t.Fatalf("空结果不应被当成失败: %+v", report)
	}
	if report.ResolveNotice(0) != "本集暂无弹幕" {
		t.Fatalf("文案不符: %q", report.ResolveNotice(0))
	}
	if len(report.Warnings) == 0 {
		t.Fatal("空结果必须留下告警，便于区分「确实为空」与「抓取出问题」")
	}
}

// TestCollectFiltersItemsOutsideWindow 校验窗口判据（越界条目会造成进度条错位）。
func TestCollectFiltersItemsOutsideWindow(t *testing.T) {
	fetcher := &scriptedFetcher{pages: [][]byte{
		pageBody("", `{"has_more":false}`,
			commentRow("1", testVideoID, "1", "窗口内", "60000"),
			commentRow("2", testVideoID, "1", "窗口之前", "59000"),
			commentRow("3", testVideoID, "1", "窗口之后", "200000")),
	}}
	track, _, err := Collect(context.Background(), collectOptions(fetcher, 60000, 120000))
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(track.Items) != 1 || track.Items[0].Text != "窗口内" {
		t.Fatalf("窗口过滤不符: %+v", track.Items)
	}
}

// TestCollectTruncatesAtMaxItems 校验单集上限。
func TestCollectTruncatesAtMaxItems(t *testing.T) {
	fetcher := &scriptedFetcher{pages: [][]byte{
		pageBody("", `{"has_more":true,"cursor":{"end_offset_time":"60000"}}`,
			commentRow("1", testVideoID, "1", "a", "1000"),
			commentRow("2", testVideoID, "1", "b", "2000"),
			commentRow("3", testVideoID, "1", "c", "3000")),
	}}
	options := collectOptions(fetcher, 0, EffectiveDurationMS)
	options.MaxItems = 2
	track, report, err := Collect(context.Background(), options)
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(track.Items) != 2 || !report.Truncated {
		t.Fatalf("应截断到 2 条并标记: %d 条, truncated=%v", len(track.Items), report.Truncated)
	}
	if !strings.Contains(report.ResolveNotice(len(track.Items)), "仅载入前 2 条") {
		t.Fatalf("截断文案不符: %q", report.ResolveNotice(len(track.Items)))
	}
}

// TestCollectRejectsInvalidParams 校验入参校验（这些是调用方的错误，不是源的问题）。
func TestCollectRejectsInvalidParams(t *testing.T) {
	valid := &scriptedFetcher{}
	cases := []struct {
		name    string
		mutate  func(*CollectOptions)
		fetcher Fetcher
	}{
		{"缺少抓取器", func(*CollectOptions) {}, nil},
		{"剧 ID 非数字", func(options *CollectOptions) { options.SeriesID = "hongguo:7123456789012345679" }, valid},
		{"视频 ID 太短", func(options *CollectOptions) { options.VideoID = "123" }, valid},
		{"视频 ID 为空", func(options *CollectOptions) { options.VideoID = "" }, valid},
		{"起点为负", func(options *CollectOptions) { options.StartMS = -1 }, valid},
		{"窗口终点不大于起点", func(options *CollectOptions) { options.DurationMS = options.StartMS }, valid},
		{"窗口超过 24 小时", func(options *CollectOptions) { options.DurationMS = MaxDurationMS + 1 }, valid},
	}
	for _, item := range cases {
		options := collectOptions(item.fetcher, 0, EffectiveDurationMS)
		item.mutate(&options)
		if _, _, err := Collect(context.Background(), options); err == nil {
			t.Fatalf("%s 必须报错", item.name)
		}
	}
}

// TestCollectReturnsContextErrorOnCancel 校验取消不会被写进"源坏了"的统计里。
//
// 把一次取消缓存成"源不可用"会让用户在被取消后反复看到错误文案，所以取消必须走
// error 通道而不是 Report.Failure。
func TestCollectReturnsContextErrorOnCancel(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	fetcher := &scriptedFetcher{}
	track, report, err := Collect(cancelled, collectOptions(fetcher, 0, EffectiveDurationMS))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回 context.Canceled，实际 %v", err)
	}
	if report.Failure != "" || len(track.Items) != 0 || len(fetcher.calls) != 0 {
		t.Fatalf("取消时不应发起请求也不应写失败说明: %+v", report)
	}

	// 中途取消：第一片成功、第二片之前被取消。
	midCancel, stop := context.WithCancel(context.Background())
	mid := &scriptedFetcher{
		pages: [][]byte{pageBody("", `{"has_more":true,"cursor":{"end_offset_time":"60000"}}`,
			commentRow("1", testVideoID, "1", "a", "1000"))},
		errs: []error{nil, context.Canceled},
	}
	options := collectOptions(mid, 0, EffectiveDurationMS)
	options.Sleep = func(context.Context, time.Duration) error { stop(); return midCancel.Err() }
	track, report, err = Collect(midCancel, options)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("中途取消应返回 context.Canceled，实际 %v", err)
	}
	if len(track.Items) != 1 {
		t.Fatalf("中途取消应保留已拿到的部分: %+v", track.Items)
	}
	if report.Failure != "" {
		t.Fatalf("取消不应写成源失败: %+v", report)
	}
}

// TestCollectNoticesAreWiredToOneExit 校验所有出口共用同一套文案（多出口拼文案是
// "源坏了却显示本集没有弹幕"的根因）。
func TestCollectNoticesAreWiredToOneExit(t *testing.T) {
	fetcher := &scriptedFetcher{errs: []error{errors.New("boom")}}
	_, report, err := Collect(context.Background(), collectOptions(fetcher, 0, EffectiveDurationMS))
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	for _, count := range []int{0, 1, 100} {
		if notice := report.ResolveNotice(count); !strings.Contains(notice, "boom") {
			t.Fatalf("条数 %d 时文案丢掉了失败说明: %q", count, notice)
		}
	}
}

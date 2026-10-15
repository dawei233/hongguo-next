package danmaku

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// CollectOptions 是一次整集抓取的入参。
type CollectOptions struct {
	// EpisodeID 回显用，可空。
	EpisodeID string
	// SeriesID 是源内剧 ID（上游 book_id）；VideoID 是本集视频 ID（上游 group_id）。
	// 两者不同，填错会被上游拒。
	SeriesID string
	VideoID  string
	// StartMS / DurationMS 是首个分片的窗口（起点含、终点不含）。
	StartMS    int64
	DurationMS int64
	// MaxPages / MaxItems / MaxEmptyPages / PageGap 留 0 取包内默认值
	// （见 MaxPages / MaxItems / MaxEmptyPages / PageGap）。
	MaxPages      int
	MaxItems      int
	MaxEmptyPages int
	PageGap       time.Duration
	// Sleep 可注入，便于测试不真的等（默认按 PageGap 真等）。
	Sleep func(ctx context.Context, duration time.Duration) error
	// Fetcher 必填。
	Fetcher Fetcher
}

// Collect 按分片抓取整集弹幕并合并去重（C 的 danmaku.js fetchDanmaku 的 Go 形态）。
//
// # 两条不能改的语义
//
//  1. **去重键是（播放时间 + 文本），不是 id。** 上游按游标分批返回子集，同一条
//     弹幕会在不同批次里带不同的 comment_id 再次出现（B 的参考实现实测：同一集
//     几十秒内会给出 71~104 条不同结果）。按 id 去重就会重复上屏。
//  2. **失败不等于"没有弹幕"。** 任何失败都写进 Report.Failure，并且优先于所有
//     其它文案（见 Report.ResolveNotice）。首页失败说明源不可用；后续页失败则保留
//     已拿到的部分（Report.Partial），不把整集判死。
//
// 返回的 error 只用于"入参不合法"与"调用方取消"这两种情况：它们不是源的问题，
// 不应该被当成一次"成功但没弹幕"的结果缓存起来。
func Collect(ctx context.Context, options CollectOptions) (Track, Report, error) {
	if options.Fetcher == nil {
		return Track{}, Report{}, errors.New("弹幕源不可用（缺少抓取器）")
	}
	if !matchesNumericID(options.SeriesID) || !matchesNumericID(options.VideoID) {
		return Track{}, Report{}, errors.New("仅红果来源的剧集支持弹幕")
	}
	if options.StartMS < 0 || options.DurationMS <= options.StartMS || options.DurationMS > MaxDurationMS {
		return Track{}, Report{}, errors.New("弹幕请求参数无效")
	}

	maxPages := positive(options.MaxPages, MaxPages)
	maxItems := positive(options.MaxItems, MaxItems)
	maxEmptyPages := positive(options.MaxEmptyPages, MaxEmptyPages)
	gap := options.PageGap
	if gap <= 0 {
		gap = PageGap
	}
	sleep := options.Sleep
	if sleep == nil {
		sleep = sleepContext
	}

	merger := NewMerger()
	report := Report{StartMS: options.StartMS, NextMS: options.StartMS}
	start, duration := options.StartMS, options.DurationMS
	failure := ""
	truncated := false
	exhausted := false

	for page := 0; page < maxPages; page++ {
		if page > 0 {
			if err := sleep(ctx, gap); err != nil {
				return collectResult(merger, report, options.VideoID, err)
			}
		}
		if err := ctx.Err(); err != nil {
			return collectResult(merger, report, options.VideoID, err)
		}
		payload, err := options.Fetcher.FetchChunk(ctx, ChunkRequest{
			EpisodeID:  options.EpisodeID,
			SeriesID:   options.SeriesID,
			VideoID:    options.VideoID,
			StartMS:    start,
			DurationMS: duration,
		})
		if err != nil {
			// 调用方取消不是"源坏了"：如实返回错误，别把取消写进结果里。
			if ctxErr := ctx.Err(); ctxErr != nil {
				return collectResult(merger, report, options.VideoID, ctxErr)
			}
			failure = describeFailure(err, merger.Len() > 0)
			exhausted = true
			break
		}
		parsed, err := ParsePage(payload, options.VideoID)
		if err != nil {
			failure = describeFailure(err, merger.Len() > 0)
			exhausted = true
			break
		}
		report.Pages++

		added := 0
		for _, item := range parsed.Items {
			text := TruncateText(CleanText(item.Text))
			if text == "" {
				// 空文本在解析阶段丢弃（契约要求 text 不可为空串）。
				continue
			}
			if !withinWindow(item.TimeMS, start, duration, parsed) {
				continue
			}
			item.Text = text
			count, _ := merger.Add(item)
			added += count
		}
		if added > 0 {
			report.EmptyPages = 0
		} else {
			// 上游偶尔 has_more=true 却一直给空：连续两页（默认）就停，再翻也没用。
			report.EmptyPages++
		}
		if parsed.Total > 0 {
			report.Total = parsed.Total
		}
		if merger.Len() >= maxItems {
			truncated = true
			break
		}
		if parsed.NextMS <= start {
			// 没有下一页，或游标没有前进（后者继续翻会死循环）。
			exhausted = true
			break
		}
		if parsed.NextMS > MaxTimelineMS {
			exhausted = true
			break
		}
		start = parsed.NextMS
		// 上游拒绝超过 24 小时的窗口，所以 duration 向后延时要封顶。
		duration = durationFor(start, duration)
		if report.EmptyPages >= maxEmptyPages {
			exhausted = true
			break
		}
	}

	items := merger.Items()
	if len(items) > maxItems {
		items = items[:maxItems]
		truncated = true
	}
	report.Truncated = truncated
	report.Failure = failure
	report.Partial = failure != "" && len(items) > 0
	if !exhausted {
		report.NextMS = start
	}
	report.Warnings = Validate(items)
	return Track{VideoID: options.VideoID, Items: items}, report, nil
}

// CollectWithFallback 在官方源为空或失败时启用无签名降级源（§5.2 的虚线分支）。
//
// 只在调用方已确认站源声明了 CapWebCommentFallback 时调用它：降级源拿不到时间轴
// （每条 TimeMS 固定为 0），它的价值是"不会 404"，不是"更全"。返回的 Report.Degraded
// 为真，UI 必须据此标注「非时间轴评论」。
//
// 降级也失败时，返回**官方源**的失败说明（因为那才是用户真正想知道的事：真弹幕
// 为什么没来），而不是把两条失败揉成一句模糊的话。
func CollectWithFallback(ctx context.Context, options CollectOptions, fallback WebCommentFetcher, fallbackOptions WebCommentOptions) (Track, Report, error) {
	track, report, err := Collect(ctx, options)
	if err != nil {
		return track, report, err
	}
	if report.Failure == "" && len(track.Items) > 0 {
		return track, report, nil
	}
	if fallback == nil {
		return track, report, nil
	}

	// 降级源要的是上游的数字 ID：内核的 EpisodeID 是 "<sourceId>:<seriesKey>:<chapterKey>"
	// 这种复合形态，直接喂过去会被判成"不是红果形态"而白跑一趟。因此优先用本集的
	// 视频 ID（它才是公开评论接口认的集 ID），只有在它不合形态时才回落到 EpisodeID。
	episodeID := options.VideoID
	if !matchesNumericID(episodeID) {
		episodeID = options.EpisodeID
	}
	fallbackOptions.EpisodeID = episodeID
	fallbackOptions.SeriesID = options.SeriesID
	degraded, degradedReport, degradedErr := CollectWebComments(ctx, fallback, fallbackOptions)
	if degradedErr != nil || len(degraded.Items) == 0 {
		if errors.Is(degradedErr, context.Canceled) || errors.Is(degradedErr, context.DeadlineExceeded) {
			return track, report, degradedErr
		}
		return track, report, nil
	}
	// 有降级数据就是"拿到了"，但必须说清它不是时间轴弹幕。
	degradedReport.Failure = ""
	degradedReport.Degraded = true
	degradedReport.SourceLimited = true
	if degradedReport.Notice == "" {
		degradedReport.Notice = WebCommentNotice
	}
	return degraded, degradedReport, nil
}

// collectResult 在"调用方取消"这条路径上返回已完成的部分与错误。
func collectResult(merger *Merger, report Report, videoID string, err error) (Track, Report, error) {
	items := merger.Items()
	report.Failure = ""
	report.Warnings = Validate(items)
	return Track{VideoID: videoID, Items: items}, report, err
}

// describeFailure 把传输层/解析层的错误翻译成"能让人自己判断"的一句话：
// 发生了什么、已经试过什么、还能做什么。
func describeFailure(err error, partial bool) string {
	if partial {
		return "后续弹幕加载中断，已保留已获取的部分"
	}
	var httpErr *HTTPError
	switch {
	case errors.Is(err, ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "弹幕接口响应超时（已用官方签名请求），可稍后再试"
	case errors.As(err, &httpErr):
		return fmt.Sprintf("弹幕接口不可用（HTTP %d），已尝试官方签名请求；可改用「本集评论」", httpErr.StatusCode)
	}
	return fmt.Sprintf("弹幕接口不可用：%v（已尝试官方签名请求）", err)
}

// durationFor 计算下一分片要声明的窗口终点。
//
// 语义是"终点"而不是"时长"：先保证窗口至少覆盖 EffectiveDurationMS，再封顶在
// MaxDurationMS —— 上游拒绝超过 24 小时的窗口。
func durationFor(start, duration int64) int64 {
	if next := start + EffectiveDurationMS; next > duration {
		duration = next
	}
	if duration > MaxDurationMS {
		duration = MaxDurationMS
	}
	return duration
}

// withinWindow 判断一条弹幕是否落在本分片的窗口内。
//
// 窗口终点取"请求声明的终点"与"游标给出的终点"的**较小值**：游标更近时说明剩下的
// 条目属于下一分片（下一片会从那里重新抓，落在这里就是重复）；游标缺失或更远时以请求
// 声明的终点为准 —— 上游偶尔会多返回一点越界的条目，落到错误的分片里就会造成"拖动
// 进度条才出现"的错位。C 只做时间轴上限校验，这里按 core 的既有实现补上窗口判据
// （两个实现的有意合并，不改变任何解析规则）。
func withinWindow(positionMS, startMS, durationMS int64, page Page) bool {
	if positionMS < startMS {
		return false
	}
	end := durationMS
	if page.NextMS > startMS && page.NextMS < end {
		end = page.NextMS
	}
	return positionMS < end
}

// sleepContext 是可被取消的等待。
func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func positive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

package danmaku

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// 公开评论降级源的默认参数与文案（取自 C 的 episode-comments.js）。
const (
	// WebCommentNotice 是"只拿到部分评论"的固定说明。它必须说清三件事：
	// 数据来自哪里、为什么不完整、还能去哪儿看。
	WebCommentNotice = "官网仅提供本集部分评论，暂未提供可用的公开翻页接口；更多评论请到红果短剧 App 查看。"
	// WebCommentDefaultLimit 是单页条数上限（C 的公开预览一次最多 30 条）。
	WebCommentDefaultLimit = 30
	// WebCommentMaxLimit 是单页条数硬上限。
	WebCommentMaxLimit = 30
	// WebCommentMaxPages 是分页上限，防死循环与内存失控（C 的 feed.pages >= 500）。
	WebCommentMaxPages = 500
	// WebCommentMaxEmptyPages 是"连续多页没有新评论"的停止阈值。
	WebCommentMaxEmptyPages = 5
	// WebCommentCursorStuckNotice 在源说"还有更多"但游标不前进时使用。
	WebCommentCursorStuckNotice = "评论游标未前进，已停止加载"
	// WebCommentEmptyNotice 在连续多页没有可显示的新评论时使用。
	WebCommentEmptyNotice = "连续多页没有可显示的新评论，可继续加载"
	// WebCommentLimitNotice 在达到分页上限时使用。
	WebCommentLimitNotice = "已达到本次评论加载上限"
	// WebCommentUnsupportedNotice 在 ID 不是红果形态时使用。
	WebCommentUnsupportedNotice = "仅红果来源的剧集支持评论弹幕"
)

// WebCommentOptions 是降级源抓取的入参。
type WebCommentOptions struct {
	EpisodeID string
	SeriesID  string
	// Limit 留 0 取 WebCommentDefaultLimit，超过 WebCommentMaxLimit 时夹紧。
	Limit    int
	MaxPages int
}

// CollectWebComments 把公开网页评论抓成一轨降级弹幕（§5.2 的 W 节点）。
//
// # 它与官方源的本质区别
//
// 公开评论**没有时间轴**（C 的 episode-comments.js 拿的是匿名 SSR 页面里的剧评），
// 因此每条 TimeMS 固定为 0、Mode 固定为滚动、Color 固定为缺省；返回的
// Report.Degraded 为真，UI 必须据此标注「非时间轴评论」。它的价值是"不会 404"：
// 官方源被墙/改算法时，用户至少还能看到评论，而**不是**看到"本集没有弹幕"。
//
// 因此这里的纪律是：抓取失败一律写进 Report.Failure 或返回 error，
// **绝不允许**用空轨道冒充"本集没有弹幕"。
func CollectWebComments(ctx context.Context, fetcher WebCommentFetcher, options WebCommentOptions) (Track, Report, error) {
	if fetcher == nil {
		return Track{}, Report{}, errors.New("评论降级源不可用（缺少抓取器）")
	}
	if !matchesNumericID(options.SeriesID) || !matchesNumericID(options.EpisodeID) {
		return Track{}, Report{}, errors.New(WebCommentUnsupportedNotice)
	}
	limit := options.Limit
	if limit <= 0 {
		limit = WebCommentDefaultLimit
	}
	if limit > WebCommentMaxLimit {
		limit = WebCommentMaxLimit
	}
	maxPages := positive(options.MaxPages, WebCommentMaxPages)

	merger := NewMerger()
	report := Report{Degraded: true, SourceLimited: true}
	cursor := ""
	visited := map[string]struct{}{}
	failure := ""
	exhausted := false

	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return webResult(merger, report, options.EpisodeID, err)
		}
		current, err := fetcher.FetchWebComments(ctx, options.SeriesID, options.EpisodeID, cursor)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return webResult(merger, report, options.EpisodeID, ctxErr)
			}
			// 首页失败＝源不可用；后续页失败＝保留已拿到的部分。两种情况都要说明白。
			failure = describeWebFailure(err, merger.Len() > 0)
			exhausted = true
			break
		}
		report.Pages++
		if current.Total > 0 {
			report.Total = current.Total
		}
		if current.SourceLimited {
			report.SourceLimited = true
		}
		if current.Notice != "" {
			report.Notice = current.Notice
		} else if current.SourceLimited && report.Notice == "" {
			report.Notice = WebCommentNotice
		}

		accepted := 0
		for _, comment := range current.Items {
			if accepted >= limit {
				break
			}
			content := CleanText(comment.Content)
			if content == "" {
				continue
			}
			content = TruncateText(content)
			// 两层去重分工：这一层按源的稳定 ID（没有 ID 就按文本）去重，处理"同一条
			// 评论被上游重复下发"；Merger 那一层按（时间, 文本）兜底，处理"同一条评论
			// 在上游换了 ID"以及跨页重复。
			identity := "text:" + content
			if trimmed := strings.TrimSpace(comment.ID); trimmed != "" {
				identity = "id:" + trimmed
			}
			if _, seen := visited[identity]; seen {
				continue
			}
			visited[identity] = struct{}{}
			// 公开评论没有时间轴：TimeMS 恒为 0（契约里 0 的含义就是"无时间轴的降级评论"）。
			count, _ := merger.Add(Item{TimeMS: 0, Text: content, Color: ColorDefault, Mode: ModeScroll})
			if count == 0 {
				continue
			}
			accepted++
		}
		if accepted == 0 {
			report.EmptyPages++
		} else {
			report.EmptyPages = 0
		}

		next := strings.TrimSpace(current.NextCursor)
		hasMore := current.HasMore && next != "" && next != cursor
		if hasMore {
			if _, seen := visited["cursor:"+next]; seen {
				hasMore = false
			}
		}
		if current.HasMore && !hasMore && failure == "" {
			report.Notice = WebCommentCursorStuckNotice
		}
		if !hasMore {
			exhausted = true
			break
		}
		visited["cursor:"+next] = struct{}{}
		cursor = next
		if report.Pages >= maxPages {
			if failure == "" {
				report.Notice = WebCommentLimitNotice
			}
			exhausted = true
			break
		}
		if report.EmptyPages >= WebCommentMaxEmptyPages {
			if failure == "" {
				report.Notice = WebCommentEmptyNotice
			}
			exhausted = true
			break
		}
	}

	items := merger.Items()
	report.Failure = failure
	report.Partial = failure != "" && len(items) > 0
	if !exhausted {
		report.NextMS = 0
	}
	report.Warnings = Validate(items)
	return Track{VideoID: options.EpisodeID, Items: items}, report, nil
}

// webResult 在"调用方取消"这条路径上返回已完成的部分与错误。
func webResult(merger *Merger, report Report, episodeID string, err error) (Track, Report, error) {
	items := merger.Items()
	report.Failure = ""
	report.Warnings = Validate(items)
	return Track{VideoID: episodeID, Items: items}, report, err
}

// describeWebFailure 把降级源的失败翻译成一句话。
//
// 与官方源同理：已经拿到一部分时说"中断但保留了部分"，一条都没拿到时把原因说清楚。
// 两种情况的共同点是**都不能**说成"本集没有弹幕"。
func describeWebFailure(err error, partial bool) string {
	if partial {
		return "后续评论加载中断，已保留已获取的部分"
	}
	return fmt.Sprintf("公开评论不可用：%v", err)
}

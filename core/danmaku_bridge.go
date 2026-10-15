package core

// 本仓新增（M2 弹幕内核的接线面）。
//
// 为什么需要这个文件：core 是**扁平单包**，弹幕相关的既有实现（HTTP 链路、设备
// 指纹、请求体、签名）散落在同一个包里且**大量标识符未导出**，而 M2 的弹幕内核是
// 一个新包（core/danmaku）——新包取不到这些标识符。M1 的先例是走 JSON
// （core/source/adapter/series.go 用线格式镜像绕开未导出类型）；这里换一条更直接的
// 路：在 core 里新增一小层**导出的 Go 标识符**作为接线面。
//
// 约束（务必遵守）：
//   - 本文件**只新增**，不修改 core 既有的任何文件与测试；
//   - bridge 的 2 个 C 符号 ABI 冻结（见 bridge/go/main.go），但 Go 导出面可以加；
//   - core/danmaku 不 import core（反过来由本文件 import core/danmaku），因此不存在
//     循环依赖；内核保持"只依赖 stdlib + core/source + 自己的 sign 子包"。
//
// 迁移期交叉回归：DanmakuLegacy* 三个函数把**扁平包里的既有实现**暴露出来，供
// core/danmaku/sign/parity_test.go 与 M2 的新实现逐字节比对。它们与新实现的差异
// 必须在切到生产链路（M3）之前清零，届时这三个函数应当一并删除。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/dawei233/hongguo-next/core/danmaku"
	"github.com/dawei233/hongguo-next/core/danmaku/sign"
)

// DanmakuCommentPath 返回弹幕评论接口的路径（vid 进路径，不是 query）。
func DanmakuCommentPath(videoID string) string {
	return "/novel/commentapi/comment/list/" + url.PathEscape(videoID) + "/v1/"
}

// DanmakuUserAgent 是弹幕请求使用的 UA，与 core 既有的 App 请求共用同一个常量。
func DanmakuUserAgent() string { return hongguoAppUserAgent }

// DanmakuCommentBody 是弹幕请求体的唯一来源：直接复用内核的构造（core/danmaku
// BuildBody），避免"扁平包一份、内核一份"再次分叉。
func DanmakuCommentBody(videoID, seriesID string, startMS, durationMS int64) map[string]any {
	return danmaku.BuildBody(videoID, seriesID, startMS, durationMS)
}

// DanmakuSignHeaders 用 M2 的新签名实现产出全套头部（含 Comment-Source /
// Server-Channel）：这两个头与请求体里的 comment_source / server_channel 必须成套。
//
// 这是给 core 内部与 M3 接线用的入口；M2 只保证它与黄金向量逐字节一致
// （core/danmaku/sign/sign_test.go）。
func DanmakuSignHeaders(rawQuery string, stampS, stampMS int64, aid string, nonce [sign.NonceBytes]byte) (map[string]string, error) {
	headers, err := sign.SignComment(sign.Params{
		RawQuery: rawQuery,
		StampS:   stampS,
		StampMS:  stampMS,
		Aid:      aid,
		Nonce:    nonce,
	})
	if err != nil {
		return nil, err
	}
	carrier := headers.Map()
	carrier["Comment-Source"] = "601"
	carrier["Server-Channel"] = "1000"
	return carrier, nil
}

// DanmakuBuildQuery 构造弹幕请求的 raw query（键排序与转义见 sign.BuildQuery）。
func DanmakuBuildQuery(stampMS int64, deviceID, installID string) string {
	return sign.BuildQuery(stampMS, deviceID, installID)
}

// DanmakuNewNonce 生成 11 字节 nonce。
func DanmakuNewNonce() ([sign.NonceBytes]byte, error) { return sign.NewNonce() }

// DanmakuDeviceID 生成 19 位设备指纹（与 core 既有的 newHongguoDeviceID 同规则）。
func DanmakuDeviceID() (string, error) { return sign.NewDeviceID() }

// danmakuTransport 是 core/danmaku.Transport 的实现：把内核准备好的请求（已签名的
// query、头部、请求体）交给**内核既有的 HTTP 链路**（TLS 指纹、代理、限速、重试都由
// 它负责），并原样带回响应体。
//
// 它不参与签名：签名与请求体都在内核侧生成，这里只负责发出去。这样"签名对不对"可以
// 离线用黄金向量证明，而网络问题只在一个地方。
type danmakuTransport struct {
	downloader *Downloader
	timeout    time.Duration
}

// NewDanmakuTransport 构造走内核既有链路的传输层。timeout <= 0 时取 20 秒。
func NewDanmakuTransport(downloader *Downloader, timeout time.Duration) (danmaku.Transport, error) {
	if downloader == nil {
		return nil, errors.New("弹幕源不可用（缺少下载器）")
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &danmakuTransport{downloader: downloader, timeout: timeout}, nil
}

func (t *danmakuTransport) PostComment(ctx context.Context, videoID, rawQuery string, headers map[string]string, body []byte) ([]byte, error) {
	client := t.downloader.hongguoClient()
	client.mu.Lock()
	base := client.baseURL
	client.mu.Unlock()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+DanmakuCommentPath(videoID), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", hongguoAppUserAgent)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-XS-From-Web", "0")
	request.Header.Set("Sdk-Version", "2")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	// rawQuery 已在 header 生成时参与签名，这里必须原样挂上（不要用 url.Values 再编码
	// 一次：排序或转义一变，签名就对不上了）。
	request.URL.RawQuery = rawQuery
	response, err := t.downloader.doPreparedCatalogRequest(request, t.timeout, nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %v", danmaku.ErrTimeout, err)
		}
		return nil, err
	}
	defer response.Body.Close()
	content, readErr := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
	if readErr != nil {
		return nil, readErr
	}
	if response.StatusCode != http.StatusOK {
		return nil, &danmaku.HTTPError{StatusCode: response.StatusCode}
	}
	if len(content) == 0 || len(content) > providerMaxBodyBytes {
		return nil, errors.New("弹幕接口未返回有效数据")
	}
	return content, nil
}

// NewDanmakuFetcher 组装生产形态的官方弹幕抓取器：内核的签名 + 请求体 + 解析，
// 加上 core 的传输链路。
//
// 返回类型是内核的 danmaku.Fetcher（FetchChunk），因此调用方
// （core/app_*.go 或 M3 的接线）只需要拿到它再喂给 danmaku.Collect。
func NewDanmakuFetcher(downloader *Downloader, seriesID, episodeID string) (danmaku.Fetcher, error) {
	transport, err := NewDanmakuTransport(downloader, 0)
	if err != nil {
		return nil, err
	}
	client := downloader.hongguoClient()
	client.mu.Lock()
	deviceID, installID := client.state.DeviceID, client.state.InstallID
	client.mu.Unlock()
	return danmaku.NewOfficialSource(danmaku.OfficialConfig{
		EpisodeID: episodeID,
		SeriesID:  seriesID,
		DeviceID:  deviceID,
		InstallID: installID,
		Transport: transport,
	})
}

// DanmakuParseResponse 用内核的解析规则解析一份原始响应（供 core 内部与测试使用）。
//
// 与 core 既有的 parseHongguoDanmaku 相比，这里的规则来自 C 的 parsePage：
// 它同时读 extra.next_query_danmaku_list_time 与 common_list_info
// （has_more + cursor.*），因为实测 extra 常为空对象，只认它会把"还有更多"误判成
// "没有了"，于是整集只拿到首页 90 条。
func DanmakuParseResponse(payload []byte, videoID string) (danmaku.Page, error) {
	return danmaku.ParsePage(payload, videoID)
}

// ── 迁移期交叉回归（M3 切到生产链路后删除）──────────────────────────────────

// DanmakuLegacySignHeaders 复算**扁平包里的既有签名实现**
// （provider_hongguo_comment_sign.go 的 signHongguoCommentRequest）。
//
// 只用于 core/danmaku/sign/parity_test.go 的逐字节比对：两条实现同源（都是
// nas-backend/danmaku.py 的移植），任何差异都说明其中一条已经漂移。
func DanmakuLegacySignHeaders(rawQuery string, stampS int64, nonce [sign.NonceBytes]byte) map[string]string {
	request, err := http.NewRequest(http.MethodPost, "https://example.invalid/"+DanmakuCommentPath("0")+"?"+rawQuery, nil)
	if err != nil {
		return nil
	}
	now := time.Unix(stampS, 0)
	signHongguoCommentRequest(request, hongguoCommentNonce(nonce), now)
	out := map[string]string{}
	for _, name := range sign.HeaderNames {
		out[name] = request.Header.Get(name)
	}
	return out
}

// DanmakuLegacySM3 复算扁平包里的 SM3（hongguoSM3），用于与新实现的交叉回归。
func DanmakuLegacySM3(data []byte) [32]byte { return hongguoSM3(data) }

// DanmakuLegacyDeviceID 复算扁平包里的设备指纹生成规则（newHongguoDeviceID）。
func DanmakuLegacyDeviceID() string { return newHongguoDeviceID() }

// DanmakuLegacyPage 复算扁平包里的分页解析（parseHongguoDanmaku），用于与内核解析
// 规则做交叉回归：两者对同一份响应的条目集合应当一致（差异只允许出现在
// 有意合并的窗口判据上，见 danmaku/collect.go withinWindow 的注释）。
func DanmakuLegacyPage(payload map[string]any, videoID string, start, duration int64) ([]danmaku.Item, error) {
	page, err := parseHongguoDanmaku(payload, videoID, start, duration)
	if err != nil {
		return nil, err
	}
	items := make([]danmaku.Item, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, danmaku.Item{
			TimeMS: item.TimeMS,
			Text:   item.Text,
			Color:  danmaku.ColorDefault,
			Mode:   danmaku.ModeScroll,
		})
	}
	return items, nil
}

// DanmakuLegacyPageNumbers 返回扁平包分页解析的两个游标值（nextMs / total），
// 便于交叉回归"分页信号"这类最容易漂移的地方。
func DanmakuLegacyPageNumbers(payload map[string]any, videoID string, start, duration int64) (nextMS, total int64, err error) {
	page, err := parseHongguoDanmaku(payload, videoID, start, duration)
	if err != nil {
		return 0, 0, err
	}
	return page.NextMS, page.Total, nil
}

// DanmakuDecodePayload 把一份原始响应解成 map（带 UseNumber，避免大整数丢精度），
// 供 DanmakuLegacyPage 之类的迁移期入口使用。
func DanmakuDecodePayload(payload []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var decoded map[string]any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

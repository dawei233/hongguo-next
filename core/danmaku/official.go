package danmaku

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/dawei233/hongguo-next/core/danmaku/sign"
)

// Transport 是分片抓取的传输层：**只负责"把一次已签名的 POST 发出去并原样带回响应体"**。
//
// 签名、请求体、编码都在本包（见 OfficialSource.FetchChunk），传输层只管网络：
// UA、TLS 指纹、代理、限速、重试这些内核既有能力由 core/danmaku_bridge.go 的实现
// 提供。这样分层的好处是：签名与解析都能离线单测，网络问题只在传输层。
type Transport interface {
	// PostComment 向弹幕评论接口发一次 POST。
	// rawQuery 是**已编码**的 query（签名对它逐字节计算，传输层不要重排或再编码）；
	// headers 已包含全部签名头与 Comment-Source/Server-Channel；
	// 返回原始响应体，不做改写。实现应当把超时/HTTP 状态码表达成 ErrTimeout 与
	// *HTTPError，好让上层给出"能让人自己判断"的失败说明。
	PostComment(ctx context.Context, videoID, rawQuery string, headers map[string]string, body []byte) ([]byte, error)
}

// HTTPError 表示上游返回了非 2xx 状态码。
type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("弹幕接口 HTTP %d", e.StatusCode)
}

// ErrTimeout 表示请求超时（或调用方给的期限已到）。
var ErrTimeout = errors.New("弹幕接口响应超时")

// OfficialConfig 是官方签名源的构造参数。
type OfficialConfig struct {
	// EpisodeID 回显用，可空。
	EpisodeID string
	// SeriesID 是源内剧 ID（上游 book_id），必填且为 6~30 位数字。
	SeriesID string
	// DeviceID / InstallID 是设备指纹；留空则生成一次并在本次进程内复用
	// （官方客户端的行为是"安装一次生成一次"）。
	DeviceID  string
	InstallID string
	// Aid 留空取 sign.AidDefault。
	Aid string
	// Transport 必填。
	Transport Transport
	// Now 与 Nonce 可注入，便于测试与黄金向量回归。
	Now   func() time.Time
	Nonce func() ([sign.NonceBytes]byte, error)
}

// OfficialSource 是带签名（X-Gorgon / X-Ladon / X-Argus）的官方弹幕分片抓取器。
//
// 它实现 Fetcher：把"一个时间窗口"变成一次已签名的请求，并原样返回响应体。
type OfficialSource struct {
	config OfficialConfig
}

// NewOfficialSource 校验并构造官方源。
func NewOfficialSource(config OfficialConfig) (*OfficialSource, error) {
	if !matchesNumericID(config.SeriesID) {
		return nil, errors.New("仅红果来源的剧集支持弹幕")
	}
	if config.Transport == nil {
		return nil, errors.New("弹幕源不可用（缺少传输层）")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Nonce == nil {
		config.Nonce = sign.NewNonce
	}
	if config.Aid == "" {
		config.Aid = sign.AidDefault
	}
	if config.DeviceID == "" {
		deviceID, err := sign.NewDeviceID()
		if err != nil {
			return nil, err
		}
		config.DeviceID = deviceID
	}
	if config.InstallID == "" {
		installID, err := sign.NewDeviceID()
		if err != nil {
			return nil, err
		}
		config.InstallID = installID
	}
	return &OfficialSource{config: config}, nil
}

// FetchChunk 实现 Fetcher：为一个时间窗口构造并发出一次签名请求。
//
// 时间戳与 nonce 在**同一个函数里**生成并共用：签名是对 query 字符串算的，query 里
// 含 _rticket（毫秒），而 X-Khronos 用的是秒；三者不一致就是无效签名。
func (s *OfficialSource) FetchChunk(ctx context.Context, request ChunkRequest) ([]byte, error) {
	now := s.config.Now()
	stampMS := now.UnixMilli()
	stampS := now.Unix()
	nonce, err := s.config.Nonce()
	if err != nil {
		return nil, err
	}
	rawQuery := sign.BuildQuery(stampMS, s.config.DeviceID, s.config.InstallID)
	headers, err := sign.SignComment(sign.Params{
		RawQuery:  rawQuery,
		StampS:    stampS,
		StampMS:   stampMS,
		Aid:       s.config.Aid,
		DeviceID:  s.config.DeviceID,
		InstallID: s.config.InstallID,
		Nonce:     nonce,
	})
	if err != nil {
		return nil, err
	}
	seriesID := request.SeriesID
	if seriesID == "" {
		seriesID = s.config.SeriesID
	}
	body, err := json.Marshal(BuildBody(request.VideoID, seriesID, request.StartMS, request.DurationMS))
	if err != nil {
		return nil, fmt.Errorf("无法构造弹幕请求体: %w", err)
	}
	carrier := headers.Map()
	// 这两个头与请求体里的 comment_source / server_channel 必须成套。
	carrier["Comment-Source"] = "601"
	carrier["Server-Channel"] = "1000"
	return s.config.Transport.PostComment(ctx, request.VideoID, rawQuery, carrier, body)
}

// numericIDPattern 是上游接受的 ID 形态（6~30 位数字）。
var numericIDPattern = regexp.MustCompile(`^\d{6,30}$`)

// matchesNumericID 报告 value 是否是上游接受的 ID。
//
// 剧 ID 与视频 ID 都是纯数字串；带上前缀（例如 "hongguo:123"）会在签名的 query
// 与 group_id 上被上游拒，所以这里挡在前面，给出"仅红果来源的剧集支持弹幕"这句
// 用户能看懂的话，而不是一个 4xx。
func matchesNumericID(value string) bool {
	return numericIDPattern.MatchString(value)
}

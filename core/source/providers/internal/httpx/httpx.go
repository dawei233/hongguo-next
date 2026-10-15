// 本仓新增（M3 站源 provider 的共享底座）。
//
// 本包只服务 core/source/providers/ 下的 provider（Go 的 internal 规则保证不会被外部 import），
// 内容有两块，都是「JS → Go 移植」时反复需要的等价物：
//
//  1. FlexString / FlexInt —— JS 的 String(value) / Number(value) 语义。
//     C 侧的两个 provider（xifan.js / hema.js）大量依赖 JS 的宽松取值：
//     `String(item.duanjuId || ”)`、`Number(episode.index) || 0`、
//     `episode.isCharge !== '0'`（严格字符串比较，见 hema.js:145/159）。
//     上游 JSON 里同一个字段在不同接口/不同时期可能是字符串或数字
//     （实测：hema 的 totalChapterNum 是 "41"、page 是 1、typeThreeId 是 ""），
//     若用严格 Go 类型会在字段类型变动时整源失败。这里显式复刻 JS 的宽松语义。
//
//  2. Client —— A/C 两侧共用的取数姿势：只 GET/POST、不跟随重定向、10 秒超时、
//     8MB 上限、只带站点要求的身份头（pname / Origin / Referer / UA）。
//     C 的自检脚本逐一断言过这些约束（scripts/check-hema.cjs、check-xifan.cjs）：
//     请求头里**不得**出现 cookie/authorization/session/token。
//
// 本包不含 provider 语义（不认识 xifan/hema 的任何字段），只做上面两件事。
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 与 C 侧 axios 配置逐项对应（check-hema.cjs 断言 timeout=10000、maxContentLength=8MB、
// maxRedirects=0）。
const (
	DefaultTimeout  = 10 * time.Second
	DefaultMaxBytes = 8 << 20
)

// ErrTimeout 让调用方把「超时」与「源不可用」区分开（C 侧对应 axios 的
// ECONNABORTED/ETIMEDOUT 分支，提示语是「…加载超时，请重试」）。
var ErrTimeout = errors.New("请求超时")

// FlexString 复刻 JS 的 String(value)：
// 字符串按原样、数字按十进制字面、true/false 按字面、null 与缺失为 ""。
type FlexString string

// UnmarshalJSON 实现 encoding/json.Unmarshaler。
func (f *FlexString) UnmarshalJSON(body []byte) error {
	text := strings.TrimSpace(string(body))
	switch {
	case text == "" || text == "null":
		*f = ""
		return nil
	case strings.HasPrefix(text, `"`):
		var value string
		if err := json.Unmarshal(body, &value); err != nil {
			return err
		}
		*f = FlexString(value)
		return nil
	case text == "true":
		*f = "true"
		return nil
	case text == "false":
		*f = "false"
		return nil
	default:
		// 数字（含科学计数法）按 JS 的数字字面处理：原样保留即可，
		// 因为 provider 只做「是否等于某个字面量」与「是否为整数 ID」两类判断。
		*f = FlexString(text)
		return nil
	}
}

// String 返回底层字符串。
func (f FlexString) String() string { return string(f) }

// StrictBool 复刻 JS 的 `value === true`（严格判定，不做宽松转换）。
//
// 用在「解锁/免费」这类安全相关字段上：JS 侧写的是 `episode.unlock !== true`、
// `episode.isCharge !== '0'`，上游把字段类型从布尔改成字符串时，
// 严格比较会**判为锁定**（保守方向）。Go 侧若用 bool 反序列化，
// 遇到 "true" 会直接解析失败，把「锁定」误报成「源不可用」，方向正好相反。
type StrictBool bool

// UnmarshalJSON 实现 encoding/json.Unmarshaler：只有 JSON 的 true 才算 true。
func (b *StrictBool) UnmarshalJSON(body []byte) error {
	*b = StrictBool(strings.TrimSpace(string(body)) == "true")
	return nil
}

// Bool 返回 bool 形态。
func (b StrictBool) Bool() bool { return bool(b) }

// FlexInt 复刻 JS 的 Number(value)（取整后使用）：
// 数字按值、数字字符串按解析结果、其余（含 null/布尔/非法串）为 0。
type FlexInt int64

// UnmarshalJSON 实现 encoding/json.Unmarshaler。
func (f *FlexInt) UnmarshalJSON(body []byte) error {
	text := strings.TrimSpace(string(body))
	if text == "" || text == "null" || text == "true" || text == "false" {
		*f = 0
		return nil
	}
	text = strings.Trim(text, `"`)
	if value, err := strconv.ParseInt(text, 10, 64); err == nil {
		*f = FlexInt(value)
		return nil
	}
	if value, err := strconv.ParseFloat(text, 64); err == nil {
		*f = FlexInt(int64(value))
		return nil
	}
	*f = 0
	return nil
}

// Int 返回 int 形态。
func (f FlexInt) Int() int { return int(f) }

// Client 是 provider 的取数器。
type Client struct {
	// BaseURL 是站点根地址（测试里替换成 httptest 地址）。
	BaseURL string
	// Headers 是每个请求都带的头（站点身份：pname / Origin / Referer）。
	Headers map[string]string
	// UserAgent 为空时取 "Mozilla/5.0"（两个 JS 实现都是这个值）。
	UserAgent string
	// HTTP 为空时用 DefaultTimeout 的默认客户端。
	HTTP *http.Client
	// MaxBytes 为 0 时取 DefaultMaxBytes。
	MaxBytes int64
}

// New 返回一个 Client；httpClient 为空时用默认（10 秒超时、不跟随重定向）。
func New(baseURL string, headers map[string]string) *Client {
	copied := make(map[string]string, len(headers))
	for name, value := range headers {
		copied[name] = value
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Headers: copied}
}

// Get 发一次 GET（query 可为 nil），返回响应体。
func (c *Client) Get(ctx context.Context, apiPath string, query url.Values) ([]byte, error) {
	return c.do(ctx, http.MethodGet, apiPath, query, nil)
}

// Post 发一次 JSON POST。
func (c *Client) Post(ctx context.Context, apiPath string, payload any) ([]byte, error) {
	return c.do(ctx, http.MethodPost, apiPath, nil, payload)
}

// ResolveURL 拼出完整地址（便于测试断言与日志）。
func (c *Client) ResolveURL(apiPath string, query url.Values) string {
	address := c.BaseURL + apiPath
	if len(query) == 0 {
		return address
	}
	return address + "?" + query.Encode()
}

func (c *Client) do(ctx context.Context, method, apiPath string, query url.Values, payload any) ([]byte, error) {
	address := c.ResolveURL(apiPath, query)
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", firstNonEmpty(c.UserAgent, "Mozilla/5.0"))
	for name, value := range c.Headers {
		request.Header.Set(name, value)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
		if request.Header.Get("Accept") == "" {
			request.Header.Set("Accept", "application/json")
		}
	} else if request.Header.Get("Accept") == "" {
		request.Header.Set("Accept", "text/html")
	}
	client := c.HTTP
	if client == nil {
		// 与 axios 的 maxRedirects:0 对应：不跟随重定向（拿到 3xx 直接当失败）。
		client = &http.Client{Timeout: DefaultTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(request)
	if err != nil {
		if isTimeout(err) {
			return nil, fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return nil, err
	}
	defer response.Body.Close()
	limit := c.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		if isTimeout(err) {
			return nil, fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("响应超过 %d 字节上限", limit)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// 刻意不回传响应体：C 的自检脚本明确要求错误信息不得泄漏上游私有内容。
		return nil, fmt.Errorf("上游 HTTP %d", response.StatusCode)
	}
	return content, nil
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	type timeout interface{ Timeout() bool }
	var asTimeout timeout
	return errors.As(err, &asTimeout) && asTimeout.Timeout()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

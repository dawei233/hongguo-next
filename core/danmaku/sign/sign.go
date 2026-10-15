// Package sign 是红果弹幕评论接口的请求签名（X-Gorgon / X-Ladon / X-Argus）。
//
// 移植自 C（hongguo-drama，GPL-3.0，与本仓同许可）的
// src/native/danmaku-sign.js（385 行）与 src/native/sm3.js，后者又是自有仓
// nas-backend/danmaku.py 的 JavaScript 移植，而 Python 版又是 core 扁平包里
// provider_hongguo_comment_sign.go 的移植 —— 三层同源，同一个算法。
//
// # 正确性怎么证明
//
// 上游接口没有公开文档，唯一能证明移植正确的办法是拿已验证可用的参考实现在固定
// 输入下的输出逐字节比对。黄金向量在 core/danmaku/testdata/vectors.json，由
// scripts/gen-danmaku-sign-vectors.py 从 nas-backend/danmaku.py 生成（文件里的
// source 字段记录了这个来源），回归测试是 sign/sign_test.go。
//
// 改本包后先跑 sign 的测试（离线、不需要网络），再谈联网验证。测试失败时不要去
// 联网碰运气 —— 那说明移植已经与参考实现不一致了。
//
// # 本包的性质
//
// 纯函数、无 IO、无时间依赖：调用方把时间戳与 nonce 传进来（见 SignComment），
// 因此可以 100% 单测，也可以生成黄金向量。
//
// # 两条硬约束（来自 C 的工程注释）
//
//  1. 不得"顺手修正"参考实现里看起来像 bug 的写法：X-Gorgon 的 RC4 类密钥调度是
//     box[i] = box[j] 赋值而非交换（见 gorgon.go 与 TestXGorgonKSAMustNotSwap）。
//  2. 不得改变位运算的宽度与截断位置：全部 64 位运算按 uint64 回绕，X-Gorgon 的
//     时间戳按 32 位大端截断（见 Test64BitTruncation）。
package sign

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

// NonceBytes 是 nonce 的字节数。
//
// 各段分别喂给不同算法：nonce[0:2] 进 X-Gorgon，nonce[2:6] 进 X-Ladon
// （且作为它的明文前缀），nonce[6:11] 进 X-Argus。长度不对不会报错，只会算出
// 服务端拒绝的签名 —— 所以本包用定长数组 [NonceBytes]byte 让类型系统兜住它。
const NonceBytes = 11

// AidDefault 是弹幕接口使用的 aid（冒充官方客户端；改动会影响上游风控判定）。
const AidDefault = "8662"

// ErrInvalidParams 表示入参不足以算出签名。
var ErrInvalidParams = errors.New("弹幕签名参数无效")

// Params 是一次签名的全部输入。
//
// 时间戳有秒与毫秒两种精度，因为**签名对 query 字符串算**：query 里的 _rticket
// 是毫秒，而 X-Khronos 是秒；两者必须来自同一时刻，否则是无效签名
// （C 的 requestWindow 把三者在同一个函数里生成，正是这个原因）。
type Params struct {
	// RawQuery 是**已经编码好**的 query 字符串（不含 '?'），签名对它逐字节计算。
	RawQuery string
	// StampS 是秒级时间戳（X-Khronos）。
	StampS int64
	// StampMS 是毫秒级时间戳：**只用于 query 里的 _rticket**（签名对它逐字节计算）。
	// 注意 X-SS-Req-Ticket 不是它，而是秒 × 1000 —— 见 SignComment 的注释。
	StampMS int64
	// Aid 为空时取 AidDefault。
	Aid string
	// DeviceID / InstallID 是设备指纹（见 NewDeviceID）；仅用于 BuildQuery 的一致性检查。
	DeviceID  string
	InstallID string
	// Nonce 是 11 字节随机数。
	Nonce [NonceBytes]byte
}

// Headers 是签名产出的全部请求头，字段名即头部名。
type Headers struct {
	XArgus       string `json:"X-Argus"`
	XLadon       string `json:"X-Ladon"`
	XGorgon      string `json:"X-Gorgon"`
	XKhronos     string `json:"X-Khronos"`
	XSSReqTicket string `json:"X-SS-Req-Ticket"`
	XSSStub      string `json:"X-SS-STUB"`
}

// HeaderNames 是签名产出的头部名集合（升序）；测试用它断言"头部集合没有多也没有少"。
var HeaderNames = []string{"X-Argus", "X-Gorgon", "X-Khronos", "X-Ladon", "X-SS-Req-Ticket", "X-SS-STUB"}

// Map 返回头部名到取值的映射，便于直接塞进 http.Header。
func (h Headers) Map() map[string]string {
	return map[string]string{
		"X-Argus":         h.XArgus,
		"X-Ladon":         h.XLadon,
		"X-Gorgon":        h.XGorgon,
		"X-Khronos":       h.XKhronos,
		"X-SS-Req-Ticket": h.XSSReqTicket,
		"X-SS-STUB":       h.XSSStub,
	}
}

// NewNonce 生成 11 字节随机 nonce。
func NewNonce() ([NonceBytes]byte, error) {
	var nonce [NonceBytes]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nonce, fmt.Errorf("无法生成弹幕签名随机数: %w", err)
	}
	return nonce, nil
}

// NewDeviceID 生成 19 位数字的设备指纹。
//
// 与参考实现一致：1e18 + 随机数 % 8e18，因此恒为 19 位、恒以 1~8 开头。
// 官方客户端的行为是"安装一次生成一次"，调用方应当把结果持久化后再复用
// （core 的 hongguoCatalogState 就是这么做的）。
func NewDeviceID() (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("无法生成弹幕设备指纹: %w", err)
	}
	value := 1_000_000_000_000_000_000 + binary.BigEndian.Uint64(random[:])%8_000_000_000_000_000_000
	return strconv.FormatUint(value, 10), nil
}

// SignComment 产出全部签名头部。
//
// 校验规则为什么这么严：签名是对 query 字符串算的，而 query 里含 _rticket。
// 时间戳、nonce 与 query 三者只要有一个不是同一次请求构造的，算出来的就是服务端
// 会拒绝的签名 —— 失败时不会有异常，只会一直 4xx。所以这里宁可报错：
//
//   - RawQuery 不可为空（空 query 的签名恒定，等于没有签名）；
//   - 只给了毫秒时补出秒（向下取整），只给了秒时补出毫秒（×1000）；
//   - 两者都给了但互不自洽（stampMS/1000 != stampS）直接报错。
func SignComment(p Params) (Headers, error) {
	if p.RawQuery == "" {
		return Headers{}, fmt.Errorf("%w: raw query 为空", ErrInvalidParams)
	}
	switch {
	case p.StampS <= 0 && p.StampMS <= 0:
		return Headers{}, fmt.Errorf("%w: 缺少时间戳", ErrInvalidParams)
	case p.StampS <= 0:
		p.StampS = p.StampMS / 1000
	case p.StampMS <= 0:
		p.StampMS = p.StampS * 1000
	case p.StampMS/1000 != p.StampS:
		return Headers{}, fmt.Errorf("%w: 秒级时间戳 %d 与毫秒级 %d 不是同一时刻", ErrInvalidParams, p.StampS, p.StampMS)
	}
	aid := p.Aid
	if aid == "" {
		aid = AidDefault
	}

	argus, err := Argus(p.RawQuery, p.StampS, p.Nonce)
	if err != nil {
		return Headers{}, err
	}
	return Headers{
		XArgus:   argus,
		XLadon:   Ladon(aid, p.StampS, p.Nonce[2:6]),
		XGorgon:  XGorgon(p.RawQuery, p.StampS, p.Nonce),
		XKhronos: strconv.FormatInt(p.StampS, 10),
		// 秒 × 1000，**不是**毫秒原值：黄金向量第 3 组的 stamp_ms = 1234567890123
		// 而 x_ss_req_ticket = 1234567890000，两者刻意不同。
		// 参考实现（nas-backend/danmaku.py `_sign_comment_headers`）与 C 都这么写；
		// core 扁平包里的 signHongguoCommentRequest 用的是 now.UnixMilli()
		// （带亚秒位），与参考实现有差异，见 TestXSSReqTicketFollowsReferenceRule。
		XSSReqTicket: strconv.FormatInt(p.StampS*1000, 10),
		// 参考实现固定送空串；它不是"忘了填"，而是这个接口不接受 STUB。
		XSSStub: "",
	}, nil
}

// BaseParams 返回固定 query 参数（按键升序）。
//
// 这些值冒充官方 Android 客户端，改动会影响上游风控判定，非必要不要动 ——
// 它们与 User-Agent、签名头必须成套，只改一个就会露馅。
func BaseParams(deviceID, installID string) [][2]string {
	return [][2]string{
		{"ac", "wifi"},
		{"aid", AidDefault},
		{"app_name", "novelread"},
		{"channel", "update_64"},
		{"device_brand", "Redmi"},
		{"device_id", deviceID},
		{"device_platform", "android"},
		{"device_type", "25053RT47C"},
		{"dpi", "520"},
		{"iid", installID},
		{"language", "zh"},
		{"manifest_version_code", "73532"},
		{"os", "android"},
		{"os_api", "36"},
		{"os_version", "16"},
		{"resolution", "1280*2772"},
		{"ssmix", "a"},
		{"update_version_code", "73532"},
		{"version_code", "73532"},
		{"version_name", "7.3.5.32"},
	}
}

// BuildQuery 构造请求的 raw query（含 _rticket），键按字典序排序。
//
// 排序与编码都必须与参考实现一致，理由见 QuotePlus 的注释：签名是对这个字符串
// 逐字节算的。参数个数或顺序变了，签名就变了。
func BuildQuery(stampMS int64, deviceID, installID string) string {
	pairs := append(BaseParams(deviceID, installID), [2]string{"_rticket", strconv.FormatInt(stampMS, 10)})
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })
	out := make([]byte, 0, 512)
	for index, pair := range pairs {
		if index > 0 {
			out = append(out, '&')
		}
		out = append(out, QuotePlus(pair[0])...)
		out = append(out, '=')
		out = append(out, QuotePlus(pair[1])...)
	}
	return string(out)
}

// QuotePlus 复刻 Python urlencode/quote_plus 的转义规则。
//
// 为什么不能直接用 Go 的 url.QueryEscape 或 JS 的 encodeURIComponent：
// 前者行为其实等价，但它是标准库承诺之外的东西；后者**不转义** ! ' ( ) * ，
// 而 Python 只保留 A-Za-z0-9_.-~。少转义一个字符（例如分辨率里的 * 变成 %2A）
// 就会算出服务端拒绝的签名。这里把规则写死，附带单测对黄金向量回归。
func QuotePlus(value string) string {
	const hexDigits = "0123456789ABCDEF"
	out := make([]byte, 0, len(value))
	for index := 0; index < len(value); index++ {
		char := value[index]
		switch {
		case char >= 'A' && char <= 'Z', char >= 'a' && char <= 'z', char >= '0' && char <= '9',
			char == '_', char == '.', char == '-', char == '~':
			out = append(out, char)
		case char == ' ':
			out = append(out, '+')
		default:
			out = append(out, '%', hexDigits[char>>4], hexDigits[char&0x0f])
		}
	}
	return string(out)
}

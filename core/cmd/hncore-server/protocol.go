// hncore-server 的线协议：帧定界、请求头、响应信封。
//
// # 帧定界选型：行分隔（NDJSON），不用长度前缀
//
// 理由（按权重排序）：
//
//  1. 载荷本身就是 JSON，而 Go 的 encoding/json 把字符串里的换行强制转义成
//     \n（U+000A 在 JSON 字符串中不能裸出现）。因此编码后的载荷内**不可能**
//     出现裸 0x0A，行分隔天然无歧义 —— 不需要长度前缀来消除歧义。
//  2. 两端都是写代码的人（Electron 主进程 child_process、Go 子进程），行分隔
//     在两边都是一行 readline / bufio.ReadSlice('\n')，无需长度前缀的粘包处理
//     与半包状态机；排障时 stdio 日志可直接肉眼读。
//  3. 长度前缀的唯一硬优势是允许载荷内出现裸换行。代价是：PC 端每帧都要维护
//     半包缓冲 + 长度头校验，且 stdout 日志不可读。本仓的载荷形态
//     （nativeInput.action 请求 / {ok,data} 响应，均为机器生成 JSON）用不上
//     这个优势。
//  4. 与既有的「JSON 进 / JSON 出」ABI 心智一致：安卓侧 2 个 C 符号传的是
//     一整串 JSON，PC 侧只是把它按行切开，语义面相同。
//
// 行分隔的已知约束（写进契约文档，调用方必须遵守）：
//   - 载荷内不得出现**未转义**的裸换行（裸换行必须是 JSON 转义形态 \n）。裸换行
//     会被当成帧边界，从而把一帧切成两帧，各自被判为「请求格式无效」。
//   - 一帧必须以 \n 结尾；最后一帧可省略结尾 \n（EOF 时按整帧处理）。
//   - 容忍 CRLF（\r\n）：行尾的 \r 会被剥除。
//   - 容忍 UTF-8 BOM 与行首尾空白；空行（纯空白帧）直接跳过，不产生响应。
//   - 单帧上限见 maxFrameBytes（默认 1 MiB，与内核自身的请求上限对齐）。
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	// serverVersion 是 hncore-server 自身版本（与内核的 "0.2.17" 无关；
	// 内核版本由 initialize 动作的响应 data.version 给出）。
	serverVersion = "0.1.0"

	// protocolVersion 是 stdio 线协议版本，随 server:ping 一起上报。
	protocolVersion = 1

	// coreMaxRequestBytes 是内核自身的单请求上限：core/app_runtime.go 的
	// NativeRequest 中 `len(raw) > 1<<20` 即以「请求格式无效」拒绝。
	// 服务端的默认帧上限与之对齐：比它更大的帧内核必然拒绝，收下来只是白白
	// 占内存；而且提前在服务端拒绝，能把「超限」与「JSON 语法错误」区分开。
	coreMaxRequestBytes = 1 << 20

	defaultMaxFrameBytes = coreMaxRequestBytes
	defaultWorkers       = 4
	defaultMinInflight   = 32
	defaultGrace         = 5 * time.Second
)

// requestHeader 是服务端从帧里**只读不写**的桥接层面字段。
//
// 关键设计：除这些字段外，整帧原样（逐字节）交给 core.NativeRequest，
// 服务端不改写、不重排、不补默认值 —— nativeInput.action 面是冻结 ABI，
// 任何「顺手规范化」都可能改变内核行为。
//
// id / timeoutMs / server / targetId 都是内核 nativeInput 里不存在的字段；
// 内核用 encoding/json 默认的宽松解码（忽略未知字段），因此额外带上它们
// 不会影响内核解析。
type requestHeader struct {
	// ID 是调用方自选的请求标识（字符串或数字），原样回显到对应响应里，
	// 用于把乱序返回的响应与请求对上。省略则不回显。
	ID json.RawMessage `json:"id"`
	// Server 非空表示这是一条**服务端控制帧**（不转发给内核）。
	Server string `json:"server"`
	// TargetID 仅用于 server=cancel，指向要放弃等待的请求 id。
	TargetID json.RawMessage `json:"targetId"`
	// TimeoutMS 是本请求的服务端等待上限（毫秒）。0/省略表示用 -timeout 配置。
	TimeoutMS int64 `json:"timeoutMs"`
}

// okFrame / errorFrame 是 contracts/bridge-api.schema.json 的 okEnvelope /
// errorEnvelope。字段名、必填性、语义都按契约来：
//
//	okEnvelope    required: ok(const true), data
//	errorEnvelope required: ok(const false), error ; code / retryAt 可选
//
// 契约对两个信封都**没有** additionalProperties:false（原文：「允许出现契约未列
// 的扩展字段（向前兼容）」），所以回显 id 不违反契约。
//
// 内核返回的响应体不经过这两个结构体重新编码，而是原样透传（见 withID）：
// 唯一字节级保证 PC 端与安卓端（FFI 直取字符串）拿到完全相同的响应字节。
type okFrame struct {
	ID   json.RawMessage `json:"id,omitempty"`
	Ok   bool            `json:"ok"`
	Data json.RawMessage `json:"data"`
}

type errorFrame struct {
	ID    json.RawMessage `json:"id,omitempty"`
	Ok    bool            `json:"ok"`
	Error string          `json:"error"`
	Code  string          `json:"code,omitempty"`
}

// 契约 §$defs/errorEnvelope.code 的**完整**枚举。服务端只使用其中三个值，
// 见 errorCode... 常量；其余为契约预留（契约原文：本仓 vendored core 当前只
// 产生 local_media 与 source_blocked 两个码）。
const (
	codeInvalidRequest = "invalid_request"
	codeNotReady       = "not_ready"
	codeUpstreamError  = "upstream_error"
)

var errNotJSONObject = errors.New("帧不是 JSON 对象")

// parseHeader 解析桥接层字段。帧合法性判定：
//   - 必须是 JSON 对象（json.Unmarshal 对 "null"/"7" 之类不会报错，故显式检查首字节）；
//   - id 若出现，必须是字符串或数字（对象/数组/布尔会让回显失去唯一性）。
func parseHeader(frame []byte) (requestHeader, error) {
	var header requestHeader
	trimmed := bytes.TrimSpace(frame)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return header, errNotJSONObject
	}
	if !json.Valid(frame) {
		return header, errors.New("JSON 语法错误")
	}
	if err := json.Unmarshal(frame, &header); err != nil {
		return header, err
	}
	if len(header.ID) > 0 && !isIDValue(header.ID) {
		return header, errors.New("id 必须是字符串或数字")
	}
	if len(header.TargetID) > 0 && !isIDValue(header.TargetID) {
		return header, errors.New("targetId 必须是字符串或数字")
	}
	return header, nil
}

func isIDValue(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	switch trimmed[0] {
	case '"', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return json.Valid(trimmed)
	default:
		return false
	}
}

// idKey 把回显用的 id 原始 JSON 文本当成映射键（`"a"` 与 `7` 都是稳定形态）。
// 空 id 返回空串，表示「该请求不可被 server:cancel 定位」。
func idKey(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return string(bytes.TrimSpace(raw))
}

func encodeOK(data json.RawMessage, id json.RawMessage) []byte {
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	body, err := json.Marshal(okFrame{ID: id, Ok: true, Data: data})
	if err != nil {
		// 只可能因为 data 不是合法 JSON，而 data 全部由本文件构造。
		return encodeError("内核服务内部错误：响应载荷无法编码", codeUpstreamError, id)
	}
	return body
}

func encodeData(value any, id json.RawMessage) []byte {
	body, err := json.Marshal(value)
	if err != nil {
		return encodeError("内核服务内部错误：响应载荷无法编码", codeUpstreamError, id)
	}
	return encodeOK(body, id)
}

func encodeError(message, code string, id json.RawMessage) []byte {
	body, err := json.Marshal(errorFrame{ID: id, Ok: false, Error: message, Code: code})
	if err != nil {
		// error 是普通字符串，正常不会失败；兜底给出手写的最小信封。
		return []byte(`{"ok":false,"error":"内核服务内部错误"}`)
	}
	return body
}

// withID 把回显 id 拼进内核已编码好的响应**文本**里，而不是重新编码整个对象。
//
// 这样做是为了字节级保真：重新编码会走 json.Marshal 的 compact + HTML 转义，
// 把 <、>、& 改成 \u003c 之类，PC 端看到的响应就与安卓端（FFI 直接拿内核返回的
// 字符串）不再逐字节一致。拼接只动最外层对象的第一个 '{'，内核的键序与全部
// 载荷字节都被保留。
func withID(payload []byte, id json.RawMessage) ([]byte, error) {
	if len(id) == 0 {
		return payload, nil
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return nil, fmt.Errorf("内核响应不是合法 JSON 对象: %s", truncateForLog(payload))
	}
	rest := bytes.TrimLeft(trimmed[1:], " \t\r\n")
	out := make([]byte, 0, len(payload)+len(id)+8)
	out = append(out, `{"id":`...)
	out = append(out, id...)
	if len(rest) > 0 && rest[0] == '}' {
		out = append(out, '}')
	} else {
		out = append(out, ',')
		out = append(out, rest...)
	}
	if !json.Valid(out) {
		return nil, errors.New("拼接 id 后响应不再是合法 JSON")
	}
	return out, nil
}

// truncateForLog 限制日志里的载荷片段长度，避免把 1 MiB 的响应灌进 stderr。
func truncateForLog(payload []byte) string {
	const limit = 512
	if len(payload) <= limit {
		return string(payload)
	}
	return string(payload[:limit]) + "…(已截断)"
}

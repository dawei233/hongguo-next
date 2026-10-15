package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// sharedDir 是给「真内核」用例共用的应用数据目录。
//
// 为什么不用 t.TempDir()：core.NativeRequest 的 initialize 是**进程级幂等**的
// （nativeState.engine 一旦建立就不再重建），第二次 initialize 只回版本号。
// 若用 t.TempDir()，第一个用例结束时目录被删，而引擎仍指着它，后续用例就会
// 打在一个已消失的目录上。故在 TestMain 里建一次、跑完统一删。
var sharedDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hncore-server-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法创建测试目录: %v\n", err)
		os.Exit(1)
	}
	sharedDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func itoa(v int) string { return strconv.Itoa(v) }

func jsonString(value string) string {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(body)
}

// ---------------------------------------------------------------------------
// 测试夹具：用 io.Pipe 假造 stdin/stdout 来驱动服务端。
//
// 服务端与测试跑在同一个进程里（run/in/out 全是注入的），因此既真正走完
// 「读帧 → 并发 → 写帧 → 优雅退出」全流程，又不需要 go build 出二进制。
// ---------------------------------------------------------------------------

type harness struct {
	t            *testing.T
	in           *io.PipeWriter
	responses    chan []byte
	finished     chan error
	runErr       error
	stopOnce     sync.Once
	finishedOnce sync.Once
}

func startServer(t *testing.T, cfg config) *harness {
	t.Helper()
	if cfg.Logger == nil {
		cfg.Logger = log.New(testLogWriter{t}, "", 0)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h := &harness{
		t:         t,
		in:        inW,
		responses: make(chan []byte, 256),
		finished:  make(chan error, 1),
	}
	go func() { h.finished <- run(context.Background(), inR, outW, cfg) }()
	go func() {
		defer close(h.responses)
		reader := bufio.NewReader(outR)
		for {
			line, err := reader.ReadBytes('\n')
			if line = bytes.TrimRight(line, "\r\n"); len(line) > 0 {
				h.responses <- line
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		h.closeInput()
		h.waitFinished(5 * time.Second)
		_ = outR.Close()
	})
	return h
}

type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Logf("服务端日志: %s", bytes.TrimRight(p, "\n"))
	return len(p), nil
}

// gate 是「卡住内核调用」的闸门：entered 在第一次被调用时关闭，open 放行。
//
// open 是幂等的，并且通过 t.Cleanup 自动注册：断言失败（t.Fatalf → Goexit）
// 时如果不放行，被卡住的调用会一直占着工作槽，清理阶段就会白等超时。
type gate struct {
	release     chan struct{}
	openOnce    sync.Once
	entered     chan struct{}
	enteredOnce sync.Once
	enteredMu   sync.Mutex
	calls       int
}

func newGate(t *testing.T) *gate {
	g := &gate{release: make(chan struct{}), entered: make(chan struct{})}
	t.Cleanup(func() { g.open() })
	return g
}

func (g *gate) open()             { g.openOnce.Do(func() { close(g.release) }) }
func (g *gate) block()            { <-g.release }
func (g *gate) wait(t *testing.T) { t.Helper(); g.waitFor(t, 3*time.Second) }
func (g *gate) waitFor(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(d):
		t.Fatalf("内核调用在 %s 内没有被发起", d)
	}
}

func (g *gate) handler() string {
	g.enteredMu.Lock()
	g.calls++
	g.enteredOnce.Do(func() { close(g.entered) })
	g.enteredMu.Unlock()
	g.block()
	return `{"ok":true,"data":{"late":true}}`
}

// send 写一帧（自动补换行）。写管道是同步的，故加超时防止测试挂死。
func (h *harness) send(line string) {
	h.t.Helper()
	h.sendRaw([]byte(line + "\n"))
}

func (h *harness) sendRaw(payload []byte) {
	h.t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := h.in.Write(payload)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.ErrClosedPipe) {
			h.t.Fatalf("写请求失败: %v", err)
		}
	case <-time.After(5 * time.Second):
		h.t.Fatalf("写请求超时（服务端可能已停止读取）: %s", truncateForLog(payload))
	}
}

func (h *harness) closeInput() {
	h.stopOnce.Do(func() { _ = h.in.Close() })
}

// waitFinished 取 run 的返回值；可重复调用（清理阶段会再调一次）。
func (h *harness) waitFinished(timeout time.Duration) error {
	h.t.Helper()
	h.finishedOnce.Do(func() {
		select {
		case err := <-h.finished:
			h.runErr = err
		case <-time.After(timeout):
			h.t.Errorf("服务端未在 %s 内退出", timeout)
		}
	})
	return h.runErr
}

// next 取一条响应（超时即失败）。
func (h *harness) next(timeout time.Duration) []byte {
	h.t.Helper()
	select {
	case line, ok := <-h.responses:
		if !ok {
			h.t.Fatalf("响应通道已关闭，服务端提前退出")
		}
		return line
	case <-time.After(timeout):
		h.t.Fatalf("在 %s 内没有收到响应", timeout)
		return nil
	}
}

// nextWithID 取响应直到拿到指定 id 的那条（中途其它 id 的响应被跳过）。
func (h *harness) nextWithID(id string, timeout time.Duration) []byte {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			h.t.Fatalf("在 %s 内没有收到 id=%s 的响应", timeout, id)
		}
		line := h.next(remaining)
		if idOf(line) == id {
			return line
		}
	}
}

// quiet 断言在给定时间内没有更多响应（用于验证「不重复响应」）。
func (h *harness) quiet(d time.Duration) {
	h.t.Helper()
	select {
	case line, ok := <-h.responses:
		if ok {
			h.t.Fatalf("收到了预期之外的额外响应: %s", line)
		}
	case <-time.After(d):
	}
}

func idOf(line []byte) string {
	var probe struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(line, &probe) != nil {
		return ""
	}
	return strings.Trim(string(probe.ID), `"`)
}

func envelope(t *testing.T, line []byte) map[string]json.RawMessage {
	t.Helper()
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(line, &parsed); err != nil {
		t.Fatalf("响应不是 JSON 对象: %v (%s)", err, line)
	}
	if _, ok := parsed["ok"]; !ok {
		t.Fatalf("响应缺少契约必填字段 ok: %s", line)
	}
	return parsed
}

func envelopeOK(t *testing.T, line []byte) map[string]json.RawMessage {
	t.Helper()
	parsed := envelope(t, line)
	if string(parsed["ok"]) != "true" {
		t.Fatalf("期望 ok=true，实际: %s", line)
	}
	if _, ok := parsed["data"]; !ok {
		t.Fatalf("成功信封缺少契约必填字段 data: %s", line)
	}
	return parsed
}

// envelopeError 校验失败信封的契约形状（ok=false + 非空 error + code 若出现
// 必须在契约枚举内），并返回文案与 code。
func envelopeError(t *testing.T, line []byte) (message string, code string) {
	t.Helper()
	parsed := envelope(t, line)
	if string(parsed["ok"]) != "false" {
		t.Fatalf("期望 ok=false，实际: %s", line)
	}
	raw, ok := parsed["error"]
	if !ok || len(raw) == 0 || string(raw) == `""` {
		t.Fatalf("失败信封缺少契约必填字段 error（且不得为空串）: %s", line)
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatalf("error 不是字符串: %v (%s)", err, line)
	}
	if rawCode, exists := parsed["code"]; exists {
		if err := json.Unmarshal(rawCode, &code); err != nil {
			t.Fatalf("code 不是字符串: %v (%s)", err, line)
		}
		assertCodeInContract(t, code)
	}
	return message, code
}

// assertCodeInContract 把响应用到的 code 钉在 contracts/bridge-api.schema.json
// 的枚举里：契约是跨端稳定面，服务端不得自造新码。
func assertCodeInContract(t *testing.T, code string) {
	t.Helper()
	switch code {
	case "invalid_request", "not_ready", "source_unsupported", "source_blocked",
		"local_media", "expired", "upstream_error":
	default:
		t.Fatalf("响应带了契约枚举之外的 code: %q", code)
	}
}

func dataField(t *testing.T, line []byte, path ...string) json.RawMessage {
	t.Helper()
	parsed := envelopeOK(t, line)
	current := parsed["data"]
	for _, key := range path {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(current, &object); err != nil {
			t.Fatalf("data 不是对象，无法取 %v: %v (%s)", path, err, line)
		}
		next, ok := object[key]
		if !ok {
			t.Fatalf("data 缺少字段 %q: %s", key, line)
		}
		current = next
	}
	return current
}

// ---------------------------------------------------------------------------
// 正常路径
// ---------------------------------------------------------------------------

func TestPassThroughKeepsKernelBytesAndEchoesID(t *testing.T) {
	const kernelBody = `{"ok":true,"data":{"title":"短剧<>&","url":"https://a/b?x=1&y=2"}}`
	h := startServer(t, config{Handler: func(string) string { return kernelBody }})

	// 带 id：只应在最外层插入 id；内核的键序与字节（尤其是 < > & 不得被转成
	// \u003c 之类）必须原样保留。
	h.send(`{"id":"a","action":"detail"}`)
	got := h.next(time.Second)
	if want := `{"id":"a",` + kernelBody[1:]; string(got) != want {
		t.Fatalf("响应被改写：\n got=%s\nwant=%s", got, want)
	}

	// 不带 id：应与安卓端 FFI 拿到的内核返回字符串逐字节相同。
	h.send(`{"action":"detail"}`)
	got = h.next(time.Second)
	if string(got) != kernelBody {
		t.Fatalf("无 id 响应未逐字节透传：\n got=%s\nwant=%s", got, kernelBody)
	}
	if bytes.Contains(got, []byte(`"id"`)) {
		t.Fatalf("调用方没给 id，响应里不应出现 id: %s", got)
	}

	// 数字 id 也要原样回显（不得字符串化）。
	h.send(`{"id":7,"action":"detail"}`)
	if got := h.next(time.Second); string(got) != `{"id":7,`+kernelBody[1:] {
		t.Fatalf("数字 id 未原样回显: %s", got)
	}
}

func TestKernelRequestIsPassedVerbatim(t *testing.T) {
	var received []string
	var mu sync.Mutex
	h := startServer(t, config{Handler: func(raw string) string {
		mu.Lock()
		received = append(received, raw)
		mu.Unlock()
		return `{"ok":true,"data":null}`
	}})

	// 请求帧必须逐字节到达内核：不补默认值、不重排键、不剥未知字段
	// （nativeInput.action 面是冻结 ABI；id/timeoutMs 由内核按未知字段忽略）。
	const frame = `{"id":"x","action":"resolve","quality":1080,"session":"s-1","timeoutMs":1234,"未知字段":{"中文":1}}`
	h.send(frame)
	h.next(time.Second)

	// 带 BOM、CRLF、行首缩进的帧，内核拿到的应是规范化后的载荷。
	h.sendRaw([]byte("\xEF\xBB\xBF" + `  {"action":"catalog","source":"hongguo"}` + "\r\n"))
	h.next(time.Second)

	// 最后一帧没有结尾换行，也必须被处理（EOF 时按整帧收）。
	h.sendRaw([]byte(`{"action":"storage"}`))
	h.closeInput()
	h.waitFinished(5 * time.Second)
	h.next(time.Second)

	mu.Lock()
	defer mu.Unlock()
	want := []string{
		frame,
		`{"action":"catalog","source":"hongguo"}`,
		`{"action":"storage"}`,
	}
	if len(received) != len(want) {
		t.Fatalf("内核收到的请求数不对: %d (%q)", len(received), received)
	}
	for i := range want {
		if received[i] != want[i] {
			t.Fatalf("第 %d 个请求被改写：\n got=%q\nwant=%q", i, received[i], want[i])
		}
	}
}

func TestBlankLinesAreSkipped(t *testing.T) {
	h := startServer(t, config{Handler: func(string) string { return `{"ok":true,"data":null}` }})
	h.sendRaw([]byte("\n\r\n   \n\t\n"))
	h.send(`{"action":"x"}`)
	h.next(time.Second)
	h.quiet(150 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// 帧容错与上限
// ---------------------------------------------------------------------------

func TestMalformedFramesAreRejectedAndStreamStaysInSync(t *testing.T) {
	h := startServer(t, config{Handler: func(raw string) string {
		return `{"ok":true,"data":{"echo":` + jsonString(raw) + `}}`
	}})

	cases := []struct {
		name  string
		frame string
	}{
		{"不是 JSON", `not json at all`},
		{"截断的 JSON", `{"action":`},
		{"数组不是对象", `[1,2,3]`},
		{"字符串不是对象", `"action"`},
		{"null 不是对象", `null`},
		{"id 是对象", `{"id":{"a":1},"action":"x"}`},
		{"id 是布尔", `{"id":true,"action":"x"}`},
		{"targetId 是数组", `{"id":"1","targetId":[1],"server":"cancel"}`},
	}
	for _, testCase := range cases {
		h.send(testCase.frame)
		message, code := envelopeError(t, h.next(2*time.Second))
		if code != codeInvalidRequest {
			t.Fatalf("%s：期望 code=invalid_request，实际 %q（%s）", testCase.name, code, message)
		}
		if !strings.Contains(message, "请求格式无效") {
			t.Fatalf("%s：错误文案应指明格式无效，实际 %q", testCase.name, message)
		}
	}

	// 非法 UTF-8（被截断的多字节序列）走字节路径。
	h.sendRaw([]byte("{\"action\":\"x\",\"pad\":\"\xC3\"}\n"))
	if _, code := envelopeError(t, h.next(2*time.Second)); code != codeInvalidRequest {
		t.Fatalf("非法 UTF-8 应回 invalid_request，实际 %q", code)
	}

	// 任何一帧出错之后，流都必须仍然同步：正常请求照常工作。
	h.send(`{"action":"ping"}`)
	envelopeOK(t, h.next(time.Second))
}

func TestOversizeFrameIsRejectedWithoutLosingFollowingFrames(t *testing.T) {
	const maxBytes = 256
	h := startServer(t, config{MaxFrameBytes: maxBytes, Handler: func(raw string) string {
		return `{"ok":true,"data":{"len":` + itoa(len(raw)) + `}}`
	}})

	// 恰好 maxBytes：合规（边界含入）。
	exact := paddedFrame(maxBytes)
	h.send(exact)
	if got := string(dataField(t, h.next(time.Second), "len")); got != itoa(maxBytes) {
		t.Fatalf("边界帧未被接受: len=%s", got)
	}

	// maxBytes+1：拒绝，且必须把这一行完整丢弃（否则残留字节会被当成新请求）。
	h.send(paddedFrame(maxBytes + 1))
	message, code := envelopeError(t, h.next(2*time.Second))
	if code != codeInvalidRequest || !strings.Contains(message, "请求过大") {
		t.Fatalf("超限帧应回 invalid_request/请求过大，实际 code=%q message=%q", code, message)
	}

	// 超长帧后面紧跟正常请求：流已同步，正常请求必须照常执行。
	const after = `{"action":"after-oversize"}`
	h.send(after)
	if got := string(dataField(t, h.next(time.Second), "len")); got != itoa(len(after)) {
		t.Fatalf("超限之后的请求未被正确处理: len=%s", got)
	}

	// 远大于读缓冲的超限帧（跨 ReadSlice 多次分片）也不能撑爆内存、不能失步。
	h.send(paddedFrame(1 << 20))
	if _, code := envelopeError(t, h.next(2*time.Second)); code != codeInvalidRequest {
		t.Fatalf("巨型超限帧应回 invalid_request，实际 %q", code)
	}
	h.send(`{"action":"still-alive"}`)
	envelopeOK(t, h.next(time.Second))
}

// paddedFrame 造一帧长度恰为 size 的合法 JSON。
func paddedFrame(size int) string {
	const prefix = `{"action":"pad","pad":"`
	const suffix = `"}`
	padding := size - len(prefix) - len(suffix)
	if padding < 0 {
		panic("size too small")
	}
	return prefix + strings.Repeat("p", padding) + suffix
}

// ---------------------------------------------------------------------------
// 并发、顺序、超时、取消
// ---------------------------------------------------------------------------

type probeRequest struct {
	ProbeID int    `json:"probeId"`
	Delay   int    `json:"delayMs"`
	Action  string `json:"action"`
}

func delayHandler(raw string) string {
	var probe probeRequest
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return `{"ok":false,"error":"夹具解析失败"}`
	}
	if probe.Delay > 0 {
		time.Sleep(time.Duration(probe.Delay) * time.Millisecond)
	}
	return `{"ok":true,"data":{"probeId":` + itoa(probe.ProbeID) + `}}`
}

// TestConcurrentRequestsMayReturnOutOfOrder 证明并发是真并发：6 个请求的
// 仿真耗时依次递减，前 3 个（并发额度 3）必然以相反顺序返回。
func TestConcurrentRequestsMayReturnOutOfOrder(t *testing.T) {
	h := startServer(t, config{Workers: 3, Handler: delayHandler})
	const total = 6
	for i := 0; i < total; i++ {
		h.send(fmt.Sprintf(`{"id":%d,"probeId":%d,"delayMs":%d,"action":"x"}`, i, i, (total-i)*40))
	}
	order := make([]string, 0, total)
	for i := 0; i < total; i++ {
		order = append(order, idOf(h.next(5*time.Second)))
	}
	// 第一批（id 0/1/2，耗时 240/200/160 ms）按耗时倒序返回。
	want := []string{"2", "1", "0"}
	for i, id := range want {
		if order[i] != id {
			t.Fatalf("并发未生效，响应顺序为 %v（期望前三为 %v）", order, want)
		}
	}
	seen := map[string]bool{}
	for _, id := range order {
		if seen[id] {
			t.Fatalf("同一 id 收到多次响应: %v", order)
		}
		seen[id] = true
	}
}

// TestSingleWorkerKeepsArrivalOrder：-workers 1 时响应顺序与请求到达顺序一致，
// 给「不想处理乱序」的调用方一条退路。
func TestSingleWorkerKeepsArrivalOrder(t *testing.T) {
	h := startServer(t, config{Workers: 1, Handler: delayHandler})
	const total = 4
	for i := 0; i < total; i++ {
		h.send(fmt.Sprintf(`{"id":%d,"probeId":%d,"delayMs":%d,"action":"x"}`, i, i, (total-i)*20))
	}
	for i := 0; i < total; i++ {
		if got := idOf(h.next(5 * time.Second)); got != itoa(i) {
			t.Fatalf("串行模式第 %d 条响应 id=%s，顺序被破坏", i, got)
		}
	}
}

func TestPerRequestTimeoutReturnsExactlyOneEnvelope(t *testing.T) {
	blocking := newGate(t)
	h := startServer(t, config{Handler: func(string) string { return blocking.handler() }})

	h.send(`{"id":"slow","action":"x","timeoutMs":60}`)
	blocking.wait(t)
	message, code := envelopeError(t, h.next(3*time.Second))
	if !strings.Contains(message, "请求超时") {
		t.Fatalf("期望超时错误，实际 %q", message)
	}
	// 超时属于「内核未按时返回」的兜底情形，契约枚举里只有 upstream_error
	// 这一档兜底码可用（服务端不擅自扩枚举）。
	if code != codeUpstreamError {
		t.Fatalf("超时应回 upstream_error，实际 %q", code)
	}

	// 内核稍后返回也不能再补一条响应。
	blocking.open()
	h.quiet(300 * time.Millisecond)
}

func TestServerCancelAbandonsPendingRequest(t *testing.T) {
	blocking := newGate(t)
	h := startServer(t, config{Handler: func(string) string { return blocking.handler() }})

	h.send(`{"id":"slow","action":"x"}`)
	blocking.wait(t)

	h.send(`{"id":"ctl","server":"cancel","targetId":"slow"}`)
	ack := h.next(3 * time.Second)
	envelopeOK(t, ack)
	if got := string(dataField(t, ack, "canceled")); got != "true" {
		t.Fatalf("取消应答异常: %s", ack)
	}

	message, code := envelopeError(t, h.nextWithID("slow", 3*time.Second))
	if !strings.Contains(message, "取消") {
		t.Fatalf("目标请求应收到「已取消」，实际 %q", message)
	}
	// 契约的 code 枚举里没有「已取消」这一档；code 本身可省略，故如实省略，
	// 不去借用语义不符的码（见 README「契约一致性」）。
	if code != "" {
		t.Fatalf("取消不应借用语义不符的 code，实际 %q", code)
	}

	// 取消之后同一个 id 可以立刻复用（不会被「id 重复」卡住）。
	h.send(`{"id":"slow","server":"ping"}`)
	envelopeOK(t, h.next(2*time.Second))

	blocking.open()
	h.quiet(300 * time.Millisecond)
}

func TestCancelOfUnknownTargetIsRejected(t *testing.T) {
	h := startServer(t, config{Handler: delayHandler})
	h.send(`{"id":"ctl","server":"cancel","targetId":"不存在"}`)
	message, code := envelopeError(t, h.next(2*time.Second))
	if code != codeInvalidRequest || !strings.Contains(message, "未找到") {
		t.Fatalf("取消未知目标应回 invalid_request/未找到，实际 code=%q message=%q", code, message)
	}
	h.send(`{"server":"cancel"}`)
	if _, code := envelopeError(t, h.next(2*time.Second)); code != codeInvalidRequest {
		t.Fatalf("缺 targetId 应回 invalid_request，实际 %q", code)
	}
}

func TestDuplicateIDWhileInflightIsRejected(t *testing.T) {
	blocking := newGate(t)
	h := startServer(t, config{Workers: 2, MaxInflight: 8, Handler: func(string) string { return blocking.handler() }})

	h.send(`{"id":"same","action":"x"}`)
	blocking.wait(t)
	h.send(`{"id":"same","action":"x"}`)
	message, code := envelopeError(t, h.next(3*time.Second))
	if code != codeInvalidRequest || !strings.Contains(message, "仍在处理中") {
		t.Fatalf("重复 id 应被拒绝，实际 code=%q message=%q", code, message)
	}
	blocking.open()
	envelopeOK(t, h.nextWithID("same", 3*time.Second))
}

func TestMaxInflightRefusesExtraWork(t *testing.T) {
	blocking := newGate(t)
	h := startServer(t, config{Workers: 1, MaxInflight: 1, Handler: func(string) string { return blocking.handler() }})

	h.send(`{"id":1,"action":"x"}`)
	blocking.wait(t)
	h.send(`{"id":2,"action":"x"}`)
	line := h.nextWithID("2", 3*time.Second)
	// 「服务繁忙」借用兜底码 upstream_error（契约枚举里没有 busy 档）。
	if message, code := envelopeError(t, line); !strings.Contains(message, "繁忙") || code != codeUpstreamError {
		t.Fatalf("超过在途上限应回「繁忙」+upstream_error，实际 code=%q message=%q", code, message)
	}
	blocking.open()
	envelopeOK(t, h.nextWithID("1", 3*time.Second))
}

// ---------------------------------------------------------------------------
// panic 隔离与优雅退出
// ---------------------------------------------------------------------------

func TestHandlerPanicDoesNotKillServer(t *testing.T) {
	h := startServer(t, config{Handler: func(raw string) string {
		if strings.Contains(raw, "boom") {
			panic("内核内部炸了")
		}
		return `{"ok":true,"data":{"alive":true}}`
	}})

	h.send(`{"id":"p","action":"boom"}`)
	line := h.next(3 * time.Second)
	message, code := envelopeError(t, line)
	if !strings.Contains(message, "处理异常") {
		t.Fatalf("panic 应回错误信封，实际 %q", message)
	}
	if code != "" {
		t.Fatalf("panic 没有对应的契约码，应省略 code，实际 %q", code)
	}
	if idOf(line) != "p" {
		t.Fatalf("panic 响应未回显 id: %s", line)
	}

	// 服务端必须还活着。
	h.send(`{"id":"after","action":"ok"}`)
	envelopeOK(t, h.next(2*time.Second))
}

func TestGracefulExitOnInputEOFDeliversInflightResponse(t *testing.T) {
	h := startServer(t, config{Grace: 2 * time.Second, Handler: func(string) string {
		time.Sleep(200 * time.Millisecond)
		return `{"ok":true,"data":{"finished":true}}`
	}})

	h.send(`{"id":"inflight","action":"x"}`)
	// 客户端立刻关掉管道（模拟「提前关管」）：在途请求仍应在宽限期内写完响应。
	h.closeInput()
	if err := h.waitFinished(5 * time.Second); err != nil {
		t.Fatalf("EOF 退出应视为正常收尾，实际返回 %v", err)
	}
	line := h.next(2 * time.Second)
	envelopeOK(t, line)
	if idOf(line) != "inflight" {
		t.Fatalf("收尾响应 id 不对: %s", line)
	}
}

func TestShutdownControlFrameExitsCleanly(t *testing.T) {
	h := startServer(t, config{Grace: 3 * time.Second, Handler: delayHandler})

	h.send(`{"id":"slow","probeId":1,"delayMs":150,"action":"x"}`)
	h.send(`{"id":"bye","server":"shutdown"}`)
	envelopeOK(t, h.nextWithID("bye", 3*time.Second))
	envelopeOK(t, h.nextWithID("slow", 5*time.Second))

	if err := h.waitFinished(5 * time.Second); err != nil {
		t.Fatalf("shutdown 应正常退出，实际 %v", err)
	}
}

func TestExitWhenClientStopsReadingStdout(t *testing.T) {
	cfg := config{Handler: func(string) string { return `{"ok":true,"data":null}` }}
	cfg.Logger = log.New(testLogWriter{t}, "", 0)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	finished := make(chan error, 1)
	go func() { finished <- run(context.Background(), inR, outW, cfg) }()

	// 读端先消失：写 stdout 立刻变成错误。服务端应把它当成「对端没了」，
	// 停止接收、收尾、以非 0 退出码返回，而不是挂死或 panic。
	_ = outR.Close()
	go func() {
		_, _ = inW.Write([]byte(`{"id":1,"action":"x"}` + "\n"))
		time.Sleep(80 * time.Millisecond)
		_ = inW.Close()
	}()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("stdout 写失败应返回非 nil（对应非 0 退出码）")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stdout 断开后服务端没有退出")
	}
}

func TestQueuedWorkIsStillServedDuringGracefulStop(t *testing.T) {
	// workers=1：第一条占着工作槽，第二条已准入但停在「等槽」上。此时收到
	// shutdown，第二条**不应**被丢掉 —— 它属于「在途请求」，宽限期就是给它
	// 的；两条响应都必须落盘，然后进程才退出。
	blocking := newGate(t)
	h := startServer(t, config{Workers: 1, Grace: 3 * time.Second, Handler: func(string) string {
		return blocking.handler()
	}})

	h.send(`{"id":"running","action":"x"}`)
	blocking.wait(t)
	h.send(`{"id":"queued","action":"x"}`)
	time.Sleep(100 * time.Millisecond)
	h.send(`{"id":"stop","server":"shutdown"}`)
	envelopeOK(t, h.nextWithID("stop", 3*time.Second))

	blocking.open()
	envelopeOK(t, h.nextWithID("running", 5*time.Second))
	envelopeOK(t, h.nextWithID("queued", 5*time.Second))
	if err := h.waitFinished(5 * time.Second); err != nil {
		t.Fatalf("shutdown 应正常退出，实际 %v", err)
	}
}

func TestGracePeriodExpiryStillExits(t *testing.T) {
	// 内核调用超出宽限期时，进程也必须退出（只记录警告），不能挂死。
	blocking := newGate(t)
	h := startServer(t, config{Grace: 300 * time.Millisecond, Handler: func(string) string {
		return blocking.handler()
	}})
	h.send(`{"id":"stuck","action":"x"}`)
	blocking.wait(t)
	h.closeInput()
	if err := h.waitFinished(5 * time.Second); err != nil {
		t.Fatalf("超期退出应视为正常收尾，实际 %v", err)
	}
}

// ---------------------------------------------------------------------------
// 控制帧与参数
// ---------------------------------------------------------------------------

func TestPingReportsServerFacts(t *testing.T) {
	h := startServer(t, config{Workers: 2, MaxFrameBytes: 4096, Handler: delayHandler})
	h.send(`{"id":"p1","server":"ping"}`)
	line := h.next(2 * time.Second)
	envelopeOK(t, line)
	if got := string(dataField(t, line, "pong")); got != "true" {
		t.Fatalf("ping 应答异常: %s", line)
	}
	if got := string(dataField(t, line, "serverVersion")); got != jsonString(serverVersion) {
		t.Fatalf("serverVersion 异常: %s", got)
	}
	if got := string(dataField(t, line, "workers")); got != "2" {
		t.Fatalf("workers 异常: %s", got)
	}
	if got := string(dataField(t, line, "maxFrameBytes")); got != "4096" {
		t.Fatalf("maxFrameBytes 异常: %s", got)
	}
}

func TestUnknownServerCommandIsRejected(t *testing.T) {
	h := startServer(t, config{Handler: delayHandler})
	h.send(`{"id":"c","server":"不存在的指令"}`)
	message, code := envelopeError(t, h.next(2*time.Second))
	if code != codeInvalidRequest || !strings.Contains(message, "不支持的服务端指令") {
		t.Fatalf("未知控制指令应回 invalid_request，实际 code=%q message=%q", code, message)
	}
}

func TestFlags(t *testing.T) {
	cfg, version, err := parseFlags(nil, io.Discard)
	if err != nil || version {
		t.Fatalf("默认参数解析失败: %v", err)
	}
	if cfg.MaxFrameBytes != defaultMaxFrameBytes || cfg.Workers != defaultWorkers ||
		cfg.MaxInflight != 0 || cfg.RequestTimeout != 0 || cfg.Grace != defaultGrace {
		t.Fatalf("默认值不对: %+v", cfg)
	}

	cfg, _, err = parseFlags([]string{"-workers", "1", "-timeout", "90s", "-max-bytes", "2048", "-grace", "0"}, io.Discard)
	if err != nil {
		t.Fatalf("参数解析失败: %v", err)
	}
	if cfg.Workers != 1 || cfg.RequestTimeout != 90*time.Second || cfg.MaxFrameBytes != 2048 || cfg.Grace != 0 {
		t.Fatalf("参数未生效: %+v", cfg)
	}

	for _, args := range [][]string{
		{"-workers", "0"}, {"-max-bytes", "0"}, {"-max-bytes", "-1"},
		{"-timeout", "-1s"}, {"-grace", "-1s"}, {"-max-inflight", "-2"},
		{"位置参数"}, {"-不存在的开关"},
	} {
		if _, _, err := parseFlags(args, io.Discard); err == nil {
			t.Fatalf("参数 %v 应被拒绝", args)
		}
	}

	if _, version, err := parseFlags([]string{"-version"}, io.Discard); err != nil || !version {
		t.Fatalf("-version 应返回 version=true: %v %v", version, err)
	}
}

func TestEntryVersionAndUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := entry([]string{"-version"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("-version 退出码应为 0，实际 %d", code)
	}
	if !strings.Contains(out.String(), serverVersion) {
		t.Fatalf("版本输出异常: %q", out.String())
	}
	if code := entry([]string{"-workers", "0"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("非法参数退出码应为 2，实际 %d", code)
	}
	// 空 stdin（立刻 EOF）是正常收尾：退出码 0。
	if code := entry(nil, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("空 stdin 应立即正常退出（0），实际 %d", code)
	}
}

// ---------------------------------------------------------------------------
// 与真内核对接（Handler 缺省 = core.NativeRequest）
// ---------------------------------------------------------------------------

// helperProcessEnv 是「新进程断言」用的开关：内核的 initialize 是**进程级幂等**
// 且无法重置的（nativeState 是 package 级变量），所以「未初始化时该怎么办」这类
// 断言只有在全新进程里才可复现；-count>1 时同一个进程会重复跑测试，第二次起内核
// 已经就绪。这里用 Go 测试的经典 helper process 写法起一个干净子进程来钉死它。
const helperProcessEnv = "HNCORE_SERVER_HELPER_PROCESS"

func TestRealKernelFreshProcessBehavior(t *testing.T) {
	if os.Getenv(helperProcessEnv) != "1" {
		// 父进程：起一个干净的子进程，只跑本用例（子进程里内核必然未初始化）。
		command := exec.Command(os.Args[0], "-test.run=^TestRealKernelFreshProcessBehavior$", "-test.v")
		command.Env = append(os.Environ(), helperProcessEnv+"=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("子进程断言失败：%v\n%s", err, output)
		}
		return
	}

	h := startServer(t, config{}) // 真内核：Handler 缺省 = core.NativeRequest

	// 未 initialize：内核如实回「尚未就绪」，而不是假装成功。
	h.send(`{"id":1,"action":"downloads"}`)
	if message, _ := envelopeError(t, h.nextWithID("1", 30*time.Second)); !strings.Contains(message, "尚未就绪") {
		t.Fatalf("未初始化时的错误文案异常: %q", message)
	}

	// 相对路径的数据目录被内核拒绝（该校验只在首次 initialize 时发生）。
	h.send(`{"id":2,"action":"initialize","directory":"relative/dir"}`)
	if message, _ := envelopeError(t, h.nextWithID("2", 30*time.Second)); !strings.Contains(message, "目录无效") {
		t.Fatalf("相对目录应被拒绝，实际 %q", message)
	}
}

func TestRealKernelHandshakeAndRefusals(t *testing.T) {
	h := startServer(t, config{})

	// 未 initialize 时会回「尚未就绪」（见 TestRealKernelFreshProcessBehavior 的
	// 新进程断言）。本进程可能已被前面的用例初始化过（内核状态进程级幂等），
	// 因此这里只要求「要么尚未就绪、要么真的成功」，不重复断言具体文案。
	h.send(`{"id":1,"action":"downloads"}`)
	line := h.nextWithID("1", 30*time.Second)
	parsed := envelope(t, line)
	if string(parsed["ok"]) == "false" {
		if message, _ := envelopeError(t, line); !strings.Contains(message, "尚未就绪") {
			t.Fatalf("未初始化时的错误文案异常: %q", message)
		}
	} else {
		envelopeOK(t, line)
	}

	// 真正的握手：PC 端启动后第一件事（已初始化时是幂等的，仍回版本号）。
	h.send(`{"id":3,"action":"initialize","directory":` + jsonString(sharedDir) + `}`)
	handshake := h.nextWithID("3", 30*time.Second)
	envelopeOK(t, handshake)
	if got := string(dataField(t, handshake, "version")); got == `""` {
		t.Fatalf("initialize 未返回内核版本: %s", handshake)
	}

	// 未知 action（需要内核已就绪才会走到这一层）。
	h.send(`{"id":4,"action":"这个动作不存在"}`)
	if message, _ := envelopeError(t, h.nextWithID("4", 30*time.Second)); !strings.Contains(message, "不支持") {
		t.Fatalf("未知 action 应回「不支持的应用操作」，实际 %q", message)
	}
}

// TestCoreOwnOversizeRejectionHasNoCode 记录一处**契约与内核实际行为的不一致**
// （只记录、不改契约）：契约把「请求格式无效」映射到 code=invalid_request 并
// 声明本仓 core 只产生 local_media / source_blocked 两个码，但内核实际对
// >1 MiB 的请求也只回文案、不带 code。
//
// 默认配置下这个分支看不见（服务端 1 MiB 上限先把超限帧拦下了，见
// TestOversizeFrameIsRejectedWithoutLosingFollowingFrames）；把上限放宽到
// 2 MiB 才能观察到内核的真实响应 —— 它会被**原样透传**，服务端不替内核补码。
func TestCoreOwnOversizeRejectionHasNoCode(t *testing.T) {
	h := startServer(t, config{MaxFrameBytes: 2 << 20})

	h.send(paddedFrame(coreMaxRequestBytes + 8))
	message, code := envelopeError(t, h.next(10*time.Second))
	if !strings.Contains(message, "请求格式无效") {
		t.Fatalf("内核应回「请求格式无效」，实际 %q", message)
	}
	if code != "" {
		t.Fatalf("内核实际未产出 code（契约却把该文案映射到 invalid_request），实际 %q", code)
	}
}

// TestRealKernelConcurrentCallsAreSafe 用真内核并发打一批**纯本地**动作：
// 期望 -race 下无数据竞争、无 panic、每个请求都有响应。
//
// 覆盖的动作刻意避开联网路径（catalog/categories/detail 等要出网），
// 只验证服务端并发模型与内核自身锁的兼容性。
func TestRealKernelConcurrentCallsAreSafe(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过")
	}
	h := startServer(t, config{Workers: 4})

	h.send(`{"id":"init","action":"initialize","directory":` + jsonString(sharedDir) + `}`)
	envelopeOK(t, h.nextWithID("init", 30*time.Second))

	requests := []string{
		`{"id":"r1","action":"resourceSettings"}`,
		`{"id":"r2","action":"rankingBoards"}`,
		`{"id":"r3","action":"storage"}`,
		`{"id":"r4","action":"downloads"}`,
		`{"id":"r5","action":"downloadDirectory"}`,
		`{"id":"r6","action":"sourceStatus","source":"hongguo"}`,
		`{"id":"r7","action":"cancelRead","session":"不存在的会话"}`,
		`{"id":"r8","action":"cancelSourceJob","source":"hongguo"}`,
		`{"id":"r9","action":"release","session":"不存在的会话"}`,
		`{"id":"r10","action":"cancelPlayback","sequence":99}`,
		`{"id":"r11","action":"resourceSettings"}`,
		`{"id":"r12","action":"rankingBoards"}`,
	}
	for _, request := range requests {
		h.send(request)
	}
	seen := map[string]bool{}
	okCount := 0
	for i := 0; i < len(requests); i++ {
		line := h.next(30 * time.Second)
		if string(envelope(t, line)["ok"]) == "true" {
			okCount++
		}
		if id := idOf(line); seen[id] {
			t.Fatalf("id=%s 收到重复响应", id)
		} else {
			seen[id] = true
		}
	}
	for _, request := range requests {
		if id := idOf([]byte(request)); !seen[id] {
			t.Fatalf("id=%s 没有得到响应", id)
		}
	}
	if okCount != len(requests) {
		t.Fatalf("本地动作应全部成功，实际成功 %d/%d", okCount, len(requests))
	}
}

// ---------------------------------------------------------------------------
// 单元级：帧读取与信封拼接
// ---------------------------------------------------------------------------

func TestReadFrameUnit(t *testing.T) {
	long := strings.Repeat("x", 600)
	// 用 16 字节的小缓冲强制走 ReadSlice 的 ErrBufferFull 分支。
	reader := bufio.NewReaderSize(strings.NewReader(
		"{\"a\":1}\r\n"+"\n"+"\xEF\xBB\xBF{\"b\":2}\n"+long+"\n"+"{\"c\":3}"), 16)

	frame, err := readFrame(reader, 4096)
	if err != nil || string(frame) != `{"a":1}` {
		t.Fatalf("CRLF 帧解析失败: %q %v", frame, err)
	}
	if _, err := readFrame(reader, 4096); !errors.Is(err, errFrameEmpty) {
		t.Fatalf("空行应回 errFrameEmpty，实际 %v", err)
	}
	frame, err = readFrame(reader, 4096)
	if err != nil || string(frame) != `{"b":2}` {
		t.Fatalf("BOM 帧解析失败: %q %v", frame, err)
	}
	frame, err = readFrame(reader, 4096)
	if err != nil || string(frame) != long {
		t.Fatalf("跨缓冲长帧解析失败（%d 字节）: %v", len(frame), err)
	}
	frame, err = readFrame(reader, 4096)
	if err != nil || string(frame) != `{"c":3}` {
		t.Fatalf("末帧（无结尾换行）解析失败: %q %v", frame, err)
	}
	if _, err := readFrame(reader, 4096); !errors.Is(err, io.EOF) {
		t.Fatalf("流结束应回 io.EOF，实际 %v", err)
	}

	// 超限：返回 errFrameTooLarge，且整行已被消费。
	reader = bufio.NewReaderSize(strings.NewReader(strings.Repeat("y", 100)+"\n{\"d\":4}\n"), 8)
	if _, err := readFrame(reader, 32); !errors.Is(err, errFrameTooLarge) {
		t.Fatalf("超限应回 errFrameTooLarge，实际 %v", err)
	}
	if frame, err := readFrame(reader, 32); err != nil || string(frame) != `{"d":4}` {
		t.Fatalf("超限后未同步到下一行: %q %v", frame, err)
	}

	// 非法 UTF-8（不完整的多字节序列）。
	reader = bufio.NewReader(strings.NewReader("{\"e\":\"\xC3\"}\n"))
	if _, err := readFrame(reader, 32); !errors.Is(err, errFrameBadUTF8) {
		t.Fatalf("非法 UTF-8 应回 errFrameBadUTF8，实际 %v", err)
	}
}

func TestWithIDShapes(t *testing.T) {
	cases := []struct {
		payload string
		id      string
		want    string
	}{
		{`{"ok":true,"data":null}`, `"a"`, `{"id":"a","ok":true,"data":null}`},
		{`{"ok":true,"data":null}`, `12`, `{"id":12,"ok":true,"data":null}`},
		{`{}`, `"a"`, `{"id":"a"}`},
		{`{ }`, `"a"`, `{"id":"a"}`},
		{`{"ok":true}`, ``, `{"ok":true}`},
	}
	for _, testCase := range cases {
		got, err := withID([]byte(testCase.payload), json.RawMessage(testCase.id))
		if err != nil {
			t.Fatalf("withID(%s) 失败: %v", testCase.payload, err)
		}
		if string(got) != testCase.want {
			t.Fatalf("withID(%s) = %s，期望 %s", testCase.payload, got, testCase.want)
		}
		if !json.Valid(got) {
			t.Fatalf("withID 产出非法 JSON: %s", got)
		}
	}
	for _, payload := range []string{`not json`, ``, `[1]`} {
		if _, err := withID([]byte(payload), json.RawMessage(`"a"`)); err == nil {
			t.Fatalf("非法内核响应 %q 应报错", payload)
		}
	}
}

func TestEnvelopeMatchesContractShape(t *testing.T) {
	ok := encodeOK(json.RawMessage(`{"items":[]}`), json.RawMessage(`"a"`))
	okParsed := map[string]any{}
	if err := json.Unmarshal(ok, &okParsed); err != nil {
		t.Fatal(err)
	}
	if okParsed["ok"] != true {
		t.Fatalf("okEnvelope.ok 必须是 true: %s", ok)
	}
	if _, exists := okParsed["data"]; !exists {
		t.Fatalf("okEnvelope 必须带 data: %s", ok)
	}
	if _, exists := okParsed["error"]; exists {
		t.Fatalf("成功信封不得带 error: %s", ok)
	}

	fail := encodeError("请求格式无效：JSON 语法错误", codeInvalidRequest, nil)
	failParsed := map[string]any{}
	if err := json.Unmarshal(fail, &failParsed); err != nil {
		t.Fatal(err)
	}
	if failParsed["ok"] != false || failParsed["error"] == "" || failParsed["code"] != codeInvalidRequest {
		t.Fatalf("errorEnvelope 形状不对: %s", fail)
	}
	if _, exists := failParsed["data"]; exists {
		t.Fatalf("失败信封不得带 data: %s", fail)
	}
	assertCodeInContract(t, codeInvalidRequest)

	// data 缺省必须是显式 null（契约允许，且 core 现状如此）。
	var probe struct {
		Data json.RawMessage `json:"data"`
	}
	encoded := encodeOK(nil, nil)
	if err := json.Unmarshal(encoded, &probe); err != nil || string(probe.Data) != "null" {
		t.Fatalf("data 缺省应显式序列化为 null: %s", encoded)
	}
}

func TestIsErrorEnvelope(t *testing.T) {
	if !isErrorEnvelope([]byte(`{"ok":false,"error":"x"}`)) {
		t.Fatal("ok=false 应判为失败信封")
	}
	for _, payload := range []string{`{"ok":true,"data":null}`, `{}`, `[]`, `bad`} {
		if isErrorEnvelope([]byte(payload)) {
			t.Fatalf("%s 不应判为失败信封", payload)
		}
	}
}

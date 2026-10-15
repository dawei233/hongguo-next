package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dawei233/hongguo-next/core"
)

// config 是服务端的全部可调项。零值即默认值（Grace 例外：0 = 不等待）。
type config struct {
	// MaxFrameBytes 是单帧上限（字节）。默认 1 MiB，与内核上限对齐。
	MaxFrameBytes int
	// Workers 是**同时对内核发起调用**的上限。默认 4；置 1 即串行执行，
	// 此时响应顺序与请求到达顺序一致（见 README「并发与顺序」）。
	Workers int
	// MaxInflight 是在途请求（含等待工作槽的）上限，超出即回「繁忙」。
	// 0 表示按 max(32, Workers*8) 推导。它保证服务端内存与内核压力有界。
	MaxInflight int
	// RequestTimeout 是单请求默认等待上限。0（默认）= 不设服务端超时，
	// 完全交给内核自身的期限（见 README「超时与取消」）。
	RequestTimeout time.Duration
	// Grace 是收到退出请求（EOF / server:shutdown / 信号）后，等待在途
	// 请求收尾的宽限期。0 表示不等待、立刻退出（命令行默认 5s）。
	Grace time.Duration
	// Handler 是内核入口，默认 core.NativeRequest。测试用它注入假内核。
	Handler func(string) string
	// Quiet 为真时丢弃全部诊断日志（命令行 -quiet）。
	Quiet bool
	// Logger 收全部诊断输出（永不写 stdout）。默认 os.Stderr。
	Logger *log.Logger
}

func (c config) normalized() config {
	if c.MaxFrameBytes <= 0 {
		c.MaxFrameBytes = defaultMaxFrameBytes
	}
	if c.Workers <= 0 {
		c.Workers = defaultWorkers
	}
	if c.MaxInflight <= 0 {
		c.MaxInflight = max(defaultMinInflight, c.Workers*8)
	}
	if c.Grace < 0 {
		c.Grace = 0
	}
	if c.Handler == nil {
		c.Handler = core.NativeRequest
	}
	if c.Logger == nil {
		c.Logger = log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)
	}
	return c
}

type pendingRequest struct {
	id       json.RawMessage
	key      string
	ticket   uint64
	canceled chan struct{}
	once     sync.Once
	answered atomic.Bool
}

func (p *pendingRequest) cancel() {
	p.once.Do(func() { close(p.canceled) })
}

type serverStats struct {
	requests  atomic.Int64
	completed atomic.Int64
	failed    atomic.Int64
	refused   atomic.Int64
	timeouts  atomic.Int64
	cancels   atomic.Int64
	panics    atomic.Int64
	oversize  atomic.Int64
	badFrames atomic.Int64
}

// server 是 stdio JSON 服务端。
//
// 并发模型（三层，各自有界）：
//
//	读循环（单 goroutine，永不阻塞在业务上）
//	  └─ 每个请求一个监督 goroutine（在途数 <= MaxInflight）
//	       └─ 每个内核调用一个执行 goroutine（并发数 <= Workers）
//
// stdout 由 writeMu 串行化，一帧一次 Flush，因此响应永远不会互相截断/交错。
type server struct {
	cfg     config
	log     *log.Logger
	out     *bufio.Writer
	gate    *ticketGate
	started time.Time
	stats   serverStats

	sequence      atomic.Uint64
	inflightMu    sync.Mutex
	inflight      map[string]*pendingRequest
	inflightCount int

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}

	writeMu  sync.Mutex
	writeErr error
}

func newServer(out io.Writer, cfg config) *server {
	cfg = cfg.normalized()
	return &server{
		cfg:      cfg,
		log:      cfg.Logger,
		out:      bufio.NewWriterSize(out, 64<<10),
		gate:     newTicketGate(cfg.Workers),
		started:  time.Now(),
		inflight: map[string]*pendingRequest{},
		stopCh:   make(chan struct{}),
	}
}

func (s *server) stopping() bool {
	select {
	case <-s.stopCh:
		return true
	default:
		return false
	}
}

// beginStop 请求停止接收新工作。幂等，可被多来源（EOF / shutdown 帧 / 信号）调用。
func (s *server) beginStop(reason string) {
	s.stopOnce.Do(func() {
		s.log.Printf("开始退出：%s", reason)
		close(s.stopCh)
	})
}

// run 跑完整条服务生命周期：读帧 → 分发 → 收尾。
//
// 返回 nil 表示正常退出（stdin EOF、收到 shutdown、收到信号、stdout 对端关闭）；
// 返回非 nil 表示不可恢复的 I/O 错误（调用方据此以非 0 退出码结束进程）。
func run(ctx context.Context, in io.Reader, out io.Writer, cfg config) error {
	s := newServer(out, cfg)
	s.log.Printf("hncore-server %s 启动（协议 v%d，并发 %d，单帧上限 %d 字节，超时 %s，宽限 %s）",
		serverVersion, protocolVersion, s.cfg.Workers, s.cfg.MaxFrameBytes, timeoutLabel(s.cfg.RequestTimeout), s.cfg.Grace)

	// 外部取消（信号）→ 转成停止。读循环本身可能阻塞在 Read 上，
	// 由调用方负责关闭 stdin 来解除阻塞（见 main.go）。
	external := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.beginStop("收到退出信号")
		case <-external:
		}
	}()
	defer close(external)

	err := s.readLoop(in)
	s.waitInflight()
	s.flush()

	if err != nil {
		if s.stopping() {
			// 退出过程中 stdin 被关闭/取消，读错误属于正常收尾噪声。
			s.log.Printf("读循环因退出而中断：%v", err)
			err = nil
		} else {
			return err
		}
	}
	if werr := s.writeError(); werr != nil {
		s.log.Printf("stdout 写入失败：%v", werr)
		if err == nil {
			err = werr
		}
	}
	s.log.Printf("已退出：请求 %d，完成 %d，失败 %d，拒绝 %d，超时 %d，取消 %d，panic %d，超限帧 %d，坏帧 %d",
		s.stats.requests.Load(), s.stats.completed.Load(), s.stats.failed.Load(), s.stats.refused.Load(),
		s.stats.timeouts.Load(), s.stats.cancels.Load(), s.stats.panics.Load(), s.stats.oversize.Load(), s.stats.badFrames.Load())
	return err
}

func (s *server) readLoop(in io.Reader) (err error) {
	// 读循环自身的 panic 不能带走进程。
	defer func() {
		if recovered := recover(); recovered != nil {
			s.stats.panics.Add(1)
			s.log.Printf("读循环 panic（已恢复）：%v\n%s", recovered, debug.Stack())
			s.beginStop("读循环 panic")
			err = nil
		}
	}()

	reader := bufio.NewReaderSize(in, 256<<10)
	for {
		if s.stopping() {
			return nil
		}
		frame, frameErr := readFrame(reader, s.cfg.MaxFrameBytes)
		switch {
		case frameErr == nil:
			s.dispatch(frame)
		case errors.Is(frameErr, io.EOF):
			s.beginStop("stdin 已关闭（EOF）")
			return nil
		case errors.Is(frameErr, errFrameEmpty):
			// 空行是合法的填充，不产生响应。
			continue
		case errors.Is(frameErr, errFrameTooLarge):
			s.stats.oversize.Add(1)
			s.refuse("请求过大：单帧上限 "+strconv.Itoa(s.cfg.MaxFrameBytes)+" 字节（内核自身亦限制 1 MiB）", codeInvalidRequest, nil)
			continue
		case errors.Is(frameErr, errFrameBadUTF8):
			s.stats.badFrames.Add(1)
			s.refuse("请求格式无效：帧不是合法 UTF-8", codeInvalidRequest, nil)
			continue
		default:
			return frameErr
		}
	}
}

// dispatch 处理一帧：控制帧本地处理，其余原样交给内核。
func (s *server) dispatch(frame []byte) {
	s.stats.requests.Add(1)
	header, err := parseHeader(frame)
	if err != nil {
		s.stats.badFrames.Add(1)
		s.refuse("请求格式无效："+err.Error(), codeInvalidRequest, nil)
		return
	}
	if header.Server != "" {
		s.handleControl(header)
		return
	}
	s.handleCore(frame, header)
}

func (s *server) handleControl(header requestHeader) {
	switch header.Server {
	case "ping":
		s.respond(encodeData(map[string]any{
			"pong":            true,
			"serverVersion":   serverVersion,
			"protocolVersion": protocolVersion,
			"pid":             os.Getpid(),
			"uptimeMs":        time.Since(s.started).Milliseconds(),
			"workers":         s.cfg.Workers,
			"maxFrameBytes":   s.cfg.MaxFrameBytes,
			"requestTimeout":  timeoutLabel(s.cfg.RequestTimeout),
			"inflight":        s.inflightLen(),
			"stopping":        s.stopping(),
		}, header.ID))
	case "shutdown":
		s.respond(encodeData(map[string]any{"stopping": true}, header.ID))
		s.beginStop("收到 server:shutdown")
	case "cancel":
		target := idKey(header.TargetID)
		if target == "" {
			s.refuse("请求格式无效：server:cancel 需要 targetId", codeInvalidRequest, header.ID)
			return
		}
		pending := s.takeInflight(target)
		if pending == nil {
			s.refuse("未找到待取消的请求："+target, codeInvalidRequest, header.ID)
			return
		}
		s.stats.cancels.Add(1)
		// 先回这条控制帧，再让目标请求自己写出「已取消」。
		s.respond(encodeData(map[string]any{"canceled": true, "targetId": header.TargetID}, header.ID))
		pending.cancel()
	default:
		s.refuse("不支持的服务端指令："+header.Server, codeInvalidRequest, header.ID)
	}
}

func (s *server) handleCore(frame []byte, header requestHeader) {
	pending := &pendingRequest{id: cloneRaw(header.ID), key: idKey(header.ID), canceled: make(chan struct{})}

	if message, code := s.admit(pending); message != "" {
		s.refuse(message, code, header.ID)
		return
	}
	// 票号在**读循环里**发放：它的顺序就是帧到达顺序。额度按票号升序发放
	// （见 ticketGate），因此「谁先拿到额度」与 goroutine 调度无关：-workers 1 时
	// 内核调用的发起顺序 = 响应写出顺序 = 帧到达顺序；并发度 >1 时不保证实际发起
	// 时刻的先后（这是并发的本义），需要严格顺序请用 -workers 1。
	pending.ticket = s.sequence.Add(1)

	timeout := s.cfg.RequestTimeout
	if header.TimeoutMS > 0 {
		timeout = time.Duration(header.TimeoutMS) * time.Millisecond
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.release(pending)
		defer s.recoverPanic(pending, "请求处理")
		s.execute(pending, frame, timeout)
	}()
}

// admit 做在途准入。返回非空 message 表示拒绝，同时给出对应的契约错误码。
func (s *server) admit(pending *pendingRequest) (message, code string) {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	if s.inflightCount >= s.cfg.MaxInflight {
		// 「服务繁忙」在契约的错误码枚举里没有对应档位，只有 upstream_error
		// 这一条兜底码语义最接近（调用方应退避重试），故借用并如实记录。
		return "内核服务繁忙：在途请求已达上限 " + strconv.Itoa(s.cfg.MaxInflight) + "，请稍后重试", codeUpstreamError
	}
	if pending.key != "" {
		if _, exists := s.inflight[pending.key]; exists {
			return "请求格式无效：id " + pending.key + " 的请求仍在处理中", codeInvalidRequest
		}
		s.inflight[pending.key] = pending
	}
	s.inflightCount++
	return "", ""
}

func (s *server) release(pending *pendingRequest) {
	s.inflightMu.Lock()
	if pending.key != "" && s.inflight[pending.key] == pending {
		delete(s.inflight, pending.key)
	}
	s.inflightCount--
	s.inflightMu.Unlock()
}

// takeInflight 摘除并返回某个在途请求（用于 server:cancel）。
//
// 摘除即「该 id 的响应由取消路径负责」，因此后续不会再被重复登记，
// 调用方也可以立刻复用同一个 id 发起新请求。
func (s *server) takeInflight(key string) *pendingRequest {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	pending, found := s.inflight[key]
	if !found {
		return nil
	}
	delete(s.inflight, key)
	return pending
}

func (s *server) inflightLen() int {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	return s.inflightCount
}

// execute 取得工作额度（按票号，即帧到达顺序）后发起内核调用，并在
// 「返回 / 超时 / 取消」三者中取先到者。
//
// 停止期间**不**打断已经开始的调用：优雅退出的语义就是让在途调用收尾，
// 总时限由 waitInflight 统一约束。注意准入是在读循环里完成的（dispatch →
// handleCore 的 admit），能走到这里就说明这条请求在停止之前已被接纳。
func (s *server) execute(pending *pendingRequest, frame []byte, timeout time.Duration) {
	if !s.gate.acquire(pending.ticket, pending.canceled) {
		s.answer(pending, encodeError("请求已被调用方取消", "", pending.id))
		return
	}

	// 额度一直占到「内核调用结束 **且** 响应写完」：
	//   - 若只等内核调用：串行模式下上一条响应还没落盘，下一条就已开始执行，
	//     响应顺序会被打乱（TestSingleWorkerKeepsArrivalOrder 抓到的就是这个）；
	//   - 若只等响应：超时/取消提前返回时内核仍在跑，并发额度会被过早放开，
	//     内核侧并发就会突破 Workers。
	// 额度按票号发放（ticketGate），因此 -workers 1 时上一条响应写完之前，
	// 下一条不会发起内核调用（与 goroutine 调度无关）。
	var coreDone, responseDone sync.WaitGroup
	coreDone.Add(1)
	responseDone.Add(1)
	go func() {
		coreDone.Wait()
		responseDone.Wait()
		s.gate.release()
	}()
	defer responseDone.Done()

	result := make(chan []byte, 1)
	go func() {
		defer coreDone.Done()
		defer func() {
			// 内核调用 panic 时也必须给出**一条**响应，否则调用方会一直等。
			// 这里只把错误信封投进 result，由监督方统一写出，避免重复响应。
			if recovered := recover(); recovered != nil {
				s.stats.panics.Add(1)
				s.log.Printf("内核调用 panic（已恢复）：%v\n%s", recovered, debug.Stack())
				result <- encodeError("内核服务处理异常，请重试", "", pending.id)
			}
		}()
		result <- []byte(s.cfg.Handler(string(frame)))
	}()

	var timer *time.Timer
	var deadline <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}

	select {
	case payload := <-result:
		s.writeCoreResponse(pending, payload)
	case <-pending.canceled:
		s.log.Printf("请求 %s 已取消，内核调用仍在执行，其结果将被丢弃", pending.label())
		s.answer(pending, encodeError("请求已被调用方取消", "", pending.id))
	case <-deadline:
		s.stats.timeouts.Add(1)
		s.log.Printf("请求 %s 超时（%s），内核调用仍在执行，其结果将被丢弃", pending.label(), timeout)
		s.answer(pending, encodeError("请求超时：内核未在 "+timeout.String()+" 内返回", codeUpstreamError, pending.id))
	}
}

// writeCoreResponse 透传内核响应；仅在需要回显 id 时做最小拼接（见 withID）。
func (s *server) writeCoreResponse(pending *pendingRequest, payload []byte) {
	if !json.Valid(payload) {
		s.log.Printf("内核返回了非法 JSON 响应：%s", truncateForLog(payload))
		s.answer(pending, encodeError("内核返回了非预期的响应", codeUpstreamError, pending.id))
		return
	}
	body, err := withID(payload, pending.id)
	if err != nil {
		s.log.Printf("内核响应无法回显 id：%v", err)
		s.answer(pending, encodeError("内核返回了非预期的响应", codeUpstreamError, pending.id))
		return
	}
	s.answer(pending, body)
}

// isErrorEnvelope 只用于统计区分（不改动响应字节）。
func isErrorEnvelope(payload []byte) bool {
	var probe struct {
		Ok *bool `json:"ok"`
	}
	if json.Unmarshal(payload, &probe) != nil || probe.Ok == nil {
		return false
	}
	return !*probe.Ok
}

// answer 写出**恰好一条**针对该请求的响应。
//
// 这条 CAS 是「不重复响应」的唯一闸门：取消与内核返回可能同时就绪，
// 竞争下也只会有一次写出口。
func (s *server) answer(pending *pendingRequest, body []byte) {
	if !pending.answered.CompareAndSwap(false, true) {
		s.log.Printf("请求 %s 已有响应，丢弃重复结果（%d 字节）", pending.label(), len(body))
		return
	}
	if isErrorEnvelope(body) {
		s.stats.failed.Add(1)
	} else {
		s.stats.completed.Add(1)
	}
	s.respond(body)
}

func (s *server) recoverPanic(pending *pendingRequest, stage string) {
	recovered := recover()
	if recovered == nil {
		return
	}
	s.stats.panics.Add(1)
	s.log.Printf("%s panic（已恢复）：%v\n%s", stage, recovered, debug.Stack())
	// panic 之后无法判断内核状态，只能如实回一条错误信封；错误码枚举里没有
	// 「内部错误」这一档，按契约 code 可省略，故不带 code（见 README 契约差异）。
	if pending != nil {
		s.answer(pending, encodeError("内核服务处理异常，请重试", "", pending.id))
	}
}

func (s *server) refuse(message, code string, id json.RawMessage) {
	s.stats.refused.Add(1)
	s.log.Printf("拒绝请求：%s", message)
	s.respond(encodeError(message, code, id))
}

func (s *server) respond(body []byte) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeErr != nil {
		s.log.Printf("stdout 已失效，丢弃一帧响应（%d 字节）", len(body))
		return
	}
	if _, err := s.out.Write(body); err != nil {
		s.writeErr = err
		s.beginStop("stdout 写入失败")
		return
	}
	if err := s.out.WriteByte('\n'); err != nil {
		s.writeErr = err
		s.beginStop("stdout 写入失败")
		return
	}
	if err := s.out.Flush(); err != nil {
		s.writeErr = err
		s.beginStop("stdout 写入失败")
	}
}

func (s *server) flush() {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeErr != nil {
		return
	}
	if err := s.out.Flush(); err != nil {
		s.writeErr = err
	}
}

func (s *server) writeError() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writeErr
}

// waitInflight 处理退出时的在途请求，语义是三态的：
//
//	Grace > 0  最多等这么久，到点记录警告并退出（残留 goroutine 随进程消失）
//	Grace == 0 完全不等，立刻退出
//	Grace < 0  一直等到全部收尾（调用方自己保证不会再挂住）
//
// 为什么到点后不强行 join：内核对外的入口 NativeRequest 没有 context 参数，
// 一旦进去就无法从外部中止（内核内部各动作有自己的 60s/10min 期限兜底），
// 所以服务端能做的只有「不再等待」，而不是「杀掉调用」。
func (s *server) waitInflight() {
	if s.cfg.Grace == 0 {
		return
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	if s.cfg.Grace < 0 {
		<-done
		return
	}
	select {
	case <-done:
	case <-time.After(s.cfg.Grace):
		s.log.Printf("宽限期 %s 已到，仍有 %d 个在途请求未完成，直接退出", s.cfg.Grace, s.inflightLen())
	}
}

func (p *pendingRequest) label() string {
	if p.key == "" {
		return "(无 id)"
	}
	return p.key
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func timeoutLabel(value time.Duration) string {
	if value <= 0 {
		return "内核自定期限"
	}
	return value.String()
}

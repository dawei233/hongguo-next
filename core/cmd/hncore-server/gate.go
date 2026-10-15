package main

import (
	"sort"
	"sync"
)

// ticketGate 是**按票号升序**发放额度的并发闸门（票号 = 帧到达顺序）。
//
// 为什么不用 `chan struct{}` 当信号量：通道只保证「已阻塞的发送者按 FIFO 唤醒」，
// 而**到达顺序 ≠ 抢锁顺序** —— 每个请求的监督 goroutine 是分别启动的，谁先被调度
// 谁就先抢到额度。实测（-workers 1）四条请求的响应会以 0,1,3,2 返回
// （TestSingleWorkerKeepsArrivalOrder 抓到的就是这个，已由本闸门修掉）。
//
// 三条不变量：
//  1. 同一时刻持有额度者不超过 limit；
//  2. 额度按票号升序发放（票号由读循环按帧到达顺序发放，见 server.sequence），
//     因此 limit==1 时「内核调用的发起顺序 = 响应写出顺序 = 帧到达顺序」；
//  3. **被取消的票号必须被「跳过」而不是「粘住队首」**：票在排队时被取消，
//     它已不在 waiters 里，但 next 迟早会走到它的号上 —— 不记账就会永久卡死
//     （canceled 集合就是为此存在；见 acquire 的取消分支与 skipLocked）。
type ticketGate struct {
	mu       sync.Mutex
	limit    int
	running  int
	next     uint64
	waiters  map[uint64]chan struct{}
	canceled map[uint64]bool
}

func newTicketGate(limit int) *ticketGate {
	if limit < 1 {
		limit = 1
	}
	// 票号从 1 开始（server.sequence 是 atomic.Uint64 的 Add(1)，首个返回值即 1），
	// 因此队首初值也必须是 1：否则第一张票永远等不到自己的号。
	return &ticketGate{limit: limit, next: 1, waiters: map[uint64]chan struct{}{}, canceled: map[uint64]bool{}}
}

// acquire 等到「轮到自己（票号 == 队首）且有空闲额度」。
//
// canceled 关闭时放弃等待并返回 false。若取消与唤醒同时发生，按「已拿到额度」
// 处理并立刻归还 —— 额度绝不能凭空消失，也绝不能白跑一次注定被丢弃的内核调用。
func (g *ticketGate) acquire(ticket uint64, canceled <-chan struct{}) bool {
	g.mu.Lock()
	g.skipLocked()
	if g.running < g.limit && ticket == g.next {
		g.next++
		g.running++
		// 队首前移可能解开后面的等待者（票号出现空档时它们本就在等），
		// 因此这里必须顺带派发一次。
		g.dispatchLocked()
		g.mu.Unlock()
		return true
	}
	waiter := make(chan struct{})
	g.waiters[ticket] = waiter
	g.mu.Unlock()

	select {
	case <-waiter:
		select {
		case <-canceled:
			g.release()
			return false
		default:
		}
		return true
	case <-canceled:
		g.mu.Lock()
		if _, waiting := g.waiters[ticket]; waiting {
			delete(g.waiters, ticket)
			// 记账：这个票号永远不会有人再来认领，next 走到它时必须跳过。
			g.canceled[ticket] = true
			g.skipLocked()
			g.dispatchLocked()
			g.mu.Unlock()
			return false
		}
		g.mu.Unlock()
		g.release() // 已被唤醒：额度已经算在我头上，归还
		return false
	}
}

// release 归还一次额度。
func (g *ticketGate) release() {
	g.mu.Lock()
	g.running--
	g.skipLocked()
	g.dispatchLocked()
	g.mu.Unlock()
}

// skipLocked 让 next 越过所有「已取消且不会再有人认领」的票号。
func (g *ticketGate) skipLocked() {
	for g.canceled[g.next] {
		delete(g.canceled, g.next)
		g.next++
	}
}

// dispatchLocked 在有空闲额度时按票号升序把额度发给队首（可能连着发多个）。
// 调用方必须已持有 g.mu。
func (g *ticketGate) dispatchLocked() {
	for {
		g.skipLocked()
		if g.running >= g.limit {
			return
		}
		waiter, waiting := g.waiters[g.next]
		if !waiting {
			return
		}
		delete(g.waiters, g.next)
		g.next++
		g.running++
		close(waiter)
	}
}

// waiting 返回当前排队的票数（仅供 ping 与测试观测）。
func (g *ticketGate) waiting() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.waiters)
}

// snapshot 返回闸门内部状态的快照（next / running / 排队票号 / 待跳过票号），
// 仅供测试在超时或断言失败时定位「卡在哪个票号上」。
func (g *ticketGate) snapshot() (next uint64, running int, queued, pending []uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	next, running = g.next, g.running
	for ticket := range g.waiters {
		queued = append(queued, ticket)
	}
	for ticket := range g.canceled {
		pending = append(pending, ticket)
	}
	sort.Slice(queued, func(i, j int) bool { return queued[i] < queued[j] })
	sort.Slice(pending, func(i, j int) bool { return pending[i] < pending[j] })
	return next, running, queued, pending
}

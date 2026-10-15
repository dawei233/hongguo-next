package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 这些用例把「额度按票号发放」单独钉住 —— 它是 -workers 1 时
// 「响应顺序 = 帧到达顺序」的唯一依据，而通道信号量做不到这一点。

// TestTicketGateServesInTicketOrder：limit=1 时**获得额度的顺序**必须严格等于
// 票号顺序，即使各票的 goroutine 是乱序启动的。
//
// 判据用「持有额度期间」的进入顺序（临界区内记录），而不是「被唤醒后的调度顺序」
// —— 后者与闸门无关。
func TestTicketGateServesInTicketOrder(t *testing.T) {
	const total = 24
	gate := newTicketGate(1)

	var mu sync.Mutex
	var order []uint64
	var wg sync.WaitGroup
	// 故意逆序启动：票 24 先跑，票 1 最后跑。
	for i := total; i >= 1; i-- {
		wg.Add(1)
		go func(ticket uint64) {
			defer wg.Done()
			if !gate.acquire(ticket, make(chan struct{})) {
				t.Errorf("票 %d 不应被取消", ticket)
				return
			}
			mu.Lock()
			order = append(order, ticket)
			mu.Unlock()
			gate.release()
		}(uint64(i))
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(order) != total {
		t.Fatalf("应获得 %d 次额度，实际 %d", total, len(order))
	}
	for i, ticket := range order {
		if ticket != uint64(i+1) {
			t.Fatalf("额度未按票号发放：%v", order)
		}
	}
}

// TestTicketGateLimitsConcurrency：并发度不得超过 limit，且不得漏额度。
func TestTicketGateLimitsConcurrency(t *testing.T) {
	const (
		limit   = 3
		tickets = 60
	)
	gate := newTicketGate(limit)
	var running, peak int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < tickets; i++ {
		wg.Add(1)
		go func(ticket uint64) {
			defer wg.Done()
			if !gate.acquire(ticket, make(chan struct{})) {
				t.Errorf("票 %d 不应被取消", ticket)
				return
			}
			current := atomic.AddInt64(&running, 1)
			mu.Lock()
			if current > peak {
				peak = current
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			atomic.AddInt64(&running, -1)
			gate.release()
		}(uint64(i + 1))
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if peak != limit {
		t.Fatalf("并发峰值应为 %d，实际 %d", limit, peak)
	}
	if gate.waiting() != 0 {
		t.Fatalf("不应有残留排队：%d", gate.waiting())
	}
}

// TestTicketGateSkipsCanceledHead：队首被取消时，后面的票必须能接上。
func TestTicketGateSkipsCanceledHead(t *testing.T) {
	gate := newTicketGate(1)
	if !gate.acquire(1, make(chan struct{})) {
		t.Fatal("票 1 应立刻拿到额度")
	}

	canceled := make(chan struct{})
	headDone := make(chan bool, 1)
	go func() { headDone <- gate.acquire(2, canceled) }()
	nextResult := make(chan bool, 1)
	go func() { nextResult <- gate.acquire(3, make(chan struct{})) }()

	time.Sleep(50 * time.Millisecond)
	close(canceled)
	if ok := <-headDone; ok {
		t.Fatal("票 2 已被取消，不应拿到额度")
	}
	gate.release()
	select {
	case ok := <-nextResult:
		if !ok {
			t.Fatal("票 3 应拿到额度")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("队首被取消后，后续票号卡住了")
	}
	gate.release()
}

// TestTicketGateSkipsCanceledBehindHead 是回归用例：票在**队首后面**排队时被取消，
// next 之后走到它的号上必须跳过，否则闸门永久卡死
// （这正是首版实现的实际缺陷：next=7、running=0，队首却停在已取消的 7 号票上）。
func TestTicketGateSkipsCanceledBehindHead(t *testing.T) {
	gate := newTicketGate(1)
	if !gate.acquire(1, make(chan struct{})) {
		t.Fatal("票 1 应立刻拿到额度")
	}

	canceled := make(chan struct{})
	// 票 3 先排队并被取消（此刻它排在票 2 后面，不是队首）。
	third := make(chan bool, 1)
	go func() { third <- gate.acquire(3, canceled) }()
	time.Sleep(30 * time.Millisecond)
	close(canceled)
	if ok := <-third; ok {
		t.Fatal("票 3 已被取消，不应拿到额度")
	}

	second := make(chan bool, 1)
	go func() { second <- gate.acquire(2, make(chan struct{})) }()
	fourth := make(chan bool, 1)
	go func() { fourth <- gate.acquire(4, make(chan struct{})) }()

	gate.release() // 票 1 放行
	select {
	case ok := <-second:
		if !ok {
			t.Fatal("票 2 应拿到额度")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("票 2 卡住了")
	}
	gate.release() // 票 2 放行：队首前进到已取消的 3 号，必须跳过并放行票 4
	select {
	case ok := <-fourth:
		if !ok {
			t.Fatal("票 4 应拿到额度")
		}
	case <-time.After(3 * time.Second):
		next, running, queued, pending := gate.snapshot()
		t.Fatalf("已取消的票号粘住了队首：next=%d running=%d queued=%v pending=%v", next, running, queued, pending)
	}
	gate.release()
}

// TestTicketGateCancelDoesNotLeakQuota：取消与唤醒撞车时额度要么被归还、
// 要么被明确持有，绝不能凭空消失。
func TestTicketGateCancelDoesNotLeakQuota(t *testing.T) {
	for round := 0; round < 200; round++ {
		gate := newTicketGate(1)
		if !gate.acquire(1, make(chan struct{})) {
			t.Fatal("票 1 应立刻拿到额度")
		}
		canceled := make(chan struct{})
		done := make(chan struct{})
		go func() {
			if gate.acquire(2, canceled) {
				// 取消与唤醒撞车时仍可能拿到额度：拿到就必须归还。
				gate.release()
			}
			close(done)
		}()
		time.Sleep(time.Duration(round%3) * 100 * time.Microsecond)
		gate.release()
		close(canceled)
		<-done

		// 无论票 2 是「被取消」还是「拿到后又归还」，票 3 都必须能拿到额度。
		acquired := make(chan bool, 1)
		go func() { acquired <- gate.acquire(3, make(chan struct{})) }()
		select {
		case ok := <-acquired:
			if !ok {
				t.Fatalf("第 %d 轮：票 3 不应被取消", round)
			}
			gate.release()
		case <-time.After(3 * time.Second):
			next, running, queued, pending := gate.snapshot()
			t.Fatalf("第 %d 轮：额度泄漏，票 3 等不到（next=%d running=%d queued=%v pending=%v）",
				round, next, running, queued, pending)
		}
		if waiting := gate.waiting(); waiting != 0 {
			t.Fatalf("第 %d 轮：排队残留 %d", round, waiting)
		}
	}
}

// TestTicketGateStressNeverStalls：500 张票、乱序到达、其中 1/7 一进场就被取消，
// 全部必须在限期内被服务（不卡死、不超并发、不残留排队）。
func TestTicketGateStressNeverStalls(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过")
	}
	const (
		limit = 4
		total = 500
	)
	gate := newTicketGate(limit)
	var running, peak int64
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(ticket uint64) {
			defer wg.Done()
			canceled := make(chan struct{})
			if ticket%7 == 0 {
				close(canceled) // 一部分票一进场就被取消
			}
			if !gate.acquire(ticket, canceled) {
				return
			}
			current := atomic.AddInt64(&running, 1)
			for {
				observed := atomic.LoadInt64(&peak)
				if current <= observed || atomic.CompareAndSwapInt64(&peak, observed, current) {
					break
				}
			}
			atomic.AddInt64(&running, -1)
			gate.release()
		}(uint64(i + 1))
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		next, running, queued, pending := gate.snapshot()
		head := uint64(0)
		if len(queued) > 0 {
			head = queued[0]
		}
		t.Fatalf("闸门卡死：next=%d running=%d 排队=%d（最小票号=%d）待跳过=%v",
			next, running, len(queued), head, pending)
	}
	if peak > limit {
		t.Fatalf("并发峰值 %d 超过上限 %d", peak, limit)
	}
	if waiting := gate.waiting(); waiting != 0 {
		t.Fatalf("不应有残留排队：%d", waiting)
	}
}

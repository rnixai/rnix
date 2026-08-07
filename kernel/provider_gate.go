package kernel

import (
	gocontext "context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Story 73.5 — per-provider LLM concurrency gate (FR7 / epic 73 收尾).
//
// 问题：N 个 agent 同时撞同一 provider 的限流 → 3N 次无意义请求的惊群放大。
// 解法：kernel 侧 per-provider counting semaphore（D2）——同一 provider 名同时
// in-flight 的 LLM 请求数 ≤ 配置上限，超出排队等待。
//
// 分层（与 73.2 一致）：drivers/ 绝不导入 kernel/（铁律），gate 放 kernel 侧
// 是因为它需要 proc.ctx 可取消（AC6）、proc.TouchHeartbeat 分块心跳（D4）、
// k.emitEvent 可观测（AC7）——这些都在 kernel 手里（D2 三条理由）。
//
// 与 BudgetPool 完全分离（AC4）：新结构独立实现，不碰 kernel/budget_pool.go
// （token/成本配额分配器，无 RPM/TPM 概念）。
//
// 最简原则（AC2 / Decker 拍板⑤）：只做 per-provider 并发数上限——无令牌桶
// 泛化、无窗口滑动、无优先级队列。opencode 的三层限流器是网关侧形态；rnix
// 是客户端，并发数上限足够消除惊群放大。

const (
	// defaultMaxConcurrency is the kernel-side default per-provider
	// concurrency limit (D1). 三条理由：
	//
	//   a) 与 MaxSpawnDepth = 4（kernel/kernel.go:70）同值同语义——「限深
	//      4 层 + 限宽 4 并发」两个维度一致，心智模型统一（epic AC1 明示
	//      限深限宽是两个正交维度）；
	//   b) epic 红线「不得为 1」——1 会把所有并发编排串行化，是性能回退
	//      而非限流保护；
	//   c) 典型编排中每个 agent 同一时刻至多 1 个 reasonStep 请求，4 允许
	//      4 个 agent 并行同 provider，超过即排队——「防惊群」而非
	//      「限吞吐」的正确刻度。
	defaultMaxConcurrency = 4

	// gateAcquireTimeout bounds a single gate wait (D4). 复用既有常量
	// maxInProcessWait（60s）不新造——「进程内等待预算」语义统一：单次
	// LLM 调用前的最坏额外等待 = gate 60s + backoff 60s 各自独立有界，
	// 组合矩阵「总等待仍受上限约束」由此满足（两段各 60s，合计 120s 硬
	// 上界，绝无无界等待）。
	gateAcquireTimeout = maxInProcessWait

	// gateWaitEmitThreshold is the queue wait above which a
	// provider_gate_wait event is emitted (D7 / NFR3) ——「进程为什么慢」是
	// 新的不可见问题，排队超过可感知时长必须留痕。正常获取（≤ 1s）零噪声
	// （healthy 流零噪声原则，48.5 同款）。
	gateWaitEmitThreshold = 1 * time.Second
)

// gateErrorKind distinguishes the two gate wait outcomes the call sites route
// differently (D5): a timeout means "this provider is congested right now"
// (→ fallback / terminal), a cancel means the process is being torn down
// (→ the interrupted path, exactly like a cancel during the write).
type gateErrorKind int

const (
	gateErrTimeout gateErrorKind = iota
	gateErrCancelled
)

// gateError is the kernel-local error returned by acquireProviderGate. It is
// deliberately NOT an LLM error and carries no llm sentinel: call sites must
// never feed it to isTransientLLMError / llmErrCode (D5 / AC6-7 — a retry
// would just re-queue on the same congested gate).
type gateError struct {
	kind     gateErrorKind
	provider string
	waited   time.Duration
}

func (e *gateError) Error() string {
	if e.kind == gateErrTimeout {
		return fmt.Sprintf("provider concurrency gate timeout: %s", e.provider)
	}
	return fmt.Sprintf("provider concurrency gate cancelled: %s", e.provider)
}

// gateEntry is one provider's counting semaphore. slots is a buffered channel
// of capacity `limit`; a free slot = an element in the buffer. Waiting never
// holds providerGate.mu — only map access does (占位 + per-entry 锁形状，
// 48.6 mount.go 的简化版：provider 首见时建 entry）。
type gateEntry struct {
	slots chan struct{}
	limit int
	// waiters is the number of acquires currently waiting on this entry
	// (guarded by providerGate.mu) — feeds the provider_gate_wait event's
	// `queued` field (D7 / AC7).
	waiters int
}

// providerGate is the per-provider counting semaphore (D2): map[provider]
// *gateEntry + mu sync.Mutex. Same provider = same bucket across all
// processes — that IS the per-provider semantic (D8).
type providerGate struct {
	mu      sync.Mutex
	entries map[string]*gateEntry
}

func newProviderGate() *providerGate {
	return &providerGate{entries: make(map[string]*gateEntry)}
}

// entry returns the provider's entry, creating it with limit on first sight.
// The limit is fixed at creation (first-wins): config is resolved per-process
// but is uniform per provider in practice, and a silent re-limit mid-flight
// would be worse than first-wins simplicity.
//
// A free slot is an element IN the buffer (the classic Go semaphore idiom):
// the channel is pre-filled with `limit` elements at creation, acquire
// RECEIVES a slot (blocks when the buffer is drained = at capacity), release
// SENDS one back.
func (g *providerGate) entry(provider string, limit int) *gateEntry {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.entries[provider]; ok {
		return e
	}
	e := &gateEntry{slots: make(chan struct{}, limit), limit: limit}
	for range limit {
		e.slots <- struct{}{}
	}
	g.entries[provider] = e
	return e
}

// release returns the slot held by a completed LLM write (D3 — release
// mirrors acquire exactly). Over-release (no matching acquire) is dropped
// rather than panicked: internal callers are trusted, and the channel must
// never silently grow past its capacity.
func (g *providerGate) release(provider string) {
	g.mu.Lock()
	e := g.entries[provider]
	g.mu.Unlock()
	if e == nil {
		return
	}
	select {
	case e.slots <- struct{}{}:
	default:
	}
}

// gate returns the kernel's provider gate, lazily created so test fixtures
// and legacy mocks that never touch the gate stay zero-config. The lazy init
// is race-free (providerGateOnce): multiple reasonStep goroutines can reach
// it at once during concurrent spawns.
func (k *KernelImpl) gate() *providerGate {
	k.providerGateOnce.Do(func() {
		k.providerGate = newProviderGate()
	})
	return k.providerGate
}

// acquireProviderGate waits for a per-provider slot before an LLM write
// (D3 — called at all three LLM write sites: reason.go primary, attemptFallback,
// compact.go). Semantics:
//
//   - limit: resolved per-process via the three-level chain (D1: project →
//     global closure → defaultMaxConcurrency) at entry creation;
//   - cancellable: select on ctx.Done, NEVER bare channel blocking (D4 — the
//     73.2 AC6 lesson: a SIGTERM arriving mid-wait must route to the
//     interrupted path, one SIGTERM = one exit_reason);
//   - heartbeat: the wait is sliced into heartbeatRefreshInterval chunks with
//     proc.TouchHeartbeat() between chunks (D4 — the 73.2 D6 lesson: an
//     un-refreshed 60s wait looks like a stall to the heartbeat monitor);
//   - hard cap: gateAcquireTimeout (60s = maxInProcessWait) bounds the wait
//     (D4);
//   - observability: waiting > gateWaitEmitThreshold emits
//     provider_gate_wait {provider, limit, queued, wait_ms}; a timeout emits
//     provider_gate_timeout {provider, limit, wait_ms} + one log line (D7).
//     Fast acquires emit nothing (healthy 流零噪声).
//
// The returned *gateError must be routed per D5: timeout → attemptFallback /
// terminal write-fail (gate text in the exit_reason), cancel → the
// interrupted path. Never into isTransientLLMError.
func (k *KernelImpl) acquireProviderGate(ctx gocontext.Context, proc *Process, provider string) error {
	if provider == "" {
		// No provider name (bare device path) — nothing to gate.
		return nil
	}
	limit := k.resolveProviderConcurrencyLimit(proc, provider)
	if limit <= 0 {
		limit = defaultMaxConcurrency
	}
	entry := k.gate().entry(provider, limit)

	// Fast path: a slot is free right now — zero events, zero heartbeat
	// (healthy flow stays silent, D7).
	select {
	case <-entry.slots:
		return nil
	default:
	}

	waitStart := time.Now()
	k.gate().mu.Lock()
	entry.waiters++
	k.gate().mu.Unlock()
	defer func() {
		k.gate().mu.Lock()
		entry.waiters--
		k.gate().mu.Unlock()
	}()

	waited := time.Duration(0)
	waitEmitted := false
	for {
		chunk := min(gateAcquireTimeout-waited, heartbeatRefreshInterval)
		select {
		case <-entry.slots:
			return nil
		case <-ctx.Done():
			return &gateError{kind: gateErrCancelled, provider: provider, waited: time.Since(waitStart)}
		case <-sleepFunc(chunk):
			proc.TouchHeartbeat()
			waited += chunk
			if waited >= gateAcquireTimeout {
				elapsed := time.Since(waitStart)
				k.emitEvent(proc, "provider_gate_timeout", map[string]any{
					"provider": provider,
					"limit":    entry.limit,
					"wait_ms":  elapsed.Milliseconds(),
				}, nil, nil, elapsed)
				log.Printf("[kernel] pid=%d provider gate timeout: provider=%s limit=%d waited=%v",
					proc.PID, provider, entry.limit, elapsed)
				return &gateError{kind: gateErrTimeout, provider: provider, waited: elapsed}
			}
			if !waitEmitted && waited >= gateWaitEmitThreshold {
				elapsed := time.Since(waitStart)
				// queued counts every acquire waiting on this provider's gate
				// INCLUDING the one emitting the event (defined once, asserted
				// fixed in tests — D7 / AC7).
				k.gate().mu.Lock()
				queued := entry.waiters
				k.gate().mu.Unlock()
				k.emitEvent(proc, "provider_gate_wait", map[string]any{
					"provider": provider,
					"limit":    entry.limit,
					"queued":   queued,
					"wait_ms":  elapsed.Milliseconds(),
				}, nil, nil, elapsed)
				waitEmitted = true
			}
		}
	}
}

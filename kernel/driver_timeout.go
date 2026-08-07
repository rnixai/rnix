package kernel

import (
	"log"
	"math"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
)

// Story 71.3 — compact 超时派生化（R2 + F3）。
//
// compact 超时不再是独立硬编码常量，而是由 driver 层超时派生：
//
//	compactTimeout = driverTimeout × compactTimeoutMultiplier   // 倍数 = 4
//
// driverTimeout 的解析链（三级，与 context_window 同源同形）：
//
//	① 项目级 .rnix/providers.yaml 的 providers[].timeout_sec
//	    → proc.ProjectConfig.Providers 类型断言（lookupProjectDriverTimeoutSec）
//	② 全局 ~/.config/rnix/providers.yaml 的 timeout_sec
//	    → k.driverTimeoutFunc 注入闭包（cmd/rnix/main.go）
//	③ driver 家族公共默认 llm.DefaultTimeout（5 分钟）
//
// 项目级优先，且项目级 miss 不得遮蔽全局命中——与 lookupProjectContextWindow
// (ctx_token_limit.go) 同形。类型断言失败 / nil config 一律视为 LOOKUP MISS
// 而非 panic。
//
// ×4 的语义理由（codex COMPACT_REQUEST_TIMEOUT_IDLE_MULTIPLIER 原文）：
// "/responses/compact is unary, so the timeout covers the full response rather
// than one idle period between stream events." 即 idle 基数 → 整体 wall-clock
// 预算的换算，不是任意安全系数。
//
// 四级优先：opts.CompactTimeout > agent.Manifest.CompactTimeout > 派生值 >
// DefaultCompactTimeout（地板）。前两级由自由函数 applyCompactTimeout 处理
// （签名不变，AC6-① 红线），本文件的 resolveCompactTimeout 仅在字段仍为 0 时
// 填派生值。

// lookupProjectDriverTimeoutSec reads providers[].timeout_sec out of the
// process's project-level providers view. Shape copied verbatim from
// lookupProjectContextWindow (ctx_token_limit.go): ProjectConfig.Providers is
// typed `any` to avoid a config→drivers/llm import cycle, and a failed
// type-assert or nil config is treated as a LOOKUP MISS (→ caller falls back
// to the global snapshot), never a panic. 0 likewise means "no declaration
// here", not "declared as zero".
//
// Unlike lookupProjectContextWindow this takes only provider (no model):
// TimeoutSec is a ProviderConfig instance-level field (drivers/llm/config.go),
// not per-model.
func lookupProjectDriverTimeoutSec(pc *config.ProjectConfig, provider string) int {
	if pc == nil || provider == "" {
		return 0
	}
	pcfg, ok := pc.Providers.(*llm.ProvidersConfig)
	if !ok || pcfg == nil {
		return 0
	}
	for i := range pcfg.Providers {
		if pcfg.Providers[i].Name != provider {
			continue
		}
		return pcfg.Providers[i].TimeoutSec
	}
	return 0
}

// resolveDriverTimeout resolves the driver per-request timeout for proc's
// provider via the three-level chain: project-level providers.yaml → injected
// global closure → llm.DefaultTimeout (the driver family's shared default,
// 5 minutes). The result is always positive.
func (k *KernelImpl) resolveDriverTimeout(proc *Process) time.Duration {
	// ① Project-level (the merged global∪project view — a project miss
	//    cannot hide a global declaration, a project hit is more specific).
	if sec := lookupProjectDriverTimeoutSec(proc.ProjectConfig, proc.Provider); sec > 0 {
		// Guard: sec × 1e9 overflows int64 for sec > ~9.2e9 (≈292 years).
		// Clamp to the driver family default rather than wrapping negative —
		// a negative Duration would bypass the ×4 overflow guard in
		// resolveCompactTimeout and could wrap back to a ~100-year positive
		// value (the exact "never times out" outcome AC2-③ forbids).
		if sec > int(math.MaxInt64/int64(time.Second)) {
			return llm.DefaultTimeout
		}
		return time.Duration(sec) * time.Second
	}
	// ② Global snapshot via injected closure.
	if k.driverTimeoutFunc != nil {
		if d := k.driverTimeoutFunc(proc.Provider); d > 0 {
			return d
		}
	}
	// ③ Driver family default. llm.DefaultTimeout is the shared 5-minute
	//    default reused by all 7 drivers (claude-cli / cursor-cli / codex-cli /
	//    qwen-cli / openai / anthropic / gemini); referencing
	//    it directly (rather than mirroring a second constant) lets kernel
	// track driver-side drift automatically.
	return llm.DefaultTimeout
}

// lookupProjectProviderConcurrencyLimit reads providers[].max_concurrency out
// of the process's project-level providers view (Story 73.5 / D1). Shape
// copied from lookupProjectDriverTimeoutSec: ProjectConfig.Providers is typed
// `any` to avoid a config→drivers/llm import cycle, and a failed type-assert,
// nil config, or empty provider is a LOOKUP MISS (→ the caller falls through
// to the global closure / default), never a panic. 0 likewise means "no
// declaration here" (unset OR explicitly zero — AC3: MaxConcurrency=0 is
// equivalent to the field being absent).
func lookupProjectProviderConcurrencyLimit(pc *config.ProjectConfig, provider string) int {
	if pc == nil || provider == "" {
		return 0
	}
	pcfg, ok := pc.Providers.(*llm.ProvidersConfig)
	if !ok || pcfg == nil {
		return 0
	}
	for i := range pcfg.Providers {
		if pcfg.Providers[i].Name != provider {
			continue
		}
		return pcfg.Providers[i].MaxConcurrency
	}
	return 0
}

// resolveProviderConcurrencyLimit resolves the per-provider concurrency limit
// via the three-level chain (Story 73.5 / D1): project-level providers.yaml →
// injected global closure → defaultMaxConcurrency. The result is always
// positive — a 0 limit would deadlock the gate forever (the entry's channel
// would never have a free slot), so unset/zero at every level lands on the
// default (AC3's "zero-value config → default 4" pin).
//
// The provider argument is the gate key itself (D8) — the fallback call gates
// on the FALLBACK provider's bucket, and its limit must be resolved from the
// fallback provider's own config, not proc.Provider's.
func (k *KernelImpl) resolveProviderConcurrencyLimit(proc *Process, provider string) int {
	// ① Project-level (the merged global∪project view — a project miss
	//    cannot hide a global declaration, a project hit is more specific).
	if n := lookupProjectProviderConcurrencyLimit(proc.ProjectConfig, provider); n > 0 {
		return n
	}
	// ② Global snapshot via injected closure.
	if k.providerConcurrencyLimitFunc != nil {
		if n := k.providerConcurrencyLimitFunc(provider); n > 0 {
			return n
		}
	}
	// ③ Kernel-side default (D1 — the three reasons live in provider_gate.go).
	return defaultMaxConcurrency
}

// resolveCompactTimeout fills proc.CompactTimeout with the derived value
// (driverTimeout × compactTimeoutMultiplier) when the field is still 0 — i.e.
// neither opts nor agent manifest supplied an explicit value. Called after
// applyCompactTimeout on the spawn path, and after resolveContextBudget on
// both resume paths (so an unconfigured process re-derives on resume without
// depending on disk persistence).
//
// When the field is already non-zero (explicit config), this function does NOT
// override it, but warns if the explicit value is shorter than the driver's
// per-request timeout (AC2-② inversion): the operator's value is respected
// (provenance — they may be deliberately failing compact fast), but the
// inversion is surfaced.
//
// 🔴 Overflow guard (AC2-③): time.Duration is int64 nanoseconds (≈292 years),
// so driverTimeout × 4 overflows negative for large timeout_sec — which would
// make gocontext.WithTimeout expire immediately and permanently disable
// compaction. codex defends with saturating_mul (client.rs:601-602); Go has
// none, so we clamp to llm.DefaultTimeout × compactTimeoutMultiplier (20 min)
// — NOT math.MaxInt64 (a 292-year timeout is indistinguishable from never,
// converting a visible defect into a silent hang).
func (k *KernelImpl) resolveCompactTimeout(proc *Process) {
	if proc == nil {
		return
	}
	driverTimeout := k.resolveDriverTimeout(proc)

	if proc.CompactTimeout == 0 {
		// Derive: driverTimeout × 4, clamped on overflow. The <= 0 check is
		// belt-and-suspenders: resolveDriverTimeout guarantees a positive
		// result, but a negative value slipping through (e.g. from a future
		// closure path) would bypass the > MaxInt64/4 guard and wrap back to
		// a large positive — the "never times out" outcome AC2-③ forbids.
		var derived time.Duration
		if driverTimeout <= 0 || driverTimeout > time.Duration(math.MaxInt64/compactTimeoutMultiplier) {
			derived = llm.DefaultTimeout * compactTimeoutMultiplier // 20 min ceiling
			log.Printf("[kernel] pid=%d driver timeout %v overflows ×%d; compact timeout clamped to %v",
				proc.PID, driverTimeout, compactTimeoutMultiplier, derived)
		} else {
			derived = driverTimeout * compactTimeoutMultiplier
		}
		proc.CompactTimeout = derived
		return
	}

	// Explicit value: warn on inversion (AC2-②), never clamp.
	if proc.compactTimeoutExplicit && proc.CompactTimeout < driverTimeout {
		log.Printf("[kernel] pid=%d compact_timeout %v is shorter than the driver's per-request timeout %v — compact may time out before a normal LLM call would",
			proc.PID, proc.CompactTimeout, driverTimeout)
	}
}

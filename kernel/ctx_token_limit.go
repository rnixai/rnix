package kernel

import (
	"log"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
)

// Story 69.2 — token 轴刻度接通。Story 71.1 — 项目级 provider 接通（R5）。
//
// 刻度来源链（配置语义的唯一路径，改任一环都会让 token 轴失准）：
//
//	providers.yaml → providers[].models[<model>].context_window
//	  → ①项目级 .rnix/providers.yaml：ipc/server_spawn.go 的 DeepMergeYAML
//	      → ProjectConfig.Providers → lookupProjectContextWindow（本文件）
//	    ②全局 ~/.config/rnix/providers.yaml：cmd/rnix/main.go 的
//	      SetContextWindowFunc 闭包 → k.contextWindowFunc(provider, model)
//	  → proc.ContextWindow
//	  → * 9/10（安全余量：本轮输出 + tool schema + 估算残差）
//	  → proc.ContextBudget（clamp 到 window）
//	  → ctx.TokenLimit（本文件的 applyCtxTokenLimit）
//	  → Context.effectiveTokenLimit() → TokenUsage().Limit
//	  → autoCompactIfNeeded 的 token 阈值分母
//
// 在 Story 69.2 之前最后一跳缺失：context.Manager.SetTokenLimit 全仓零调用方，
// 故 ctx.TokenLimit 恒 0 并回落 DefaultTokenLimit(200k)。声明 context_window:
// 983616 的 provider 被当 20 万用，token 阈值永不触发，256 条消息槽位成了事实
// 上的容量天花板。
//
// 在 Story 71.1 之前 ②是唯一来源，而那个闭包按值捕获 daemon 启动期的 GLOBAL
// 快照：项目级配置合并后只流向 ProjectConfig.Providers / driver factories /
// status cache 三处，无任何一条边回到 contextWindowFunc。于是"只在项目级声明"
// 的 provider 刻度恒为 0 —— 69.2 的接线完好，喂进去的数却是 0。①补上了那条边。
//
// 取 ContextBudget（window*9/10）而非裸 window 是刻意的：debug/ctx_profile.go
// 的 usagePct、ipc/server_process.go 的 ContextStats.UsagePct、cmd/rnix/top.go
// 的 top 显示分母都已是 ContextBudget，取 budget 让 TokenUsage().Percentage 与
// 这四处观测口径天然一致，不新增分母分叉。

// lookupProjectContextWindow reads models[model].context_window out of the
// process's project-level providers view. That view is the already
// DeepMergeYAML'd global+project result assembled by ipc/server_spawn.go, so a
// hit here is the most specific declaration available.
//
// Degradation is copied verbatim from projectHasProvider (spawn.go):
// ProjectConfig.Providers is typed `any` to avoid a config→drivers/llm import
// cycle, and a failed type-assert or nil config is treated as a LOOKUP MISS
// (→ caller falls back to the global snapshot), never a panic. 0 likewise means
// "no declaration here", not "declared as zero".
func lookupProjectContextWindow(pc *config.ProjectConfig, provider, model string) int {
	if pc == nil || provider == "" || model == "" {
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
		return pcfg.Providers[i].Models[model].ContextWindow
	}
	return 0
}

// resolveContextBudget resolves proc.ContextWindow from the project-level
// providers view first and the injected contextWindowFunc second, derives
// proc.ContextBudget (9/10 of the window) when no explicit budget was supplied,
// and clamps an over-large budget back to the window. It is the single owner of
// that three-step arithmetic — spawn and the checkpoint-resume path both call it
// so the 9/10 ratio and the clamp cannot drift between call sites (the
// wire-mirror lesson of Epic 66-4, applied here).
//
// Lookup order is project-then-global because the project view is the merged
// (global ∪ project) result: a project miss cannot hide a global declaration,
// while a project hit is by definition the more specific one.
//
// A non-zero result wins over any window inherited from disk: providers.yaml is
// the current truth, so a resumed process honours a window that was edited after
// its snapshot was written. A zero result (unknown model, or no source at all)
// leaves an inherited window intact rather than erasing it — the spawn path has
// nothing to inherit there, so that guard is a no-op for spawn and only protects
// the two resume paths (whose ProjectConfig may legitimately be nil).
func (k *KernelImpl) resolveContextBudget(proc *Process) {
	if proc == nil {
		return
	}

	// A zero result means "this provider+model declares no window" and must
	// NOT erase a window the resume paths already inherited from disk
	// (proc-info.json persists context_window). On the spawn path
	// proc.ContextWindow is always 0 here, so the guard is a no-op there and
	// this stays byte-equivalent to the pre-69.2 inline assignment.
	if w := lookupProjectContextWindow(proc.ProjectConfig, proc.Provider, proc.Model); w > 0 {
		proc.ContextWindow = w
	} else if k.contextWindowFunc != nil {
		if w := k.contextWindowFunc(proc.Provider, proc.Model); w > 0 {
			proc.ContextWindow = w
		}
	}

	if proc.ContextBudget == 0 && proc.ContextWindow > 0 {
		proc.ContextBudget = proc.ContextWindow * 9 / 10
	}
	if proc.ContextBudget > 0 && proc.ContextWindow > 0 && proc.ContextBudget > proc.ContextWindow {
		log.Printf("[kernel] pid=%d clamped context_budget %d to context_window %d",
			proc.PID, proc.ContextBudget, proc.ContextWindow)
		proc.ContextBudget = proc.ContextWindow
	}
}

// applyCtxTokenLimit propagates the resolved proc.ContextBudget into the
// process's context so the token axis measures against the configured window
// instead of DefaultTokenLimit. Call it after resolveContextBudget and after
// the context has been allocated — never at CtxAlloc time, where proc.Provider
// and proc.Model are still empty and contextWindowFunc would return 0
// (a connection that "succeeds" while always writing 0 is the silent-failure
// version of this fix).
//
// Degradation is deliberate and must stay: without a resolved window
// ctx.TokenLimit stays 0 so effectiveTokenLimit() falls back to
// DefaultTokenLimit. Writing any other sentinel here would fabricate a scale
// the operator never configured.
//
// The gate is proc.contextCapacityConfigured() — i.e. proc.ContextWindow > 0,
// NOT merely a non-zero budget, because ContextBudget is an overloaded field. See
// Process.contextCapacityConfigured (kernel/process.go) for the full rationale;
// the short version is that an explicit per-step input-token leash (4096 is a
// realistic manifest value) says nothing about the model's total window, and
// treating one as a capacity scale would put a tight-leash agent permanently over
// the compact threshold and compact it on every step — a behaviour change no AC
// asked for. So an explicit budget still feeds the four existing usagePct
// denominators (debug/ctx_profile.go, ipc/server_process.go, cmd/rnix/top.go)
// exactly as before, but it only becomes the ctx token scale when a real
// context_window backs it.
//
// SetTokenLimit failures are logged, never fatal. A degraded scale is an
// observability-precision problem; failing the spawn/resume would escalate it
// into an availability problem.
func (k *KernelImpl) applyCtxTokenLimit(proc *Process) {
	if proc == nil || proc.CtxID == 0 || k.ctxMgr == nil {
		return
	}
	if !proc.contextCapacityConfigured() {
		return
	}
	if err := k.ctxMgr.SetTokenLimit(proc.CtxID, proc.ContextBudget); err != nil {
		log.Printf("[kernel] pid=%d SetTokenLimit(cid=%d, %d) failed: %v — token axis falls back to the default limit",
			proc.PID, proc.CtxID, proc.ContextBudget, err)
	}
}

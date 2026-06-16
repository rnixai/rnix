// Package inspector — raw_lens.go (Story 56.4 · CAP-3 路② Raw I/O lens)
//
// Raw lens（Lens ❻）的 pure helper：消费一条 vfs.RawCapture + width，输出
// 渲染串。镜像 meta_lens.go / system_lens.go 形态——包级 pure function、无
// dashboardModel 状态依赖、ASCII 降级走 ui.IsASCIIMode、nil/空零值安全。
//
// 数据形状（56.2/56.3 已落盘 · 本 story 只读消费）：
//   - kind="api": request{method,url,headers(脱敏),body} / response{status,body,headers}
//   - kind="cli": request{argv([]string),stdin,env(脱敏)} / response{stdout,stderr,exit_code}
//
// RenderRawLens 按 rc.Kind 分支取键（map[string]any · JSON roundtrip 后 headers
// 是 map[string]any，argv 是 []any——见 deferred #23 预警）。effort 真实值在
// API 的 body（reasoning_effort）/ CLI 的 argv（--effort / -c model_reasoning_effort）
// 中，这正是 CAP-1/CAP-3 要可见的核心。
//
// 范围铁律（Story 56.4 AC#5）：读到的是已脱敏指纹（redacted(len=..,sha256=..)），
// 本文件零反脱敏代码——读到什么显示什么。截断标记（Truncated / OriginalBytes）
// 在顶部可见。
//
// SKELETON (Story 56.4 RED): 当前仅返回占位串保证编译。dev 移除 raw_lens_test.go
// 的 t.Skip 后填充 API/CLI 分支渲染 + 截断/parse-error 标记 + ASCII 降级，并
// 验证 RED→GREEN。
package inspector

import (
	"github.com/rnixai/rnix/vfs"
)

// RenderRawLens 渲染单条 RawCapture（API / CLI 两族）为人类可读串。
// rc==nil → 占位串（懒加载未命中 / 该 step 无 raw 记录）。
//
// SKELETON: dev 实现 kind 分支 + 字段渲染 + 截断标记 + ASCII 降级。
func RenderRawLens(rc *vfs.RawCapture, width int) string {
	if rc == nil {
		return "  (no raw capture for this step)"
	}
	// SKELETON placeholder — dev 替换为按 rc.Kind 分支的完整渲染。
	return "  (raw lens not yet implemented)"
}

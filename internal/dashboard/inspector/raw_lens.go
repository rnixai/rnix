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
package inspector

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/vfs"
)

// RenderRawLens 渲染单条 RawCapture（API / CLI 两族）为人类可读串。
// rc==nil → 占位串（懒加载未命中 / 该 step 无 raw 记录）。
func RenderRawLens(rc *vfs.RawCapture, width int) string {
	if rc == nil {
		return "  (no raw capture for this step)"
	}

	var b strings.Builder

	// 顶部元信息行 + 截断标记（AC#3 / AC#5：Truncated/OriginalBytes 可见）。
	kind := rc.Kind
	if kind == "" {
		kind = "unknown"
	}
	fmt.Fprintf(&b, "%s step %d · kind=%s\n", rawWarnGlyph(rc.Truncated), rc.Step, kind)
	if rc.Truncated {
		fmt.Fprintf(&b, "%s truncated (original %s bytes)\n",
			rawWarnGlyph(true), FormatThousands(int(rc.OriginalBytes)))
	}
	b.WriteString("\n")

	switch rc.Kind {
	case "cli":
		renderRawCLI(&b, rc, width)
	default:
		// "api" 与未知 kind 均走 API 渲染（字段缺失时各 helper 自然跳过）。
		renderRawAPI(&b, rc, width)
	}

	return strings.TrimRight(b.String(), "\n")
}

// rawWarnGlyph 返回截断/警告前缀字形（ASCII 降级）。truncated=false 时返回
// 普通项目符号。
func rawWarnGlyph(warn bool) string {
	if warn {
		if ui.IsASCIIMode() {
			return "!"
		}
		return "⚠"
	}
	if ui.IsASCIIMode() {
		return "*"
	}
	return "•"
}

// renderRawAPI 渲染 API 族 request{method,url,headers,body} / response{status,body}。
func renderRawAPI(b *strings.Builder, rc *vfs.RawCapture, width int) {
	b.WriteString(RenderMetaSectionHeader("Request", width) + "\n")
	if m := rc.Request; m != nil {
		if method := rawString(m["method"]); method != "" {
			fmt.Fprintf(b, "  method: %s\n", method)
		}
		if url := rawString(m["url"]); url != "" {
			fmt.Fprintf(b, "  url:    %s\n", url)
		}
		renderRawHeaders(b, m["headers"])
		if body := rawString(m["body"]); body != "" {
			b.WriteString("  body:\n")
			b.WriteString(indentBlock(body))
		}
	}

	b.WriteString("\n" + RenderMetaSectionHeader("Response", width) + "\n")
	if m := rc.Response; m != nil {
		if status := rawStatusString(m["status"]); status != "" {
			fmt.Fprintf(b, "  status: %s\n", status)
		}
		renderRawHeaders(b, m["headers"])
		if body := rawString(m["body"]); body != "" {
			b.WriteString("  body:\n")
			b.WriteString(indentBlock(body))
		}
	}
}

// renderRawCLI 渲染 CLI 族 request{argv,stdin,env} / response{stdout,stderr,exit_code}。
func renderRawCLI(b *strings.Builder, rc *vfs.RawCapture, width int) {
	b.WriteString(RenderMetaSectionHeader("Request", width) + "\n")
	if m := rc.Request; m != nil {
		if argv := rawStringSlice(m["argv"]); len(argv) > 0 {
			fmt.Fprintf(b, "  argv:   %s\n", strings.Join(argv, " "))
		}
		if stdin := rawString(m["stdin"]); stdin != "" {
			b.WriteString("  stdin:\n")
			b.WriteString(indentBlock(stdin))
		}
		renderRawEnv(b, m["env"])
	}

	b.WriteString("\n" + RenderMetaSectionHeader("Response", width) + "\n")
	if m := rc.Response; m != nil {
		if ec := m["exit_code"]; ec != nil {
			fmt.Fprintf(b, "  exit_code: %s\n", rawStatusString(ec))
		}
		if stdout := rawString(m["stdout"]); stdout != "" {
			b.WriteString("  stdout:\n")
			b.WriteString(indentBlock(stdout))
		}
		if stderr := rawString(m["stderr"]); stderr != "" {
			b.WriteString("  stderr:\n")
			b.WriteString(indentBlock(stderr))
		}
	}
}

// renderRawHeaders 渲染 headers map（脱敏指纹原样显示，AC#5）。键按字母序排序
// 以保证渲染稳定。
func renderRawHeaders(b *strings.Builder, v any) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("  headers:\n")
	for _, k := range keys {
		fmt.Fprintf(b, "    %s: %s\n", k, rawString(m[k]))
	}
}

// renderRawEnv 渲染 env map（脱敏指纹原样显示，AC#5）。
func renderRawEnv(b *strings.Builder, v any) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("  env:\n")
	for _, k := range keys {
		fmt.Fprintf(b, "    %s=%s\n", k, rawString(m[k]))
	}
}

// indentBlock 以 4 空格缩进多行文本块，保证 body/stdin/stdout 视觉归属。
func indentBlock(s string) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("    " + line + "\n")
	}
	return b.String()
}

// rawString 把 any 安全转为 string（非 string 用 %v 兜底，nil → 空串）。
func rawString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// rawStatusString 渲染 status / exit_code（JSON roundtrip 后是 float64）为整数串。
func rawStatusString(v any) string {
	switch n := v.(type) {
	case float64:
		return fmt.Sprintf("%d", int(n))
	case int:
		return fmt.Sprintf("%d", n)
	case int64:
		return fmt.Sprintf("%d", n)
	case string:
		return n
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// rawStringSlice 把 argv 安全转为 []string（JSON roundtrip 后是 []any）。
func rawStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, e := range s {
			out = append(out, rawString(e))
		}
		return out
	default:
		return nil
	}
}

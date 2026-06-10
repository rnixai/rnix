package intentdriver

import (
	"context"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================
// ATDD RED PHASE — Story 54.3: AC4.2 + AC4.3（drivers/intent 运行时文本去设备路径）
//
// 两个 LLM 可见泄漏点（Decision 45 模块③显式纳入）：
//   AC4.2 — driver.go:59 IntentDecompose 的 auto_start 参数 ToolDef.Description
//           （注入 LLM system prompt）：「（可经 /dev/shell 绕过）」→「（可经 Bash 绕过）」。
//   AC4.3 — driver.go:258 auto_start daemon-restart 守卫 error 文本
//           （经 tool_exec.go:270 格式化进 tool result → LLM 直读）：
//           「子任务仍可经 /dev/shell（kill/pkill/socat 等）绕过」→「…可经 Bash…」。
//
// 载体：
//   AC4.2 — driver.ToolDefs() 导航 Parameters["properties"]["auto_start"]["description"]
//           （ToolDef.Subpath 标 json:"-" 不泄漏 LLM；description 是 LLM 可见文本）。
//   AC4.3 — 复用 37-3 的 newGuardTestDriver + FileFactory 行为触发 driver.go:258
//           DriverError，再 err.Error() 断言（与 atdd_37_4 P0 同构）。
//
// ⚠️ AC4.3 关键陷阱：internal/types/types.go:151 的 DriverError.Error() =
//   "[Code] Op: Device (Err)"，driver.go:258 的 Device = f.devicePath = "/dev/intent/decompose"
//   → err.Error() **永远含 /dev/intent/decompose**（Device+Subpath 字段，AC7 保留的路由
//   锚点 + story §范围问题#1 已知的 Error() 泄漏，本 story 不处理）。故 INT-017 必须精确
//   断言「不含 /dev/shell」，绝不能用泛 /dev/（否则被 /dev/intent/decompose 永久命中、
//   即使 dev 正确改了也红）。路由锚点护栏（INT-020）改为直接读 de.Device 字段，与文本断言正交。
//
//   🔴 RED: INT-015/016/017/018
//   🟢 护栏: INT-019（best-effort 诚实化语义保留，AC4.3/AC6）、INT-020（Device 锚点，AC7）
//
// AC6 边界：37-4 既有测试（atdd_37_4_..._test.go:56-57 硬断言 /dev/shell）的同步由
// dev-story 阶段修（硬回归，本 ATDD 不碰该文件）；INT-017/018/019 在此覆盖 driver.go:258
// 最终态的同等语义（去 /dev/shell + 引入 Bash + 保留 best-effort）。
//
// 设计说明：被测对象（ToolDefs / newGuardTestDriver / FileFactory）均已存在 → 可编译
// → RED 为运行时断言失败，非 t.Skip。
// ============================================================

// atdd543AutoStartDescription 取 IntentDecompose 工具 auto_start 参数的 description
// （driver.go:59，LLM 可见）。
func atdd543AutoStartDescription(t *testing.T) string {
	t.Helper()
	driver, _ := newGuardTestDriver("[]") // ToolDefs 不依赖 mock nodes，空数组即可
	for _, d := range driver.ToolDefs() {
		if d.Name != "IntentDecompose" {
			continue
		}
		props, ok := d.Parameters["properties"].(map[string]any)
		if !ok {
			t.Fatalf("IntentDecompose Parameters[\"properties\"] 非 map[string]any: %T", d.Parameters["properties"])
		}
		autoStart, ok := props["auto_start"].(map[string]any)
		if !ok {
			t.Fatalf("properties[\"auto_start\"] 非 map[string]any: %T", props["auto_start"])
		}
		desc, ok := autoStart["description"].(string)
		if !ok {
			t.Fatalf("auto_start[\"description\"] 非 string: %T", autoStart["description"])
		}
		return desc
	}
	t.Fatal("未在 ToolDefs() 找到 IntentDecompose 工具定义")
	return ""
}

// atdd543TriggerGuardError 触发 driver.go:258 的 auto_start daemon-restart 守卫
// DriverError（与 atdd_37_4 P0 同构：含 "rnix daemon stop" 节点 + auto_start=true）。
func atdd543TriggerGuardError(t *testing.T) error {
	t.Helper()
	nodesJSON := `[{"id":"r","intent":"运行 rnix daemon stop 重启 daemon","depends_on":[]}]`
	driver, _ := newGuardTestDriver(nodesJSON)
	file, ferr := FileFactory(driver)("/decompose", vfs.O_RDWR, "")
	if ferr != nil {
		t.Fatalf("FileFactory open /decompose: %v", ferr)
	}
	err := file.Write(context.Background(), []byte(`{"intent":"restart the daemon","auto_start":true}`))
	if err == nil {
		t.Fatal("expected auto_start guard to reject daemon-restart node, got nil")
	}
	return err
}

// --- 54.3-INT-015: [P0] AC4.2 auto_start 参数 description 无 /dev/shell ---
// 🔴 RED: 当前 :59 「（可经 /dev/shell 绕过）」。
func TestATDD_54_3_AC4_IntentAutoStartDesc_NoShellDevicePath(t *testing.T) {
	desc := atdd543AutoStartDescription(t)
	if strings.Contains(desc, "/dev/shell") {
		t.Errorf("IntentDecompose auto_start 参数 description 仍含 \"/dev/shell\" (AC4.2: :59 （可经 /dev/shell 绕过）→（可经 Bash 绕过）)\n  desc: %s", desc)
	}
}

// --- 54.3-INT-016: [P1] AC4.2 auto_start 参数 description 用 Bash ---
// 🔴 RED: 当前 description 无 "Bash"。
// 注意：description 含合法 CLI 命令名 "rnix daemon stop/restart"（公开接口，保留），
// 故仅断言去 /dev/shell + 引入 Bash，不误伤 CLI 命令名。
func TestATDD_54_3_AC4_IntentAutoStartDesc_UsesBash(t *testing.T) {
	desc := atdd543AutoStartDescription(t)
	if !strings.Contains(desc, "Bash") {
		t.Errorf("IntentDecompose auto_start 参数 description 未引用 \"Bash\" (AC4.2: 绕过途径 /dev/shell → Bash)\n  desc: %s", desc)
	}
}

// --- 54.3-INT-017: [P0] AC4.3 守卫 error 文本无 /dev/shell ---
// 🔴 RED: 当前 :258 「子任务仍可经 /dev/shell（kill/pkill/socat 等）绕过」。
// ⚠️ 精确断言「不含 /dev/shell」——见文件头陷阱说明（Device 字段注入 /dev/intent）。
func TestATDD_54_3_AC4_AutoStartGuardError_NoShellDevicePath(t *testing.T) {
	msg := atdd543TriggerGuardError(t).Error()
	if strings.Contains(msg, "/dev/shell") {
		t.Errorf("auto_start 守卫 error 仍含 \"/dev/shell\" (AC4.3: :258 仍可经 /dev/shell 绕过 → 仍可经 Bash 绕过)\n  msg: %s", msg)
	}
}

// --- 54.3-INT-018: [P1] AC4.3 守卫 error 文本用 Bash ---
// 🔴 RED: 当前 error 文本无 "Bash"。
func TestATDD_54_3_AC4_AutoStartGuardError_UsesBash(t *testing.T) {
	msg := atdd543TriggerGuardError(t).Error()
	if !strings.Contains(msg, "Bash") {
		t.Errorf("auto_start 守卫 error 未引用 \"Bash\" (AC4.3: 绕过途径 /dev/shell → Bash)\n  msg: %s", msg)
	}
}

// --- 54.3-INT-019: [P1] AC4.3/AC6 守卫 error 保留 37-4 best-effort 诚实化语义 ---
// 🟢 护栏: 当前即绿。去设备路径时只换「绕过途径」称呼，不得删 37-4 诚实化措辞
// （best-effort 性质 + 明示绕过 + 无绝对保证），见 [[intent-autostart-daemon-restart-decision]]。
func TestATDD_54_3_AC4_AutoStartGuardError_PreservesBestEffortHonesty(t *testing.T) {
	msg := atdd543TriggerGuardError(t).Error()
	if !strings.Contains(msg, "best-effort") && !strings.Contains(msg, "尽力而为") {
		t.Errorf("auto_start 守卫 error 丢失 best-effort 诚实化语义 (AC6 红线: 只换绕过途径称呼 /dev/shell→Bash，不得删 37-4 诚实化措辞)\n  msg: %s", msg)
	}
}

// --- 54.3-INT-020: [P1] AC7 DriverError.Device 路由锚点不变 ---
// 🟢 护栏: 当前即绿。Device 字段是 Layer 1 内核路由锚点（Decision 45 保留），本 story
// 只改 Err 文本不动 Device。（注：Device 经 Error() 泄漏 /dev/intent/decompose 给 LLM 是
// story §范围问题#1 的已知项，不在 54.3 范围；此护栏读字段值、与「去 LLM 文本设备路径」正交。）
func TestATDD_54_3_AC7_AutoStartGuardError_PreservesDeviceRoutingAnchor(t *testing.T) {
	err := atdd543TriggerGuardError(t)
	var de *types.DriverError
	if !isDriverError(err, &de) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if de.Device != "/dev/intent/decompose" {
		t.Errorf("DriverError.Device = %q, want \"/dev/intent/decompose\" (AC7 红线: Layer 1 路由锚点不得改)", de.Device)
	}
}

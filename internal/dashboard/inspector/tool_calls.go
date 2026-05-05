// Package inspector — tool_calls.go (Story 38-5 PR11 Step 4(c))
//
// BuildToolCallNameMap 把 messages 切片中的 tool_use ID → tool name 收集
// 为 lookup map（cmd/rnix.buildToolCallNameMap 主体迁出 · 0 dashboardModel
// 依赖 · 仅 ipc.MessageWire 入参）。
package inspector

import "github.com/rnixai/rnix/ipc"

// BuildToolCallNameMap 收集 messages 切片中所有 tool_use 的 ID → Name 映射。
//
// 用途：tool_result block 通过 tool_use_id 反查工具名（formatRoleTag 等
// 显示场景）· 与 cmd/rnix.buildToolCallNameMap 行为 byte-for-byte 等价。
//
// 边界：
//   - 空 msgs 或 nil → 返回空 map（非 nil）;
//   - 多个 message 中重复 ID → 后到的覆盖前面的（map insertion 语义）;
//   - 无 ToolCalls 的 message → 跳过（map 仍按其他 message 收集）.
func BuildToolCallNameMap(msgs []ipc.MessageWire) map[string]string {
	names := make(map[string]string)
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			names[tc.ID] = tc.Name
		}
	}
	return names
}

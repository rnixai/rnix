package ipc

import (
	"encoding/json"
	"strings"
	"testing"
)

// Story 67.1 — IPC 死配额端删除的「端到端回归」套件。
//
// 与 atdd_67_1_dead_ipc_removal_test.go 的区别：
//   - removal 测试是包级 *源码内容扫描*（断言 budget_status/sla_status 符号
//     从 .go 源文件消失），属静态合规。
//   - 本文件是 *运行时端到端* 回归：真实起 IPC server over Unix socket、真实
//     client 往返（复用 server_test.go 的 setupTestServer/dial/sendRequest），
//     验证删除后的运行时行为正确、活体邻居未被连带破坏。
//
// 删除型 story 的 E2E 价值不在「测新功能」，而在「回归」：
//   1. 旧客户端若仍调用 budget_status/sla_status，server 必须优雅降级
//      （unknown method + INVALID），而非 panic/崩溃。
//   2. server 进程在收到已删方法调用后必须存活，仍能服务后续连接。
//   3. 与被删 handler 同文件（server_status.go）的活体邻居 provider_status
//      端到端仍正常服务——证明删除 handleBudgetStatus/handleSLAStatus 没有
//      连带破坏 handleProviderStatus/handleReputationStatus。

// TestE2E_67_1_DeletedBudgetStatus_UnknownMethod 验证删除后旧客户端调用
// budget_status 得到优雅的 "unknown method" 降级（server.go default 分支），
// 而非命中已删的 handleBudgetStatus。
func TestE2E_67_1_DeletedBudgetStatus_UnknownMethod(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	// Method 是 string 别名——直接传字面量模拟旧客户端仍发已删的 method 串。
	resp := sendRequest(t, conn, Method("budget_status"), nil)

	if resp.OK {
		t.Fatal("budget_status 已在 Story 67.1 删除，应作为未知方法失败，而非 OK")
	}
	if resp.Error == nil {
		t.Fatal("期望 budget_status 返回错误 payload，得到 nil")
	}
	if resp.Error.Code != "INVALID" {
		t.Errorf("error code = %q, want INVALID", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "unknown method") {
		t.Errorf("error message = %q, 期望包含 %q", resp.Error.Message, "unknown method")
	}
}

// TestE2E_67_1_DeletedSLAStatus_UnknownMethod 与上同——sla_status 无 wire 镜像，
// 但删除后同样应命中 default 分支优雅降级。
func TestE2E_67_1_DeletedSLAStatus_UnknownMethod(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, Method("sla_status"), nil)

	if resp.OK {
		t.Fatal("sla_status 已在 Story 67.1 删除，应作为未知方法失败，而非 OK")
	}
	if resp.Error == nil {
		t.Fatal("期望 sla_status 返回错误 payload，得到 nil")
	}
	if resp.Error.Code != "INVALID" {
		t.Errorf("error code = %q, want INVALID", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "unknown method") {
		t.Errorf("error message = %q, 期望包含 %q", resp.Error.Message, "unknown method")
	}
}

// TestE2E_67_1_ServerSurvivesDeletedMethodCall 验证 server 进程在收到已删方法
// 调用后仍存活：default 分支响应后会关闭该连接，但 server 本身不崩溃——用一条
// 全新连接发 ping 成功来证明进程整体健壮。
func TestE2E_67_1_ServerSurvivesDeletedMethodCall(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)

	// 第一条连接：调用已删方法，触发 default 分支（响应后 server 关闭此连接）。
	c1 := dial(t, sockPath)
	resp := sendRequest(t, c1, Method("budget_status"), nil)
	if resp.OK {
		t.Fatal("budget_status 应失败（已删除）")
	}

	// 第二条全新连接：ping 必须成功——若 server 因上一步崩溃/退出，这里 dial 或
	// ping 会失败。成功即证明删除面未损伤 server 主循环健壮性。
	c2 := dial(t, sockPath)
	pingResp := sendRequest(t, c2, MethodPing, nil)
	if !pingResp.OK {
		t.Fatalf("调用已删方法后 server 应存活并正常服务新连接，ping 却失败: %+v", pingResp.Error)
	}
	var pr PingResponse
	if err := json.Unmarshal(pingResp.Payload, &pr); err != nil {
		t.Fatalf("unmarshal ping 响应: %v", err)
	}
	if pr.Version != "0.1.0-test" {
		t.Errorf("ping version = %q, want %q", pr.Version, "0.1.0-test")
	}
}

// TestE2E_67_1_LiveStatusNeighborsStillServed 验证与被删 handler 同处
// server_status.go 的活体邻居 provider_status 端到端仍正常服务。ATDD removal
// 测试只断言了 MethodProviderStatus 常量值；本测试更进一步——真实往返跑通
// handleProviderStatus，断言其在删除 handleBudgetStatus/handleSLAStatus 后
// 依旧返回 OK 且响应可反序列化。
func TestE2E_67_1_LiveStatusNeighborsStillServed(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodProviderStatus, nil)
	if !resp.OK {
		t.Fatalf("provider_status 活体邻居应正常服务，却失败: %+v", resp.Error)
	}

	var ps ProviderStatusResponse
	if err := json.Unmarshal(resp.Payload, &ps); err != nil {
		t.Fatalf("unmarshal ProviderStatusResponse: %v", err)
	}
	// setupTestServer 未注入 provider registry，Providers 为空切片（非 nil）——
	// 关键是 handler 跑通并返回结构化响应，而非因删除连带损坏。
	if ps.Providers == nil {
		t.Error("Providers 应为非 nil 空切片（handler nil→[] 归一化），得到 nil")
	}
}

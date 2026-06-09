package ipc

import "testing"

// ATDD Story 54.2 — green-guard ③：IPC method 字符串是 CLI↔daemon 的 RPC 协议名，与 PascalCase
// 工具呈现名【异命名空间】（同字符串、不同概念）。本 story 改工具呈现名（intent_status 工具→
// IntentStatus）时，这些 IPC method 常量必须【零修改】——改名会破坏 IPC 协议（AC5 护栏）。
// 立即绿、全程拦回归（无 t.Skip）。
func TestATDD_54_2_910_GreenGuard_IntentIPCMethodsUnchanged(t *testing.T) {
	if MethodIntentStatus != "intent_status" {
		t.Errorf("MethodIntentStatus = %q, want %q（IPC 协议名不可随工具呈现名改）", MethodIntentStatus, "intent_status")
	}
	if MethodIntentConfirm != "intent_confirm" {
		t.Errorf("MethodIntentConfirm = %q, want %q", MethodIntentConfirm, "intent_confirm")
	}
}

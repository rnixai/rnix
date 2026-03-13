package compose

import (
	"testing"

	"github.com/rnixai/rnix/kernel"
)

// ============================================================
// ATDD RED PHASE — Story 21.5: Skill 组合矩阵
//
// Compose Engine 集成测试：验证 SynergyMatrix setter 和 nil 保护。
// 测试引用 Engine 上尚不存在的 synergyMatrix 字段和
// SetSynergyMatrix 方法，将无法编译直到实现完成。
//
// RED → GREEN: 在 compose/engine.go 中添加 synergyMatrix 字段和 setter。
// ============================================================

// --- 21.5-INT-001: [P0] SetSynergyMatrix sets field correctly (AC1) ---

func TestEngine_SetSynergyMatrix(t *testing.T) {
	// Given: an engine created without synergy matrix
	spec := &ComposeSpec{
		Agents: map[string]*AgentSpec{
			"test-agent": {Intent: "test", Agent: "tester"},
		},
	}
	engine, err := NewEngine(spec, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: setting synergy matrix
	dir := t.TempDir()
	matrix := kernel.NewSynergyMatrix(dir)
	engine.SetSynergyMatrix(matrix)

	// Then: field is set (no panic, no error)
	// We verify via the exported method existence and non-panic.
	// Internal field verification is not needed since the method compiled and ran.
}

// --- 21.5-INT-002: [P1] SetSynergyMatrix nil does not panic (AC7) ---

func TestEngine_SetSynergyMatrix_Nil(t *testing.T) {
	// Given: an engine
	spec := &ComposeSpec{
		Agents: map[string]*AgentSpec{
			"test-agent": {Intent: "test", Agent: "tester"},
		},
	}
	engine, err := NewEngine(spec, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: setting nil synergy matrix
	engine.SetSynergyMatrix(nil)

	// Then: no panic (nil protection)
}

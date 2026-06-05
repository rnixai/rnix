package intentdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ATDD red-phase scaffolds for Story 37-4 (A 范围 — best-effort 诚实化).
//
// 经 spec owner Decker 二次重协商（spec-37-4 「Renegotiation Note (A 范围)」+ ADR
// Decision 43），37-4 撤回了「检测下沉 / 检测增强 / 覆盖面统一 / 保护边界解禁」，
// 仅保留两件事：
//   (1) 措辞降级——把 best-effort 护栏的注释与错误信息如实声明（去绝对措辞）。
//   (2) 红线固化现状——用测试钉死当前能力边界，消除"护栏已验证"的虚假信心。
//
// 测试分两类，dev-story 阶段须分辨：
//   - P0「措辞降级」是真正的 RED（断言降级后的诚实措辞；当前生产代码用绝对措辞 → 失败）。
//     dev-story 实现降级后转绿。绿目标契约见 TestATDD_37_4_P0_* 顶部注释。
//   - P1/P2「现状固化」是特征化/防回归测试（锁定当前 best-effort 行为；现在即绿）。
//     它们的价值是：若有人擅自增强 pattern / 扩面护栏（违反 A 范围决定），这些红线
//     立即失败，提示先重协商 spec-37-4 与 ADR Decision 43。
//
// AC2「已知能拦不回退」由既有 atdd_37_3 的
// TestIntentFile_AutoStart_Guard_RejectsDaemonStop / RejectsDaemonRestart 覆盖，此处不重复。

// --- P0 (RED→GREEN): AC1 措辞诚实 ------------------------------------------
//
// 绿目标契约（dev-story 降级 drivers/intent/driver.go:255 错误信息时须满足）：
//   (a) 明示 best-effort 性质：含 "best-effort" 或 "尽力而为"。
//   (b) 明示绕过途径：含 "/dev/shell"。
//   (c) 去除绝对保证：不得含 "取消编排自身"（及同类硬保证措辞）。
// 当前生产措辞 "...重启会取消编排自身;..." 三条全违反 → 本测试当前为 RED。
func TestATDD_37_4_P0_AutoStartGuardError_IsBestEffort_NotAbsolute(t *testing.T) {
	nodesJSON := `[{"id":"r","intent":"运行 rnix daemon stop 重启 daemon","depends_on":[]}]`
	driver, _ := newGuardTestDriver(nodesJSON)
	file, _ := FileFactory(driver)("/decompose", vfs.O_RDWR, "")

	err := file.Write(context.Background(), []byte(`{"intent":"restart the daemon","auto_start":true}`))
	if err == nil {
		t.Fatal("expected guard to reject a daemon-stop node under auto_start")
	}
	msg := err.Error()

	// (a) best-effort 性质如实声明
	if !strings.Contains(msg, "best-effort") && !strings.Contains(msg, "尽力而为") {
		t.Errorf("AC1 RED: 错误信息须明示 best-effort（含 \"best-effort\" 或 \"尽力而为\"），当前未声明：%q", msg)
	}
	// (b) 明示可经 /dev/shell 绕过
	if !strings.Contains(msg, "/dev/shell") {
		t.Errorf("AC1 RED: 错误信息须明示可经 /dev/shell 绕过，当前未提及：%q", msg)
	}
	// (c) 不得含绝对保证措辞
	for _, absolute := range []string{"取消编排自身", "会取消编排"} {
		if strings.Contains(msg, absolute) {
			t.Errorf("AC1 RED: 错误信息含绝对保证措辞 %q，应降级为 best-effort 表述：%q", absolute, msg)
		}
	}
}

// --- P1 (现状固化): AC4 已知误报 -------------------------------------------
//
// best-effort 字面匹配的固有误报：纯描述性 intent（不实际执行 daemon 重启）只要含
// 字面 "rnix daemon stop" 子串即被拒。A 范围接受此误伤（安全侧宁拒）并固化为红线，
// 拒绝"护栏精准"的虚假信心。当前正则会匹配 → 当前即绿。
func TestATDD_37_4_P1_KnownFalsePositive_DescriptiveIntentRejected(t *testing.T) {
	nodesJSON := `[{"id":"check","intent":"检查用户是否手动运行过 rnix daemon stop 命令","depends_on":[]}]`
	driver, spawner := newGuardTestDriver(nodesJSON)
	file, _ := FileFactory(driver)("/decompose", vfs.O_RDWR, "")

	err := file.Write(context.Background(), []byte(`{"intent":"audit daemon usage","auto_start":true}`))
	if err == nil {
		t.Fatal("已知误报红线: 描述性 intent 含字面 'rnix daemon stop' 当前应被误拒，却放行了——正则行为变更需复核 spec-37-4 AC4")
	}
	var de *types.DriverError
	if !isDriverError(err, &de) || de.Code != types.ErrInvalid {
		t.Fatalf("expected ErrInvalid DriverError (已知误报), got %v", err)
	}
	if spawner.pidAlloc != 0 {
		t.Errorf("误报拦截须在 spawn 前，SpawnIntent 被调用 %d 次", spawner.pidAlloc)
	}
}

// --- P1 (现状固化): AC3 已知漏报 by-design --------------------------------
//
// 37-3 的 KnownBypasses 6 例（见 atdd_37_3_daemon_restart_guard_test.go 的
// TestIntentFile_AutoStart_Guard_KnownBypasses）在 A 范围下语义升级：从"待增强的已知
// 局限"变为"by-design 接受的 best-effort 边界"——根本解走 eval 外移(rnix-eval)，文本
// 护栏不再追加增强。此处用 2 个代表样本锚定该决定；若某样本被拦截，说明有人增强了
// pattern（违反 A 范围），须先重协商 spec-37-4 与 ADR Decision 43。当前即绿。
func TestATDD_37_4_P1_KnownUnderReporting_ByDesignNotTODO(t *testing.T) {
	representativeBypasses := []struct{ name, nodeIntent string }{
		{"pkill", "pkill -f rnix"},
		{"chinese_paraphrase", "停止 rnix 守护进程"},
	}
	for _, tc := range representativeBypasses {
		t.Run(tc.name, func(t *testing.T) {
			nodesJSON := fmt.Sprintf(`[{"id":"n","intent":%q,"depends_on":[]}]`, tc.nodeIntent)
			driver, spawner := newGuardTestDriver(nodesJSON)
			file, _ := FileFactory(driver)("/decompose", vfs.O_RDWR, "")

			err := file.Write(context.Background(), []byte(`{"intent":"x","auto_start":true}`))
			if err != nil {
				t.Fatalf("A 范围红线: %q 当前应放行(best-effort 漏报 by-design)，却被拦截 %v — 若有意增强 pattern，须先重协商 spec-37-4 / ADR Decision 43", tc.nodeIntent, err)
			}
			if spawner.pidAlloc == 0 {
				t.Errorf("%q 放行后应执行(spawn)", tc.nodeIntent)
			}
		})
	}
}

// --- P2 (现状固化): AC5 现状覆盖面边界 ------------------------------------
//
// A 范围明确不扩面：护栏只在 auto_start decompose 路径生效。手动分步
// decompose→confirm→execute 这条同步执行路径当前【无护栏】——含 daemon stop 的节点
// 会被放行执行。本红线固化"此路径裸奔"为已知现状（非遗漏）。若将来扩面到此路径
// （撤销 A 决定），ErrInvalid 断言会失败，提示同步更新 spec-37-4 与本红线。当前即绿。
func TestATDD_37_4_P2_ManualConfirmExecute_NoGuard_ByDesign(t *testing.T) {
	ctx := context.Background()
	nodesJSON := `[{"id":"r","intent":"rnix daemon stop","depends_on":[]}]`
	driver, spawner := newGuardTestDriver(nodesJSON)

	// 1. 手动 decompose（无 auto_start）——护栏不触发，得 await_confirm 树
	decFile, _ := FileFactory(driver)("/decompose", vfs.O_RDWR, "")
	if err := decFile.Write(ctx, []byte(`{"intent":"restart daemon manually"}`)); err != nil {
		t.Fatalf("手动 decompose 不应被护栏拦截，got: %v", err)
	}
	data, err := decFile.Read(1 << 20)
	if err != nil {
		t.Fatalf("read decompose response: %v", err)
	}
	var tree intent.IntentTree
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatalf("unmarshal tree: %v", err)
	}
	if tree.ID == "" {
		t.Fatal("expected non-empty intent ID from manual decompose")
	}

	// 2. 手动 confirm——当前无护栏
	confFile, _ := FileFactory(driver)("/confirm", vfs.O_RDWR, "")
	confInput, _ := json.Marshal(map[string]string{"intent_id": string(tree.ID)})
	if err := confFile.Write(ctx, confInput); err != nil {
		t.Fatalf("手动 confirm 当前不应被护栏拦截（A 范围不扩面），got: %v", err)
	}

	// 3. 手动 execute——当前无护栏 → 含 daemon stop 的节点被放行执行
	execFile, _ := FileFactory(driver)("/execute", vfs.O_RDWR, "")
	execInput, _ := json.Marshal(map[string]string{"intent_id": string(tree.ID)})
	execErr := execFile.Write(ctx, execInput)

	// A 范围红线：手动 execute 路径当前不拦截 daemon-restart 节点。
	if execErr != nil {
		var de *types.DriverError
		if isDriverError(execErr, &de) && de.Code == types.ErrInvalid {
			t.Fatalf("手动 execute 出现 ErrInvalid 护栏拦截——A 范围明确【不】扩面到此路径；若有意扩面请先更新 spec-37-4/ADR Decision 43 并改本红线: %v", execErr)
		}
		// [Review][Patch] 非护栏失败（DAG 构建 / spawn / 其它内部错误）也须显式暴露：
		// 否则 execErr 被静默放过，末尾 pidAlloc 断言要么以"应放行执行"的误导消息失败、
		// 要么在碰巧已 spawn 时假绿吞掉真实 execute 错误，削弱本红线的诊断性。
		t.Fatalf("手动 execute 非预期失败（非护栏错误）——P2 固化的是'放行并执行成功'的现状，execute 不应因其它原因失败: %v", execErr)
	}
	if spawner.pidAlloc == 0 {
		t.Error("手动 confirm→execute 应放行执行(spawn)含 daemon stop 的节点——固化'此同步路径无护栏'的已知现状边界")
	}
}

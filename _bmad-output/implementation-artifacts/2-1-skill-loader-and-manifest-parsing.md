# Story 2.1: Skill 加载器与 manifest 解析

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want 系统能从 `manifest.yaml` 和 `instructions.md` 加载 Skill 定义,
So that 智能体可以获得专业化的能力和指令。

## Acceptance Criteria

1. **SkillManifest 类型定义** — Given `skills/types.go` 已实现，When 查看 SkillManifest 类型，Then 包含 `Name`、`Description`、`Tools`（[]string 设备路径列表）、`Models`（provider/preferred/fallback）、`ContextBudget` 字段
2. **Skill 加载成功** — Given `skills/loader.go` 已实现，When 调用 `SkillLoader.Load("lib/skills/code-analyst")`，Then 解析 `manifest.yaml` 为 `SkillManifest` 结构（使用泛型 `LoadYAML[SkillManifest]`），And 读取 `instructions.md` 为原始文本，And 返回完整的 `SkillInfo`（manifest + instructions 内容）
3. **manifest.yaml 格式无效** — Given manifest.yaml 格式无效或缺少必填字段，When 调用 Load，Then 返回清晰的错误信息，标注具体缺失字段
4. **Skill 目录不存在** — Given Skill 目录不存在，When 调用 Load，Then 返回 `*kernel.SyscallError`，`Code` 为 `ErrNotFound`
5. **泛型 LoadYAML[T] 实现** — Given `skills/loader.go` 包含 `LoadYAML[T]` 泛型函数，When 传入任意 YAML 文件路径和目标类型，Then 正确反序列化为对应结构体，And 可复用于其他 YAML 加载场景

## Tasks / Subtasks

- [x] Task 1: 引入 YAML 依赖 (AC: #5)
  - [x] 1.1 执行 `go get github.com/goccy/go-yaml` 引入 YAML 解析库（替换已停止维护的 `gopkg.in/yaml.v3`）
  - [x] 1.2 验证 `go mod tidy` 成功，无冲突

- [x] Task 2: 创建 skills/types.go — 类型定义 (AC: #1)
  - [x] 2.1 创建 `skills/types.go` 文件，包名 `skills`
  - [x] 2.2 定义 `SkillManifest` 结构体，字段：`Name string`、`Description string`、`Tools []string`、`Models SkillModels`、`ContextBudget int`
  - [x] 2.3 定义 `SkillModels` 结构体，字段：`Provider string`、`Preferred string`、`Fallback string`
  - [x] 2.4 定义 `SkillInfo` 结构体，字段：`Manifest SkillManifest`、`Instructions string`
  - [x] 2.5 所有 YAML 字段使用 `yaml:"snake_case"` tag（与 manifest.yaml 格式一致）

- [x] Task 3: 创建 skills/loader.go — 加载器实现 (AC: #2, #3, #4, #5)
  - [x] 3.1 创建 `skills/loader.go` 文件
  - [x] 3.2 实现泛型函数 `LoadYAML[T any](path string) (T, error)` — 读取文件 + `yaml.Unmarshal` 到泛型目标
  - [x] 3.3 实现 `SkillLoader` 结构体，字段：`basePath string`（skills 根目录）
  - [x] 3.4 实现 `NewSkillLoader(basePath string) *SkillLoader` 构造函数
  - [x] 3.5 实现 `SkillLoader.Load(skillName string) (*SkillInfo, error)` 方法：
    - 构建 skill 目录路径：`filepath.Join(basePath, skillName)`
    - 检查目录是否存在（不存在返回 `*kernel.SyscallError` + `ErrNotFound`）
    - 调用 `LoadYAML[SkillManifest]` 加载 `manifest.yaml`
    - 验证必填字段（Name 非空），缺失返回描述性错误
    - 读取 `instructions.md` 为原始文本（`os.ReadFile`）
    - 组装并返回 `*SkillInfo`
  - [x] 3.6 instructions.md 不存在时返回描述性错误（不是 SyscallError，因为目录存在但缺少文件）

- [x] Task 4: 创建测试 fixtures (AC: #2, #3, #4)
  - [x] 4.1 创建 `skills/testdata/mock-skill/manifest.yaml`，包含完整有效字段
  - [x] 4.2 创建 `skills/testdata/mock-skill/instructions.md`，包含示例指令文本
  - [x] 4.3 创建 `skills/testdata/invalid-manifest/manifest.yaml`，包含格式无效的 YAML 内容
  - [x] 4.4 创建 `skills/testdata/missing-fields/manifest.yaml`，缺少 Name 必填字段
  - [x] 4.5 创建 `skills/testdata/no-instructions/manifest.yaml`，有效 manifest 但无 instructions.md

- [x] Task 5: 创建 skills/loader_test.go — 单元测试 (AC: #1-5)
  - [x] 5.1 `TestLoadYAML_Success` — 泛型 YAML 加载成功
  - [x] 5.2 `TestLoadYAML_FileNotFound` — 文件不存在返回错误
  - [x] 5.3 `TestLoadYAML_InvalidYAML` — YAML 格式无效返回解析错误
  - [x] 5.4 `TestSkillLoader_Load_Success` — 完整加载 mock-skill，验证所有字段
  - [x] 5.5 `TestSkillLoader_Load_DirNotFound` — 目录不存在返回 `*kernel.SyscallError` + `ErrNotFound`
  - [x] 5.6 `TestSkillLoader_Load_InvalidManifest` — 无效 YAML 返回解析错误
  - [x] 5.7 `TestSkillLoader_Load_MissingRequiredFields` — 缺少 Name 返回验证错误
  - [x] 5.8 `TestSkillLoader_Load_NoInstructions` — 缺少 instructions.md 返回错误
  - [x] 5.9 所有测试函数命名遵循 `Test<Type>_<Method>` 格式

- [x] Task 6: 全量回归测试 (AC: #1-5)
  - [x] 6.1 `go test -race ./skills/...` 全部通过
  - [x] 6.2 `go test -race ./...` 全量通过（确认无回归）
  - [x] 6.3 `go vet ./...` 无警告

## Dev Notes

### 核心实现分析

**Story 2.1 是 Epic 2 的基础** — Skill 加载器为后续 Story 2.4（Skill 注入与设备权限白名单）和 Story 2.5（code-analyst 参考 Skill）提供基础设施。本 Story 聚焦于"加载与解析"能力，不涉及 Skill 注入到 Spawn 流程或权限白名单逻辑。

**`skills/` 目录当前不存在** — 需要从零创建整个包结构。

**YAML 解析库选择** — `gopkg.in/yaml.v3` 是 Go 生态中最成熟的 YAML 库，testify 也依赖它。当前 `go.mod` 未包含此依赖，需要显式引入。

### 架构约束（必须遵循）

**依赖方向：**
```
skills/ → internal/types/（使用 ErrCode 等）
skills/ → kernel/（使用 SyscallError、NewSyscallError）
skills/ 不导入 vfs/、drivers/、cmd/、context/、debug/
```

**注意：** skills 包导入 kernel 包仅用于 `SyscallError` 类型。这是架构文档允许的方向（`kernel/ → skills/` 是允许的，`skills/` 使用 kernel 的错误类型也是允许的，因为 kernel 不导入 skills）。

**泛型使用：**
- `LoadYAML[T any]` — 架构文档明确要求使用泛型配置加载
- 不要创建额外的泛型抽象——这是一个简单的 YAML 解析函数

**错误处理模式：**
- Skill 目录不存在 → `*kernel.SyscallError{Syscall: "Load", Code: ErrNotFound}`
- YAML 格式无效 → 普通 `fmt.Errorf` 包含文件路径和解析错误
- 必填字段缺失 → 普通 `fmt.Errorf` 标注缺失字段名
- instructions.md 不存在 → 普通 `fmt.Errorf` 包含文件路径

### manifest.yaml 预期格式

```yaml
name: code-analyst
description: "分析代码质量并识别问题"
tools:
  - "/dev/fs"
  - "/dev/shell"
models:
  provider: claude
  preferred: sonnet
  fallback: haiku
context_budget: 4096
```

字段说明：
- `name`（必填）— Skill 名称，全小写连字符分隔
- `description`（可选）— Skill 描述
- `tools`（可选）— 设备路径白名单，空列表表示无限制
- `models`（可选）— LLM 提供商和模型偏好
- `context_budget`（可选）— 上下文 token 预算，0 表示使用默认

### 已有代码复用（严格遵循）

**`internal/xsync/Registry[T]`** — skills 包不需要使用 Registry，因为 Story 2.1 仅实现加载器。Registry 在 Story 2.4（注入与白名单）中可能使用。

**`kernel.SyscallError` + `kernel.NewSyscallError`** — 用于目录不存在的错误。参考 `kernel/errors.go`：
```go
kernel.NewSyscallError("Load", 0, skillPath, err, types.ErrNotFound)
```
PID 为 0，因为 Skill 加载发生在 Spawn 之前。

**泛型 LoadYAML[T] 签名参考：**
```go
func LoadYAML[T any](path string) (T, error) {
    var zero T
    data, err := os.ReadFile(path)
    if err != nil {
        return zero, fmt.Errorf("read %s: %w", path, err)
    }
    var result T
    if err := yaml.Unmarshal(data, &result); err != nil {
        return zero, fmt.Errorf("parse %s: %w", path, err)
    }
    return result, nil
}
```

### 前序 Story 经验（必须吸收）

**Story 2.0 VFSFile.Write 签名变更：**
- `VFSFile.Write` 已扩展为 `Write(ctx context.Context, data []byte) error`
- 所有驱动和 mock 实现已适配
- 本 Story 不涉及 VFSFile，但需要注意接口签名已变更

**Story 1.5 CommandBuilder 注入模式：**
- 外部命令调用通过注入点实现可测试性
- 本 Story 不涉及外部命令调用，但加载器的 `basePath` 参数遵循类似的依赖注入思想

**Story 1.1 类型位置：**
- 共享类型在 `internal/types/types.go`
- ErrCode 常量在同一文件（ErrTimeout、ErrNotFound 等）
- skills 包的类型（SkillManifest、SkillInfo）定义在 `skills/types.go`（非共享）

### Git 智能分析

**最近代码模式：**
- 每个 Story 修改严格在架构边界内
- 测试与实现在同一 Story 完成
- `go test -race ./...` 作为质量门禁
- 提交消息格式：`Verb + Story N.M: Description`

**相关提交：**
- `12f0533` — 最近的 CLI 参数重构，可能影响 Spawn 调用方式
- `ecd4fd9` — Story 2.0 完成，LLM 驱动错误处理已修复
- `4bc0835` — Story 1.8 完成，E2E 测试稳定

### Project Structure Notes

**本 Story 创建的文件：**

```
skills/
├── types.go                     (新建 — SkillManifest + SkillModels + SkillInfo)
├── loader.go                    (新建 — SkillLoader + LoadYAML[T])
├── loader_test.go               (新建 — 9 个测试用例)
└── testdata/
    ├── mock-skill/
    │   ├── manifest.yaml        (新建 — 有效完整 fixture)
    │   └── instructions.md      (新建 — 示例指令)
    ├── invalid-manifest/
    │   └── manifest.yaml        (新建 — 格式无效)
    ├── missing-fields/
    │   └── manifest.yaml        (新建 — 缺少 Name)
    └── no-instructions/
        └── manifest.yaml        (新建 — 有效 manifest 无 instructions.md)
```

**不需要修改的文件：**
- `kernel/` 下任何文件 — 错误类型已存在
- `vfs/` 下任何文件 — 加载器不涉及 VFS
- `drivers/` 下任何文件 — 加载器不涉及驱动
- `context/` 下任何文件
- `internal/types/types.go` — ErrCode 常量已存在
- `internal/xsync/` 下任何文件
- `internal/ui/` 下任何文件
- `cmd/crux/main.go` — 本 Story 不集成到 CLI（Story 2.4 集成）

**需要修改的文件：**
- `go.mod` / `go.sum` — 新增 `gopkg.in/yaml.v3` 依赖

### 与架构文档的一致性

| 架构要求 | 本 Story 实现 |
|---------|-------------|
| `skills/loader.go` — SkillLoader + LoadYAML[T] | ✅ Task 3 |
| `skills/types.go` — SkillManifest + SkillInfo | ✅ Task 2 |
| `skills/testdata/mock-skill/` — 测试 fixtures | ✅ Task 4 |
| `skills/loader_test.go` — 单元测试 | ✅ Task 5 |
| 泛型 LoadYAML[T] | ✅ Task 3.2 |
| manifest.yaml 字段全小写 snake_case | ✅ Task 2 yaml tags |
| 错误返回 *SyscallError（目录不存在） | ✅ Task 3.5 |
| skills 包不导入 vfs/drivers/cmd/ | ✅ 依赖约束 |

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.1] — AC 和 User Story 定义
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — skills/ 目录结构
- [Source: _bmad-output/planning-artifacts/architecture.md#泛型策略] — LoadYAML[T] 泛型要求
- [Source: _bmad-output/planning-artifacts/architecture.md#命名模式] — manifest.yaml 字段 snake_case
- [Source: _bmad-output/planning-artifacts/architecture.md#依赖方向] — skills/ 的依赖约束
- [Source: _bmad-output/project-context.md#错误处理] — SyscallError 规范
- [Source: _bmad-output/project-context.md#测试规则] — 测试命名和 -race 要求
- [Source: _bmad-output/project-context.md#泛型必用场景] — LoadYAML 必须泛型
- [Source: kernel/errors.go:29-37] — NewSyscallError 构造函数
- [Source: internal/types/types.go:17-23] — ErrCode 常量定义
- [Source: internal/xsync/registry.go] — Registry[T] 模式参考
- [Source: 2-0-tech-debt-llm-driver-error-handling.md] — 前序 Story 经验和 VFSFile.Write 签名变更

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- Task 1: YAML 库选择变更 — 原 Story 指定 `gopkg.in/yaml.v3`（已停止维护），经用户确认后替换为 `github.com/goccy/go-yaml v1.19.2`（活跃维护，API 兼容）
- Task 2: `skills/types.go` — 定义 SkillManifest、SkillModels、SkillInfo 三个结构体，所有 YAML 字段使用 `yaml:"snake_case"` tag
- Task 3: `skills/loader.go` — 实现泛型 `LoadYAML[T]` 和 `SkillLoader.Load`，错误处理严格遵循架构约束：目录不存在用 `*kernel.SyscallError`，其他用 `fmt.Errorf`
- Task 4: 创建 4 组 testdata fixtures 覆盖成功、无效 YAML、缺少必填字段、缺少 instructions.md 场景
- Task 5: 8 个单元测试全部通过，覆盖 AC #1-#5 全部验收标准
- Task 6: `go test -race ./...` 全量通过（零回归），`go vet ./...` 无警告

### Code Review Record (2026-02-24)

**Reviewer:** Amelia (Dev Agent) — Code Review 工作流
**Model:** Claude Opus 4.6

**发现与修复：**

- [M1] `loader.go:44` — `os.Stat` 非 NotExist 错误被静默丢弃 → 已修复：增加完整错误分支处理
- [M2] `loader_test.go` — 自定义 `containsSubstring` 替代 `strings.Contains` → 已修复：删除自定义函数，使用标准库
- [M3] `loader.go:57` — 错误消息使用 Go 字段名 "Name" 而非 YAML 字段名 "name" → 已修复
- [M4] `loader_test.go:99` — 硬编码 `"NOT_FOUND"` → 已修复：使用 `types.ErrNotFound` 常量
- [L1] `loader_test.go` — Instructions 断言过弱 → 已修复：验证包含 "Code Analyst" 关键字
- [L2] `loader.go` — 未验证路径是目录 → 已修复：增加 `fi.IsDir()` 检查
- [L3] `loader.go` — 方法接收器 `sl` → 已修复：改为单字母 `l`

**结果：** 全部 4 个 MEDIUM + 3 个 LOW 问题已修复，`go test -race ./...` 全量通过，`go vet ./...` 无警告

### File List

- `skills/types.go` (新建)
- `skills/loader.go` (新建)
- `skills/loader_test.go` (新建)
- `skills/testdata/mock-skill/manifest.yaml` (新建)
- `skills/testdata/mock-skill/instructions.md` (新建)
- `skills/testdata/invalid-manifest/manifest.yaml` (新建)
- `skills/testdata/missing-fields/manifest.yaml` (新建)
- `skills/testdata/no-instructions/manifest.yaml` (新建)
- `go.mod` (修改 — 新增 `github.com/goccy/go-yaml v1.19.2`)
- `go.sum` (修改)

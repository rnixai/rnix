# Story 24.4: /v1/models Provider 发现

Status: done

## Story

As a 外部工具用户,
I want 通过 `/v1/models` 端点发现所有可用的 LLM provider 和模型,
So that 工具（如 Open WebUI）可以自动发现并展示可用模型列表。

## Acceptance Criteria

1. **Given** `/v1/models` GET 端点已实现
   **When** 发送 GET `/v1/models`
   **Then** 返回所有已注册且健康的 provider 及其可用模型列表
   **And** 响应格式兼容 OpenAI Models API（FR149）

2. **Given** 响应格式定义
   **When** 查看 `/v1/models` 响应
   **Then** 包含 `object: "list"` 和 `data` 数组
   **And** 每个 model entry 包含 `id`（provider 名或 provider:model 格式）、`object: "model"`、`created`、`owned_by`（provider 名）

3. **Given** 多个 provider 已注册
   **When** daemon 注册了 claude、cursor、ollama 三个 provider
   **Then** `/v1/models` 返回包含所有三个 provider 的模型列表

4. **Given** provider 健康状态集成
   **When** 某个 provider 健康检查失败（标记为不可用）
   **Then** `/v1/models` 不返回该 provider 的模型
   **And** 其他健康 provider 正常列出

## Tasks / Subtasks

### Task 1: 定义 OpenAI Models API 响应类型（AC: #2）

- [x] 1.1 在 `ipc/http_openai.go` 中定义 `ModelListResponse` 类型：`Object string` (固定 "list") + `Data []ModelEntry`
- [x] 1.2 定义 `ModelEntry` 类型：`ID string`、`Object string` (固定 "model")、`Created int64`、`OwnedBy string`
- [x] 1.3 JSON tag 使用 `snake_case`：`object`、`id`、`created`、`owned_by`

### Task 2: 替换 handleListModels 的 501 stub 为真正实现（AC: #1, #3）

- [x] 2.1 替换 `handleListModels` 中当前的 `writeError(w, 501, ...)` 返回（`http_openai.go:157-161`）
- [x] 2.2 使用 `s.driverReg.Names()` 获取所有已注册 provider 名称的排序列表
- [x] 2.3 对每个 provider 调用 `s.driverReg.Get(name)` 获取 driver 实例
- [x] 2.4 调用 `driver.Info()` 获取 `DriverInfo`（含 `Name`、`Provider`、`DefaultModel`、`DriverType`）
- [x] 2.5 构建 `ModelEntry`：`ID` = provider 名（如 "ollama"），`Object` = "model"，`Created` = 服务启动时间或固定时间戳，`OwnedBy` = provider 名
- [x] 2.6 如果 driver 的 `DefaultModel` 非空，额外生成一个 `ModelEntry`：`ID` = "provider:model" 格式（如 "ollama:llama3"），使外部工具可选择具体模型
- [x] 2.7 组装 `ModelListResponse{Object: "list", Data: entries}`
- [x] 2.8 设置 `Content-Type: application/json`，用 `json.NewEncoder(w).Encode()` 返回

### Task 3: 健康状态过滤（AC: #4）

- [x] 3.1 在遍历 provider 时，调用 `s.driverReg.GetHealth(name)` 检查健康状态
- [x] 3.2 如果健康状态为 `HealthStatusUnhealthy`，跳过该 provider（不加入返回列表）
- [x] 3.3 `HealthStatusUnchecked` 和 `HealthStatusHealthy` 的 provider 正常列出（unchecked 视为可用）

### Task 4: 单元测试（AC: #1-#4）

- [x] 4.1 `TestListModels_Basic`：注册 2 个 provider，验证 GET `/v1/models` 返回 200 + `object: "list"` + `data` 数组含所有 provider
- [x] 4.2 `TestListModels_ResponseFormat`：验证每个 entry 的 `id`、`object`、`created`、`owned_by` 字段格式正确
- [x] 4.3 `TestListModels_ModelID`：验证 provider 名作为 model ID，如果有 default_model 则额外包含 "provider:model" 格式的 entry
- [x] 4.4 `TestListModels_HealthFiltering`：标记一个 provider 为 unhealthy，验证返回列表不包含该 provider
- [x] 4.5 `TestListModels_UncheckedHealth`：未检查健康状态的 provider 应包含在返回列表中
- [x] 4.6 `TestListModels_EmptyRegistry`：空 registry 返回 `{"object":"list","data":[]}`
- [x] 4.7 `TestListModels_ContentType`：验证 `Content-Type` 为 `application/json`
- [x] 4.8 `TestListModels_Sorted`：验证返回的 model 列表按 ID 排序
- [x] 4.9 所有测试启用 `-race` 检测

## Dev Notes

### 核心设计决策

**1. 替换 `handleListModels` 中的 501 stub 为 OpenAI Models API 兼容实现。**
- Story 24.1 留下了 `handleListModels` 的 501 返回占位（`ipc/http_openai.go:158-161`）
- 本 Story 将该方法替换为遍历 `DriverRegistry` 并返回 OpenAI 格式的 model 列表
- 实现范围小：仅修改 `handleListModels` 方法体 + 新增 2 个类型定义

**2. Model ID 策略：每个 provider 生成至少一个 entry。**
- 基础 entry：`id` = provider 名（如 "ollama"），对应 `parseModel("ollama")` → 使用 default_model
- 可选 entry：`id` = "provider:model"（如 "ollama:llama3"），对应 `parseModel("ollama:llama3")` → 显式指定模型
- 这与 `handleChatCompletions` 中的 `parseModel()` 路由逻辑完全一致——外部工具从 `/v1/models` 列表中选择的 model ID 可直接用于 `/v1/chat/completions` 请求
- Open WebUI 等工具会展示 `/v1/models` 返回的所有 model ID 供用户选择

**3. 健康状态过滤逻辑。**
- `HealthStatusUnhealthy` → 排除（provider 明确不可用）
- `HealthStatusHealthy` → 包含
- `HealthStatusUnchecked` → 包含（daemon 刚启动时所有 provider 为 unchecked，不应全部隐藏）
- 复用 `DriverRegistry.GetHealth(name)` 查询健康状态

**4. 不使用 `DriverRegistry.HealthStatuses()` 方法。**
- `HealthStatuses()` 返回 `[]ProviderStatus{Name, Driver, Health}`，信息不足（缺少 DefaultModel）
- 需要 `driver.Info()` 获取 `DriverInfo{DefaultModel}` 来构建 model entry
- 因此使用 `Names()` + `Get()` + `Info()` + `GetHealth()` 组合

### 关键类型和接口参考

**`DriverInfo` 类型（`drivers/llm/driver.go:50-55`）：**
```go
type DriverInfo struct {
    Name         string `json:"name"`
    Provider     string `json:"provider"`
    DefaultModel string `json:"default_model"`
    DriverType   string `json:"driver_type"`
}
```

**`DriverRegistry` 可用方法（`drivers/llm/registry.go`）：**
```go
reg.Names() []string              // 排序后的 provider 名列表
reg.Get(name) (LLMDriver, bool)   // 获取驱动实例
reg.GetHealth(name) HealthStatus  // 查询健康状态
reg.Len() int                     // 已注册数量
```

**`HealthStatus` 常量（`drivers/llm/registry.go:12-16`）：**
```go
HealthStatusHealthy   HealthStatus = "healthy"
HealthStatusUnhealthy HealthStatus = "unhealthy"
HealthStatusUnchecked HealthStatus = "unchecked"
```

**OpenAI Models API 响应格式参考：**
```json
{
  "object": "list",
  "data": [
    {
      "id": "ollama",
      "object": "model",
      "created": 1710000000,
      "owned_by": "ollama"
    },
    {
      "id": "ollama:llama3",
      "object": "model",
      "created": 1710000000,
      "owned_by": "ollama"
    }
  ]
}
```

### DriverRegistry 和 Driver API 速查

```go
// 获取所有 provider 名称（排序后）
names := s.driverReg.Names()  // []string{"claude", "ollama"}

// 获取驱动
driver, ok := s.driverReg.Get("ollama")  // (LLMDriver, bool)

// 获取驱动元信息
info := driver.Info()  // DriverInfo{Name:"ollama", Provider:"ollama", DefaultModel:"llama3", DriverType:"openai-compat"}

// 获取健康状态
health := s.driverReg.GetHealth("ollama")  // HealthStatus
```

### 已有代码中可直接复用的部分

| 已有代码 | 文件位置 | 复用方式 |
|---------|---------|---------|
| `handleListModels` stub | `ipc/http_openai.go:157-161` | 替换方法体 |
| `DriverRegistry.Names()` | `drivers/llm/registry.go:54-62` | 获取排序后的 provider 列表 |
| `DriverRegistry.Get()` | `drivers/llm/registry.go:49-51` | 获取驱动实例 |
| `DriverRegistry.GetHealth()` | `drivers/llm/registry.go:81-86` | 查询健康状态 |
| `LLMDriver.Info()` | `drivers/llm/driver.go:62` | 获取 DriverInfo |
| `writeError()` | `ipc/http_openai.go:325-335` | 错误响应（本 Story 可能不需要错误场景） |
| `stubDriver` 测试 helper | `ipc/http_openai_test.go:22-36` | 测试中复用 |
| `newTestRegistry()` | `ipc/http_openai_test.go:60-70` | 测试中复用 |

### 变更范围

| 文件 | 变更类型 | 估算行数 |
|------|---------|---------|
| `ipc/http_openai.go` | **修改** | +25~35 行（2 个新类型 + handleListModels 方法体替换） |
| `ipc/http_openai_test.go` | **修改** | +100~150 行（~8 个新测试函数） |

**不修改的文件：**
- `drivers/llm/driver.go` — LLMDriver 接口不变，DriverInfo 类型不变
- `drivers/llm/registry.go` — DriverRegistry 不变
- `ipc/server.go` — Unix socket IPC server 不受影响
- `kernel/` — 内核不受影响（HTTP 绕过 Kernel）
- `cmd/rnix/` — serve 命令在 Story 24.5 实现

### 架构合规

- **依赖方向**：`ipc/` -> `drivers/llm/`（与 Story 24.1/24.2/24.3 一致，Decision 13 允许）
- **不引入新依赖**：仅使用已有的标准库 `encoding/json`、`net/http`、`sort`
- **线程安全**：`DriverRegistry` 内部使用 `xsync.Registry` 和 `xsync.SyncMap`，已线程安全
- **HTTP 绕过 Kernel**：与 Story 24.1/24.2/24.3 一致，直接调用 DriverRegistry
- **错误格式**：本端点为 GET 列表查询，正常情况不会产生错误（空列表返回空 data 数组）

### 测试策略

**复用已有测试 helper：**
- `stubDriver` — 已实现 `Info()` 返回 `DriverInfo`，满足本 Story 测试需求
- `newTestRegistry()` — 已注册 "claude" 和 "ollama" 两个 stubDriver
- 可直接创建新 registry 并设置不同健康状态来测试过滤逻辑

**健康状态测试：**
- `reg.SetHealth("ollama", llm.HealthStatusUnhealthy)` 标记不健康
- 验证返回列表中不包含该 provider

**空 registry 测试：**
- `llm.NewDriverRegistry()` 创建空 registry
- 验证返回 `{"object":"list","data":[]}`

**请求构造：**
- `httptest.NewRequest("GET", "/v1/models", nil)` + `httptest.NewRecorder()`
- 通过 `s.buildMux().ServeHTTP(w, r)` 调用

### 前置 Story 智能

**来自 Story 24-3（SSE 流式响应）：**
- Code Review 创建了 `ChatDelta` 类型解决 omitempty 序列化问题——新类型定义需注意 JSON tag 一致性
- 测试使用 `httptest.NewRecorder()` + `buildMux().ServeHTTP()` 模式，本 Story 沿用
- 12 个测试函数 + 4 个 helper 已添加，测试文件约 1200 行

**来自 Story 24-2（同步模式）：**
- `handleChatCompletions` 使用 `parseModel()` 解析 model 参数——`/v1/models` 返回的 ID 必须与 `parseModel()` 的输入格式一致
- 错误处理模式：`writeError()` 辅助函数

**来自 Story 24-1（核心框架）：**
- 路由已注册：`mux.HandleFunc("GET /v1/models", s.handleListModels)`（`buildMux()` 中）
- `handleListModels` 当前返回 501（占位）

### Git 智能

最近提交：
- `be84f8d feat: 24-3`
- `b1761ec feat: 24-2implement synchronous mode for /v1/chat/completions`
- `b33b865 feat: story 24-1`

### 跨 Story 边界（本 Story 不实现）

| 功能 | 归属 | 说明 |
|------|------|------|
| `rnix serve` CLI 命令 | Story 24.5 | Cobra 命令和 daemon 集成 |
| 分页支持 | 未规划 | OpenAI Models API 支持分页，但 provider 数量少无需分页 |
| model 详细信息 | 未规划 | OpenAI 有 `/v1/models/{model}` 端点，本 Epic 不实现 |
| 动态 model 列表 | 未规划 | 某些 provider（如 Ollama）可能有多个本地模型，当前仅返回 default_model |

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| /v1/models 端点 | /v1/chat/completions | 一致性：models 返回的 ID 可直接用于 chat completions 的 model 参数 | 是 |
| handleListModels | DriverRegistry.Names() | 调用：获取排序后的 provider 列表 | 是 |
| handleListModels | DriverRegistry.GetHealth() | 调用：过滤不健康 provider | 是 |
| handleListModels | LLMDriver.Info() | 调用：获取 default_model 构建 model entry | 是 |
| handleListModels | handleHealth | 独立：两个 GET 端点互不影响 | 否 |
| 健康过滤 | DriverRegistry.SetHealth() | 响应：健康状态变化后 models 列表实时反映 | 是 |

### Project Structure Notes

- 仅修改 `ipc/http_openai.go` 和 `ipc/http_openai_test.go` — 不新建文件
- 与 Story 24.1/24.2/24.3 建立的文件结构完全一致
- 与架构文档 Epic 24 -> `ipc/` 包映射一致

### References

- [Source: ipc/http_openai.go:157-161] — handleListModels 的 501 stub 代码（需替换）
- [Source: ipc/http_openai.go:38-44] — buildMux 中已注册 GET /v1/models 路由
- [Source: ipc/http_openai.go:324-335] — writeError 辅助函数
- [Source: drivers/llm/driver.go:50-55] — DriverInfo 类型定义
- [Source: drivers/llm/driver.go:58-62] — LLMDriver 接口含 Info() 方法
- [Source: drivers/llm/registry.go:54-62] — DriverRegistry.Names() 方法
- [Source: drivers/llm/registry.go:49-51] — DriverRegistry.Get() 方法
- [Source: drivers/llm/registry.go:81-86] — DriverRegistry.GetHealth() 方法
- [Source: drivers/llm/registry.go:12-16] — HealthStatus 常量定义
- [Source: ipc/http_openai_test.go:22-36] — stubDriver 测试 mock
- [Source: ipc/http_openai_test.go:60-70] — newTestRegistry() 测试 helper
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision-13] — 架构决策 13
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#LLM-Serve-Gateway] — 实现模式规则
- [Source: _bmad-output/planning-artifacts/epics/epic-24-*#Story-24.4] — Epic 24 Story 24.4 AC 定义
- FRs covered: FR149
- NFRs covered: N/A

## Dev Agent Record

### Agent Model Used

claude-4.6-opus-high-thinking (Cursor)

### Debug Log References

- 全部 9 个 Story 24-4 测试 PASS（含 `-race`）
- `make all` 通过：lint 0 issues, vet 通过, 全包测试 PASS, 构建成功

### Completion Notes List

- Task 1: 定义 `ModelListResponse` 和 `ModelEntry` 类型于 `ipc/http_openai.go:346-358`，JSON tag 使用 snake_case（`object`, `id`, `created`, `owned_by`）
- Task 2: 替换 `handleListModels` 的 501 stub 为完整实现（`http_openai.go:162-199`）。使用 `Names()` + `Get()` + `Info()` + `GetHealth()` 组合遍历 provider，基础 entry 用 provider 名作为 ID，有 DefaultModel 时额外生成 `provider:model` 格式 entry
- Task 3: 健康状态过滤——`HealthStatusUnhealthy` 跳过，`HealthStatusHealthy` 和 `HealthStatusUnchecked` 正常列出
- Task 4: 8 个测试函数 + 2 个已有测试更新，覆盖 AC #1-#4 所有场景
- 已有 `TestHandleListModels_ReturnsModelList` 从 501 断言更新为验证 200 + model list 格式
- 已有 `TestOpenAIServer_RoutesRegistered` 中 `/v1/models` 期望状态从 501 更新为 200

### Senior Developer Review (AI)

**审查日期:** 2026-03-13
**审查模型:** claude-4.6-opus-high-thinking (Cursor)
**结果:** Approve（修复后）

**发现并修复的问题：**
- H1: `TestListModels_Sorted` 增强为含 DefaultModel 的排序验证（5 entries 包括 provider:model 格式）
- H2: Dev Agent Record 的已有测试更新数从 1 修正为 2
- M1: sprint-status.yaml 补入 File List
- M2: atdd-checklist-24-4.md 补入 File List
- M3: `entries` slice 容量从 `len(names)` 修正为 `2*len(names)` 避免不必要的内存重分配
- L2: `TestListModels_ContentType` 改用 `strings.Contains` 匹配，与文件内其他测试风格一致

**已知低优先级事项（不阻塞）：**
- L1: `Created` 时间戳使用请求时间而非固定值，与 OpenAI 行为略有不同（本地代理场景可接受）
- L3: 缺少 `/v1/models` 与 `/v1/chat/completions` 的跨端点一致性测试

### File List

| 文件 | 变更类型 |
|------|---------|
| `ipc/http_openai.go` | 修改：+2 类型定义 + handleListModels 方法体替换 |
| `ipc/http_openai_test.go` | 修改：+8 新测试函数 + 2 已有测试更新 |
| `_bmad-output/implementation-artifacts/24-4-models-endpoint-provider-discovery.md` | 修改：任务标记完成 + Dev Agent Record |
| `_bmad-output/implementation-artifacts/sprint-status.yaml` | 修改：24-4 状态同步为 done |
| `_bmad-output/test-artifacts/atdd-checklist-24-4.md` | 新增：ATDD 验收检查清单 |

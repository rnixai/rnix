# Story 23.6: Provider 健康检查与状态报告

Status: dev-complete

## Story

As a 运维人员,
I want daemon 启动时对 HTTP API provider 执行健康检查,
So that 我能及时知道哪些 provider 不可用。

## Acceptance Criteria

1. **Given** HTTP API 类 provider 已注册
   **When** daemon 启动完成后
   **Then** 对每个 HTTP API provider 执行轻量健康检查（`GET /v1/models` 或 `HEAD`）
   **And** 单个健康检查耗时 <= 3 秒（NFR32）

2. **Given** 健康检查失败
   **When** provider 端点不可达
   **Then** daemon 正常启动（不因单个 provider 失败而拒绝启动）
   **And** 该 provider 标记为 `unhealthy`
   **And** 日志输出 warning：`provider "groq": health check failed: connection refused`

3. **Given** CLI 类 provider（claude、cursor）
   **When** daemon 启动
   **Then** 跳过健康检查（CLI 可用性在首次调用时验证）

4. **Given** 用户执行 `rnix daemon status`
   **When** 查看输出
   **Then** 显示所有已注册 provider 的状态（healthy / unhealthy / unchecked）

## Tasks / Subtasks

### Task 1: DriverRegistry 增加健康状态存储（AC: #1, #2, #3）

- [x] 1.1 在 `drivers/llm/registry.go` 中定义 `HealthStatus` 类型和常量：
  ```go
  type HealthStatus string

  const (
      HealthStatusHealthy   HealthStatus = "healthy"
      HealthStatusUnhealthy HealthStatus = "unhealthy"
      HealthStatusUnchecked HealthStatus = "unchecked"
  )
  ```

- [x] 1.2 在 `DriverRegistry` 中新增 `health` 字段（使用 `xsync.SyncMap`）：
  ```go
  type DriverRegistry struct {
      registry *xsync.Registry[LLMDriver]
      health   *xsync.SyncMap[string, HealthStatus]
  }
  ```
  在 `NewDriverRegistry` 中初始化：
  ```go
  func NewDriverRegistry() *DriverRegistry {
      return &DriverRegistry{
          registry: xsync.NewRegistry[LLMDriver](),
          health:   xsync.NewSyncMap[string, HealthStatus](),
      }
  }
  ```

- [x] 1.3 新增方法 `SetHealth`、`GetHealth`、`HealthStatuses`：
  ```go
  func (r *DriverRegistry) SetHealth(name string, status HealthStatus) {
      r.health.Store(name, status)
  }

  func (r *DriverRegistry) GetHealth(name string) HealthStatus {
      if status, ok := r.health.Load(name); ok {
          return status
      }
      return HealthStatusUnchecked
  }

  // ProviderStatus represents a provider's name, driver type, and health status.
  type ProviderStatus struct {
      Name   string       `json:"name"`
      Driver string       `json:"driver"`
      Health HealthStatus `json:"health"`
  }

  func (r *DriverRegistry) HealthStatuses() []ProviderStatus {
      var statuses []ProviderStatus
      r.registry.Range(func(name string, drv LLMDriver) bool {
          health := r.GetHealth(name)
          driverType := drv.Info().DriverType
          statuses = append(statuses, ProviderStatus{
              Name:   name,
              Driver: driverType,
              Health: health,
          })
          return true
      })
      sort.Slice(statuses, func(i, j int) bool {
          return statuses[i].Name < statuses[j].Name
      })
      return statuses
  }
  ```

- [x] 1.4 在 `Register` 方法中，注册成功后自动将健康状态设为 `unchecked`：
  ```go
  func (r *DriverRegistry) Register(path string, driver LLMDriver) error {
      if err := r.registry.Register(path, driver); err != nil {
          return err
      }
      r.health.Store(path, HealthStatusUnchecked)
      return nil
  }
  ```

### Task 2: OpenAICompatDriver 增加 HealthCheck 方法（AC: #1）

- [x] 2.1 在 `drivers/llm/openai_compat.go` 中新增 `HealthCheck` 方法：
  ```go
  // HealthCheck performs a lightweight GET /models check against the provider endpoint.
  // Returns nil if the provider is reachable and responds with HTTP 2xx.
  func (d *OpenAICompatDriver) HealthCheck(ctx context.Context) error {
      url := d.baseURL + "/models"
      req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
      if err != nil {
          return fmt.Errorf("create request: %w", err)
      }
      if d.apiKey != "" {
          req.Header.Set("Authorization", "Bearer "+d.apiKey)
      }
      resp, err := d.httpClient.Do(req)
      if err != nil {
          return err
      }
      defer resp.Body.Close()
      io.Copy(io.Discard, resp.Body)
      if resp.StatusCode >= 400 {
          return fmt.Errorf("HTTP %d", resp.StatusCode)
      }
      return nil
  }
  ```

- [x] 2.2 定义可选接口 `HealthChecker`（在 `drivers/llm/driver.go` 中）：
  ```go
  // HealthChecker is an optional interface for drivers that support health checks.
  type HealthChecker interface {
      HealthCheck(ctx context.Context) error
  }
  ```
  这允许在 factory 或 daemon 中通过类型断言 `driver.(HealthChecker)` 检测是否支持健康检查。

### Task 3: 在 RegisterProviders 后执行异步健康检查（AC: #1, #2, #3）

- [x] 3.1 在 `drivers/llm/factory.go` 中新增 `RunHealthChecks` 函数：
  ```go
  // RunHealthChecks performs async health checks on all registered providers
  // that implement the HealthChecker interface. CLI-based drivers are marked unchecked.
  // The function returns immediately; health checks run in background goroutines.
  // timeout is the per-provider health check deadline.
  func RunHealthChecks(cfg *ProvidersConfig, reg *DriverRegistry, timeout time.Duration) {
      for _, pc := range cfg.Providers {
          drv, ok := reg.Get(pc.Name)
          if !ok {
              continue
          }
          hc, ok := drv.(HealthChecker)
          if !ok {
              // CLI drivers (claude-cli, cursor-cli) don't support health check
              continue
          }
          go func(name string, checker HealthChecker) {
              ctx, cancel := context.WithTimeout(context.Background(), timeout)
              defer cancel()
              if err := checker.HealthCheck(ctx); err != nil {
                  reg.SetHealth(name, HealthStatusUnhealthy)
                  log.Printf("[llm] provider %q: health check failed: %v", name, err)
              } else {
                  reg.SetHealth(name, HealthStatusHealthy)
                  log.Printf("[llm] provider %q: healthy", name)
              }
          }(pc.Name, hc)
      }
  }
  ```
  **设计要点：**
  - 异步执行，不阻塞 daemon 启动主流程
  - 每个 provider 独立 goroutine，使用 `context.WithTimeout` 限制 3 秒
  - CLI 类 driver 不实现 `HealthChecker`，自然跳过（状态保持 `unchecked`）

- [x] 3.2 在 `cmd/rnix/main.go` 的 `runDaemon` 中调用 `RunHealthChecks`。在 `RegisterProviders` 之后、`ipc.NewServer` 之前添加：
  ```go
  llm.RunHealthChecks(providersCfg, driverReg, 3*time.Second)
  ```
  位置：约 L1068（`RegisterProviders` 调用之后）。

### Task 4: 扩展 IPC 协议——新增 provider 状态查询（AC: #4）

- [x] 4.1 在 `ipc/protocol.go` 新增 `MethodProviderStatus` 和响应类型：
  ```go
  MethodProviderStatus Method = "provider_status"

  // ProviderStatusResponse is the payload for MethodProviderStatus.
  type ProviderStatusResponse struct {
      Providers []ProviderStatusWire `json:"providers"`
  }

  type ProviderStatusWire struct {
      Name   string `json:"name"`
      Driver string `json:"driver"`
      Health string `json:"health"` // "healthy", "unhealthy", "unchecked"
  }
  ```

- [x] 4.2 在 `ipc/server.go` 中：
  - `Server` 结构体新增 `providerStatuses func() []llm.ProviderStatus` 字段
  - 新增 `SetProviderStatusFunc` setter 方法
  - 在 `handleConnection` switch 中新增 `case MethodProviderStatus`
  - 实现 `handleProviderStatus`：
  ```go
  func (s *Server) handleProviderStatus(conn net.Conn) {
      var wires []ProviderStatusWire
      if s.providerStatuses != nil {
          for _, ps := range s.providerStatuses() {
              wires = append(wires, ProviderStatusWire{
                  Name:   ps.Name,
                  Driver: ps.Driver,
                  Health: string(ps.Health),
              })
          }
      }
      if wires == nil {
          wires = []ProviderStatusWire{}
      }
      payload, _ := json.Marshal(ProviderStatusResponse{Providers: wires})
      writeResponse(conn, Response{OK: true, Payload: payload})
  }
  ```

- [x] 4.3 在 `ipc/client.go` 新增 `ProviderStatus` 方法：
  ```go
  func (c *Client) ProviderStatus() ([]ProviderStatusWire, error) {
      resp, err := c.call(MethodProviderStatus, nil)
      if err != nil {
          return nil, err
      }
      var pr ProviderStatusResponse
      if err := json.Unmarshal(resp.Payload, &pr); err != nil {
          return nil, fmt.Errorf("ipc: unmarshal provider_status: %w", err)
      }
      return pr.Providers, nil
  }
  ```

- [x] 4.4 在 `cmd/rnix/main.go` 的 `runDaemon` 中，将 `driverReg.HealthStatuses` 注入 Server：
  ```go
  srv.SetProviderStatusFunc(driverReg.HealthStatuses)
  ```
  位置：在 `srv.SetKernel(k)` 附近。

### Task 5: 扩展 `rnix daemon status` CLI 输出（AC: #4）

- [x] 5.1 修改 `cmd/rnix/main.go` 的 `runDaemonStatus` 函数，新增 provider 状态查询：
  ```go
  func runDaemonStatus(cmd *cobra.Command, args []string) error {
      w := cmd.OutOrStdout()
      sockPath := ipc.SocketPath()
      client, err := ipc.Dial(sockPath)
      if err != nil {
          fmt.Fprintf(w, "status: stopped\nsocket: %s\n", sockPath)
          return nil
      }
      defer client.Close()

      version, err := client.Ping()
      if err != nil {
          fmt.Fprintf(w, "status: unreachable\nsocket: %s\nerror:  %v\n", sockPath, err)
          return nil
      }

      procs, _ := client.ListProcs()
      active := 0
      for _, p := range procs {
          if p.State == types.StateRunning {
              active++
          }
      }

      fmt.Fprintf(w, "status:    running\nversion:   %s\nsocket:    %s\nprocs:     %d active / %d total\n",
          version, sockPath, active, len(procs))

      // Provider health status
      providers, err := client.ProviderStatus()
      if err == nil && len(providers) > 0 {
          fmt.Fprintf(w, "providers:\n")
          for _, p := range providers {
              fmt.Fprintf(w, "  %-12s %s (%s)\n", p.Name, p.Health, p.Driver)
          }
      }

      return nil
  }
  ```

### Task 6: 单元测试（AC: #1-#4）

- [x] 6.1 新增 `drivers/llm/registry_test.go` 健康状态测试（如文件已有，追加）：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestDriverRegistry_HealthStatus_DefaultUnchecked` | 注册 driver 后不设健康状态 | `GetHealth` 返回 `unchecked` |
  | `TestDriverRegistry_SetHealth_Healthy` | 调用 `SetHealth("x", HealthStatusHealthy)` | `GetHealth("x")` 返回 `healthy` |
  | `TestDriverRegistry_SetHealth_Unhealthy` | 调用 `SetHealth("x", HealthStatusUnhealthy)` | `GetHealth("x")` 返回 `unhealthy` |
  | `TestDriverRegistry_HealthStatuses_Sorted` | 注册多个 driver | `HealthStatuses()` 返回按 name 排序的列表 |

- [x] 6.2 新增 `drivers/llm/openai_compat_test.go` 健康检查测试（追加到现有文件）：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestOpenAICompatDriver_HealthCheck_Success` | httptest server 返回 200 + models JSON | `HealthCheck` 返回 nil |
  | `TestOpenAICompatDriver_HealthCheck_ServerDown` | 无可达服务器 | `HealthCheck` 返回 error |
  | `TestOpenAICompatDriver_HealthCheck_HTTP401` | httptest server 返回 401 | `HealthCheck` 返回含 "HTTP 401" 的 error |
  | `TestOpenAICompatDriver_HealthCheck_Timeout` | httptest server 延迟 5 秒响应 + 1 秒 context deadline | `HealthCheck` 返回 deadline exceeded |

- [x] 6.3 新增 `drivers/llm/factory_test.go` 异步健康检查测试（追加）：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestRunHealthChecks_HTTPProvider_Healthy` | OpenAI compat driver + healthy httptest server | 状态最终变为 `healthy` |
  | `TestRunHealthChecks_HTTPProvider_Unhealthy` | OpenAI compat driver + 不可达地址 | 状态最终变为 `unhealthy` |
  | `TestRunHealthChecks_CLIProvider_Skipped` | Claude CLI driver | 状态保持 `unchecked` |
  | `TestRunHealthChecks_NonBlocking` | 调用 RunHealthChecks 后立即返回 | 函数不阻塞（耗时 < 100ms） |

- [x] 6.4 新增 `kernel/atdd_23_6_health_check_status_test.go` 集成测试：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestATDD_23_6_AC1_HTTPProviderHealthCheck` | 注册 OpenAI compat driver 并执行健康检查 | 健康 provider 标记 `healthy` |
  | `TestATDD_23_6_AC2_UnreachableProvider` | HTTP provider 端点不可达 | daemon 正常（不 panic），provider 标记 `unhealthy` |
  | `TestATDD_23_6_AC2_HealthCheckTimeout` | 健康检查 > 3 秒 | 超时后标记 `unhealthy`，总耗时 <= 3 秒 |
  | `TestATDD_23_6_AC3_CLIProviderSkipped` | claude/cursor CLI driver | 状态保持 `unchecked` |
  | `TestATDD_23_6_AC4_DaemonStatusShowsProviders` | 通过 IPC 查询 provider 状态 | 返回正确的 name/driver/health 列表 |

- [x] 6.5 确保所有测试启用 `-race` 检测

## Dev Notes

### 核心设计决策

**1. 健康状态存储在 `DriverRegistry` 中（而非单独的 health manager）。**
- `DriverRegistry` 已是 driver 的统一管理入口，增加 health 维度是自然扩展
- 使用 `xsync.SyncMap` 保证线程安全——健康检查 goroutine 写入，IPC handler 读取
- 不引入新的 manager 类型，减少架构复杂度

**2. 使用可选接口 `HealthChecker`（而非在 `LLMDriver` 接口中添加方法）。**
- `LLMDriver` 接口已有 `Call`/`Stream`/`Info` 三个方法，新增 `HealthCheck` 会破坏所有现有实现
- 可选接口通过类型断言检测：`if hc, ok := drv.(HealthChecker); ok { ... }`
- 只有 `OpenAICompatDriver` 需要健康检查（HTTP endpoint），CLI 类 driver 不需要
- 未来新增 HTTP 类 driver 只需实现 `HealthChecker` 接口即可自动支持

**3. 健康检查异步执行，不阻塞 daemon 启动。**
- Epic 要求"健康检查异步执行，不阻塞 daemon 启动主流程"
- 每个 provider 独立 goroutine，互不影响
- 使用 `context.WithTimeout(3s)` 限制单个检查耗时（NFR32）
- 如果 daemon 启动后立即查询状态，可能看到 `unchecked`（检查尚未完成），这是预期行为

**4. 通过新增 `MethodProviderStatus` IPC 方法（而非扩展 PingResponse）。**
- `PingResponse` 语义为存活检测（轻量），不应承载 provider 详情
- 新增独立方法遵循 IPC 扩展标准 4 步流程（protocol → server → client → CLI）
- `runDaemonStatus` 调用两个方法：`Ping` + `ProviderStatus`，任一失败都优雅降级

**5. 健康检查使用 `GET /v1/models`（而非 `HEAD /`）。**
- `/v1/models` 是 OpenAI 兼容 API 标准端点，所有兼容服务器都支持
- `OpenAICompatDriver.baseURL` 已 TrimRight("/")，拼接 `"/models"` 即为正确路径
- 如果 baseURL 包含 `/v1` 前缀（如 `https://api.openai.com/v1`），则路径为 `/v1/models`
- 如果 baseURL 不包含 `/v1`（如 Ollama `http://localhost:11434`），实际路径为 `/models`（Ollama 也支持）
- 携带 API Key（如果有），验证认证是否有效

### 变更范围

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `drivers/llm/registry.go` | **修改** | 新增 `HealthStatus` 类型、`health` SyncMap 字段、`SetHealth`/`GetHealth`/`HealthStatuses` 方法 |
| `drivers/llm/driver.go` | **修改** | 新增 `HealthChecker` 可选接口 |
| `drivers/llm/openai_compat.go` | **修改** | 新增 `HealthCheck(ctx) error` 方法 |
| `drivers/llm/factory.go` | **修改** | 新增 `RunHealthChecks` 函数 |
| `ipc/protocol.go` | **修改** | 新增 `MethodProviderStatus` + `ProviderStatusResponse` + `ProviderStatusWire` |
| `ipc/server.go` | **修改** | 新增 `providerStatuses` 字段 + `SetProviderStatusFunc` + `handleProviderStatus` |
| `ipc/client.go` | **修改** | 新增 `ProviderStatus()` 方法 |
| `cmd/rnix/main.go` | **修改** | `runDaemon` 中调用 `RunHealthChecks` 并注入 provider 状态函数；`runDaemonStatus` 增加 provider 输出 |
| `drivers/llm/registry_test.go` | **修改** | 新增健康状态测试 |
| `drivers/llm/openai_compat_test.go` | **修改** | 新增 HealthCheck 测试 |
| `drivers/llm/factory_test.go` | **修改** | 新增 RunHealthChecks 测试 |
| `kernel/atdd_23_6_health_check_status_test.go` | **新增** | ATDD 集成测试 |

**不修改的文件：**
- `kernel/kernel.go` — Kernel 不参与健康检查，健康状态仅在 driver 层和 IPC 层处理
- `kernel/process.go` — 进程与健康检查无关
- `agents/` — Agent 不感知 provider 健康状态（fallback 逻辑已在 Story 23.5 中实现）
- `debug/` — 健康检查不产生 strace 事件

### 架构合规

- **依赖方向**：`drivers/llm` 不依赖 `kernel`（只添加自包含的健康检查）；`ipc` 依赖 `drivers/llm` 的类型（`ProviderStatus`）——这是新增的跨包依赖，但 `ipc` 已依赖 `kernel` 和其他包，方向合理
- **替代方案**：如果不希望 `ipc` 直接依赖 `drivers/llm`，可以在 `ipc/protocol.go` 中定义独立的 wire 类型，通过闭包注入 `func() []ProviderStatusWire`，完全解耦。推荐此方案——`Server.providerStatuses` 类型为 `func() []ProviderStatusWire`，在 `main.go` 中用闭包桥接 `driverReg.HealthStatuses()`
- **线程安全**：`SyncMap` 保证 `SetHealth`（写，健康检查 goroutine）和 `HealthStatuses`（读，IPC handler goroutine）的并发安全
- **命名规范**：`HealthCheck` 是标准 Go 健康检查命名；`HealthChecker` 用 `-er` 后缀（项目规范）
- **错误处理**：健康检查失败不返回 `SyscallError`（不是 syscall），直接用标准 `error`
- **IPC 扩展**：严格遵循 project-context.md 中的 4 步流程（protocol → server → client → CLI）

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| HealthCheck goroutine | daemon 启动 | 独立：不阻塞 daemon | 是（TestRunHealthChecks_NonBlocking） |
| HealthStatus 存储 | DriverRegistry.Register | Register 后默认 unchecked | 是（TestDriverRegistry_HealthStatus_DefaultUnchecked） |
| ProviderStatus IPC | Ping IPC | 独立方法，互不影响 | 是（AC #4 测试） |
| 健康检查 | Fallback（23.5） | 无直接交互：fallback 基于调用失败触发，不依赖健康状态；未来可优化为优先选择 healthy provider | 否（本 Story 不实现） |
| 健康检查 | Compose/Init（23.7） | 无交互：compose/init 不查询健康状态 | 否 |

### 前置 Story 智能

**来自 Story 23-1（config.go）：**
- `ProviderConfig` 有 `Driver string` 字段区分 `claude-cli`/`cursor-cli`/`openai-compat`
- `LoadOrDefaultProvidersConfig()` 返回 `*ProvidersConfig`，可传给 `RunHealthChecks`

**来自 Story 23-2（factory.go）：**
- `RegisterProviders` 在 daemon 启动时执行，是调用 `RunHealthChecks` 的最佳时机
- `CreateDriver` 根据 `DriverOpenAICompat`/`DriverClaudeCLI`/`DriverCursorCLI` 创建不同 driver
- 只有 `openai-compat` 返回的 `OpenAICompatDriver` 会实现 `HealthChecker`

**来自 Story 23-3（kernel.go）：**
- `resolveLLMDevice` 不需要修改——健康检查不影响 provider 解析
- `k.SetProviderResolver(driverReg.Names, ...)` 中 `Names()` 已有，无需变更

**来自 Story 23-4（factory.go）：**
- API Key 在 `CreateDriver` 中通过 `WithAPIKey` 注入
- `HealthCheck` 方法可以使用 `d.apiKey` 发送认证头，验证 Key 有效性

**来自 Story 23-5（kernel.go）：**
- Fallback 逻辑在 `reasonStep` 中基于调用失败触发，不查询健康状态
- 未来优化可能：fallback 时优先选择 `healthy` 的 provider（本 Story 不实现）

**已有测试模式：**
- `drivers/llm/factory_test.go` 使用 `httptest.NewServer` 测试 OpenAI compat driver
- `drivers/llm/openai_compat_test.go` 使用 `httptest.NewServer` 模拟 API endpoint
- `drivers/llm/registry_test.go` 已有 `TestDriverRegistry_*` 测试模式

### 测试策略

- **httptest.NewServer**：模拟 HTTP API endpoint，控制响应状态码和延迟
- **类型断言验证**：确认 `OpenAICompatDriver` 实现 `HealthChecker`，`ClaudeCliDriver` 不实现
- **异步等待**：`RunHealthChecks` 是异步的，测试中使用 `time.Sleep` 或轮询等待状态变更
- **不可达地址**：使用 `http://127.0.0.1:1`（几乎不可能被使用的端口）模拟不可达
- **超时验证**：httptest server 中 `time.Sleep(5s)` + 1s context deadline → 确认超时触发
- **竞态检测**：所有测试运行 `-race`

### Git 智能

最近提交：
- `d63d3ac feat: implement Story 23.5 - Provider Fallback Mechanism`
- `c9318b4 feat: implement Story 23.4 - API Key Management for HTTP API Provider`
- `e4c0324 feat: implement Story 23.3 - Dynamic Provider Resolution and Whitelist Removal`

提交建议：`feat: implement Story 23.6 - Provider Health Check and Status Report`

### 跨 Story 边界（本 Story 不实现）

| 功能 | 归属 | 说明 |
|------|------|------|
| Compose/Init provider 配置 | 23-7 | 与健康检查无关 |
| Fallback 基于健康状态优化 | 未规划 | 当前 fallback 基于调用失败触发，未来可优先选 healthy provider |
| 定期健康检查（background probe） | 未规划 | 本 Story 仅在启动时检查一次 |
| `rnix provider health` 独立 CLI 命令 | 未规划 | 本 Story 将状态集成到 `daemon status` |

### Project Structure Notes

- 变更主要在 `drivers/llm/` 包（registry、driver、openai_compat、factory）和 `ipc/` 包（protocol、server、client）
- CLI 变更在 `cmd/rnix/main.go`（daemon 启动和 status 输出）
- ATDD 测试在 `kernel/atdd_23_6_health_check_status_test.go`
- 不引入新外部依赖
- 与统一项目结构完全对齐

### DriverInfo.DriverType 字段

`HealthStatuses()` 需要返回 driver type（`claude-cli`/`cursor-cli`/`openai-compat`）。需确认 `DriverInfo` 结构体是否有 `DriverType` 字段。如果没有，可通过以下方式获取：
- 方案 A：在 `DriverInfo` 中新增 `DriverType string` 字段（推荐）
- 方案 B：在 `DriverRegistry` 中额外存储 `driverType SyncMap[name, string]`
- 方案 C：`HealthStatuses` 不返回 driver type，仅返回 name + health

建议检查 `drivers/llm/driver.go` 中 `DriverInfo` 定义。如果已有 `DriverType` 或类似字段则直接使用；如果没有，优先方案 A（改动最小且语义清晰）。

### References

- [Source: drivers/llm/registry.go#1-51] — `DriverRegistry` 完整实现（需扩展）
- [Source: drivers/llm/openai_compat.go#21-31] — `OpenAICompatDriver` 结构体（`baseURL`、`apiKey`、`httpClient`）
- [Source: drivers/llm/openai_compat.go#47-48] — `WithHTTPClient` option（测试注入）
- [Source: drivers/llm/driver.go#56-61] — `LLMDriver` 接口（新增 `HealthChecker` 可选接口）
- [Source: drivers/llm/factory.go#56-84] — `RegisterProviders`（健康检查调用点）
- [Source: drivers/llm/config.go#29-40] — `ProvidersConfig`/`ProviderConfig`（`Driver` 字段用于判断类型）
- [Source: ipc/protocol.go#17-43] — Method 常量（新增 `MethodProviderStatus`）
- [Source: ipc/protocol.go#344-349] — `PingResponse`（不扩展，新增独立方法）
- [Source: ipc/server.go#59-78] — `Server` 结构体（新增 `providerStatuses` 字段）
- [Source: ipc/server.go#255-312] — handleConnection switch（新增 `case MethodProviderStatus`）
- [Source: ipc/server.go#316-326] — `handlePing`/`handleListProcs` 模式参考
- [Source: ipc/client.go#44-54] — `Ping` 客户端方法模式参考
- [Source: cmd/rnix/main.go#1027-1054] — `runDaemonStatus`（扩展输出）
- [Source: cmd/rnix/main.go#1056-1069] — `runDaemon`（调用 `RunHealthChecks`）
- [Source: cmd/rnix/main.go#1101-1104] — Server 注入点（新增 `SetProviderStatusFunc`）
- [Source: _bmad-output/project-context.md#154-163] — IPC 扩展标准 4 步流程
- FRs covered: FR141（配置解析后的可用性验证）
- NFRs covered: NFR32（单个健康检查耗时 <= 3 秒）

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

N/A

### Completion Notes List

- All 6 tasks and 15 subtasks implemented per story spec
- Added `DriverType` field to `DriverInfo` to support driver type reporting in `HealthStatuses()`
- Used recommended architecture: `ipc.Server.providerStatuses` is `func() []ProviderStatusWire` closure — zero cross-package dependency between `ipc` and `drivers/llm`
- All 20 packages pass with `-race` detection, zero failures
- Unit tests: 7 registry health tests, 7 HealthCheck tests, 4 RunHealthChecks tests
- ATDD tests: 16 integration tests covering AC1-AC4 + mixed provider integration

### File List

| File | Change | Description |
|------|--------|-------------|
| `drivers/llm/driver.go` | Modified | Added `DriverType` field to `DriverInfo`, added `HealthChecker` optional interface |
| `drivers/llm/registry.go` | Modified | Added `HealthStatus` type/constants, `health` SyncMap, `SetHealth`/`GetHealth`/`HealthStatuses` methods, `ProviderStatus` struct |
| `drivers/llm/openai_compat.go` | Modified | Added `HealthCheck(ctx)` method, `DriverType` in `Info()`, compile-time `HealthChecker` check |
| `drivers/llm/claude_cli.go` | Modified | Added `DriverType` in `Info()` |
| `drivers/llm/cursor_cli.go` | Modified | Added `DriverType` in `Info()` |
| `drivers/llm/factory.go` | Modified | Added `RunHealthChecks` async function |
| `ipc/protocol.go` | Modified | Added `MethodProviderStatus`, `ProviderStatusResponse`, `ProviderStatusWire` |
| `ipc/server.go` | Modified | Added `providerStatuses` field, `SetProviderStatusFunc`, `handleProviderStatus` handler, switch case |
| `ipc/client.go` | Modified | Added `ProviderStatus()` client method |
| `cmd/rnix/main.go` | Modified | `runDaemon`: call `RunHealthChecks` + inject `SetProviderStatusFunc`; `runDaemonStatus`: provider health output |
| `drivers/llm/registry_test.go` | Modified | Added 7 health status tests |
| `drivers/llm/openai_compat_test.go` | Modified | Added 7 HealthCheck tests |
| `drivers/llm/factory_test.go` | Modified | Added 4 RunHealthChecks tests |
| `kernel/atdd_23_6_health_check_status_test.go` | Modified | Replaced RED skip tests with 16 GREEN passing ATDD tests |

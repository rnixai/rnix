# Non-Functional Requirements

## Performance

- **NFR1:** 单智能体 spawn→完成（含 LLM 调用），端到端延迟 ≤ 30 秒（简单任务如单文件分析）
- **NFR2:** `rnix ps` 响应时间 ≤ 100ms（本地进程表查询，不涉及 LLM）
- **NFR3:** `strace` 输出延迟 ≤ 500ms（从 syscall 发生到终端显示）
- **NFR4:** VFS 本地文件读取（`/dev/fs`）额外延迟 < 10ms，不超过直接文件 I/O 延迟的 2 倍
- **NFR5:** 上下文组装（ctx → prompt）时间 ≤ 1 秒（不含 LLM 调用本身）

## Reliability

- **NFR6:** 连续 20 次 spawn→完成路径，成功率 ≥ 95%
- **NFR7:** LLM API 超时/错误时，进程在 5 秒内正确转入 Zombie 状态，不卡死在 Running
- **NFR8:** 进程退出后，goroutine 和 context 内存在 10 秒内释放，无泄漏
- **NFR9:** 内核进程表在任意进程异常退出后保持一致性（无悬挂 PID、无状态不一致）
- **NFR10:** CLI 进程（rnix 二进制本身）在智能体异常退出时不崩溃

## Integration

- **NFR11:** LLM 驱动层调用时，正确传递 system prompt、工具声明、模型选择、输出格式等参数
- **NFR12:** LLM 驱动层支持流式结构化输出模式，用于 strace 实时数据采集
- **NFR13:** 宿主文件系统通过 `/dev/fs` 访问时，遵循宿主 OS 的文件权限（不绕过宿主权限模型）
- **NFR14:** Shell 驱动（`/dev/shell`）执行命令时，继承当前用户的环境变量和 PATH

## Security

- **NFR15:** `/dev/shell` 执行的命令继承当前用户权限，不提供额外提权能力
- **NFR16:** Skill 的 `SKILL.md` 中 `allowed-tools` 声明作为智能体可访问设备的白名单——Agent 引用的所有 Skill 的 `allowed-tools` 聚合后，未声明的设备不可访问
- **NFR17:** MVP 阶段不实现完整 Capability 权限系统，但 Skill `allowed-tools` 聚合白名单作为最小安全边界

## Maintainability（可维护性）

- **NFR18:** 内核代码遵循 Go 标准项目布局，通过 `go vet` 和 `golint` 无警告
- **NFR19:** syscall ABI 设计遵循 45 syscall 架构规范的子集，确保 Phase 2 扩展时向后兼容
- **NFR20:** LLM 驱动层封装在单一模块中，外部 LLM 接口变更时只需修改此模块

## Multi-Agent Performance（多智能体性能，Phase 2）

- **NFR21:** Compose 编排 N 个智能体（N ≤ 10）的启动延迟（不含 LLM 调用本身）≤ 2 秒
- **NFR22:** IPC Send/Recv 单条消息端到端延迟 ≤ 50ms（进程间，不含 LLM 推理）
- **NFR23:** Pipe 管道数据传递吞吐量 ≥ 1MB/s（智能体间文本数据流）
- **NFR24:** 系统可以同时运行 ≥ 10 个智能体进程，进程表操作（PS/Spawn/Kill）延迟不超过单进程场景的 2 倍

## MCP Integration Quality（MCP 集成质量，Phase 2）

- **NFR25:** MCP 服务挂载延迟（从 Mount syscall 到服务可用）≤ 500ms
- **NFR26:** MCP 服务异常退出时不影响内核稳定性，对应 VFS 路径在 3 秒内返回明确错误（`ErrServiceUnavailable`）而非卡死
- **NFR27:** 系统兼容 MCP 协议标准版本，可接入符合 MCP 标准的第三方服务器，无需 Rnix 侧代码修改

## Observability & Ecosystem（可观测性与生态，Phase 2）

- **NFR28:** `rnix top` TUI 刷新间隔 ≤ 500ms，单核 CPU 占用 ≤ 5%（10 个并发进程场景）
- **NFR29:** `rnix log` 输出延迟 ≤ 200ms（从推理事件发生到终端显示）
- **NFR30:** 社区 Skill 通过 `skill install` 安装后无需修改即可被任意 Agent 引用，Skill 格式兼容性通过标准 SKILL.md frontmatter 验证

---

## Debugging Toolchain Performance（调试工具链性能，Phase 3）

- **NFR31:** agdb Attach 延迟 ≤ 200ms，断点触发到暂停延迟 ≤ 100ms
- **NFR32:** 时间旅行录制开启后，智能体执行性能开销 ≤ 20%（相比无录制）
- **NFR33:** 分布式追踪的 Trace/Span 传播不增加 IPC 消息延迟超过 10ms
- **NFR34:** ctx-profiler 分析结果延迟 ≤ 1s（≤ 100k token 上下文）
- **NFR35:** agtest 单个测试用例的框架开销（不含 LLM 调用）≤ 500ms

## Visualization Dashboard Performance（可视化面板性能，Phase 3）

- **NFR36:** dashboard TUI 刷新间隔 ≤ 500ms，10 个并发进程场景下单核 CPU 占用 ≤ 10%
- **NFR37:** dashboard 智能体树渲染支持 ≥ 50 个进程节点无明显卡顿

## AgentShell Scripting Performance（AgentShell 脚本性能，Phase 3）

- **NFR38:** AgentShell 脚本解析时间 ≤ 50ms（≤ 1000 行脚本）
- **NFR39:** AgentShell 循环和函数调用的运行时开销（不含 spawn/LLM）≤ 1ms/次

## Emergence Layer Performance（涌现层性能，Phase 3）

- **NFR40:** Reconciler 从检测到 drift 到启动调和行动的延迟 ≤ 5s（事件驱动模式）
- **NFR41:** OODA 单轮循环（Observe→Orient→Decide→Act）的框架开销（不含 LLM）≤ 200ms
- **NFR42:** 干细胞分化的 Skill 匹配和加载过程 ≤ 3s（≤ 10 个候选 Skill）
- **NFR43:** Token 预算分配决策延迟 ≤ 100ms
- **NFR44:** Immune Daemon 行为监控的 CPU 开销 ≤ 3%（10 个并发进程）
- **NFR45:** 能力迁移（任务从崩溃进程转移到替代进程）≤ 10s
- **NFR46:** Synergy 组合检测开销 ≤ 100ms（≤ 20 个已加载 Skill）

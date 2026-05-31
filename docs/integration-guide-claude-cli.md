# Claude CLI Driver 集成指南

本文档描述 Rnix 如何与 Claude Code CLI 集成，包括二进制解析、能力探测、权限模式配置和故障排查。

## 二进制解析策略

Claude CLI driver 通过以下顺序查找可用的 Claude CLI 二进制：

1. **PATH 搜索**：按 `fallbackBins` 列表顺序（默认 `["claude", "openclaude"]`）调用 `exec.LookPath`
2. **扩展目录搜索**：对每个候选名称，依次检查：
   - `~/.local/bin/` — pipx、npm global 等常见安装路径
   - `$NVM_DIR/versions/node/*/bin/` — 取最新版本目录（按版本号降序排列）
   - `~/.bun/bin/` — Bun 包管理器安装路径

首个匹配的可执行文件将被缓存为 `resolvedBin`（绝对路径），后续所有 CLI 调用使用此路径。

## Capability 探测行为

Driver 在首次 `Call()` 或 `Stream()` 调用时，通过 `sync.Once` 懒执行探测：

```
claude -p --help   # 5 秒超时
```

扫描 stdout 输出，检测以下标志是否存在：

| 标志 | 对应能力 | 用途 |
|------|----------|------|
| `--include-partial-messages` | `partialMessages` | 启用流式部分消息输出 |
| `--add-dir` | `addDir` | 注入外部目录（skill bundle） |
| `--permission-mode` | `permissionMode` | 设置权限模式 |

**探测失败行为**：超时、二进制缺失、非零退出码、输出过大（>4MB）均静默降级为 "全部不支持"（保守模式）。仅打印一次警告日志。

## Permission Mode 使用

### 配置方式

在 `providers.yaml` 中配置：

```yaml
providers:
  claude:
    driver: claude-cli
    model: sonnet
    permission_mode: bypassPermissions  # 默认值
```

### 有效值

| Mode | 说明 |
|------|------|
| `bypassPermissions` | 跳过所有权限确认（daemon 模式默认值） |
| `acceptEdits` | 自动接受文件编辑，其余操作仍需确认 |
| `plan` | 仅规划模式，不执行实际操作 |
| `default` | 使用 Claude CLI 原生默认行为 |

### 安全语义

`bypassPermissions` 是 Rnix daemon 的默认值。这是安全的，因为：
- Rnix 进程运行在 daemon 管理下，没有 TTY 交互能力
- 权限控制由 Rnix VFS 设备白名单（`allowed_tools`）实现
- CLI 的权限弹窗在无 TTY 环境下会阻塞进程

## 故障排查

### Partial Messages 不工作

**症状**：进程输出中看不到流式中间消息，所有内容在完成后一次性返回。

**原因**：本地安装的 Claude CLI 版本不支持 `--include-partial-messages` 标志。

**修复方法**：
1. 检查 CLI 版本：`claude --version`
2. 更新到最新版本：`npm update -g @anthropic-ai/claude-code`
3. 验证标志支持：`claude -p --help | grep include-partial-messages`

### 权限卡死

**症状**：进程在首次 LLM 调用时挂起，daemon 日志无响应。

**原因**：`--permission-mode` 标志不被支持，CLI 弹出 TTY 交互提示但 daemon 无 TTY。

**修复方法**：
1. 确认 CLI 支持 permission mode：`claude -p --help | grep permission-mode`
2. 如果不支持，将 `permission_mode` 设为空字符串（不传该标志）
3. 或更新 CLI 到支持版本

### 二进制找不到

**症状**：Spawn 失败，错误信息 `no claude-compatible CLI found in PATH: tried [claude openclaude]`。

**原因**：系统 PATH 和扩展搜索路径中均未找到 Claude CLI 二进制。

**修复方法**：
1. 安装 Claude CLI：`npm install -g @anthropic-ai/claude-code`
2. 确认安装路径在 PATH 中：`which claude`
3. 如果使用 nvm/bun 安装，确保 `$NVM_DIR` 环境变量正确
4. 可通过 `providers.yaml` 自定义二进制名称：
   ```yaml
   providers:
     claude:
       driver: claude-cli
       command: /custom/path/claude  # 指定完整路径
   ```

### Dashboard 不显示 Driver 信息

**症状**：Dashboard Detail pane 中没有 `Binary:` / `Capabilities:` 行。

**原因**：进程使用的 driver 不是 Claude CLI（例如 Anthropic API、OpenAI 等），这些 driver 不实现 `DriverMetaProvider` 接口。

**修复方法**：这是预期行为。`DriverMeta` 信息仅对 Claude CLI driver 显示。

# Raw LLM Request/Response Logging

> Story 56.1 地基；Epic 56 余 story 在此基础上填充各 driver 的捕获实现。

Rnix 自 Epic 56 起为每个进程的 LLM 调用记录原始请求与响应（HTTP body / CLI argv+stdin+stdout 的字节流），用于回答“这个 reasoning_effort / thinking_budget / temperature 究竟有没有提交给 LLM？”这类透传可观察问题。

## 默认行为

- **默认开启**（CAP-4 红线）。`raw_capture.enabled: true`。
- 每次 `reasonStep` 内一次成功的 LLM 读响应后，kernel 把当次原始请求 / 响应追加为 **一行 NDJSON** 到 `<dataDir>/steps/<uuid>/raw.jsonl`。
- `raw.jsonl` 与 `steps.jsonl` / `events.jsonl` / `ctx-profile.json` / `process-meta.json` 同目录、同生命周期，受现有 gc 策略统一清理。
- 56.1 是地基 story：**没有任何 driver 实现捕获**。`raw.jsonl` 文件采用 **lazy creation**——首次成功写入才 `O_CREATE`；driver 一次都不 opt-in 时该文件不会出现，避免在 `<uuid>/` 目录留空文件。56.2（API driver）/ 56.3（CLI driver）后续填充。

## 配置（`~/.config/rnix/config.yaml`）

```yaml
raw_capture:
  enabled: true            # 默认 true；设 false 则全局关闭原始记录
  max_output_bytes: 4194304  # 单条 request/response 字段超过这个字节数会被截断（默认 4MB）
```

- `enabled: false` → 全局关闭，所有进程不再写 `raw.jsonl`。
- `max_output_bytes` 上限只截断**单条记录内的字段**（如某次响应 body），不截断累计写入量；累计上限走 gc。
- 配置是 **kernel 级正交配置**，与 `features` profile 完全独立——baseline profile 不会让 raw 记录关闭。

## 落盘格式

`raw.jsonl` 每行一条 `vfs.RawCapture`，字段全 `snake_case`：

```jsonc
{
  "ts_ms": 1734528342000,
  "step": 7,
  "kind": "api",                    // 或 "cli"
  "request":  { /* 由 driver 决定字段集 */ },
  "response": { /* 由 driver 决定字段集 */ },
  "truncated": false,
  "original_bytes": 0
}
```

| 族 | `kind` | `request` 通用形状 | `response` 通用形状 |
|----|-------|-------------------|---------------------|
| API driver | `"api"` | `method` / `url` / `headers`（已脱敏）/ `body` | `status` / `headers` / `body`（含原始 SSE 字节流） |
| CLI driver | `"cli"` | `argv` / `stdin` / `env`（已脱敏） | `stdout` / `stderr` / `exit_code` |

具体字段名 56.1 不锁死，由 56.2 / 56.3 填充。

## 脱敏（写盘前横切）

`vfs.RedactHeaders` / `vfs.RedactCredential` 是纯函数：

- 凭据 header（`Authorization` / `proxy-authorization` / `api-key` / `x-api-key` / `x-auth-token` / `cookie` / `set-cookie`）大小写不敏感匹配。
- `"Bearer <secret>"` / `"Basic <secret>"` 之类 scheme-token 形式保留 scheme，只把 secret 部分换成指纹。
- 指纹形态：`redacted(len=<N>,prefix=<前 3 字节>,sha256=<sha256 前 12 hex>)`。包含足够区分凭据形态的线索（长度、prefix、hash），但**不可逆推原值**。
- **幂等**：`Redact(Redact(x)) == Redact(x)`，driver 已脱敏 + kernel 二次脱敏不会嵌套。
- 非凭据 header 原样透传。
- 多值 header（`map[string]any` 中的 `[]string` / `[]any`）逐元素 redact，类型不丢——避免「把多值默默丢成空」造成 secret 整条消失。

> **风险声明：vendor class 可见**
>
> `prefix=<前 3 字节>` 对短 vendor-prefixed token 几乎暴露 key class（`sk-` → OpenAI、`ghp_` → GitHub、`glpat-` → GitLab、`xoxb-` → Slack）。这是有意保留——便于运维粗筛 key 来源——但威胁模型若禁止暴露 vendor class，需要扩展 `RedactCredential` 接受一个 prefix-disable 选项（出 56.1 范围）。如果 disk/log 不允许出现 vendor 标识，请将凭据轮换到无 prefix 的 raw token，或在 driver 层提前对 header 做更严格的 redact。

`drivers/llm/*` 在生成 `RawCapture` 之前必须先脱敏；kernel 在写盘前再做一次 defense-in-depth pass，避免单点漏脱。

## 截断（per-record，非 per-field）

写盘前 kernel 把整条 `RawCapture` marshal 一遍：
- 整行 ≤ `max_output_bytes` → 不动
- 整行 > `max_output_bytes` → 按 `Response` 优先、`Request` 次之的顺序，**逐次驱逐当前 map 内最大的 string 字段**，把字段值替换成 `<truncated: <N> bytes, max=<M>>` 标记并累加 `original_bytes`，置 `truncated: true`，直到整行落入预算

进程不会因截断中断；语义对齐 `drivers/mcp/transport.go::truncateResultLocked`。`max_output_bytes` 与 `ReadAllRaw` 内 `bufio.Scanner` 上限是一致语义（per-record 行长度），不会出现「截断后仍读不回来」的内部矛盾。

## 留存（gc 复用）

`raw.jsonl` 落在 `.rnix/data/steps/<uuid>/` 内，受 `kernel/gc.go` 的 `retention_days` + `max_entries` 整目录清理覆盖；Running / Suspended 进程永久豁免。本 story 不新增 gc 代码。

## 故障容忍

- `Enabled=false` → 不创建 `raw.jsonl`，hook 在 cfg 检查处直接退出。
- 写盘失败 → 仅 `log.Printf` 一行 `[raw_writer] write error pid=… step=…: …`，**reasonStep 继续执行**（best-effort 可观察）。
- driver 未实现 `vfs.RawCaptureProvider` → 56.1 全部 driver 当前状态——hook 在 type-assert 处返回 nil，不写任何记录。

## 接口与代码锚点

- `vfs.RawCaptureProvider` — 可选 pull 接口，类比 `vfs.ReasoningEffortProvider`（`vfs/raw_capture.go`）
- `vfs.RawCapture` — 中立信封 struct（`vfs/raw_capture.go`）
- `vfs.RedactHeaders` / `vfs.RedactCredential` — 脱敏 helper（`vfs/redact.go`）
- `kernel.RawCaptureConfig` + `SetRawCaptureConfig` / `RawCaptureCfg` — kernel 级开关（`kernel/raw_capture_config.go`）
- `kernel.RawWriter` + `NewRawWriter` / `WriteRaw` / `ReadAllRaw` / `ReadRawForStep` — NDJSON writer（`kernel/raw_writer.go`）
- `kernel.captureRawLLMCall` — reasonStep 钩子（`kernel/raw_writer.go`，落点 LLM Read 事件之后；review patch P6 自 Write 事件后挪到 Read 事件后，以便未来 Stream-style driver 在 Read 期间累积响应）
- `LLMFile.LastRawCapture()` — driver 层委托（`drivers/llm/vfsfile.go`），driver 实现 `rawCaptureDriver`（`drivers/llm` 内部接口）即可填充

## 与现有归一化层的关系

- `steps.jsonl` 的 `messages` / `raw_response` **不变**：那是 driver 解析后的归一化形态（`{role, content, tool_calls...}`）。
- `events.jsonl` 的 `Write` 事件 args 维持 `{fd, size, model, reasoning_effort}` 四字段不变。
- `raw.jsonl` 是**新增**第三类落盘文件，独立于上述两类，互不读写。

# Raw LLM Request/Response Logging

> Story 56.1 地基；Story 56.2 接通 4 个 API driver（openai / openai-compat / anthropic / gemini）；Story 56.3 接通 4 个 CLI driver（claude-cli / codex-cli / cursor-cli / qwen-cli）。8/8 driver 全量产生 `raw.jsonl`。

Rnix 自 Epic 56 起为每个进程的 LLM 调用记录原始请求与响应（HTTP body / CLI argv+stdin+stdout 的字节流），用于回答"这个 reasoning_effort / thinking_budget / temperature 究竟有没有提交给 LLM？"这类透传可观察问题。

## 默认行为

- **默认开启**（CAP-4 红线）。`raw_capture.enabled: true`。
- 每次 `reasonStep` 内一次成功的 LLM 读响应后，kernel 把当次原始请求 / 响应追加为 **一行 NDJSON** 到 `<dataDir>/steps/<uuid>/raw.jsonl`。
- `raw.jsonl` 与 `steps.jsonl` / `events.jsonl` / `ctx-profile.json` / `process-meta.json` 同目录、同生命周期，受现有 gc 策略统一清理。
- 56.2 / 56.3 完成后 8 个 driver（4 个 API + 4 个 CLI）的 Call 与 Stream 路径都会产生 `raw.jsonl`。`raw.jsonl` 文件采用 **lazy creation**——首次成功写入才 `O_CREATE`；driver 一次都没产生 capture 时该文件不会出现，避免在 `<uuid>/` 目录留空文件。

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
  "original_bytes": 0,
  "outcome": "error",               // 仅失败记录出现（56.7）；成功记录无此键
  "error": "gateway returned …"     // 仅失败记录出现（56.7）：callErr.Error()
}
```

| 族 | `kind` | `request` 通用形状 | `response` 通用形状 |
|----|-------|-------------------|---------------------|
| API driver | `"api"` | `method` / `url` / `headers`（已脱敏）/ `body`（实际发送的 HTTP body 字符串） | `status` / `headers`（已脱敏）/ `body`（含原始 SSE 字节流） |
| CLI driver | `"cli"` | `argv: []string`（含 binary，已脱敏）/ `stdin: string`（driver 构造的 prompt 字符串） / `env`（可选，已脱敏） | `stdout: string` / `stderr: string` / `exit_code: int` |

字段类型铁律（56.3 裁决 3）：

- `argv` 是 `[]string` 保留结构，便于审计 / 查询；逃 kernel per-record 截断（裁决权衡），凭据脱敏由 driver 层 `vfs.RedactArgv` 主负责（唯一防线，kernel 二次脱敏只 walk headers 字段）。
- `stdin` / `stdout` / `stderr` **必须是 `string` 类型**：kernel `truncateRawCapture` → `largestStringKey` 只截断 map 中 string 值字段；非 string 会逃过 per-record 截断。stdout 可能巨大（stream-json 全量），必须 string 才能被 4MB 截断。
- `exit_code` 是 `int`（CLI driver 写 `cmd.ProcessState.ExitCode()`，启动失败 / pre-Wait 状态返回 `-1`）。
- API driver 的字段在 56.2 已锁死并由 4 个 driver 共享填充；CLI driver 的字段在 56.3 已锁死并由 4 个 driver 共享填充。
- `outcome` / `error`（56.7，additive + `omitempty`）：kernel hook 在调用**失败**时于深拷贝后的记录上填 `outcome: "error"` + `error: <callErr 文本>`；成功记录两键不序列化，旧格式文件读回为零值——新旧双向兼容。

## 失败调用语义（56.7）

失败恰恰是最需要「取回真实请求」的场景。56.7 把 raw capture 链路补到失败路径：

- **primary Write 失败**（transient 与终态都算；suspend 早退除外——挂起非失败，进程将 resume）→ 该次调用照常落盘，带 `outcome`/`error` 标记。
- **fallback 调用**（`attemptFallback` 的 fbFD）→ 成败均落盘。primary 失败 + fallback 成功时同一 `step` 会有**两条**记录（前者带 outcome=error，后者无 outcome 键）。
- **同 step 多条记录的查询语义**：`ReadRawForStep` / `get_raw_capture --step` 取 **last-match**（终态优先——fallback 记录晚于 primary 失败记录）。旧数据（每 step 单条）行为不变。transient retry 因 reason 循环的 `continue` 会 `step++`，失败记录与重试成功记录分属相邻 step，不叠加。
- **Stream 失败归集**（`LLMFile.writeStream`）：error 事件不再提前 return——drain 到 channel close 再归集 sink（56.3 时序铁律：`sink.set` 先于 `close(ch)`），失败调用的 `LastRawCapture()` 是含已累积 stdout/stderr/exit_code 的终态。`ErrStreamIncomplete` 泛化错误在 capture 有 stderr 时追加 `(exit N; stderr: <尾部≤2KB>)` 诊断。
- **codex/cursor stderr 透传**：Stream 无 result 退出时 error 事件消息追加 `(exit N): <stderr 尾部≤2KB>`（`stderrTail`，UTF-8 边界安全）；全量 stderr 仍在 raw capture 的 `response.stderr`。claude/qwen 的 no-result 路径原本就先 `cmd.Wait()` 再读 stderr，行为不变。
- **渲染**：`RenderRawLens` 对 `outcome=="error"` 记录在 Response 段之前渲染一行 `⚠ outcome: error — <error>`，`rnix strace --raw` 与 dashboard Raw lens 双路自动生效。

## 脱敏（写盘前横切）

`vfs.RedactHeaders` / `vfs.RedactCredential` / `vfs.RedactArgv` 是纯函数：

- 凭据 header（`Authorization` / `proxy-authorization` / `api-key` / `x-api-key` / `x-goog-api-key` / `x-auth-token` / `cookie` / `set-cookie`）大小写不敏感匹配。
- 凭据 argv flag（`--api-key` / `--apikey` / `--token` / `--auth-token` / `--password` / `--secret` / `--bearer` / `--access-key` / `--access-token` / `-H` / `--header`，大小写不敏感）的值经 `vfs.RedactArgv` 指纹化；`-H Authorization: Bearer …` 路由到 `redactHeaderLine`，与 header 走同一脱敏规则。
- effort 透传 flag（`--effort high` / `-c model_reasoning_effort=high` / `--model haiku` / `--output-format json`）**保留真实值**——CAP-1 核心，本 driver 层主脱敏的正向验收点。
- `"Bearer <secret>"` / `"Basic <secret>"` 之类 scheme-token 形式保留 scheme，只把 secret 部分换成指纹。
- 指纹形态：`redacted(len=<N>,prefix=<前 3 字节>,sha256=<sha256 前 12 hex>)`。包含足够区分凭据形态的线索（长度、prefix、hash），但**不可逆推原值**。
- **幂等**：`Redact(Redact(x)) == Redact(x)`，driver 已脱敏 + kernel 二次脱敏不会嵌套；`vfs.RedactArgv(vfs.RedactArgv(x))` 同样幂等。
- 非凭据 header / argv 元素 / stdin 原样透传。stdin 不脱敏（裁决 4：stdin 是用户内容，审计需看原文；prompt 偶含明文凭据属用户输入问题，不在本 story 兜底）。
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
- driver 未实现 `vfs.RawCaptureProvider` → hook 在 type-assert 处返回 nil，不写任何记录。当前 8 个内置 driver（4 API + 4 CLI）全部接通；自定义 driver 不实现 `LastRawCapture` 不影响主流程。
- API driver 调 driver.Call/Stream 出错 → sink 已落入的 Request 形态保留（便于审计排错），Response 字段缺失。
- CLI driver `cmd.Run` / `cmd.Start` 失败 → sink 已设的 Request 保留；Response 不会填，`exit_code` 为 `-1`（`processExitCode` 判 nil ProcessState 兜底）。
- CLI Stream 路径 ctx 未挂 sink（生产路径必有，仅测试场景） → driver 跳过 tee + 不分配 `rawStdoutBuf`，零开销 fast path。

## 接口与代码锚点

- `vfs.RawCaptureProvider` — 可选 pull 接口，类比 `vfs.ReasoningEffortProvider`（`vfs/raw_capture.go`）
- `vfs.RawCapture` — 中立信封 struct（`vfs/raw_capture.go`）
- `vfs.RedactHeaders` / `vfs.RedactCredential` / `vfs.RedactArgv` — 脱敏 helper（`vfs/redact.go`）
- `kernel.RawCaptureConfig` + `SetRawCaptureConfig` / `RawCaptureCfg` — kernel 级开关（`kernel/raw_capture_config.go`）
- `kernel.RawWriter` + `NewRawWriter` / `WriteRaw` / `ReadAllRaw` / `ReadRawForStep` — NDJSON writer（`kernel/raw_writer.go`）
- `kernel.captureRawLLMCall` — reasonStep 钩子（`kernel/raw_writer.go`，落点 LLM Read 事件之后；review patch P6 自 Write 事件后挪到 Read 事件后，以便未来 Stream-style driver 在 Read 期间累积响应）
- `LLMFile.LastRawCapture()` — field-first / fallback-委托（`drivers/llm/vfsfile.go`）：优先返回 per-Open `f.lastRawCapture`，nil 时 fallback 委托 `rawCaptureDriver`
- `drivers/llm/raw_capture.go` — driver 层共享设施：
  - `rawCaptureSink` + `withRawSink` / `rawSinkFromContext` — ctx-scoped sink（裁决 1 并发铁律：driver 是跨进程共享单例，capture 不能存 driver 字段）
  - `captureMiddlewareFunc` — openai-go / anthropic-sdk-go 的 `option.Middleware` 共享函数（两 SDK Middleware 都是 type alias 同底层签名）
  - `captureRoundTripper` + `wrapHTTPClientWithCapture` — gemini SDK 与 openai-compat 共用的 RoundTripper 包装
  - `newCLIRawCapture` / `fillCLIRequest` / `fillCLIResponse` / `processExitCode` — 56.3 引入的 CLI 族 helper：driver 在 Call/Stream 内联直接填 sink，无需 SDK hook

## 各 API driver 的捕获机制（56.2）

| Driver | 类型 | 捕获机制 | 注入点 |
|--------|------|---------|--------|
| `openai-compat` | 手写 HTTP | `wrapHTTPClientWithCapture` 包装 `d.httpClient.Transport` 为 `captureRoundTripper` | `NewOpenAICompatDriver` |
| `openai` | SDK（openai-go v3） | `option.WithMiddleware(captureMiddlewareFunc)` 追加到 `cfg.sdkOpts` | `NewOpenAIDriver` |
| `anthropic` | SDK（anthropic-sdk-go） | `option.WithMiddleware(captureMiddlewareFunc)` 追加到 `cfg.sdkOpts` | `NewAnthropicDriver` |
| `gemini` | SDK（genai） | `wrapHTTPClientWithCapture(d.httpClient)` → `ClientConfig.HTTPClient` | `GeminiDriver.newClient`（per-Call/Stream） |

**SDK middleware / 自定义 HTTPClient 仍属「driver 层捕获」**，不违反 SPEC Constraint「捕获点在 driver 层、不在网络层；不引入 MITM 代理或网络层抓包」——它在 driver 代码内、针对 driver 自有的 SDK client 配置（不是独立代理进程，不解密 TLS，不 hook 全局 `net.Dial`）。它是兑现 CAP-2「SSE 原样字节」的唯一途径——SDK 内部已解析掉 SSE，driver 代码层取不到原始字节，只有在 HTTP 层 tee `resp.Body` 才能取到原样字节流。

## 各 CLI driver 的捕获机制（56.3）

CLI driver 直接掌握 argv（`buildArgs` 构造）、stdin（driver 构造的 prompt）、stdout（`bytes.Buffer` for Call / `StdoutPipe` for Stream），**无需任何 SDK / middleware**。每个 driver 在 Call/Stream 内联取 ctx-scoped sink + `s.set(cap)`：

| Driver | prompt 传递 | effort 透传 flag | Stream 路径捕获方式 |
|--------|------------|-----------------|---------------------|
| `claude-cli` | stdin（`StdinPipe` + `writeStdinSafe`） | `--effort <v>`（two-token form） | `io.TeeReader(stdoutPipe, &rawStdoutBuf)`；scanner 改读 tee reader |
| `codex-cli` | argv 末尾（buildArgs append） | `-c model_reasoning_effort=<v>`（KV form） | 同上 |
| `cursor-cli` | argv 末尾 | 不支持（thinking 绑 model 名后缀） | 同上 |
| `qwen-cli` | stdin（`cmd.Stdin = strings.NewReader`） | 不支持（Qwen3-Coder 无 effort 概念） | 同上 |

判定：CLI driver 内联捕获**完全属于 driver 层、非 MITM**——不起外部进程，不 hook 网络栈，不解密 TLS（CLI 协议是 stdin/stdout，无 TLS）。捕获点严格落在 driver 自身的 `Call`/`Stream` 函数体内。

### CLI Stream 路径时序铁律

各 CLI driver 的 Stream goroutine 已有 `defer close(ch)`、`defer cancel()`、`defer cmd.Wait()` 等清理。**`sink.set` 不能塞进 defer**（LIFO 顺序易错、`cmd.Wait` 之前的 ProcessState 还未取到 exit code）。正确做法：

1. **在 goroutine 函数体末尾**（scanner 循环之后、`close(ch)` 触发之前）；
2. **显式 `_ = cmd.Wait()`** 取 exit code；
3. **`fillCLIResponse(rawCap, rawStdoutBuf.String(), stderrBuf.String(), processExitCode(cmd))`** 填响应；
4. **`sink.set(rawCap)`** 发布；
5. 让 `defer close(ch)` 自然触发（LLMFile.range ch 退出时 sink 已为终态）。

claude / qwen 在 Stream 中有两条退出路径（`result` 事件 + scanner 退出无 result），两条都需 `fillCLIResponse + sink.set` 才不漏 capture。codex / cursor 单分支退出。

## 时序铁律（streaming SSE）

streaming 路径下捕获原始 SSE 字节流要点：

1. **不能 `io.ReadAll(resp.Body)`** —— 那会一次性吞掉流，SDK 读不到、阻塞流。
2. **必须 `io.TeeReader` 包 `resp.Body`** —— SDK 读 SSE 的同时 tee 进 sinkBuf。`captureResponseBody.Read` 实际就是 `tee.Read`。
3. **sink.Response 在 `resp.Body.Close()` 时才落入** —— SDK 读完 stream goroutine 退出（`defer close(ch)` 之前 `defer resp.Body.Close()`/`stream.Close()`），`captureResponseBody.Close` 把累积的 sinkBuf 字节落入 cap.Response。
4. **LLMFile 在 `range ch` 结束后归集 sink** —— happens-before 链：driver goroutine `defer close(ch)` 晚于 `defer Body.Close`，所以 LLMFile range 退出时 sink 已为终态。
5. **request body 重置** —— middleware/RoundTripper 读 `req.Body` 拿真实发送字节后，必须用 `req.GetBody()`（标准约定）或 `io.NopCloser(bytes.NewReader)` 重置，否则 SDK 实际发送拿到空 body。`readAndResetReqBody` 优先 GetBody，fallback 到重置。

## 与现有归一化层的关系

- `steps.jsonl` 的 `messages` / `raw_response` **不变**：那是 driver 解析后的归一化形态（`{role, content, tool_calls...}`）。
- `events.jsonl` 的 `Write` 事件 args 维持 `{fd, size, model, reasoning_effort}` 四字段不变。
- spawn 早期的 `ConfigResolve` 事件（携带 `provider` / `model` / `reasoning_effort` 等）自 **56.5（CAP-5）** 起落盘 `events.jsonl`——此前 EventWriter 在 spawn 末尾才 attach，早于此的 `ConfigResolve` 被丢进 nil writer（实测命中 0）；56.5 把 attach 提前到 `ConfigResolve` emit 之前，命中 0→>0。
- `raw.jsonl` 是**新增**第三类落盘文件，独立于上述两类，互不读写。

## 查询（事后取回 · 56.4 · CAP-3）

落盘后的 `raw.jsonl` 由 **三路** 取回并展示——给定一个已结束（reaped）进程的 uuid（+ 可选 step），三者取到的是**同一条**底层记录。三路共用同一个新 IPC 方法 `get_raw_capture` 作为**唯一数据后端**，天然一致。

### 单一后端：`get_raw_capture` IPC 方法

| 项 | 内容 |
|----|------|
| 方法常量 | `MethodGetRawCapture = "get_raw_capture"`（`ipc/protocol.go`） |
| Request | `GetRawCaptureRequest{ PID, UUID string, Step int }`——`Step==0` 取全部记录，`Step>0` 取该 step 单条 |
| Response | `GetRawCaptureResponse{ Records []vfs.RawCapture, ParseErrors int }`——直接复用 `vfs.RawCapture`，不另造 wire 类型 |
| UUID 解析 | `UUID==""` 时由 PID 经 `GetProcess`（live）→ `FindHistoryByPID`（reaped 历史）解析 |
| 读盘 | `resolveRawPath(uuid)` → `<baseDir>/steps/<uuid>/raw.jsonl`；`kernel.ReadAllRawWithErrors` / `kernel.ReadRawForStep` |
| 无文件 / 空 uuid | 返回 `{Records: []}`（`OK=true` 空列表，**不报错**，与 `list_events` 一致） |

### 路① `rnix strace <pid> --raw`

```bash
rnix strace <pid> --raw              # 渲染该进程全部 raw 记录（人类可读）
rnix strace <pid> --raw --step 3     # 仅 step 3 一条
rnix strace <pid> --raw --uuid <u>   # 直接按 uuid 定位已 reaped 进程（覆盖 PID 解析）
rnix strace <pid> --raw --json       # 输出原始 vfs.RawCapture JSON（NDJSON）
```

`--raw` 是 `runStrace` 顶部**早分支**，与实时 `AttachDebug` 流互斥——未传 `--raw` 时实时 strace 行为零变化。无记录时给出 `[strace] no raw captures for <target>` 友好提示，非报错退出。

### 路② dashboard inspector — Raw I/O lens（❻）

Step Inspector 新增第 6 个 lens `❻ Raw I/O`（按键 `6`），展示当前 step 的原始请求/响应。数据**懒加载**：仅当 Raw lens 激活（或在该 lens 下切 step）时经 `get_raw_capture` 按 step 拉取，缓存进 `InspectorState.RawByStep`——不进 `GetStepDetail`，以免给非 raw lens 的每次 step 拉取增重（raw body 可达 4MB）。

### 渲染收敛

strace `--raw` 与 dashboard Raw lens 共用同一个 pure helper `inspector.RenderRawLens(*vfs.RawCapture, width)`——按 `Kind`（`api` / `cli`）分支取键渲染：API 显示 method/url/headers/body，CLI 显示 argv/stdin/env + stdout/stderr/exit_code。`reasoning_effort`（API body）/ `--effort`（CLI argv）的真实值在渲染中可见，这正是 CAP-3 要兑现的核心。

### 脱敏 / 截断在读路径的语义

- **读路径零反脱敏**：`raw.jsonl` 落盘时凭据已被脱敏为 `redacted(len=..,prefix=..,sha256=..)` 指纹，三路读到什么显示什么，**绝不还原**。
- **截断可见**：超 `max_output_bytes` 的记录其 `Truncated` / `OriginalBytes` 在三路均可见（lens 顶部一行 `⚠ truncated (original N bytes)`）。
- **malformed 可见**：`ReadAllRawWithErrors` 计数 unmarshal 失败的行（`ParseErrors`），strace / lens 在 `>0` 时提示 `N line(s) skipped (malformed)`——消化此前静默 `continue` 跳过的可观察性缺口（deferred #17）。

---
title: '/dev/fs 写入和列目录支持 + WorkDir 路径提示'
type: 'feature'
created: '2026-03-20'
status: 'done'
baseline_commit: '5df41e1'
context: []
---

# /dev/fs 写入和列目录支持 + WorkDir 路径提示

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** toolProtocol 系统提示承诺 /dev/fs 支持写文件（`data={"content":"..."}`）和列目录（`data={"op":"list"}`），但 hostfs 驱动只允许 O_RDONLY，导致 LLM agent 执行这两种操作时触发 "read-only device" 错误并被 circuit breaker 终止。此外，toolProtocol 未说明路径相对于 WorkDir 解析，LLM 会在路径中包含项目名（如 `/dev/fs/echo-matrix/...`），导致 WorkDir 拼接后路径重复。

**Approach:** 在 hostfs 驱动中实现 Write→Read 命令模式（仿照 ShellDriver 模式），支持 O_WRONLY/O_RDWR 打开；在 Write 中解析 JSON payload 执行写文件或列目录操作，结果缓存供 Read 返回。同时更新 toolProtocol 注明路径相对于项目根目录。

## Boundaries & Constraints

**Always:**
- 写文件自动创建中间目录（`os.MkdirAll`）
- 写入使用 0o644 权限，目录使用 0o755 权限
- 列目录返回 JSON 数组，每项包含 name、size、is_dir 字段
- 保持 O_RDONLY 模式的现有行为不变（直接读取文件内容）
- 路径沙箱：写入路径必须在 WorkDir 内（如有 WorkDir），防止 LLM 写任意位置

**Ask First:** 是否需要对写入的文件大小设限

**Never:** 不实现删除文件/目录功能、不修改文件权限

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| 读文件 (现有) | Open(O_RDONLY, "/dev/fs/src/main.go"), Read | 文件内容 | ErrNotFound / ErrPermission |
| 写文件 | Open(O_RDWR, "/dev/fs/stories/1-1.md"), Write({"content":"..."}) | 创建文件, Read→"ok" | ErrPermission 如路径越界 |
| 写文件自动建目录 | Open(O_RDWR, "/dev/fs/new/dir/file.txt"), Write({"content":"x"}) | 自动 MkdirAll, 创建文件 | 磁盘错误→ErrDriver |
| 列目录 | Open(O_RDWR, "/dev/fs/src"), Write({"op":"list"}) | JSON: [{"name":"main.go","size":1024,"is_dir":false},...] | ErrNotFound 如目录不存在 |
| 列空目录 | Open(O_RDWR, "/dev/fs/empty"), Write({"op":"list"}) | JSON: [] | N/A |
| 路径越界 | Open(O_RDWR, "/dev/fs/../../etc/passwd"), Write({"content":"x"}) | ErrPermission "path outside sandbox" | 沙箱检查拦截 |
| 无 Write 直接 Read (O_RDWR) | Open(O_RDWR, "/dev/fs/x"), Read() | 空/错误 | ErrDriver "write first" |

</frozen-after-approval>

## Code Map

- `drivers/fs/hostfs.go` -- fs 驱动核心实现，需扩展 Write 和 Open 逻辑
- `drivers/fs/hostfs_test.go` -- 单元测试，需新增写/列目录/沙箱测试
- `kernel/kernel.go:58-93` -- toolProtocol 常量，需更新路径说明

## Tasks & Acceptance

**Execution:**
- [ ] `drivers/fs/hostfs.go` -- 扩展 HostFSFile 支持命令模式：添加 mode/response/workDir 字段；FileFactory 接受 O_WRONLY/O_RDWR 时创建命令模式文件（不立即打开 os.File）；Write 解析 JSON payload 执行 write-content 或 list 操作并缓存结果；Read 在命令模式下返回缓存结果；添加路径沙箱检查
- [ ] `kernel/kernel.go` -- 更新 toolProtocol：注明路径相对于项目工作目录，无需包含项目名
- [ ] `drivers/fs/hostfs_test.go` -- 新增测试：写文件成功、自动建目录、列目录、列空目录、路径沙箱拦截、无 Write 直接 Read 错误；更新 TestFileFactory_WriteFlag_Rejected 为 TestFileFactory_WriteFlag_Accepted

**Acceptance Criteria:**
- Given agent 用 O_RDWR 打开 /dev/fs/path 并 Write({"content":"hello"}), when Read, then 文件已创建且 Read 返回成功确认
- Given agent 用 O_RDWR 打开 /dev/fs/dir 并 Write({"op":"list"}), when Read, then 返回该目录的 JSON 文件列表
- Given WorkDir=/project/root 且 agent 打开 /dev/fs/src/main.go, when 路径解析, then 实际路径为 /project/root/src/main.go（不含项目名重复）
- Given agent 尝试写入 WorkDir 之外的路径, when Open+Write, then 返回 ErrPermission
- Given 现有 O_RDONLY 读文件测试, when 运行, then 全部通过（回归无破坏）

## Verification

**Commands:**
- `go test -race -run TestFileFactory ./drivers/fs/...` -- expected: 所有新旧测试通过
- `go test -race ./kernel/...` -- expected: kernel 测试无回归
- `make all` -- expected: lint + vet + test + build 全部通过

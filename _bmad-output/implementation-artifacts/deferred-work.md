# Deferred Work

## From tech-spec-devfs-write-list-support (2026-03-20)

1. **checkSandbox symlink 穿透** — `checkSandbox()` 使用 `filepath.Abs()` + 字符串前缀匹配，不解析符号链接。如果 workDir 内存在指向外部的 symlink，写入可能穿透沙箱。需要评估是否使用 `filepath.EvalSymlinks()` 以及性能影响。

2. **Read-mode 沙箱检查** — O_RDONLY 模式未执行沙箱验证（checkSandbox 仅在命令模式写入时调用）。LLM 理论上可通过 `/dev/fs/../../etc/passwd` 读取 workDir 外的文件。需独立评估是否对读操作也加沙箱。

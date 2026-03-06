# Rnix 系统监控工具

## 概述

`monitor.sh` 是一个用于持续监控 Rnix daemon 运行状态的脚本，每 30 秒报告一次系统状态摘要。

## 功能

- **Daemon 状态**: 检查 daemon 是否运行中
- **进程信息**: 显示 daemon 的 PID、内存使用（RSS/VSZ）
- **管理进程**: 统计当前管理的进程数量
- **系统资源**: 监控整体 CPU 和内存使用率
- **日志记录**: 所有状态报告都被记录到日志文件

## 使用方法

### 基本启动

```bash
./scripts/monitor.sh
```

### 后台运行

```bash
./scripts/monitor.sh > /dev/null 2>&1 &
```

或使用 nohup：

```bash
nohup ./scripts/monitor.sh &
```

### 查看日志

```bash
tail -f logs/rnix-monitor.log          # 实时查看
cat logs/rnix-monitor.log              # 查看完整日志
grep "Daemon" logs/rnix-monitor.log    # 过滤特定内容
```

## 输出示例

```
═══════════════════════════════════════════
[2026-03-04 15:30:45] Rnix 系统监控报告
═══════════════════════════════════════════
▸ Daemon 状态: ✓ 运行中
▸ Daemon 进程: PID=12345 | RSS=25MB | VSZ=150MB
▸ 管理进程数: 8
▸ 系统资源: CPU=12.5% | MEM=45.2%
═══════════════════════════════════════════
```

## 状态解释

| 状态 | 含义 |
|------|------|
| ✓ 运行中 | Daemon 正常运行，可响应请求 |
| ⚠ Socket 存在但无响应 | 进程可能崩溃或卡死 |
| ✗ 离线 | Daemon 未运行 |

## 配置选项

### 修改监控间隔

编辑脚本中的睡眠时间（默认 30 秒）：

```bash
sleep 30  # 改为其他值，如 sleep 60 表示 60 秒
```

### 修改日志目录

设置环境变量：

```bash
export RNIX_LOG_DIR=/var/log/rnix
./scripts/monitor.sh
```

## 停止监控

按 `Ctrl+C` 即可停止监控脚本。

## 故障排除

### 权限问题

```bash
chmod +x ./scripts/monitor.sh
```

### Socket 路径问题

如果监控无法连接到 daemon，检查 socket 路径：

```bash
# 查看实际 socket 路径
echo $XDG_RUNTIME_DIR/rnix/rnix.sock
# 或
ls -la /tmp/rnix-$(id -u)/
```

### Daemon 无法启动

```bash
# 手动启动 daemon
./bin/rnix daemon

# 在另一个终端查看状态
./scripts/monitor.sh
```

## 与其他工具集成

### 发送告警（示例）

修改脚本的 `check_daemon_status` 函数以添加告警逻辑：

```bash
if [ "$(check_daemon_status)" == "✗ 离线" ]; then
    # 发送通知或重启 daemon
    ./bin/rnix daemon &
fi
```

## 相关命令

```bash
# 查看进程列表
rnix ps

# 查看特定进程
rnix ps <pid>

# 跟踪进程执行
rnix strace <pid>

# 杀死进程
rnix kill <pid>
```

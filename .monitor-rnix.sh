#!/bin/bash
# Rnix 系统监控脚本 - 每 30 秒输出一次状态摘要

print_status() {
    echo "════════════════════════════════════════════════════════════"
    echo "📊 Rnix 系统状态摘要 - $(date '+%Y-%m-%d %H:%M:%S')"
    echo "════════════════════════════════════════════════════════════"

    # 1. Git 状态
    if git rev-parse --git-dir > /dev/null 2>&1; then
        branch=$(git rev-parse --abbrev-ref HEAD)
        changes=$(git status --short 2>/dev/null | wc -l)
        echo "📁 Git 分支: $branch"
        echo "   修改文件: $changes 个"
    fi

    # 2. 构建和测试状态
    if [ -f "Makefile" ] && [ -f "rnix" ]; then
        rnix_time=$(stat -c %y ./rnix 2>/dev/null | cut -d' ' -f1-2)
        echo "🔨 最近构建: $rnix_time"
    fi

    # 3. Daemon 状态
    daemon_sock="${XDG_RUNTIME_DIR:-/tmp/rnix-$(id -u)}/rnix/rnix.sock"
    if [ -S "$daemon_sock" ]; then
        echo "✅ Rnix Daemon: 运行中"
        pid_file="${daemon_sock%/*}/rnix.pid"
        if [ -f "$pid_file" ]; then
            pid=$(cat "$pid_file" 2>/dev/null)
            echo "   PID: $pid"
        fi
    else
        echo "⛔ Rnix Daemon: 已停止"
    fi

    # 4. 后台任务
    jobs_count=$(jobs -r 2>/dev/null | wc -l)
    echo "🔄 后台任务: $jobs_count 个"

    # 5. 磁盘空间
    disk_info=$(df -h . | awk 'NR==2 {print $5 " / " $4}')
    echo "💾 磁盘: 已用 $disk_info"

    # 6. 内存使用（如果可用）
    if command -v free &> /dev/null; then
        mem=$(free -h | awk 'NR==2 {print $3 " / " $2}')
        echo "🧠 内存: $mem"
    fi

    echo "════════════════════════════════════════════════════════════"
    echo ""
}

# 持续监控循环
echo "🚀 启动 Rnix 系统监控 (每 30 秒报告一次，按 Ctrl+C 停止)"
echo ""

while true; do
    print_status
    sleep 30
done

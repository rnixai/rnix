#!/bin/bash

# Rnix 系统状态监控脚本 - 每 30 秒报告一次状态

set -euo pipefail

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志文件
LOG_FILE="${RNIX_LOG_DIR:-./logs}/rnix-monitor.log"
mkdir -p "$(dirname "$LOG_FILE")"

# 获取 socket 路径
get_socket_path() {
  if [ -n "${XDG_RUNTIME_DIR:-}" ]; then
    echo "${XDG_RUNTIME_DIR}/rnix/rnix.sock"
  else
    echo "/tmp/rnix-$(id -u)/rnix.sock"
  fi
}

# 检查 daemon 是否运行
check_daemon_status() {
  local socket=$(get_socket_path)
  if [ -S "$socket" ]; then
    # 尝试 ping daemon
    if echo '{"method":"ping"}' | socat - UNIX-CONNECT:"$socket" 2>/dev/null | grep -q "pong" 2>/dev/null; then
      echo -e "${GREEN}✓ 运行中${NC}"
      return 0
    else
      echo -e "${YELLOW}⚠ Socket 存在但无响应${NC}"
      return 1
    fi
  else
    echo -e "${RED}✗ 离线${NC}"
    return 1
  fi
}

# 获取 daemon 进程信息
get_daemon_process_info() {
  local pid=$(pgrep -f "rnix daemon" | head -1)
  if [ -n "$pid" ]; then
    local rss=$(ps -p "$pid" -o rss= 2>/dev/null | awk '{print int($1/1024) "MB"}')
    local vsz=$(ps -p "$pid" -o vsz= 2>/dev/null | awk '{print int($1/1024) "MB"}')
    echo "PID=$pid | RSS=$rss | VSZ=$vsz"
  else
    echo "进程未找到"
  fi
}

# 获取 rnix ps 输出（进程列表）
get_process_list() {
  local count=$(rnix ps 2>/dev/null | tail -n +2 | wc -l 2>/dev/null || echo "0")
  echo "$count"
}

# 获取系统资源信息
get_system_resources() {
  local cpu=$(top -bn1 | grep "Cpu(s)" | awk '{print $2}' | cut -d'%' -f1)
  local mem=$(free | grep Mem | awk '{printf "%.1f%%", ($3/$2)*100}')
  echo "CPU=$cpu% | MEM=$mem"
}

# 生成时间戳
timestamp() {
  date '+%Y-%m-%d %H:%M:%S'
}

# 报告状态
report_status() {
  local time=$(timestamp)

  echo ""
  echo -e "${BLUE}═══════════════════════════════════════════${NC}"
  echo -e "${BLUE}[${time}] Rnix 系统监控报告${NC}"
  echo -e "${BLUE}═══════════════════════════════════════════${NC}"

  echo -e "${BLUE}▸ Daemon 状态:${NC} $(check_daemon_status)"
  echo -e "${BLUE}▸ Daemon 进程:${NC} $(get_daemon_process_info)"
  echo -e "${BLUE}▸ 管理进程数:${NC} $(get_process_list)"
  echo -e "${BLUE}▸ 系统资源:${NC} $(get_system_resources)"
  echo -e "${BLUE}═══════════════════════════════════════════${NC}"

  # 同时记录到日志
  {
    echo "[${time}] Daemon: $(check_daemon_status 2>&1 | sed 's/\\033\[[0-9;]*m//g')"
    echo "[${time}] Process: $(get_daemon_process_info)"
    echo "[${time}] Managed processes: $(get_process_list)"
    echo "[${time}] System: $(get_system_resources)"
    echo ""
  } >> "$LOG_FILE"
}

# 主监控循环
main() {
  echo -e "${YELLOW}启动 Rnix 系统监控（间隔 30 秒）...${NC}"
  echo "日志文件: $LOG_FILE"
  echo "按 Ctrl+C 停止监控"
  echo ""

  while true; do
    report_status
    sleep 30
  done
}

# 处理中断信号
trap 'echo -e "\n${YELLOW}监控已停止${NC}"; exit 0' INT TERM

# 启动监控
main

#\!/bin/bash

while true; do
  clear
  echo "=== Rnix 系统状态摘要 [$(date '+%Y-%m-%d %H:%M:%S')] ==="
  echo ""
  
  # 1. Daemon 状态
  echo "📡 Daemon 状态:"
  RNIX_SOCK="${XDG_RUNTIME_DIR:-/tmp}/rnix/rnix.sock"
  if [ -S "$RNIX_SOCK" ]; then
    echo "   ✓ Daemon 运行中"
  else
    echo "   ✗ Daemon 未运行"
  fi
  echo ""
  
  # 2. 进程列表
  echo "⚙️  活跃进程:"
  if command -v rnix &> /dev/null; then
    PROC_COUNT=$(timeout 2 rnix list-procs 2>/dev/null | grep -c "\"pid\"" || echo "0")
    echo "   进程总数: $PROC_COUNT"
  else
    echo "   (rnix 命令不可用)"
  fi
  echo ""
  
  # 3. 磁盘使用
  echo "💾 磁盘使用:"
  df -h . | tail -1 | awk '{print "   " $1 ": " $3 "/" $2 " (" $5 ")"}'
  echo ""
  
  # 4. 内存
  echo "🧠 内存使用:"
  free -h | grep "^Mem:" | awk '{print "   总计: " $2 ", 已用: " $3 ", 可用: " $7}'
  echo ""
  
  # 5. CPU 负载
  echo "⚡ CPU 负载:"
  uptime | awk -F'load average:' '{print "   " $2}'
  echo ""
  
  echo "⏱️  下次更新: 30秒后"
  echo "========================================="
  
  sleep 30
done

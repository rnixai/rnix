#!/usr/bin/env bash
set -euo pipefail

# Isolated-daemon Tier1 runner — `make agtest` (Story 68.3 AC1 / Dev Notes
# 裁决 1).
#
# Three environment variables redirect every daemon-touching path (socket /
# session data / global config) into throwaway mktemp directories, so this
# NEVER touches — or is visible to — the user's ambient `rnix daemon`:
#
#   XDG_RUNTIME_DIR  -> ipc.SocketPath()      (ipc/protocol.go)
#   RNIX_DATA_DIR    -> config.DataDir()      (internal/config/paths.go)
#   XDG_CONFIG_HOME  -> config.GlobalDir()    (internal/config/paths.go)
#
# `rnix daemon start` execs a child process (ipc.EnsureDaemon ->
# exec.Command(os.Args[0], "daemon", "--internal")) that inherits this
# process's environment, so all three variables apply to the daemon's entire
# lifetime without any Go code changes.
#
# Ordering matters: providers.yaml must exist under the isolated
# XDG_CONFIG_HOME BEFORE the daemon starts. The daemon reads global config
# exactly once at startup (cmd/rnix/main.go) and falls back to
# DefaultProvidersConfig — which has no "replay" driver — if the file is
# missing. Starting the daemon first would make every Tier1 case fail with
# "device not found: /dev/llm/replay".

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

RNIX_RUN_XDG_RUNTIME_DIR="$(mktemp -d)"
RNIX_RUN_DATA_DIR="$(mktemp -d)"
RNIX_RUN_XDG_CONFIG_HOME="$(mktemp -d)"

export XDG_RUNTIME_DIR="$RNIX_RUN_XDG_RUNTIME_DIR"
export RNIX_DATA_DIR="$RNIX_RUN_DATA_DIR"
export XDG_CONFIG_HOME="$RNIX_RUN_XDG_CONFIG_HOME"

# Runs on every exit path (success, `set -e` abort, or a later `exit N`) so
# the isolated daemon and its temp directories are cleaned up whether the
# suite passed or failed. `daemon stop` shares this same env (关键防错 #1) —
# never omit the export lines above before this trap could fire, or `stop`
# would target the user's real daemon instead of the isolated one.
cleanup() {
  local status=$?
  ./rnix daemon stop >/dev/null 2>&1 || true
  # `daemon stop` (client.Shutdown) 只发请求即返回，daemon 随后还有
  # k.Shutdown / immune 落盘 / recall index 保存要写数据目录。socket 文件
  # 是 daemon 退出序列最后移除的（cmd/rnix/main.go），等它消失再 rm -rf，
  # 否则撞上 "Directory not empty"（CI 实测竞态）。有界等待防挂死。
  local sock="$RNIX_RUN_XDG_RUNTIME_DIR/rnix/rnix.sock"
  local tries=0
  while [ -e "$sock" ] && [ "$tries" -lt 100 ]; do
    sleep 0.1
    tries=$((tries + 1))
  done
  rm -rf "$RNIX_RUN_XDG_RUNTIME_DIR" "$RNIX_RUN_DATA_DIR" "$RNIX_RUN_XDG_CONFIG_HOME"
  exit "$status"
}
trap cleanup EXIT

mkdir -p "$RNIX_RUN_XDG_CONFIG_HOME/rnix"
cp tests/agtest/providers.example.yaml "$RNIX_RUN_XDG_CONFIG_HOME/rnix/providers.yaml"

echo "[run-tier1] isolated daemon: XDG_RUNTIME_DIR=$XDG_RUNTIME_DIR RNIX_DATA_DIR=$RNIX_DATA_DIR XDG_CONFIG_HOME=$XDG_CONFIG_HOME"

# All rnix invocations below use the freshly built ./rnix (this script only
# runs via the `agtest: build` Makefile target) rather than whatever `rnix`
# happens to be on PATH — 关键防错 #3, immune to
# [[rnix-session-data-daemon-version]].
./rnix daemon start

# --tier1 enforces the same agtest.ValidateTier1 discipline the repository
# guard test (agtest/tier1_guard_test.go) already checks at `make test` time
# — belt-and-suspenders against a case that slipped past review (Dev Notes
# 裁决 2).
./rnix agtest tests/agtest/tier1/ --tier1

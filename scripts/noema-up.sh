#!/usr/bin/env bash
# 一键启动：就是 README 里的 `go run ./cmd/noema -listen`。
# 不另起一套进程模型。默认 loopback。BIND_KEY 必填。HOME 与 STATE_DIR 分开。
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

listen="${NOEMA_LISTEN:-127.0.0.1:8787}"
home_dir="${NOEMA_HOME:-$root/data/lucky-home}"
state_dir="${NOEMA_STATE_DIR:-$root/data/noema-state}"
mode="${NOEMA_MODE:-live}"

case "$home_dir" in
  # 子串匹配。路径里碰巧含 .luckyagent 也会拒绝，避免指到用户自己的 Lucky 家目录。
  *".luckyagent"*)
    echo "noema-up: NOEMA_HOME must not be ~/.luckyagent ($home_dir)" >&2
    exit 1
    ;;
esac

if [[ -z "${NOEMA_BIND_KEY:-}" ]]; then
  echo "noema-up: NOEMA_BIND_KEY is required. /v1/* returns 401 without it, including loopback." >&2
  exit 1
fi

case "$mode" in
  live)
    if [[ -z "${NOEMA_API_KEY:-}" ]]; then
      echo "noema-up: NOEMA_API_KEY is empty, so a live turn cannot reach a provider." >&2
      echo "noema-up: export NOEMA_API_KEY, or re-run with NOEMA_MODE=probe for healthz/capabilities only." >&2
      exit 1
    fi
    ;;
  probe)
    ;;
  *)
    echo "noema-up: NOEMA_MODE must be live or probe (got $mode)" >&2
    exit 1
    ;;
esac

export NOEMA_HOME="$home_dir"
export NOEMA_STATE_DIR="$state_dir"
mkdir -p "$NOEMA_HOME" "$NOEMA_STATE_DIR"

if [[ "$mode" == "probe" ]]; then
  echo "noema-up: probe mode. This process serves /healthz and /v1/capabilities. It does not prove a model turn." >&2
fi

exec env GOWORK=off go run ./cmd/noema -listen "$listen" -home "$NOEMA_HOME" -state-dir "$NOEMA_STATE_DIR"

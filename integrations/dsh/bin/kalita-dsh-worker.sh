#!/usr/bin/env bash
# kalita-dsh-worker.sh — supervisor loop: one DSH headless run per task.
#
# Prereqs: `dsh` on PATH (`npx @deepseek-ai/dsh`), a kalita node running, and either
#   KALITA_TOKEN (agent bearer token), or
#   KALITA_BOOTSTRAP_SECRET + KALITA_WORKER_ID + KALITA_WORKER_ROLE (self-register).
# Optional env: KALITA_URL (default http://127.0.0.1:8080), KALITA_ROLE (prompt label).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OVERLAY="$ROOT/cordis.yml"
NODE="${KALITA_URL:-http://127.0.0.1:8080}"

if [[ -z "${KALITA_TOKEN:-}" ]]; then
  if [[ -z "${KALITA_BOOTSTRAP_SECRET:-}" ]]; then
    echo "set KALITA_TOKEN or KALITA_BOOTSTRAP_SECRET" >&2
    exit 1
  fi
  RESP="$(curl -fsS -X POST "$NODE/api/bootstrap" \
    -H 'Content-Type: application/json' \
    -d "{\"secret\":\"$KALITA_BOOTSTRAP_SECRET\",\"id\":\"$KALITA_WORKER_ID\",\"role\":\"$KALITA_WORKER_ROLE\"}")"
  export KALITA_TOKEN="$(printf '%s' "$RESP" | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')"
  echo "bootstrapped agent $KALITA_WORKER_ID as $KALITA_WORKER_ROLE"
fi

ROLE="${KALITA_ROLE:-${KALITA_WORKER_ROLE:-agent}}"
TASK="You are a kalita worker (role: $ROLE). Load the kalita-worker skill and follow it. Poll your kalita task pool ONCE with mcp__kalita__wait_for_task. If a task arrives, take it, do the work, report and complete or fail it. If the poll times out with no task, reply with just 'no task'. Touch nothing outside mcp__kalita__* tools."

while true; do
  dsh --profile headless --patch "$OVERLAY" "$TASK" || echo "dsh exited $?" >&2
  sleep 2
done

#!/usr/bin/env bash
# Federation demo (LLD-07 §6): two kalita nodes on one machine + a bridge worker
# between them. Scenario: a lead captured on the SITE node (packs/site) appears,
# exactly once, as a mirrored lead on the CRM node — carrying its origin.
#
#   site node A  --(read_journal: event.emitted site.LeadCaptured)-->  bridge
#   bridge       --(emit_event: LeadMirrored, origin={A, lead-id})  -->  crm node B
#   node B       --(on event LeadMirrored: create MirroredLead)     -->  mirror row
#
# No sync-RPC between the nodes (HLD §8a): A is pulled, B is written; A never
# waits for B. Both nodes run in-memory (dev). For durable schemas set
# KALITA_PG_DSN and give each node its own KALITA_PG_SCHEMA (kalita_a/kalita_b).
#
# Usage:  bash deploy/federation-demo/run.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

KALITA="$ROOT/kalita.exe"; [ -x "$KALITA" ] || KALITA="$ROOT/kalita"
PY="$(command -v py || command -v python3 || command -v python)"
CURL="$(command -v curl)"

SECRET="fed-demo-secret"
A_URL="http://127.0.0.1:8101"
B_URL="http://127.0.0.1:8102"
WORK="$ROOT/tmp/federation-demo"
rm -rf "$WORK"; mkdir -p "$WORK/a" "$WORK/b"
CURSOR="$WORK/bridge_cursor.json"

PIDS=()
cleanup() {
  for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
}
trap cleanup EXIT

echo "== building node binary =="
go build -o "$KALITA" ./cmd/kalita

start_node() {  # $1=port $2=datadir
  KALITA_BOOTSTRAP_SECRET="$SECRET" \
  KALITA_BOOTSTRAP_ROLES="Operator,BridgeReader,BridgeEmitter" \
    "$KALITA" serve --pack packs/site --listen "127.0.0.1:$1" --data-dir "$2" &
  PIDS+=($!)
}

wait_ready() {  # $1=url
  for _ in $(seq 1 50); do
    if "$CURL" -fsS "$1/readyz" >/dev/null 2>&1; then return 0; fi
    sleep 0.2
  done
  echo "node $1 never became ready"; exit 1
}

echo "== starting site node A ($A_URL) and crm node B ($B_URL) =="
start_node 8101 "$WORK/a"
start_node 8102 "$WORK/b"
wait_ready "$A_URL"
wait_ready "$B_URL"

bootstrap() {  # $1=url $2=id $3=role  -> prints token
  "$CURL" -fsS -X POST "$1/api/bootstrap" -H 'Content-Type: application/json' \
    -d "{\"secret\":\"$SECRET\",\"id\":\"$2\",\"role\":\"$3\"}" \
    | "$PY" -c 'import sys,json;print(json.load(sys.stdin)["token"])'
}

echo "== bootstrapping least-privilege actors =="
OP_A="$(bootstrap "$A_URL" op-a Operator)"          # files + captures leads on A
OP_B="$(bootstrap "$B_URL" op-b Operator)"          # reads mirrors on B (verify)
READER_A="$(bootstrap "$A_URL" bridge-reader BridgeReader)"   # bridge source identity
EMITTER_B="$(bootstrap "$B_URL" bridge-emitter BridgeEmitter)" # bridge target identity

echo "== node A: capture a lead =="
LEAD_ID="$("$CURL" -fsS -X POST "$A_URL/api/records/Lead" -H "Authorization: Bearer $OP_A" \
  -H 'Content-Type: application/json' \
  -d '{"values":{"name":"Ada Lovelace","company":"Analytical Engines","email":"ada@ae.io"},"basis":{"type":"human","id":"op-a"},"idempotency_key":"demo-lead-0001"}' \
  | "$PY" -c 'import sys,json;print(json.load(sys.stdin)["id"])')"
echo "   lead $LEAD_ID created on A"
"$CURL" -fsS -X POST "$A_URL/api/records/Lead/$LEAD_ID/act" -H "Authorization: Bearer $OP_A" \
  -H 'Content-Type: application/json' \
  -d '{"action":"capture","basis":{"type":"human","id":"op-a"},"idempotency_key":"demo-capture-0001"}' >/dev/null
echo "   lead captured -> event.emitted site.LeadCaptured"

count_mirrors() {  # -> integer
  "$CURL" -fsS "$B_URL/api/records/MirroredLead" -H "Authorization: Bearer $OP_B" \
    | "$PY" -c 'import sys,json;print(len(json.load(sys.stdin).get("records",[])))'
}
show_mirror() {
  "$CURL" -fsS "$B_URL/api/records/MirroredLead" -H "Authorization: Bearer $OP_B" \
    | "$PY" -c 'import sys,json
rs=json.load(sys.stdin).get("records",[])
for r in rs:
    v=r.get("values",r)
    print("   mirror:",v.get("name"),"| origin_node=",v.get("origin_node"),"origin_id=",v.get("origin_id"))'
}

run_bridge() {  # $1=cursor-note
  SOURCE_URL="$A_URL" SOURCE_TOKEN="$READER_A" \
  TARGET_URL="$B_URL" TARGET_TOKEN="$EMITTER_B" \
  SOURCE_NODE_ID="site-node-a" \
  KALITA_BRIDGE_CONFIG="$ROOT/deploy/federation-demo/bridge-config.json" \
  KALITA_BRIDGE_CURSOR="$CURSOR" \
    "$PY" "$ROOT/workers/bridge/bridge.py" --once
}

echo "== running the bridge (pass 1) =="
run_bridge
N1="$(count_mirrors)"; show_mirror
echo "   node B now has $N1 mirrored lead(s)"

echo "== exactly-once: drop the cursor and replay from seq=0 (pass 2) =="
rm -f "$CURSOR"
run_bridge
N2="$(count_mirrors)"
echo "   node B still has $N2 mirrored lead(s) after full replay"

echo
if [ "$N1" = "1" ] && [ "$N2" = "1" ]; then
  echo "DEMO OK: lead mirrored exactly once (origin stamped), replay did not duplicate."
else
  echo "DEMO FAILED: expected 1 mirror after each pass, got pass1=$N1 pass2=$N2"; exit 1
fi

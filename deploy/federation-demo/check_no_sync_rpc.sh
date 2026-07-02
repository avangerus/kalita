#!/usr/bin/env bash
# LLD-07 §7 test 5 (TestNoSyncRPC), architectural: the constitution (HLD §8a)
# forbids any sync-RPC between nodes. A node must never address another node —
# all cross-node flow goes through the bridge WORKER, which pulls the source
# journal and writes the target via emit_event. This greps the core (internal/)
# for any outbound HTTP client or remote-node addressing and FAILS if it finds
# one. It is deliberately not pytest: it guards Go source, and lives with the
# demo so the whole federation story checks out from one place.
#
# Exit 0 = clean (no sync-RPC in core). Exit 1 = a violation was found.
set -u

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT" || exit 2

# Outbound HTTP from the core would be the smoking gun of a node calling another
# node synchronously. The connector/bridge/search PROXY code lives in workers/
# and internal/api/ragproxy + callproxy (a governed, same-node capability proxy,
# not a node->node call) — those are out of scope for this invariant.
PATTERN='http\.Get|http\.Post|http\.Client|http\.NewRequest|urllib|net/http".*Client|DialContext.*node'

# scan internal/, excluding tests and the intentional same-node capability proxy
# (ragproxy/callproxy proxy to a WORKER on localhost, never to another kalita node).
HITS="$(grep -REn "$PATTERN" internal/ \
  --include='*.go' \
  | grep -v '_test.go' \
  | grep -viE 'internal/api/(ragproxy|callproxy)\.go' \
  || true)"

if [ -n "$HITS" ]; then
  echo "FAIL: core (internal/) contains outbound HTTP / remote-node addressing:"
  echo "$HITS"
  echo
  echo "The constitution (HLD §8a) forbids sync-RPC between nodes. Cross-node"
  echo "flow must go through the bridge worker (workers/bridge), not the core."
  exit 1
fi

echo "OK: internal/ contains no node->node sync-RPC (all cross-node flow is via the bridge worker)."
exit 0

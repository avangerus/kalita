#!/usr/bin/env python3
"""«Аудит НСИ» demo driver — datapulse pack (ENH-01..04 tied together).

Runs the REAL datapulse pipeline end to end against a live kalita node and
prints a plain-text audit report:

    nsi_directory.csv
        -> [ConnectorAgent]  Source + N MasterRecord   (workers/connectors/excel_ingest.py)
        -> [MatchAgent]      duplicate MatchGroups      (workers/connectors/exact_match.py)
        -> [Steward]         the review queue a human would open

This file does NOT reimplement either worker's logic — it imports
excel_ingest.ingest() and exact_match.run() directly and calls them, exactly
as workers/agent_runner or a cron entrypoint would. Its own MCP traffic is
limited to plain reads (`get_record`) under the Steward token, mirroring how
a human Steward would open the board after the two agents have run — it never
creates or transitions a StewardshipTask, and never writes MasterRecord.

Governance boundary this script keeps (see packs/datapulse/security.dsl):
  - ConnectorAgent's own token/grants create Source + MasterRecord only.
  - MatchAgent's own token/grants create MatchGroup only, and only reads
    MasterRecord/MatchGroup.
  - This driver's OWN mcp_call() traffic runs under the Steward token and
    issues `get_record` ONLY — no create_record/update_record/act call is
    ever made under Steward here. Steward's `create [StewardshipTask]` /
    `act [start_review, approve, reject, close]` grants exist on the node
    but are deliberately never exercised by this script (see README + the
    task's own guardrails: proposing/deciding is a human's job, not this
    driver's).

Idempotency / safety on a live shared node: excel_ingest.ingest() derives its
Source/MasterRecord idempotency keys from the file PATH alone (uuid5, no
randomness) and exact_match.run() derives its MatchGroup keys from the sorted
member-id set + key-columns config — both already dedup at the node's
Append layer. Re-running this script against the same file/node is therefore
safe: it converges to the SAME Source, MasterRecords and MatchGroups every
time, never a duplicate set (see workers/connectors/test_excel_ingest.py's
and test_exact_match.py's own idempotency tests for the proof of that
contract; this driver merely relies on it, per the task's instruction not to
reinvent state).

Usage:
  python run_demo.py
  python run_demo.py --file nsi_directory.xlsx   # exercise the xlsx shape instead
"""
import argparse
import json
import os
import sys
import urllib.error
import urllib.request

_HERE = os.path.dirname(os.path.abspath(__file__))
_REPO_ROOT = os.path.abspath(os.path.join(_HERE, "..", "..", ".."))
_CONNECTORS_DIR = os.path.join(_REPO_ROOT, "workers", "connectors")

# The live node this demo drives — must be set BEFORE excel_ingest/exact_match
# are imported, since each captures NODE = os.environ.get("KALITA_URL", ...)
# as a module-level constant at its own import time.
os.environ.setdefault("KALITA_URL", "http://127.0.0.1:8090")
NODE = os.environ["KALITA_URL"]

# Bearer tokens for the three identities this demo drives, read from the
# environment ONLY -- deliberately never hardcoded. A `--demo` node mints
# FRESH tokens on every start (printed to its startup log, worthless once the
# in-memory process exits), so a literal here would be both a dead value and,
# in a public repo whose whole pitch is the secrets-boundary (I-4), a bad
# habit a secret-scanner would rightly flag. Copy the three from the node log:
#
#   kalita serve --pack packs/datapulse --demo --listen 127.0.0.1:8090
#   export DATAPULSE_CONNECTOR_TOKEN=<demo-agent-ConnectorAgent token>
#   export DATAPULSE_MATCH_TOKEN=<demo-agent-MatchAgent token>
#   export DATAPULSE_STEWARD_TOKEN=<demo-Steward token>
#   python run_demo.py
def _require_token(env_name, identity):
    val = os.environ.get(env_name)
    if not val:
        sys.exit(
            f"[run_demo] missing ${env_name} — the bearer token for the "
            f"{identity} identity.\n"
            f"  Start the demo node and export the tokens it prints:\n"
            f"    kalita serve --pack packs/datapulse --demo --listen 127.0.0.1:8090\n"
            f"    export DATAPULSE_CONNECTOR_TOKEN=<demo-agent-ConnectorAgent>\n"
            f"    export DATAPULSE_MATCH_TOKEN=<demo-agent-MatchAgent>\n"
            f"    export DATAPULSE_STEWARD_TOKEN=<demo-Steward>")
    return val


CONNECTOR_TOKEN = _require_token("DATAPULSE_CONNECTOR_TOKEN", "ConnectorAgent")
MATCH_TOKEN = _require_token("DATAPULSE_MATCH_TOKEN", "MatchAgent")
STEWARD_TOKEN = _require_token("DATAPULSE_STEWARD_TOKEN", "Steward")

KEY_COLUMNS = ["name", "inn"]
PRIMARY_COLUMN = "name"
SOURCE_NAME = "Аудит НСИ — справочник контрагентов (demo)"

sys.path.insert(0, _CONNECTORS_DIR)

# excel_ingest and exact_match each read KALITA_TOKEN ONCE at import time into
# their own module-level TOKEN constant, and each uses ONLY that constant in
# every mcp() call they make thereafter -- so importing them under different
# KALITA_TOKEN values in sequence gives each module its own fixed identity for
# the rest of the process, with no cross-talk between the two.
os.environ["KALITA_TOKEN"] = CONNECTOR_TOKEN
import excel_ingest as ei  # noqa: E402

os.environ["KALITA_TOKEN"] = MATCH_TOKEN
import exact_match as em  # noqa: E402


class DemoError(Exception):
    """A denial or transport failure surfaced by this driver itself (as
    opposed to IngestError/MatchError, which the connectors raise for their
    own governed calls) -- e.g. a Steward-token get_record coming back
    denied. Reported, never worked around by escalating to another token."""


def steward_get(entity, record_id):
    """A single `get_record` call under the STEWARD token -- the ONLY MCP
    traffic this driver emits directly (see module docstring). Raises
    DemoError on any denial/transport failure rather than silently
    continuing or retrying under a different identity."""
    body = json.dumps({
        "jsonrpc": "2.0", "id": 1, "method": "tools/call",
        "params": {"name": "get_record", "arguments": {"entity": entity, "id": record_id}},
    }).encode()
    req = urllib.request.Request(NODE + "/mcp", data=body, headers={
        "Authorization": "Bearer " + STEWARD_TOKEN, "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            out = json.load(resp)
    except urllib.error.URLError as ex:
        raise DemoError(f"Steward get_record({entity}, {record_id}) transport failure: {ex}") from ex
    res = out.get("result", {})
    content = res.get("content", [])
    payload = json.loads(content[0]["text"]) if content else {}
    if res.get("isError"):
        raise DemoError(f"Steward get_record({entity}, {record_id}) denied/rejected: {payload}")
    return payload


def hr(title):
    print()
    print("=" * 78)
    print(title)
    print("=" * 78)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--file", default="nsi_directory.csv",
                     help="dirty directory file to ingest, relative to this "
                          "script's directory (default: nsi_directory.csv)")
    args = ap.parse_args()
    data_path = args.file if os.path.isabs(args.file) else os.path.join(_HERE, args.file)

    print("АУДИТ НСИ -- демо-прогон datapulse (ConnectorAgent -> MatchAgent -> Steward)")
    print(f"node: {NODE}")
    print(f"file: {data_path}")

    # -----------------------------------------------------------------
    # Stage 1 -- ConnectorAgent: ingest the file into Source + MasterRecords
    # -----------------------------------------------------------------
    hr("[1/3] ConnectorAgent: ингест файла (excel_ingest.ingest)")
    try:
        ingest_result = ei.ingest(data_path, SOURCE_NAME, primary_column=PRIMARY_COLUMN)
    except ei.IngestError as ex:
        sys.exit(f"[run_demo] FINDING -- ConnectorAgent ingest denied/failed: {ex}")

    print(f"  Source id:      {ingest_result['source_id']}")
    print(f"  ingest status:  {ingest_result['status']}")
    print(f"  record_count:   {ingest_result['record_count']}")

    # Steward independently confirms the Source under its OWN read grant --
    # proves the governance seam (ConnectorAgent wrote it, Steward reads it
    # through a completely different token/role), not a re-use of ei's call.
    try:
        src = steward_get("Source", ingest_result["source_id"])
    except DemoError as ex:
        sys.exit(f"[run_demo] FINDING -- {ex}")
    print(f"  [Steward read-back] kind={src['values']['kind']} "
          f"ingest_status={src['values']['ingest_status']} "
          f"record_count={src['values']['record_count']}")

    # -----------------------------------------------------------------
    # Stage 2 -- MatchAgent: level-1 exact-key dedup, scoped to this Source
    # -----------------------------------------------------------------
    hr("[2/3] MatchAgent: точное сопоставление уровня 1 (exact_match.run)")
    print(f"  key columns: {', '.join(KEY_COLUMNS)}")
    try:
        match_result = em.run(KEY_COLUMNS, source_id=ingest_result["source_id"])
    except em.MatchError as ex:
        sys.exit(f"[run_demo] FINDING -- MatchAgent match denied/failed: {ex}")

    print(f"  records scanned:  {match_result['records_scanned']}")
    print(f"  groups proposed:  {match_result['groups_created']} (this run's create_record attempts)")
    print(f"  groups linked to this Source (incl. already-existing from a prior run): "
          f"{len(match_result['group_ids'])}")

    # -----------------------------------------------------------------
    # Stage 3 -- Steward queue: what a human would open to review
    # -----------------------------------------------------------------
    hr("[3/3] Steward: очередь на проверку (duplicate clusters)")
    grouped_ids = set()
    if not match_result["group_ids"]:
        print("  (no MatchGroup to review)")
    for i, gid in enumerate(match_result["group_ids"], start=1):
        try:
            group = steward_get("MatchGroup", gid)
        except DemoError as ex:
            sys.exit(f"[run_demo] FINDING -- {ex}")
        gv = group["values"]
        member_ids = gv["records"]
        grouped_ids.update(member_ids)
        print(f"\n  MatchGroup {i}  id={gid}  status={gv['status']}  score={gv['score']}  "
              f"members={len(member_ids)}")
        for mid in member_ids:
            try:
                mr = steward_get("MasterRecord", mid)
            except DemoError as ex:
                sys.exit(f"[run_demo] FINDING -- {ex}")
            raw = mr["values"].get("raw", {})
            print(f"      - {mid}  name={raw.get('name')!r}  inn={raw.get('inn')!r}  "
                  f"city={raw.get('city')!r}")

    singleton_count = ingest_result["record_count"] - len(grouped_ids)
    hr("Итог")
    print(f"  {ingest_result['record_count']} MasterRecord(s) ingested from 1 Source")
    print(f"  -> {match_result['groups_created']} duplicate MatchGroup(s) proposed by MatchAgent "
          f"({len(grouped_ids)} record(s) flagged for review)")
    print(f"  -> {singleton_count} record(s) have no exact-match duplicate under level-1 "
          f"(genuine singles, plus near-duplicates the matcher correctly left apart -- see README.md)")
    print(f"  -> Steward queue: {len(match_result['group_ids'])} MatchGroup(s) awaiting human triage "
          f"(start_review/approve/reject/close -- not exercised by this driver)")


if __name__ == "__main__":
    main()

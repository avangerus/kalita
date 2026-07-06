#!/usr/bin/env python3
"""Exact-duplicate matcher — datapulse ENH-04, Sprint 1, level-1 ONLY.

Same spine as workers/connectors/excel_ingest.py (the sibling this file
mirrors): `from bootstrap import get_token` for identity, `mcp(name,
arguments)` for MCP tools/call, stdlib-only, a BATCH job (run once per
invocation, not a server). KEY DIFFERENCE: excel_ingest.py WRITES
MasterRecord candidates; this file only READS them (via the `query` MCP
tool) and PROPOSES MatchGroup records — it never creates or touches a
MasterRecord.

Model (packs/datapulse/entities.dsl — FIXED, do not edit):
  MasterRecord: source ref, raw:json (the whole row, header-keyed), display_
                name, status enum default=Draft.
  MatchGroup:   records array[ref[MasterRecord]] (the group's members),
                score float, status enum[New,ToReview,Resolved,Dismissed]
                default=New. This worker sets ONLY records + score; status
                is left to the model default (New) — deciding New vs
                ToReview vs anything else is a Steward/workflow call
                (workflows.dsl), never this worker's.

Identity: bootstrap as MatchAgent (KALITA_BOOTSTRAP_SECRET +
KALITA_WORKER_ID + KALITA_WORKER_ROLE=MatchAgent) or pass KALITA_TOKEN
directly. Grants (security.dsl): read [MasterRecord, MatchGroup]; create
[MatchGroup]; denied update MasterRecord, delete *; holds NO grant at all
on StewardshipTask (not even a deny — the compiler rejects a deny with
nothing underneath, per security.dsl's own comment — so absence of any
grant already default-denies it). Every mcp() call this file emits stays
inside that shape (see test_exact_match.py's governance test): on any
node-side denial this worker surfaces the error and stops; it never
retries under another identity or escalates.

LEVEL-1 IS EXACT ONLY (M1). No fuzzy/edit-distance/phonetic matching lives
here or ever should — that is explicitly out of scope for this file. The
ONLY latitude this worker takes is normalization, and it is deliberately
conservative:
  - trim leading/trailing whitespace
  - collapse internal whitespace runs to one space
  - case-fold
  - stringify non-string values with plain str() before the above
Deliberately NOT done (all of this is a semantic judgement call, not a
formatting one, and belongs to a later fuzzy-match milestone if ever): no
accent/punctuation stripping, no locale-aware collation, no type coercion
(int 5 and float 5.0 stringify to "5" and "5.0" — different keys, on
purpose), no synonym/abbreviation expansion.

Key scheme: a record's exact-match key is the tuple of normalized values of
a CONFIGURED set of `raw` column names (--key-columns), in the order given
— config, not hardcoded, mirroring excel_ingest.py's --primary-column /
field-mapping precedent. A record whose `raw` is missing ANY configured key
column ENTIRELY (as opposed to holding an empty string for it) is excluded
from grouping altogether — it cannot be evaluated on the full configured
key, and letting two such records join a group purely because they share
the same *absence* would be a semantic judgement call this worker must not
make. A present-but-empty value is a real (if boring) value and normalizes
like any other.

Idempotency (LLD-01 §5 discipline, mirroring excel_ingest.py's idem_key):
CREATE calls get a deterministic key so a duplicate attempt dedups at the
node's Append layer (internal/eventstore) to the group already proposed —
no randomness. The key is a pure function of the SORTED set of member
record ids (order-independent — re-querying in a different row order still
re-derives the identical key) plus the --key-columns config (so a
differently configured run that happens to produce the same id-set is not
silently folded into this one's dedup). This worker is CREATE-ONLY: unlike
excel_ingest.py's Source.ingest_status, there is no status field this
worker ever writes after the fact — a MatchGroup is proposed once and its
whole subsequent lifecycle (New -> ToReview -> Resolved/Dismissed) moves
only through a Steward's `act()` calls, never through a MatchAgent
update_record — so the "fresh key for updates" question excel_ingest.py's
docstring reasons through does not arise here: there are no updates.

Scope of the read: by default this worker reads every MasterRecord its
MatchAgent identity can see (read [MasterRecord] is unscoped in
security.dsl) — cross-source dedup is the deliberate default, since
catching the SAME entity ingested twice from two different Source files is
exactly the point of MDM-lite matching, not merely within one file. Pass
--source to bound a run to one ingest batch instead.

Usage:
  python exact_match.py --key-columns name,email
  python exact_match.py --key-columns name,email --source <source-record-id>
"""
import argparse
import json
import os
import re
import sys
import urllib.request
import uuid

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from bootstrap import get_token  # noqa: E402

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
TOKEN = get_token()

# autonomous-match provenance: no task/human decision drives this batch run,
# so the basis is the standing automation rule itself — same provenance kind
# excel_ingest.py's RULE_BASIS uses, not a task.
RULE_BASIS = {"type": "rule", "id": "datapulse-match/exact_match"}

DEFAULT_PAGE_SIZE = 500  # query() paging — see fetch_master_records()

_WS_RE = re.compile(r"\s+")


class MatchError(Exception):
    """A governed rejection or transport failure. Fail-closed: raised and
    surfaced to the caller, never retried under another identity."""


def mcp(name, arguments):
    """Call one MCP tool; return (payload, is_error). Mirrors excel_ingest.py's mcp()."""
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                       "params": {"name": name, "arguments": arguments}}).encode()
    req = urllib.request.Request(NODE + "/mcp", data=body, headers={
        "Authorization": "Bearer " + TOKEN, "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=60) as resp:
        out = json.load(resp)
    res = out.get("result", {})
    content = res.get("content", [])
    payload = json.loads(content[0]["text"]) if content else {}
    return payload, bool(res.get("isError"))


# ---------------------------------------------------------------------------
# reading candidates
# ---------------------------------------------------------------------------

def fetch_master_records(source_id=None, page_size=DEFAULT_PAGE_SIZE):
    """Read MasterRecord candidates via the `query` MCP tool, paged with
    limit/offset until a short page proves there is no more. See module
    docstring for the cross-source-by-default scoping rationale."""
    out = []
    offset = 0
    while True:
        args = {"entity": "MasterRecord", "limit": page_size, "offset": offset}
        if source_id:
            args["filter"] = {"source": source_id}
        payload, is_err = mcp("query", args)
        if is_err:
            raise MatchError(f"query MasterRecord denied/rejected (offset={offset}): {payload}")
        page = payload.get("records", [])
        out.extend(page)
        if len(page) < page_size:
            break
        offset += page_size
    return out


# ---------------------------------------------------------------------------
# exact key + grouping
# ---------------------------------------------------------------------------

def normalize_value(value):
    """Conservative, non-semantic normalization of one raw cell value —
    see module docstring for exactly what is and is not done."""
    s = "" if value is None else str(value)
    s = _WS_RE.sub(" ", s.strip())
    return s.casefold()


def record_key(raw, key_columns):
    """The exact-match key for one MasterRecord.raw, or None if `raw` is
    missing any configured key column entirely (see module docstring)."""
    if not all(col in raw for col in key_columns):
        return None
    return tuple(normalize_value(raw[col]) for col in key_columns)


def group_by_exact_key(records, key_columns):
    """Group records sharing an identical exact key. Returns a list of
    lists of record ids — one list per key with >= 2 members. A key with
    exactly one member is a singleton, NOT a duplicate, and is dropped: N
    records sharing a key become ONE group of N, never pairwise groups."""
    buckets = {}
    for rec in records:
        raw = (rec.get("values") or {}).get("raw") or {}
        key = record_key(raw, key_columns)
        if key is None:
            continue
        buckets.setdefault(key, []).append(rec["id"])
    return [ids for ids in buckets.values() if len(ids) >= 2]


def group_idem_key(member_ids, key_columns):
    """Deterministic idempotency key for a MatchGroup CREATE — see module
    docstring's Idempotency section for the reasoning."""
    ids = sorted(member_ids)
    name = "exact_match|" + ",".join(key_columns) + "|" + "|".join(ids)
    return str(uuid.uuid5(uuid.NAMESPACE_URL, name))


# ---------------------------------------------------------------------------
# run
# ---------------------------------------------------------------------------

def run(key_columns, source_id=None):
    """One matcher pass: read -> group -> propose. Create-only against
    MatchGroup — no update/delete call is ever emitted (contract #2). Fails
    closed: the first denial raises MatchError and stops, nothing partial
    is left half-reconciled because there is no ongoing status to correct
    (unlike excel_ingest.py's Source, MatchGroup has no worker-owned
    lifecycle field to mark Failed)."""
    records = fetch_master_records(source_id)
    groups = group_by_exact_key(records, key_columns)

    group_ids = []
    for member_ids in groups:
        payload, is_err = mcp("create_record", {
            "entity": "MatchGroup", "basis": RULE_BASIS,
            "values": {"records": sorted(member_ids), "score": 1.0},
            "idempotency_key": group_idem_key(member_ids, key_columns),
        })
        if is_err:
            raise MatchError(f"create MatchGroup denied/rejected for members {member_ids}: {payload}")
        group_ids.append(payload["id"])

    return {
        "records_scanned": len(records),
        "groups_created": len(group_ids),
        "group_ids": group_ids,
    }


def main():
    ap = argparse.ArgumentParser(
        description="datapulse exact-duplicate matcher (ENH-04, level-1 exact only)")
    ap.add_argument("--key-columns", required=True,
                     help="comma-separated raw column names forming the exact match key, e.g. 'name,email'")
    ap.add_argument("--source", help="Source record id to scope the match run to "
                                     "(default: every MasterRecord this identity can read)")
    args = ap.parse_args()

    key_columns = [c.strip() for c in args.key_columns.split(",") if c.strip()]
    if not key_columns:
        sys.exit("[exact_match] FAILED: --key-columns must name at least one raw column")

    try:
        result = run(key_columns, source_id=args.source)
    except MatchError as ex:
        sys.exit(f"[exact_match] FAILED: {ex}")
    print(f"[exact_match] scanned {result['records_scanned']} MasterRecord(s), "
          f"proposed {result['groups_created']} MatchGroup(s)")


if __name__ == "__main__":
    main()

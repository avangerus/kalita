#!/usr/bin/env python3
"""Excel/CSV batch connector — datapulse ENH-02, Sprint 1.

Same spine as workers/connectors/inbound.py (the RÓДНЯ this file mirrors):
bootstrap.get_token() for identity, mcp(name, arguments) for MCP tools/call,
stdlib-only parsing, whitelist field-mapping, least-privilege connector
identity so the node's permissions and audit apply and untrusted parsing
never touches the core.

KEY DIFFERENCE from inbound.py: inbound is an HTTP listener — one record per
POST. This is a BATCH job: one local .xlsx/.csv file -> one Source record ->
many MasterRecord candidates, run once per invocation (cron/manual), not a
server.

Model (packs/datapulse — FIXED, do not edit):
  Source:       kind enum[Excel,CSV,...], ingest_status enum[New,Ingesting,
                Ingested,Failed] default=New, last_ingest, record_count, path.
  MasterRecord: source ref, raw:json (the whole row, header-keyed), display_
                name, status enum[...] default=Draft. This worker never sets
                status: ConnectorAgent holds no update grant on MasterRecord
                at all (security.dsl deny [update MasterRecord]) — the model
                default (Draft) is what every ingested row gets.

Identity: bootstrap as ConnectorAgent (KALITA_BOOTSTRAP_SECRET +
KALITA_WORKER_ID + KALITA_WORKER_ROLE=ConnectorAgent) or pass KALITA_TOKEN
directly. Grants (security.dsl): read [Source], create [Source,
MasterRecord], update [Source.ingest_status, Source.last_ingest,
Source.record_count]; denied update Source.path, update MasterRecord, create
StewardshipTask, delete *. Every mcp() call this file emits stays inside that
shape (see test_excel_ingest.py's governance test) — on any node-side denial
this worker surfaces the error and stops; it never retries under another
identity or escalates.

xlsx parsing: a .xlsx is a zip of OOXML parts. workers/knowvault_indexer/
extract.py already unzips xlsx, but only to dump every <t> text run as flat
prose for RAG — it throws away which cell/row each run came from, so it
cannot rebuild a header-keyed row. This file parses xl/workbook.xml (sheet
list), xl/_rels/workbook.xml.rels (sheet name -> part), xl/sharedStrings.xml
and the worksheet's <sheetData> directly with xml.etree.ElementTree (stdlib)
— no new dependency: grepped the repo first (`xlsx|openpyxl|pandas`) and
nothing already imports a spreadsheet library, so pure stdlib is both
sufficient and the least additive choice.

Idempotency (LLD-01 §5 discipline, mirroring workers/knowvault_indexer/
indexer.py's idem_key): CREATE calls get a deterministic, content-derived key
so a duplicate attempt dedups at the node's Append layer (internal/eventstore)
to the record already made — no randomness, so re-deriving it on a rerun
reproduces the identical key:
  Source:       uuid5(path, "source")
  MasterRecord: uuid5(path, "row", <1-based data-row position>)
Row-identity is POSITIONAL — the Nth data row in the file, header excluded.
Re-running the SAME file re-derives the same per-row keys -> zero duplicate
MasterRecords. Known limitation (documented, not hidden): reordering rows
inside an otherwise-unchanged file is indistinguishable from editing them —
position, not content-hash, is the identity. UPDATE calls (ingest_status
transitions) are not part of this dedup contract — record creation, not
status bookkeeping, is what must never duplicate — so they get a fresh
key per call (mirroring inbound.py's per-attempt uuid4): reusing a
deterministic key there would make the SECOND real run's Ingested/Failed
transition silently fail to journal (the engine's Update() still applies the
new values, but the idempotency-keyed Append dedups to the FIRST run's event
instead of recording this one) — a truthful-audit regression, not a feature.

Usage:
  python excel_ingest.py FILE.csv --name "Customers"
  python excel_ingest.py FILE.xlsx --name "Customers" --primary-column Name --sheet Sheet1
"""
import argparse
import csv
import json
import os
import re
import sys
import urllib.request
import uuid
import zipfile
from datetime import datetime, timezone
from xml.etree import ElementTree as ET

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from bootstrap import get_token  # noqa: E402

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
TOKEN = get_token()

# autonomous-ingest provenance: no task/human decision drives this batch run,
# so the basis is the standing automation rule itself — same provenance kind
# internal/mcp/datapulse_dogfood_test.go's ruleBasis and internal/engine/
# automation.go's scheduled rules use, not a task.
RULE_BASIS = {"type": "rule", "id": "datapulse-ingest/excel_ingest"}

KIND_BY_EXT = {".csv": "CSV", ".xlsx": "Excel"}  # Source.kind enum (entities.dsl)

_NS = {
    "m": "http://schemas.openxmlformats.org/spreadsheetml/2006/main",
    "r": "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
}


class IngestError(Exception):
    """A governed rejection or transport failure. Once the Source exists,
    every raise of this is caught by ingest() and turned into a truthful
    Failed Source (LLD-01 §3: side-effect errors must be journaled, never
    swallowed as a silent crash) — it is never retried under another role."""


def mcp(name, arguments):
    """Call one MCP tool; return (payload, is_error). Mirrors inbound.py's mcp()."""
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


def idem_key(path, *parts):
    """Deterministic idempotency key for a CREATE call: pure function of the
    file path plus a call-site label — no randomness (uuid5, not uuid4)."""
    name = str(path) + "|" + "|".join(str(p) for p in parts)
    return str(uuid.uuid5(uuid.NAMESPACE_URL, name))


def _fresh_key():
    """Non-deterministic idempotency key for an UPDATE call — one per attempt,
    like inbound.py's per-POST uuid4. See module docstring for why status
    transitions are deliberately NOT content-deterministic."""
    return str(uuid.uuid4())


# ---------------------------------------------------------------------------
# CSV — stdlib csv module, a true lazy generator over the open file so a
# truncated/malformed row raises exactly when reached, not upfront.
# ---------------------------------------------------------------------------

def read_csv_rows(path):
    with open(path, newline="", encoding="utf-8-sig") as f:
        yield from csv.reader(f)


# ---------------------------------------------------------------------------
# XLSX — zipfile + xml.etree.ElementTree only (stdlib). Whole-document parse
# (not row-streamed): a .xlsx's sheetData is one XML document, not a line-
# oriented stream, so a corrupt/truncated xlsx fails at open/parse time,
# before any row is yielded — a different (also truthful) failure shape than
# CSV's row-level failure, both land the Source in Failed the same way.
# ---------------------------------------------------------------------------

def _col_to_index(ref):
    """'B7' -> 1 (0-based column index)."""
    letters = re.match(r"[A-Za-z]+", ref)
    letters = letters.group(0) if letters else ""
    idx = 0
    for ch in letters:
        idx = idx * 26 + (ord(ch.upper()) - ord("A") + 1)
    return idx - 1 if idx else 0


def _shared_strings(z):
    if "xl/sharedStrings.xml" not in z.namelist():
        return []
    root = ET.fromstring(z.read("xl/sharedStrings.xml"))
    t_tag = "{%s}t" % _NS["m"]
    return ["".join(t.text or "" for t in si.iter(t_tag)) for si in root.findall("m:si", _NS)]


def _sheet_part(z, sheet_name=None):
    """Resolve the worksheet XML part: sheet_name if given, else the first
    sheet in workbook order (xl/workbook.xml's <sheets> + its .rels)."""
    wb = ET.fromstring(z.read("xl/workbook.xml"))
    sheets = wb.findall("m:sheets/m:sheet", _NS)
    if not sheets:
        raise IngestError("xlsx has no worksheets")
    target = sheets[0]
    if sheet_name:
        matches = [s for s in sheets if s.get("name") == sheet_name]
        if not matches:
            raise IngestError(f"sheet {sheet_name!r} not found")
        target = matches[0]
    rid = target.get("{%s}id" % _NS["r"])
    rels = ET.fromstring(z.read("xl/_rels/workbook.xml.rels"))
    for rel in rels:
        if rel.get("Id") == rid:
            return "xl/" + rel.get("Target").lstrip("/")
    raise IngestError("workbook relationship for sheet not found")


def _cell_value(c, shared):
    t = c.get("t")
    if t == "s":
        v = c.find("m:v", _NS)
        idx = int(v.text) if v is not None and v.text else 0
        return shared[idx] if 0 <= idx < len(shared) else ""
    if t == "inlineStr":
        is_el = c.find("m:is", _NS)
        if is_el is None:
            return ""
        t_tag = "{%s}t" % _NS["m"]
        return "".join(el.text or "" for el in is_el.iter(t_tag))
    if t == "str":  # cached formula result — plain text
        v = c.find("m:v", _NS)
        return v.text if v is not None and v.text is not None else ""
    if t == "b":
        v = c.find("m:v", _NS)
        return bool(v is not None and v.text == "1")
    v = c.find("m:v", _NS)  # numeric (t missing or "n")
    if v is None or v.text is None:
        return ""
    txt = v.text
    try:
        return int(txt) if re.fullmatch(r"-?\d+", txt) else float(txt)
    except ValueError:
        return txt


def read_xlsx_rows(path, sheet_name=None):
    """Yield rows (list of cell values, gaps filled '') from the first (or
    named) worksheet — cell refs (e.g. "C4") place values by column, so
    skipped/blank cells in the XML still land at the right position."""
    with zipfile.ZipFile(path) as z:
        shared = _shared_strings(z)
        part = _sheet_part(z, sheet_name)
        root = ET.fromstring(z.read(part))
        for row_el in root.findall("m:sheetData/m:row", _NS):
            cells = {}
            maxcol = -1
            for c in row_el.findall("m:c", _NS):
                ref = c.get("r") or ""
                col = _col_to_index(ref) if ref else maxcol + 1
                maxcol = max(maxcol, col)
                cells[col] = _cell_value(c, shared)
            yield [cells.get(i, "") for i in range(maxcol + 1)]


def _rows_generator(path, ext, sheet=None):
    if ext == ".csv":
        return read_csv_rows(path)
    if ext == ".xlsx":
        return read_xlsx_rows(path, sheet)
    raise IngestError(f"unsupported file extension {ext!r} (only .csv/.xlsx)")


# ---------------------------------------------------------------------------
# row -> MasterRecord.values mapping
# ---------------------------------------------------------------------------

def _row_to_values(header, row, primary_column, source_id):
    """raw = the WHOLE row keyed by column header (native `json` scalar,
    entities.dsl) — no field whitelist here, unlike inbound.py's Lead
    mapping, because raw is intentionally the whole row by design (PACK
    REPORT). display_name is a best-effort convenience label only: the
    configured primary_column's cell if present and non-empty, else the
    first non-empty cell in header order. status is left OUT of values
    entirely — the model defaults it to Draft, and ConnectorAgent holds no
    update grant on MasterRecord at all, so this worker must never send it."""
    raw = {}
    for i, h in enumerate(header):
        key = h if h not in (None, "") else f"col_{i + 1}"
        raw[key] = row[i] if i < len(row) else ""

    display_name = ""
    if primary_column and primary_column in raw and raw[primary_column] not in (None, ""):
        display_name = str(raw[primary_column])
    if not display_name:
        for v in raw.values():
            if v not in (None, ""):
                display_name = str(v)
                break

    return {"source": source_id, "raw": raw, "display_name": display_name}


def _set_source(source_id, values):
    """update_record within ConnectorAgent's field-scoped grant ONLY —
    Source.ingest_status / Source.last_ingest / Source.record_count. Never
    Source.path (create-once, deny-after per security.dsl) — values passed
    in here must never carry that key."""
    assert "path" not in values, "must never update Source.path post-create"
    _, is_err = mcp("update_record", {
        "entity": "Source", "id": source_id, "basis": RULE_BASIS,
        "values": values, "idempotency_key": _fresh_key(),
    })
    if is_err:
        raise IngestError(f"update Source {source_id} denied/rejected: {values}")


def ingest(path, source_name, kind=None, primary_column=None, sheet=None):
    """Ingest ONE local file into ONE Source + many MasterRecord candidates.
    Returns {"source_id", "record_count", "status"}. Raises IngestError for
    failures BEFORE the Source exists (bad kind/path); every failure AFTER
    that point is caught here and turned into a truthful Failed Source with a
    record_count that counts only rows actually created — never re-raised
    past the point where a caller could mistake it for success."""
    ext = os.path.splitext(path)[1].lower()
    kind = kind or KIND_BY_EXT.get(ext)
    if kind not in ("CSV", "Excel"):
        raise IngestError(f"cannot infer Source.kind for {path!r}; pass kind='CSV'|'Excel'")

    src, is_err = mcp("create_record", {
        "entity": "Source", "basis": RULE_BASIS,
        "values": {"name": source_name, "kind": kind, "path": path},
        "idempotency_key": idem_key(path, "source"),
    })
    if is_err:
        raise IngestError(f"create Source denied/rejected: {src}")
    source_id = src["id"]

    _set_source(source_id, {"ingest_status": "Ingesting"})

    created = 0
    try:
        rows = _rows_generator(path, ext, sheet)
        try:
            header = next(rows)
        except StopIteration:
            header = None  # a genuinely empty file — zero data rows, not a failure
        if header is not None:
            for i, row in enumerate(rows, start=1):
                values = _row_to_values(header, row, primary_column, source_id)
                _, is_err = mcp("create_record", {
                    "entity": "MasterRecord", "basis": RULE_BASIS,
                    "values": values,
                    "idempotency_key": idem_key(path, "row", i),
                })
                if is_err:
                    raise IngestError(f"create MasterRecord row {i} denied/rejected")
                created += 1
    except Exception as ex:
        # a TRUTHFUL terminal state, never a success-lie: record_count counts
        # only what was actually created before the failure.
        _set_source(source_id, {"ingest_status": "Failed", "record_count": created})
        raise IngestError(f"ingest of {path!r} failed after {created} row(s): {ex}") from ex

    _set_source(source_id, {
        "ingest_status": "Ingested",
        "record_count": created,
        "last_ingest": datetime.now(timezone.utc).isoformat(),
    })
    return {"source_id": source_id, "record_count": created, "status": "Ingested"}


def main():
    ap = argparse.ArgumentParser(description="datapulse Excel/CSV batch connector (ENH-02)")
    ap.add_argument("file", help="local path to a .csv or .xlsx file")
    ap.add_argument("--name", help="Source.name (default: the file's basename)")
    ap.add_argument("--kind", choices=["CSV", "Excel"], help="override the kind inferred from the extension")
    ap.add_argument("--primary-column", help="header whose cell becomes MasterRecord.display_name")
    ap.add_argument("--sheet", help="xlsx worksheet name (default: the first sheet)")
    args = ap.parse_args()

    name = args.name or os.path.basename(args.file)
    try:
        result = ingest(args.file, name, kind=args.kind, primary_column=args.primary_column, sheet=args.sheet)
    except IngestError as ex:
        sys.exit(f"[excel_ingest] FAILED: {ex}")
    print(f"[excel_ingest] Source {result['source_id']} Ingested, "
          f"{result['record_count']} MasterRecord(s) from {args.file}")


if __name__ == "__main__":
    main()

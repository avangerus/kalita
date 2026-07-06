#!/usr/bin/env python3
"""Tests for the datapulse Excel/CSV batch connector (ENH-02), workers/
connectors/excel_ingest.py.

No live kalita node required: excel_ingest.mcp() is monkeypatched with an
in-process FakeNode that mimics just enough of the real node's behavior to
prove the contract:
  - create_record dedups by idempotency_key (mirrors internal/eventstore
    MemStore.Append's byIdemKey short-circuit) so the idempotency test proves
    an actual zero-duplicate outcome, not merely "the same key was passed".
  - every call is logged (name, arguments) for the governance test to assert
    against, the same style as workers/knowvault_indexer/test_perimeter_
    ingest.py's `calls` list.

Run:  python -m pytest test_excel_ingest.py -v
  or: python test_excel_ingest.py
"""
import csv
import io
import os
import sys
import tempfile
import unittest
import uuid
import zipfile
from unittest import mock

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

# excel_ingest's module-level `TOKEN = get_token()` must not try to bootstrap
# against a real node at import time. bootstrap.get_token()'s FIRST check is
# `if os.environ.get("KALITA_TOKEN")` -- an immediate, no-network return -- so
# setting this env var (rather than stubbing sys.modules["bootstrap"], the
# pattern test_bugs_p1.py/test_perimeter_ingest.py use) is what's actually
# robust here: when this file runs inside `pytest workers/` alongside
# workers/agent_runner/test_runner.py, that test imports the REAL bootstrap
# module first (runner.py calls get_token() lazily, not at import time, so it
# never trips sys.exit) -- by the time this file's import ran,
# sys.modules.setdefault("bootstrap", ...) would be a silent no-op against an
# already-real module, and excel_ingest's eager TOKEN = get_token() would hit
# the real bootstrap logic and sys.exit for lack of a live node.
os.environ.setdefault("KALITA_TOKEN", "test-token")

import excel_ingest as ei  # noqa: E402


# ===========================================================================
# FakeNode — an in-process stand-in for the MCP layer
# ===========================================================================

class FakeNode:
    """Mimics create_record/update_record's observable behavior, including
    idempotency_key dedup on create_record (internal/eventstore.MemStore.
    Append: a repeated key returns the ALREADY-CREATED record, nothing new is
    appended) — so a test against this fake proves the same zero-duplicate
    outcome a real node would produce, not just that the same key was sent."""

    def __init__(self):
        self.records = {}   # entity -> {id: values}
        self.idem = {}       # idempotency_key -> id
        self.calls = []      # (name, arguments) — the governance test's log
        self.fail_at = None  # (entity, "create"/"update", nth_call) -> force is_error

    def __call__(self, name, arguments):
        self.calls.append((name, dict(arguments)))
        if name == "create_record":
            return self._create(arguments)
        if name == "update_record":
            return self._update(arguments)
        raise AssertionError(f"unexpected mcp call: {name} {arguments}")

    def _create(self, args):
        entity = args["entity"]
        key = args.get("idempotency_key")
        if key and key in self.idem:
            rid = self.idem[key]
            return {"id": rid, "values": self.records[entity][rid]}, False
        rid = str(uuid.uuid4())
        self.records.setdefault(entity, {})[rid] = dict(args["values"])
        if key:
            self.idem[key] = rid
        return {"id": rid, "values": self.records[entity][rid]}, False

    def _update(self, args):
        entity, rid = args["entity"], args["id"]
        self.records[entity][rid].update(args["values"])
        return {"id": rid, "values": self.records[entity][rid]}, False

    def master_records(self):
        return self.records.get("MasterRecord", {})

    def sources(self):
        return self.records.get("Source", {})


# ===========================================================================
# fixtures — a small CSV and a small XLSX with the SAME logical content
# ===========================================================================

def _write_csv(path, header, rows):
    with open(path, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(header)
        w.writerows(rows)


def _col_letter(i):
    i += 1
    s = ""
    while i:
        i, rem = divmod(i - 1, 26)
        s = chr(65 + rem) + s
    return s


def _write_xlsx(path, header, rows):
    """A minimal, valid .xlsx (zip of OOXML) built with stdlib zipfile only —
    mirrors test_bugs_p1.py's _make_minimal_docx helper for the same repo
    convention of hand-rolled Office XML test fixtures."""
    strings = []

    def sidx(s):
        s = str(s)
        if s not in strings:
            strings.append(s)
        return strings.index(s)

    all_rows = [header] + rows
    row_xml = []
    for r_i, row in enumerate(all_rows, start=1):
        cells = "".join(
            f'<c r="{_col_letter(c_i)}{r_i}" t="s"><v>{sidx(val)}</v></c>'
            for c_i, val in enumerate(row)
        )
        row_xml.append(f'<row r="{r_i}">{cells}</row>')

    sheet_xml = (
        '<?xml version="1.0" encoding="UTF-8"?>'
        '<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">'
        f'<sheetData>{"".join(row_xml)}</sheetData></worksheet>'
    )
    shared_xml = (
        '<?xml version="1.0" encoding="UTF-8"?>'
        '<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" '
        f'count="{len(strings)}" uniqueCount="{len(strings)}">'
        + "".join(f"<si><t>{s}</t></si>" for s in strings) + "</sst>"
    )
    workbook_xml = (
        '<?xml version="1.0" encoding="UTF-8"?>'
        '<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" '
        'xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">'
        '<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>'
    )
    workbook_rels = (
        '<?xml version="1.0" encoding="UTF-8"?>'
        '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
        '<Relationship Id="rId1" '
        'Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" '
        'Target="worksheets/sheet1.xml"/></Relationships>'
    )
    content_types = (
        '<?xml version="1.0" encoding="UTF-8"?>'
        '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
        '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
        '<Default Extension="xml" ContentType="application/xml"/>'
        '<Override PartName="/xl/workbook.xml" '
        'ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>'
        '<Override PartName="/xl/worksheets/sheet1.xml" '
        'ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>'
        '<Override PartName="/xl/sharedStrings.xml" '
        'ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>'
        '</Types>'
    )
    root_rels = (
        '<?xml version="1.0" encoding="UTF-8"?>'
        '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
        '<Relationship Id="rId1" '
        'Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" '
        'Target="xl/workbook.xml"/></Relationships>'
    )
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("[Content_Types].xml", content_types)
        z.writestr("_rels/.rels", root_rels)
        z.writestr("xl/workbook.xml", workbook_xml)
        z.writestr("xl/_rels/workbook.xml.rels", workbook_rels)
        z.writestr("xl/worksheets/sheet1.xml", sheet_xml)
        z.writestr("xl/sharedStrings.xml", shared_xml)


HEADER = ["name", "city"]
DATA_ROWS = [["Acme LLC", "Springfield"], ["Globex Inc", "Shelbyville"]]


# ===========================================================================
# (a) happy path — CSV and XLSX
# ===========================================================================

class TestHappyPath(unittest.TestCase):
    def _run(self, path):
        node = FakeNode()
        with mock.patch.object(ei, "mcp", node):
            result = ei.ingest(path, "Customers")
        return node, result

    def test_happy_path_csv(self):
        tmp = tempfile.mktemp(suffix=".csv")
        _write_csv(tmp, HEADER, DATA_ROWS)
        try:
            node, result = self._run(tmp)
        finally:
            os.unlink(tmp)

        self.assertEqual(result["record_count"], 2)
        self.assertEqual(result["status"], "Ingested")

        mrs = node.master_records()
        self.assertEqual(len(mrs), 2, "must create exactly one MasterRecord per data row")
        raws = {v["raw"]["name"]: v["raw"] for v in mrs.values()}
        self.assertEqual(raws["Acme LLC"], {"name": "Acme LLC", "city": "Springfield"})
        self.assertEqual(raws["Globex Inc"], {"name": "Globex Inc", "city": "Shelbyville"})
        for v in mrs.values():
            self.assertNotIn("status", v, "status must be left to the model default (Draft)")

        src = node.sources()[result["source_id"]]
        self.assertEqual(src["kind"], "CSV")
        self.assertEqual(src["ingest_status"], "Ingested")
        self.assertEqual(src["record_count"], 2)
        self.assertIn("last_ingest", src)

    def test_happy_path_xlsx(self):
        tmp = tempfile.mktemp(suffix=".xlsx")
        _write_xlsx(tmp, HEADER, DATA_ROWS)
        try:
            node, result = self._run(tmp)
        finally:
            os.unlink(tmp)

        self.assertEqual(result["record_count"], 2)
        self.assertEqual(result["status"], "Ingested")

        mrs = node.master_records()
        self.assertEqual(len(mrs), 2)
        raws = {v["raw"]["name"]: v["raw"] for v in mrs.values()}
        self.assertEqual(raws["Acme LLC"], {"name": "Acme LLC", "city": "Springfield"})
        self.assertEqual(raws["Globex Inc"], {"name": "Globex Inc", "city": "Shelbyville"})
        for v in mrs.values():
            self.assertNotIn("status", v)

        src = node.sources()[result["source_id"]]
        self.assertEqual(src["kind"], "Excel")
        self.assertEqual(src["ingest_status"], "Ingested")
        self.assertEqual(src["record_count"], 2)


# ===========================================================================
# (b) failure path — malformed/truncated CSV, mid-stream
# ===========================================================================

class TestFailurePath(unittest.TestCase):
    def test_malformed_csv_marks_source_failed_truthfully(self):
        """An unterminated quoted field that runs past csv's field_size_limit
        makes the stdlib csv reader raise _csv.Error exactly when it reaches
        that (3rd) row -- genuinely mid-stream: 2 good rows are already
        created before the failure. Source must end Failed, never a
        success-lie, and record_count must report exactly what was created
        (2), not the 3 rows the file nominally has."""
        tmp = tempfile.mktemp(suffix=".csv")
        oversized_unterminated_field = "x" * 200_000
        with open(tmp, "w", newline="", encoding="utf-8") as f:
            f.write("name,city\r\n")
            f.write("Acme LLC,Springfield\r\n")
            f.write("Globex Inc,Shelbyville\r\n")
            f.write('"' + oversized_unterminated_field + "\r\n")  # never closed

        node = FakeNode()
        try:
            with mock.patch.object(ei, "mcp", node):
                with self.assertRaises(ei.IngestError):
                    ei.ingest(tmp, "Customers")
        finally:
            os.unlink(tmp)

        self.assertEqual(len(node.master_records()), 2,
                         "the 2 good rows read before the malformed one must have been created")

        [src] = node.sources().values()
        self.assertEqual(src["ingest_status"], "Failed", "must never claim success on a mid-stream failure")
        self.assertEqual(src["record_count"], 2,
                         "record_count must be truthful -- exactly what was created, not the file's nominal row count")
        self.assertNotEqual(src.get("ingest_status"), "Ingested")


# ===========================================================================
# (c) idempotency — rerun the SAME file, zero duplicate MasterRecords
# ===========================================================================

class TestIdempotency(unittest.TestCase):
    def test_rerun_same_file_creates_zero_duplicate_master_records(self):
        tmp = tempfile.mktemp(suffix=".csv")
        _write_csv(tmp, HEADER, DATA_ROWS)
        try:
            node = FakeNode()  # ONE fake node/journal shared across both runs
            with mock.patch.object(ei, "mcp", node):
                first = ei.ingest(tmp, "Customers")
                mrs_after_first = set(node.master_records())
                second = ei.ingest(tmp, "Customers")
                mrs_after_second = set(node.master_records())
        finally:
            os.unlink(tmp)

        self.assertEqual(first["record_count"], 2)
        self.assertEqual(second["record_count"], 2)
        self.assertEqual(mrs_after_first, mrs_after_second,
                         "re-running the same file must create ZERO new MasterRecord ids")
        self.assertEqual(len(node.master_records()), 2,
                         "two runs of the identical file must still total exactly 2 MasterRecords")
        self.assertEqual(first["source_id"], second["source_id"],
                         "re-running the same file must dedup to the SAME Source, not register a second one")

        create_calls = [a for n, a in node.calls if n == "create_record"]
        self.assertEqual(len(create_calls), 6,
                         "2 runs x (1 Source create + 2 MasterRecord creates) = 6 create_record calls total, "
                         "even though only 3 records actually exist -- proves the DEDUP happened at create time")


# ===========================================================================
# (d) governance — never touches what ConnectorAgent isn't granted
# ===========================================================================

class TestGovernance(unittest.TestCase):
    def test_never_writes_forbidden_fields_or_entities(self):
        """Replays the happy path and inspects the CAPTURED mcp() call log:
        ConnectorAgent (security.dsl) may create [Source, MasterRecord] and
        update [Source.ingest_status, Source.last_ingest, Source.record_count]
        only -- it is denied update Source.path, update MasterRecord, and
        create StewardshipTask. This asserts the worker's OWN emitted calls
        never cross that line, regardless of what a real node would enforce."""
        tmp = tempfile.mktemp(suffix=".csv")
        _write_csv(tmp, HEADER, DATA_ROWS)
        node = FakeNode()
        try:
            with mock.patch.object(ei, "mcp", node):
                ei.ingest(tmp, "Customers")
        finally:
            os.unlink(tmp)

        self.assertTrue(node.calls, "no calls captured -- test fixture is broken")

        for name, args in node.calls:
            entity = args.get("entity")
            # never creates a StewardshipTask (no grant at all on that entity)
            if name == "create_record":
                self.assertNotEqual(entity, "StewardshipTask",
                                    "ConnectorAgent must never create a StewardshipTask")
                if entity == "MasterRecord":
                    self.assertNotIn("status", args["values"],
                                     "must never set MasterRecord.status -- leave the Draft default")
            # never updates MasterRecord at all, and never Source.path post-create
            if name == "update_record":
                self.assertNotEqual(entity, "MasterRecord",
                                    "ConnectorAgent holds no update grant on MasterRecord at all")
                if entity == "Source":
                    self.assertNotIn("path", args["values"],
                                     "must never rewrite Source.path after creation")
                    allowed = {"ingest_status", "last_ingest", "record_count"}
                    self.assertTrue(set(args["values"]).issubset(allowed),
                                    f"Source update must stay within {allowed}, got {args['values']}")
            # every mutating call must carry basis + idempotency_key
            if name in ("create_record", "update_record"):
                self.assertIn("basis", args)
                self.assertIn("idempotency_key", args)
                self.assertTrue(8 <= len(args["idempotency_key"]) <= 128)


# ===========================================================================
# bonus — xlsx row/column parsing correctness in isolation (sparse cells)
# ===========================================================================

class TestXlsxRowParsing(unittest.TestCase):
    def test_sparse_cells_align_by_column_ref(self):
        """A row missing a middle cell in the XML (common when a cell is
        blank) must still land its later values in the right column, not
        shifted left."""
        tmp = tempfile.mktemp(suffix=".xlsx")
        try:
            _write_xlsx(tmp, ["a", "b", "c"], [["1", "2", "3"]])
            # hand-edit the sheet to drop the middle cell (simulate a blank
            # cell the writer omitted rather than emitted empty)
            with zipfile.ZipFile(tmp) as z:
                sheet = z.read("xl/worksheets/sheet1.xml").decode()
                names = z.namelist()
                other = {n: z.read(n) for n in names if n != "xl/worksheets/sheet1.xml"}
            sheet = sheet.replace('<c r="B2" t="s"><v>4</v></c>', "")
            with zipfile.ZipFile(tmp, "w", zipfile.ZIP_DEFLATED) as z:
                for n, data in other.items():
                    z.writestr(n, data)
                z.writestr("xl/worksheets/sheet1.xml", sheet)

            rows = list(ei.read_xlsx_rows(tmp))
            self.assertEqual(rows[0], ["a", "b", "c"])
            self.assertEqual(rows[1], ["1", "", "3"], "the dropped B2 cell must be blank, not shift C2 into B")
        finally:
            os.unlink(tmp)


if __name__ == "__main__":
    unittest.main(verbosity=2)

#!/usr/bin/env python3
"""Deterministic unit tests for P1 correctness bugs BUG-01, BUG-02, BUG-03.

No live stack required — mocks cover Qdrant/LM Studio calls.
Run:  python -m pytest test_bugs_p1.py -v
  or: python test_bugs_p1.py
"""
import io
import json
import os
import sys
import tempfile
import unittest
import urllib.error
import uuid
import zipfile
from http.server import BaseHTTPRequestHandler
from unittest import mock

# ---------------------------------------------------------------------------
# Path setup: let this file run from its own directory or from the repo root.
# ---------------------------------------------------------------------------
_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)


# ===========================================================================
# BUG-01: one source of truth for the embedding model
# ===========================================================================

class TestBug01ServiceHasNoIndependentEmbedModel(unittest.TestCase):
    """service.py must not read an embedding model from its own env var.
    The only source of truth is the sentinel point stored by the indexer."""

    def test_no_embed_model_env_in_service_source(self):
        """Static check: EMBED_MODEL must not appear in service.py."""
        svc_path = os.path.join(_HERE, "..", "knowvault_search", "service.py")
        with open(svc_path, encoding="utf-8") as f:
            source = f.read()
        self.assertNotIn("EMBED_MODEL", source,
                         "service.py still references EMBED_MODEL — BUG-01 not fixed")
        self.assertNotIn("KV_EMBED_MODEL", source,
                         "service.py still reads KV_EMBED_MODEL env — BUG-01 not fixed")

    def test_get_collection_model_function_exists_in_service(self):
        """service.py must expose get_collection_model() that reads from Qdrant."""
        svc_path = os.path.join(_HERE, "..", "knowvault_search", "service.py")
        with open(svc_path, encoding="utf-8") as f:
            source = f.read()
        self.assertIn("get_collection_model", source,
                      "get_collection_model not found in service.py — BUG-01 not fixed")

    def test_meta_id_is_identical_in_indexer_and_service(self):
        """Both modules must use the same sentinel key so they compute the same UUID.
        We check that the uuid5 namespace string 'kv_collection_meta' is present in
        both files — that's what makes the UUIDs agree by construction."""
        sentinel_key = "kv_collection_meta"

        for fname, module_path in [
            ("indexer.py", os.path.join(_HERE, "indexer.py")),
            ("service.py", os.path.join(_HERE, "..", "knowvault_search", "service.py")),
        ]:
            with open(module_path, encoding="utf-8") as f:
                source = f.read()
            self.assertIn(sentinel_key, source,
                          f"{fname} does not contain sentinel key {sentinel_key!r} — "
                          f"indexer and search worker will compute different META_IDs")

    def test_get_collection_model_reads_sentinel(self):
        """get_collection_model() returns the model stored in the sentinel point."""
        # Import service module with its http function patched so no real network call.
        svc_path = os.path.join(_HERE, "..", "knowvault_search", "service.py")
        spec = _load_module_from_path("knowvault_search_service", svc_path)
        svc = spec

        sentinel_model = "text-embedding-bge-m3"
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))

        def fake_http(url, body=None, method=None):
            if meta_id in url:
                return {"result": {"id": meta_id,
                                   "payload": {"_meta": True,
                                               "model": sentinel_model,
                                               "dim": 1024}}}
            raise AssertionError(f"unexpected http call: {url}")

        with mock.patch.object(svc, "http", fake_http):
            result = svc.get_collection_model("kv_test-workspace")

        self.assertEqual(result, sentinel_model,
                         "get_collection_model must return the model from the sentinel point")

    def test_answer_question_uses_collection_model_not_env(self):
        """answer_question embeds the question with the model from the collection,
        NOT from an environment variable."""
        svc_path = os.path.join(_HERE, "..", "knowvault_search", "service.py")
        svc = _load_module_from_path("knowvault_search_service2", svc_path)

        collection_model = "text-embedding-bge-m3"
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))
        used_models = []

        def fake_http(url, body=None, method=None):
            if meta_id in url:
                return {"result": {"id": meta_id,
                                   "payload": {"_meta": True,
                                               "model": collection_model,
                                               "dim": 1024}}}
            if "/v1/embeddings" in url:
                used_models.append(body.get("model"))
                return {"data": [{"embedding": [0.1] * 1024}]}
            if "/points/search" in url:
                return {"result": []}
            return {}

        with mock.patch.object(svc, "http", fake_http):
            svc.answer_question("test question", ["ws-1"])

        self.assertTrue(used_models, "embed was never called")
        self.assertEqual(used_models[0], collection_model,
                         f"embed used {used_models[0]!r} instead of collection model "
                         f"{collection_model!r} — BUG-01 not fixed")


# ===========================================================================
# BUG-02: model change triggers collection rebuild
# ===========================================================================

class TestBug02ModelChangeTriggerRebuild(unittest.TestCase):

    def _make_indexer_http_mock(self, existing_model, existing_dim, target_dim):
        """fake http that models GET collection (dim), GET sentinel (model),
        and DELETE.  existing_model=None simulates a pre-sentinel collection."""
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))
        calls = []

        def fake_http(url, body=None, method=None, headers=None):
            calls.append((method or "GET", url))
            # sentinel point lookup
            if f"/points/{meta_id}" in url and method in (None, "GET"):
                if existing_model:
                    return {"result": {"id": meta_id,
                                       "payload": {"_meta": True,
                                                   "model": existing_model,
                                                   "dim": existing_dim}}}
                return {"result": None}  # no sentinel
            # collection config lookup (dim)
            if "/collections/" in url and "/points" not in url and method in (None, "GET"):
                if existing_dim is None:
                    raise urllib.error.HTTPError(url, 404, "not found", {}, None)
                return {"result": {"config": {"params": {"vectors": {"size": existing_dim}}}}}
            if method == "DELETE" and "/collections/" in url:
                return {}
            return {}

        return fake_http, calls

    def test_reconcile_drops_on_model_mismatch(self):
        """reconcile_collection drops the collection when the sentinel model
        differs from the target (same dim)."""
        import indexer as idx
        fake_http, calls = self._make_indexer_http_mock(
            existing_model="text-embedding-nomic-embed-text-v1.5",
            existing_dim=1024, target_dim=1024)
        with mock.patch.object(idx, "http", fake_http):
            result = idx.reconcile_collection("kv_ws1", "text-embedding-bge-m3", 1024)
        self.assertTrue(result, "must rebuild on model mismatch")
        self.assertTrue([c for c in calls if c[0] == "DELETE"], "collection must be deleted")

    def test_reconcile_drops_pre_sentinel_collection(self):
        """ROOT 2: a pre-sentinel collection (model unknown) whose dim differs
        from target must be dropped — the live-acceptance failure case."""
        import indexer as idx
        fake_http, calls = self._make_indexer_http_mock(
            existing_model=None,  # old collection, no sentinel
            existing_dim=768, target_dim=1024)
        with mock.patch.object(idx, "http", fake_http):
            result = idx.reconcile_collection("kv_ws1", "text-embedding-bge-m3", 1024)
        self.assertTrue(result,
                        "pre-sentinel collection with mismatched dim MUST be dropped — "
                        "this is exactly the bug that survived the mock tests")
        self.assertTrue([c for c in calls if c[0] == "DELETE"], "collection must be deleted")

    def test_reconcile_noop_when_consistent(self):
        """reconcile_collection does nothing when model AND dim already match."""
        import indexer as idx
        fake_http, calls = self._make_indexer_http_mock(
            existing_model="text-embedding-bge-m3", existing_dim=1024, target_dim=1024)
        with mock.patch.object(idx, "http", fake_http):
            result = idx.reconcile_collection("kv_ws1", "text-embedding-bge-m3", 1024)
        self.assertFalse(result, "no rebuild when consistent")
        self.assertFalse([c for c in calls if c[0] == "DELETE"], "must NOT delete when consistent")

    def test_reconcile_noop_when_absent(self):
        """A non-existent collection is a no-op (ensure_collection will create it)."""
        import indexer as idx
        fake_http, calls = self._make_indexer_http_mock(
            existing_model=None, existing_dim=None, target_dim=1024)
        with mock.patch.object(idx, "http", fake_http):
            result = idx.reconcile_collection("kv_ws1", "text-embedding-bge-m3", 1024)
        self.assertFalse(result, "absent collection → no rebuild flag")
        self.assertFalse([c for c in calls if c[0] == "DELETE"], "nothing to delete")

    def test_reindex_action_sets_force_rebuild_in_index_source(self):
        """index_source detects the reindex path (status=New, last_indexed set)
        and passes force_rebuild=True to sync_source."""
        import indexer as idx

        sync_calls = []

        def mock_sync_source(sid, src, basis, tid, force_rebuild=False):
            sync_calls.append({"force_rebuild": force_rebuild})
            return "0 files"

        src_with_history = {
            "values": {
                "status": "New",
                "last_indexed": "2026-06-01T10:00:00Z",  # previously indexed
                "workspace": "ws1",
            }
        }

        # BUG-12 added a probe-embed call in index_source (New/Failed path) that
        # runs BEFORE sync_source: it calls the real `embed()`, which hits LM
        # Studio over the network. Without stubbing it here, the probe fails with
        # a URLError in this offline test env, index_source hits its except-branch
        # (fail_task + early return), and sync_source is never reached — a false
        # negative unrelated to BUG-02. Stub embed() so the probe succeeds and the
        # test actually exercises the force_rebuild wiring it's meant to check.
        with mock.patch.object(idx, "tool", return_value=src_with_history), \
             mock.patch.object(idx, "embed", return_value=[[0.1]]), \
             mock.patch.object(idx, "sync_source", mock_sync_source):
            idx.index_source({"id": "tid-1", "record_id": "sid-1"})

        self.assertTrue(sync_calls, "sync_source was not called")
        self.assertTrue(sync_calls[0]["force_rebuild"],
                        "reindex (status=New + last_indexed set) must pass force_rebuild=True")

    def test_failed_retry_does_not_force_rebuild(self):
        """index_source does NOT set force_rebuild for a Failed->retry path."""
        import indexer as idx

        sync_calls = []

        def mock_sync_source(sid, src, basis, tid, force_rebuild=False):
            sync_calls.append({"force_rebuild": force_rebuild})
            return "0 files"

        src_failed = {
            "values": {
                "status": "Failed",
                "last_indexed": None,
                "workspace": "ws1",
            }
        }

        # Same BUG-12 probe-embed issue as the reindex test above: stub embed()
        # so the New/Failed probe in index_source succeeds and execution actually
        # reaches sync_source, instead of bailing out early on a real network call.
        with mock.patch.object(idx, "tool", return_value=src_failed), \
             mock.patch.object(idx, "embed", return_value=[[0.1]]), \
             mock.patch.object(idx, "sync_source", mock_sync_source):
            idx.index_source({"id": "tid-2", "record_id": "sid-2"})

        self.assertTrue(sync_calls, "sync_source was not called")
        self.assertFalse(sync_calls[0]["force_rebuild"],
                         "Failed->retry must NOT force rebuild (incremental is correct here)")


# ===========================================================================
# BUG-02 (live defect): full sync against a REAL Qdrant-behaving HTTP server.
# This is NOT a mock that hides Qdrant's behavior — it is an in-process server
# that ENFORCES the two behaviors that made the production bug invisible:
#   1. PUT /collections/{name} returns 409 if the collection already exists.
#   2. PUT .../points rejects a vector whose length != the collection's dim
#      (HTTP 400) — exactly how Qdrant refuses a dim-mismatched upsert.
# The scenario: a 768-dim collection WITHOUT a sentinel (built by old code) +
# a target model that yields 1024-dim, then a rebuild.  Expectation: the
# collection is recreated at 1024, sentinel = target model, no old vectors, and
# the upsert does NOT fail.
# ===========================================================================

class _FakeQdrant(BaseHTTPRequestHandler):
    """A tiny Qdrant look-alike that enforces dim on upsert and 409 on re-create.
    State lives on the server instance (self.server.collections)."""

    def log_message(self, *_):  # silence
        pass

    # ---- helpers ----
    def _send(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def _read_body(self):
        n = int(self.headers.get("Content-Length", 0))
        return json.loads(self.rfile.read(n)) if n else {}

    def _col_name(self):
        # /collections/<name>[/...]
        parts = self.path.split("?")[0].strip("/").split("/")
        return parts[1] if len(parts) > 1 else None

    # ---- routes ----
    def do_GET(self):
        cols = self.server.collections
        name = self._col_name()
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))
        if f"/points/{meta_id}" in self.path:
            col = cols.get(name)
            if col and meta_id in col["points"]:
                return self._send(200, {"result": {"id": meta_id,
                                                   "payload": col["points"][meta_id]["payload"]}})
            return self._send(404, {"status": {"error": "not found"}})
        # GET collection config
        if name and "/points" not in self.path:
            col = cols.get(name)
            if not col:
                return self._send(404, {"status": {"error": "not found"}})
            return self._send(200, {"result": {"config": {"params": {
                "vectors": {"size": col["dim"], "distance": "Cosine"}}}}})
        return self._send(404, {"status": {"error": "unknown"}})

    def do_PUT(self):
        cols = self.server.collections
        name = self._col_name()
        body = self._read_body()
        if name and self.path.split("?")[0].endswith(name):
            # create collection
            if name in cols:
                return self._send(409, {"status": {"error": "already exists"}})
            dim = body["vectors"]["size"]
            cols[name] = {"dim": dim, "points": {}}
            return self._send(200, {"result": True})
        if "/points" in self.path:
            col = cols.get(name)
            if not col:
                return self._send(404, {"status": {"error": "no collection"}})
            for p in body.get("points", []):
                if len(p["vector"]) != col["dim"]:
                    # this is the real Qdrant dim-mismatch rejection
                    return self._send(400, {"status": {
                        "error": f"dim mismatch: got {len(p['vector'])}, want {col['dim']}"}})
                col["points"][p["id"]] = {"vector": p["vector"], "payload": p["payload"]}
            return self._send(200, {"result": {"status": "completed"}})
        return self._send(404, {"status": {"error": "unknown"}})

    def do_POST(self):
        cols = self.server.collections
        name = self._col_name()
        body = self._read_body()
        col = cols.get(name)
        if self.path.split("?")[0].endswith("/points/scroll"):
            pts = []
            if col:
                for pid, p in col["points"].items():
                    pl = p["payload"]
                    if pl.get("_meta"):
                        continue
                    pts.append({"id": pid, "payload": pl})
            return self._send(200, {"result": {"points": pts, "next_page_offset": None}})
        if self.path.split("?")[0].endswith("/points/delete"):
            return self._send(200, {"result": {"status": "completed"}})
        return self._send(404, {"status": {"error": "unknown"}})

    def do_DELETE(self):
        cols = self.server.collections
        name = self._col_name()
        cols.pop(name, None)
        return self._send(200, {"result": True})


class TestBug02LiveRebuildAgainstRealQdrantBehavior(unittest.TestCase):
    """Reproduces the production failure with a server that enforces dim + 409,
    not a mock that hides them."""

    def setUp(self):
        from http.server import ThreadingHTTPServer
        import threading
        self.srv = ThreadingHTTPServer(("127.0.0.1", 0), _FakeQdrant)
        self.srv.collections = {}
        self.port = self.srv.server_address[1]
        self.t = threading.Thread(target=self.srv.serve_forever, daemon=True)
        self.t.start()

    def tearDown(self):
        self.srv.shutdown()
        self.srv.server_close()

    def test_rebuild_over_old_dim_collection_without_sentinel(self):
        import indexer as idx

        ws = "ws-live"
        col = f"kv_{ws}"
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))

        # --- seed: a 768-dim collection with an OLD vector and NO sentinel ---
        self.srv.collections[col] = {
            "dim": 768,
            "points": {"old-point-1": {"vector": [0.0] * 768,
                                       "payload": {"source": "src-old", "file": "old.txt",
                                                   "fp": "1:1", "text": "stale"}}},
        }

        # a real source dir with one file to index
        tmpdir = tempfile.mkdtemp()
        with open(os.path.join(tmpdir, "doc.txt"), "w", encoding="utf-8") as f:
            f.write("The new model speaks in 1024 dimensions.")

        src = {"values": {"workspace": ws, "kind": "Files", "path": tmpdir}}

        # target model yields 1024-dim vectors (different from the old 768)
        TARGET_MODEL = "text-embedding-bge-m3"
        TARGET_DIM = 1024

        def fake_embed(model, texts):
            self.assertEqual(model, TARGET_MODEL)
            return [[0.01 * (i + 1)] * TARGET_DIM for i, _ in enumerate(texts)]

        # point indexer at our fake Qdrant; stub only LM (embed), settings, node calls
        with mock.patch.object(idx, "QDRANT", f"http://127.0.0.1:{self.port}"), \
             mock.patch.object(idx, "embed", fake_embed), \
             mock.patch.object(idx, "settings", return_value=(TARGET_MODEL, 512)), \
             mock.patch.object(idx, "tool", return_value={}):
            # rebuild path (user pressed reindex)
            note = idx.sync_source("src-new", src, {}, "tid-live", force_rebuild=True)

        cols = self.srv.collections
        self.assertIn(col, cols, "collection must exist after rebuild")
        # 1. recreated at the target dim
        self.assertEqual(cols[col]["dim"], TARGET_DIM,
                         "collection must be recreated at the target dim (1024), not left at 768")
        # 2. sentinel present and points to the target model
        self.assertIn(meta_id, cols[col]["points"], "sentinel must be written")
        self.assertEqual(cols[col]["points"][meta_id]["payload"]["model"], TARGET_MODEL)
        self.assertEqual(cols[col]["points"][meta_id]["payload"]["dim"], TARGET_DIM)
        # 3. old 768-dim vectors are gone
        self.assertNotIn("old-point-1", cols[col]["points"],
                         "stale vectors from the old collection must not survive a rebuild")
        # 4. the new doc's chunk(s) were upserted (upsert did NOT fail on dim)
        content_points = [p for pid, p in cols[col]["points"].items()
                          if not p["payload"].get("_meta")]
        self.assertTrue(content_points, "new content must be indexed — upsert must not have failed")
        for p in content_points:
            self.assertEqual(len(p["vector"]), TARGET_DIM)
        self.assertIn("1 re-embedded", note, f"unexpected sync note: {note}")


# ===========================================================================
# BUG-03: Upload goes through extract path, not raw decode
# ===========================================================================

class TestBug03UploadUsesExtract(unittest.TestCase):

    def _make_minimal_docx(self, text):
        """Create a valid minimal .docx (Office Open XML) in memory with the
        given text.  No external dependencies — just zipfile + stdlib."""
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as z:
            # [Content_Types].xml
            z.writestr("[Content_Types].xml", (
                '<?xml version="1.0" encoding="UTF-8"?>'
                '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
                '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
                '<Default Extension="xml" ContentType="application/xml"/>'
                '<Override PartName="/word/document.xml"'
                ' ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>'
                '</Types>'
            ))
            # _rels/.rels
            z.writestr("_rels/.rels", (
                '<?xml version="1.0" encoding="UTF-8"?>'
                '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
                '<Relationship Id="rId1"'
                ' Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"'
                ' Target="word/document.xml"/>'
                '</Relationships>'
            ))
            # word/document.xml with the text in a <w:t> run
            z.writestr("word/document.xml", (
                '<?xml version="1.0" encoding="UTF-8"?>'
                '<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">'
                '<w:body><w:p><w:r>'
                f'<w:t>{text}</w:t>'
                '</w:r></w:p></w:body></w:document>'
            ))
        return buf.getvalue()

    def test_extract_text_docx_returns_content_not_garbage(self):
        """extract_text() on a real .docx returns the embedded text string,
        not binary garbage from a raw .decode()."""
        import extract

        known_sentence = "KnowVault upload extraction test sentence"
        docx_bytes = self._make_minimal_docx(known_sentence)

        with tempfile.NamedTemporaryFile(suffix=".docx", delete=False) as f:
            f.write(docx_bytes)
            tmp_path = f.name
        try:
            result = extract.extract_text(tmp_path)
        finally:
            os.unlink(tmp_path)

        self.assertIsNotNone(result, "extract_text returned None for a valid .docx")
        preview = repr(result[:200]) if result else "None"
        self.assertIn(known_sentence, result,
                      f"Known sentence not found in extracted text.\n"
                      f"Got: {preview}\n"
                      f"This means the extraction path produces garbage — BUG-03 not fixed")

    def test_raw_decode_docx_is_garbage(self):
        """Confirm that raw .decode('utf-8', 'ignore') on a docx binary IS garbage —
        i.e., the old code was indeed broken and the fix is necessary."""
        known_sentence = "KnowVault upload extraction test sentence"
        docx_bytes = self._make_minimal_docx(known_sentence)
        raw = docx_bytes.decode("utf-8", "ignore")
        # The raw decode should NOT produce the clean sentence; it will be
        # interleaved with XML/ZIP binary noise.
        self.assertNotEqual(raw.strip(), known_sentence,
                            "Surprisingly, raw decode equals the clean text — "
                            "the test fixture is too simple to prove the bug")

    def test_upload_unit_loader_uses_extract_upload(self):
        """The Upload branch of units() in sync_source must call _extract_upload,
        not call http_bytes(...).decode(...)."""
        import indexer as idx

        extract_called = []
        raw_decode_called = []

        def mock_extract_upload(url, name):
            extract_called.append((url, name))
            return "extracted text from docx"

        # Patch http_bytes to detect if raw decode is ever attempted directly
        original_http_bytes = idx.http_bytes

        def mock_http_bytes(url):
            raw_decode_called.append(url)
            return b"\x50\x4b\x03\x04"  # docx magic bytes — not valid UTF-8 text

        src = {"values": {
            "workspace": "ws1",
            "kind": "Upload",
            "document": {"name": "report.docx", "hash": "abc123"},
        }}

        with mock.patch.object(idx, "_extract_upload", mock_extract_upload), \
             mock.patch.object(idx, "http_bytes", mock_http_bytes), \
             mock.patch.object(idx, "settings", return_value=("text-embedding-bge-m3", 512)), \
             mock.patch.object(idx, "reconcile_collection", return_value=False), \
             mock.patch.object(idx, "indexed_fingerprints", return_value={}), \
             mock.patch.object(idx, "embed", return_value=[[0.1] * 1024]), \
             mock.patch.object(idx, "ensure_collection"), \
             mock.patch.object(idx, "http", return_value={}), \
             mock.patch.object(idx, "tool", return_value={}):
            idx.sync_source("sid-1", src, {}, "tid-1")

        self.assertTrue(extract_called,
                        "_extract_upload was never called for an Upload source — BUG-03 not fixed")
        self.assertFalse(raw_decode_called,
                         "http_bytes was called directly (raw decode path) for Upload — BUG-03 not fixed")


# ===========================================================================
# helpers
# ===========================================================================

def _load_module_from_path(name, path):
    """Import a module from an absolute file path without modifying sys.path."""
    import importlib.util
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    # Stub heavy imports so the module can be loaded without a real environment.
    sys.modules.setdefault("bootstrap", _FakeBootstrap())
    try:
        spec.loader.exec_module(mod)
    except Exception:
        # service.py may try to resolve env at import time — that's ok,
        # we already patched what we need above
        pass
    return mod


class _FakeBootstrap:
    """Minimal stub for the bootstrap module used by indexer.py."""
    def get_token(self):
        return "test-token"

    def __getattr__(self, name):
        return lambda *a, **kw: "test-token"


# ===========================================================================
# entrypoint
# ===========================================================================

if __name__ == "__main__":
    unittest.main(verbosity=2)

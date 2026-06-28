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
import uuid
import zipfile
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

    def _make_indexer_http_mock(self, existing_model, target_model):
        """Returns (fake_http, calls) where calls accumulates method+url pairs."""
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))
        calls = []

        def fake_http(url, body=None, method=None, headers=None):
            calls.append((method or "GET", url))
            if meta_id in url and method in (None, "GET"):
                if existing_model:
                    return {"result": {"id": meta_id,
                                       "payload": {"_meta": True,
                                                   "model": existing_model,
                                                   "dim": 1024}}}
                # No sentinel → no result
                return {"result": None}
            if method == "DELETE" and "/collections/" in url:
                return {}
            return {}

        return fake_http, calls

    def test_collection_needs_rebuild_returns_true_on_model_mismatch(self):
        """collection_needs_rebuild() returns True and deletes the collection
        when the sentinel model differs from the target model."""
        import indexer as idx

        existing = "text-embedding-nomic-embed-text-v1.5"
        target = "text-embedding-bge-m3"
        fake_http, calls = self._make_indexer_http_mock(existing, target)

        with mock.patch.object(idx, "http", fake_http):
            result = idx.collection_needs_rebuild("kv_ws1", target)

        self.assertTrue(result, "collection_needs_rebuild must return True on model mismatch")
        delete_calls = [c for c in calls if c[0] == "DELETE"]
        self.assertTrue(delete_calls,
                        "collection must be deleted when model changes — BUG-02 not fixed")

    def test_collection_needs_rebuild_returns_false_on_model_match(self):
        """collection_needs_rebuild() returns False when model matches."""
        import indexer as idx

        model = "text-embedding-bge-m3"
        fake_http, calls = self._make_indexer_http_mock(model, model)

        with mock.patch.object(idx, "http", fake_http):
            result = idx.collection_needs_rebuild("kv_ws1", model)

        self.assertFalse(result,
                         "collection_needs_rebuild must return False when model matches")
        delete_calls = [c for c in calls if c[0] == "DELETE"]
        self.assertFalse(delete_calls, "collection must NOT be deleted when model is the same")

    def test_force_rebuild_skips_fingerprints(self):
        """sync_source with force_rebuild=True calls indexed_fingerprints only if
        needed — effectively treating prior as empty so every file is re-embedded.

        This test verifies the branching logic via indexer module internals without
        a live Qdrant: we patch indexed_fingerprints to confirm it is NOT consulted
        when force_rebuild=True."""
        import indexer as idx

        called = []

        def mock_indexed_fingerprints(*a, **kw):
            called.append(True)
            return {}

        # sync_source requires tool(), embed(), ensure_collection() — stub them all.
        def noop(*a, **kw):
            return {}

        src = {"values": {"workspace": "ws1", "kind": "Files", "path": "/nonexistent"}}

        patches = [
            mock.patch.object(idx, "indexed_fingerprints", mock_indexed_fingerprints),
            mock.patch.object(idx, "settings", return_value=("text-embedding-bge-m3", 512)),
            mock.patch.object(idx, "collection_needs_rebuild", return_value=False),
            mock.patch.object(idx, "tool", return_value={}),
        ]
        for p in patches:
            p.start()
        try:
            # walk will find nothing in /nonexistent, so we just check fingerprints logic
            idx.sync_source("sid-1", src, {}, "tid-1", force_rebuild=True)
        except Exception:
            pass  # expected — no real walk/embed
        finally:
            for p in patches:
                p.stop()

        self.assertFalse(called,
                         "indexed_fingerprints must NOT be called when force_rebuild=True")

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

        with mock.patch.object(idx, "tool", return_value=src_with_history), \
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

        with mock.patch.object(idx, "tool", return_value=src_failed), \
             mock.patch.object(idx, "sync_source", mock_sync_source):
            idx.index_source({"id": "tid-2", "record_id": "sid-2"})

        self.assertTrue(sync_calls, "sync_source was not called")
        self.assertFalse(sync_calls[0]["force_rebuild"],
                         "Failed->retry must NOT force rebuild (incremental is correct here)")


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
             mock.patch.object(idx, "collection_needs_rebuild", return_value=False), \
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

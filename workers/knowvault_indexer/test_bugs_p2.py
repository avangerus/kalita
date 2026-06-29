#!/usr/bin/env python3
"""Deterministic unit tests for P2 quality bugs BUG-04..BUG-07.

Contracts under test (not implementation details):
  BUG-04 — structural chunking: no fragment cut mid-line; table rows intact.
  BUG-05 — relevance threshold: hits below threshold → honest "not found".
  BUG-06 — default workspace: SearchView defaults to a real workspace, not "".
  BUG-07 — wide pool + reranker: pool > old limit; rerank path invoked (or
            documented degradation).

Run:  python -m pytest test_bugs_p2.py -v
  or: python test_bugs_p2.py
"""
import importlib.util
import json
import os
import sys
import unittest
from unittest import mock

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

_SVC_PATH = os.path.join(_HERE, "..", "knowvault_search", "service.py")
_APP_JS_PATH = os.path.join(_HERE, "..", "..", "web", "app.js")


# ---------------------------------------------------------------------------
# helpers (defined early so the bootstrap stub can be injected before any
# test class setUp runs; mirrors the pattern from test_bugs_p1.py)
# ---------------------------------------------------------------------------

class _FakeBootstrap:
    def get_token(self):
        return "test-token"

    def __getattr__(self, name):
        return lambda *a, **kw: "test-token"


# Inject the stub NOW so that 'import indexer' inside setUp / test methods
# finds a fake bootstrap module and does not call sys.exit.
sys.modules.setdefault("bootstrap", _FakeBootstrap())


# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------

def _load_service(mod_name="svc_p2"):
    # bootstrap stub already in sys.modules from module-level injection above
    spec = importlib.util.spec_from_file_location(mod_name, _SVC_PATH)
    mod = importlib.util.module_from_spec(spec)
    try:
        spec.loader.exec_module(mod)
    except Exception:
        pass
    return mod


# ===========================================================================
# BUG-04 — structural chunking
# ===========================================================================

class TestBug04StructuralChunking(unittest.TestCase):
    """chunk() must not cut in the middle of a line/word."""

    def setUp(self):
        import indexer as idx
        self.chunk = idx.chunk

    # --- helpers ---

    def _assert_no_mid_line_cut(self, pieces, original_lines):
        """Every line from the original text must appear in exactly one chunk,
        without being split across characters (i.e. a proper prefix/suffix)."""
        for orig_line in original_lines:
            if not orig_line.strip():
                continue
            found = [p for p in pieces if orig_line in p]
            self.assertTrue(
                found,
                f"Original line not found intact in any chunk.\n"
                f"  Line:   {orig_line!r}\n"
                f"  Chunks: {pieces}",
            )

    # --- tests ---

    def test_markdown_table_rows_not_split_mid_line(self):
        """Table rows must land whole in a chunk, not be sliced by character."""
        table = (
            "| Column A | Column B | Column C |\n"
            "|----------|----------|----------|\n"
            "| Value 1  | Value 2  | Value 3  |\n"
            "| Value 4  | Value 5  | Value 6  |"
        )
        # Use a small-ish size (80) so the table paragraph (>80 chars total) forces
        # the line-split path.  Each individual row is well under 80 chars.
        pieces = self.chunk(table, 80)
        self.assertTrue(pieces, "chunk() returned empty list for a markdown table")
        table_lines = table.split("\n")
        self._assert_no_mid_line_cut(pieces, table_lines)

    def test_table_embedded_in_prose(self):
        """Table inside a document (preceded and followed by prose paragraphs)."""
        doc = (
            "Introduction paragraph with some text.\n\n"
            "| H1 | H2 | H3 |\n"
            "|----|----|----|--|\n"
            "| A  | B  | C  |\n"
            "| D  | E  | F  |\n\n"
            "Conclusion paragraph with more text."
        )
        pieces = self.chunk(doc, 60)
        self.assertTrue(pieces)
        # Table rows must survive intact.
        for row in ["| H1 | H2 | H3 |", "| A  | B  | C  |", "| D  | E  | F  |"]:
            self._assert_no_mid_line_cut(pieces, [row])

    def test_no_fragment_cut_mid_word(self):
        """When a paragraph is split, all resulting fragments start/end on word
        boundaries — no word is cut across two chunks."""
        # A paragraph without newlines that exceeds size — forces word-split path.
        long_para = " ".join(["word"] * 50)  # "word word word ... word" (254 chars)
        pieces = self.chunk(long_para, 60)
        self.assertTrue(pieces, "no chunks produced")
        for piece in pieces:
            # Each piece must not start or end in the middle of "word".
            self.assertTrue(
                piece == piece.strip(),
                f"Chunk has leading/trailing whitespace: {piece!r}",
            )
            # Reconstruct: rejoining with spaces must recover all words.
        all_words = " ".join(pieces).split()
        self.assertEqual(all_words, ["word"] * 50,
                         "Rejoining all chunks must recover all original words exactly")

    def test_small_paragraph_under_size_never_split(self):
        """Paragraphs that fit within size must never be split."""
        text = "Short para one.\n\nShort para two.\n\nShort para three."
        pieces = self.chunk(text, 200)
        # All three should be coalesced into one chunk (total < 200).
        self.assertEqual(len(pieces), 1, f"Expected 1 chunk, got {len(pieces)}: {pieces}")

    def test_empty_text_returns_empty_list(self):
        self.assertEqual(self.chunk("", 512), [])

    def test_no_line_within_chunk_is_hardsliced(self):
        """Per-LINE invariant: every line inside every chunk is either <= size
        OR a single space-less token.  (A multi-row table chunk may exceed size
        as a whole to stay coherent — that is allowed by the BUG-04 contract;
        what is NOT allowed is slicing a line mid-word.)"""
        text = (
            "First paragraph is short.\n\n"
            "| Column One | Column Two | Column Three |\n"
            "|------------|------------|---------------|\n"
            "| Alpha Beta | Gamma Delt | Epsilon Zeta  |\n\n"
            "A long run-on sentence without any double-newline breaks "
            "that goes on and on for quite a while to exceed the size limit."
        )
        size = 80
        pieces = self.chunk(text, size)
        for piece in pieces:
            for line in piece.split("\n"):
                if len(line) > size:
                    self.assertNotIn(
                        " ", line,
                        f"A line was hard-sliced beyond size={size} and is NOT a "
                        f"single token:\n  {line!r}",
                    )

    def test_no_chunk_is_pure_markup_or_punctuation(self):
        """The regression contract: after chunking, NO chunk may consist solely
        of table markup / punctuation / whitespace.  Every chunk must carry at
        least one alphanumeric character (real content)."""
        ascii_table = (
            "Quarterly revenue by region (USD millions):\n\n"
            "+----------------------------+------------+------------+\n"
            "| Region                     | Q1         | Q2         |\n"
            "+----------------------------+------------+------------+\n"
            "| North America              | 12.4       | 14.1       |\n"
            "| Europe Middle East Africa  | 9.7        | 10.3       |\n"
            "| Asia Pacific               | 7.2        | 8.8        |\n"
            "|                            |            |            |\n"
            "| Total                      | 29.3       | 33.2       |\n"
            "+----------------------------+------------+------------+"
        )
        markdown_table = (
            "## Pricing tiers\n\n"
            "| Tier       | Seats | Price per month |\n"
            "|------------|-------|-----------------|\n"
            "| Starter    | 5     | 49              |\n"
            "| Team       | 25    | 199             |\n"
            "| Enterprise | 100   | 799             |\n"
            "| Custom     |       |                 |"
        )
        for label, doc in [("ascii", ascii_table), ("markdown", markdown_table)]:
            for size in (60, 120, 512):
                pieces = self.chunk(doc, size)
                self.assertTrue(pieces, f"{label}@{size}: no chunks produced")
                for piece in pieces:
                    self.assertTrue(
                        any(c.isalnum() for c in piece),
                        f"{label}@{size}: a chunk is pure markup/punctuation "
                        f"(BUG-04 regression):\n  {piece!r}",
                    )

    def test_formatting_only_rows_never_isolated(self):
        """Specifically: separator/border rows (+---+, |---|, blank | |) must not
        appear as their own chunk — the exact failure the coordinator observed."""
        doc = (
            "+------------+------------+\n"
            "| Name       | Score      |\n"
            "+------------+------------+\n"
            "| Alpha      | 91         |\n"
            "| Beta       | 88         |\n"
            "|            |            |\n"
            "| Gamma      | 77         |\n"
            "+------------+------------+"
        )
        # Force the row-group path with a small size.
        pieces = self.chunk(doc, 50)
        self.assertTrue(pieces)
        formatting_only = [p for p in pieces if not any(c.isalnum() for c in p)]
        self.assertEqual(
            formatting_only, [],
            f"Formatting-only chunks leaked into the index: {formatting_only}",
        )

    def test_table_kept_as_coherent_block_with_header(self):
        """A table that fits within size stays as ONE coherent chunk including
        its header — not exploded into per-row chunks."""
        table = (
            "| Product | Stock |\n"
            "|---------|-------|\n"
            "| Widget  | 120   |\n"
            "| Gadget  | 64    |"
        )
        pieces = self.chunk(table, 512)  # comfortably larger than the table
        self.assertEqual(len(pieces), 1,
                         f"Table within size must be ONE coherent chunk, got: {pieces}")
        self.assertIn("Product", pieces[0])
        self.assertIn("Widget", pieces[0])
        self.assertIn("Gadget", pieces[0])

    def test_oversized_table_groups_repeat_header(self):
        """When a table exceeds size it is split into row GROUPS (not single
        rows), and each group is self-describing (carries the header)."""
        rows = "\n".join(f"| Item{i:02d} | Value number {i} |" for i in range(20))
        table = "| Item | Description |\n|------|-------------|\n" + rows
        size = 90
        pieces = self.chunk(table, size)
        self.assertGreater(len(pieces), 1, "a 20-row table should split into groups")
        for p in pieces:
            self.assertIn("Item", p, "each group must carry the header row")
            self.assertTrue(any(c.isalnum() for c in p))


# ===========================================================================
# BUG-05 — relevance threshold
# ===========================================================================

class TestBug05ScoreThreshold(unittest.TestCase):
    """answer_question must reject all hits below SCORE_THRESHOLD and return
    a honest 'not found' message instead of hallucinating from noise."""

    def setUp(self):
        self.svc = _load_service("svc_p2_b05")

    def _make_fake_http(self, hit_scores, embed_dim=8):
        """Build a fake http() that returns the given scores for /points/search."""
        import uuid
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))

        def fake_http(url, body=None, method=None):
            if meta_id in url:
                return {"result": {"payload": {"_meta": True,
                                               "model": "text-embedding-bge-m3",
                                               "dim": embed_dim}}}
            if "/v1/embeddings" in url:
                return {"data": [{"embedding": [0.1] * embed_dim}]}
            if "/points/search" in url:
                results = [
                    {"score": s, "payload": {"file": f"doc{i}.txt", "text": f"content {i}"}}
                    for i, s in enumerate(hit_scores)
                ]
                return {"result": results}
            if "/v1/rerank" in url:
                raise Exception("reranker not available")
            if "/v1/chat/completions" in url:
                return {"choices": [{"message": {"content": "answer"}}]}
            return {}

        return fake_http

    def test_all_hits_below_threshold_returns_not_found(self):
        """When every hit's score is below SCORE_THRESHOLD, the answer must
        indicate nothing relevant was found — not generate a response."""
        svc = self.svc
        threshold = svc.SCORE_THRESHOLD  # read the module constant

        # All scores well below threshold.
        fake_http = self._make_fake_http([threshold - 0.1, threshold - 0.05])
        with mock.patch.object(svc, "http", fake_http):
            result = svc.answer_question("What is the policy?", ["ws-1"])

        self.assertEqual(result["sources"], [],
                         "sources must be empty when nothing passes threshold")
        answer = result["answer"].lower()
        self.assertTrue(
            any(kw in answer for kw in ("not found", "no relevant", "outside", "no indexed")),
            f"Expected a 'not found' message, got: {result['answer']!r}",
        )

    def test_hit_above_threshold_produces_context(self):
        """A hit at or above SCORE_THRESHOLD must reach the LLM context."""
        svc = self.svc
        threshold = svc.SCORE_THRESHOLD
        chat_calls = []

        def fake_http(url, body=None, method=None):
            import uuid
            meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))
            if meta_id in url:
                return {"result": {"payload": {"_meta": True,
                                               "model": "text-embedding-bge-m3",
                                               "dim": 8}}}
            if "/v1/embeddings" in url:
                return {"data": [{"embedding": [0.1] * 8}]}
            if "/points/search" in url:
                return {"result": [
                    {"score": threshold + 0.10, "payload": {"file": "good.txt", "text": "relevant content"}},
                    {"score": threshold - 0.05, "payload": {"file": "bad.txt", "text": "irrelevant noise"}},
                ]}
            if "/v1/rerank" in url:
                raise Exception("reranker not available")
            if "/v1/chat/completions" in url:
                chat_calls.append(body)
                return {"choices": [{"message": {"content": "The policy is X."}}]}
            return {}

        with mock.patch.object(svc, "http", fake_http):
            result = svc.answer_question("What is the policy?", ["ws-1"])

        self.assertTrue(chat_calls, "LLM was never called despite a hit above threshold")
        context_sent = chat_calls[0]["messages"][1]["content"]
        self.assertIn("relevant content", context_sent,
                      "The above-threshold hit must appear in the LLM context")
        self.assertNotIn("irrelevant noise", context_sent,
                         "The below-threshold hit must NOT appear in the LLM context")
        self.assertIn("good.txt", result["sources"])
        self.assertNotIn("bad.txt", result["sources"])

    def test_score_threshold_is_configurable(self):
        """SCORE_THRESHOLD must be a named constant (not a magic number)."""
        svc = self.svc
        self.assertTrue(hasattr(svc, "SCORE_THRESHOLD"),
                        "service.py must define SCORE_THRESHOLD constant")
        self.assertIsInstance(svc.SCORE_THRESHOLD, float,
                              "SCORE_THRESHOLD must be a float")

    def test_score_threshold_env_override(self):
        """KV_SCORE_THRESHOLD env var controls the threshold."""
        import importlib
        with mock.patch.dict(os.environ, {"KV_SCORE_THRESHOLD": "0.55"}):
            spec = importlib.util.spec_from_file_location("svc_p2_env", _SVC_PATH)
            mod = importlib.util.module_from_spec(spec)
            try:
                spec.loader.exec_module(mod)
            except Exception:
                pass
        self.assertAlmostEqual(mod.SCORE_THRESHOLD, 0.55,
                               msg="SCORE_THRESHOLD must read from KV_SCORE_THRESHOLD env")


# ===========================================================================
# BUG-06 — default workspace selection
# ===========================================================================

class TestBug06DefaultWorkspace(unittest.TestCase):
    """SearchView in app.js must default to a real workspace id, not ''."""

    def _load_app_js(self):
        with open(_APP_JS_PATH, encoding="utf-8") as f:
            return f.read()

    def test_default_state_is_not_empty_string(self):
        """useState('') for the project must not be the final word — the
        useEffect must overwrite it with a real id on load."""
        src = self._load_app_js()
        # The useEffect that loads workspaces must call setProject with a real id.
        self.assertIn("setProject(ws[", src,
                      "app.js SearchView must call setProject with a workspace id "
                      "(e.g. ws[ws.length-1].id) in the useEffect — BUG-06 not fixed")

    def test_all_projects_option_still_exists(self):
        """The 'All projects' option must remain so the user can deliberately
        choose it — it must just not be the default."""
        src = self._load_app_js()
        self.assertIn("All projects", src,
                      "The 'All projects' option must still exist in the select — "
                      "it should be non-default, not removed")

    def test_default_is_last_workspace(self):
        """The default must be the last workspace in the list (ws.length-1)."""
        src = self._load_app_js()
        self.assertIn("ws.length - 1", src,
                      "Default workspace must be ws[ws.length - 1].id "
                      "(last workspace, consistent with insertion order)")


# ===========================================================================
# BUG-07 — wide pool + reranker
# ===========================================================================

class TestBug07WidePoolAndReranker(unittest.TestCase):

    def setUp(self):
        self.svc = _load_service("svc_p2_b07")

    def test_search_k_is_larger_than_old_limit(self):
        """SEARCH_K (per-workspace candidates) must exceed the old hardcoded 4."""
        svc = self.svc
        self.assertTrue(hasattr(svc, "SEARCH_K"),
                        "service.py must define SEARCH_K constant")
        self.assertGreater(svc.SEARCH_K, 4,
                           f"SEARCH_K={svc.SEARCH_K} is not wider than the old hardcoded 4")

    def test_search_uses_search_k_not_hardcoded_4(self):
        """answer_question must pass SEARCH_K (not 4) as 'limit' to Qdrant search."""
        svc = self.svc
        import uuid
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))
        search_limits = []

        def fake_http(url, body=None, method=None):
            if meta_id in url:
                return {"result": {"payload": {"_meta": True,
                                               "model": "text-embedding-bge-m3",
                                               "dim": 8}}}
            if "/v1/embeddings" in url:
                return {"data": [{"embedding": [0.1] * 8}]}
            if "/points/search" in url:
                search_limits.append(body.get("limit"))
                return {"result": []}
            if "/v1/rerank" in url:
                raise Exception("reranker not available")
            return {}

        with mock.patch.object(svc, "http", fake_http):
            svc.answer_question("anything", ["ws-1"])

        self.assertTrue(search_limits, "Qdrant search was never called")
        self.assertEqual(
            search_limits[0], svc.SEARCH_K,
            f"Qdrant search used limit={search_limits[0]}, expected SEARCH_K={svc.SEARCH_K}",
        )
        self.assertGreater(search_limits[0], 4,
                           "Search limit must be greater than the old hardcoded 4")

    def test_rerank_function_exists(self):
        """service.py must define a rerank() function."""
        svc = self.svc
        self.assertTrue(hasattr(svc, "rerank"),
                        "service.py must define rerank() — BUG-07 not fixed")
        self.assertTrue(callable(svc.rerank))

    def test_rerank_called_with_wider_pool(self):
        """rerank() receives the full wider pool (not just top-5) for scoring."""
        svc = self.svc
        import uuid
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))
        rerank_calls = []

        # Provide 8 hits (more than old limit=4); rerank should see all 8.
        fake_hits = [
            {"score": 0.9 - i * 0.05, "payload": {"file": f"doc{i}.txt", "text": f"content {i}"}}
            for i in range(8)
        ]

        def fake_http(url, body=None, method=None):
            if meta_id in url:
                return {"result": {"payload": {"_meta": True,
                                               "model": "text-embedding-bge-m3",
                                               "dim": 8}}}
            if "/v1/embeddings" in url:
                return {"data": [{"embedding": [0.1] * 8}]}
            if "/points/search" in url:
                return {"result": fake_hits}
            if "/v1/rerank" in url:
                raise Exception("reranker not available (expected degradation)")
            if "/v1/chat/completions" in url:
                return {"choices": [{"message": {"content": "answer"}}]}
            return {}

        original_rerank = svc.rerank

        def capturing_rerank(question, candidates):
            rerank_calls.append(len(candidates))
            return original_rerank(question, candidates)

        with mock.patch.object(svc, "http", fake_http), \
             mock.patch.object(svc, "rerank", capturing_rerank):
            svc.answer_question("query", ["ws-1"])

        self.assertTrue(rerank_calls, "rerank() was never called")
        self.assertGreater(
            rerank_calls[0], 4,
            f"rerank() received only {rerank_calls[0]} candidates — "
            f"must receive the full wider pool (> old limit of 4)",
        )

    def test_rerank_degrades_gracefully_when_endpoint_unavailable(self):
        """When /v1/rerank fails, answer_question must still return a result
        (degraded to raw scores), not raise."""
        svc = self.svc
        import uuid
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))

        def fake_http(url, body=None, method=None):
            if meta_id in url:
                return {"result": {"payload": {"_meta": True,
                                               "model": "text-embedding-bge-m3",
                                               "dim": 8}}}
            if "/v1/embeddings" in url:
                return {"data": [{"embedding": [0.1] * 8}]}
            if "/points/search" in url:
                return {"result": [
                    {"score": 0.9, "payload": {"file": "a.txt", "text": "good content"}}
                ]}
            if "/v1/rerank" in url:
                raise Exception("Unexpected endpoint or method. (POST /v1/rerank)")
            if "/v1/chat/completions" in url:
                return {"choices": [{"message": {"content": "answer from raw scores"}}]}
            return {}

        with mock.patch.object(svc, "http", fake_http):
            result = svc.answer_question("question", ["ws-1"])

        self.assertNotIn("error", result.get("answer", "").lower(),
                         "Reranker degradation must not surface as an error to the user")
        self.assertIn("a.txt", result.get("sources", []),
                      "Result must still contain the relevant document on degraded path")

    def test_rerank_parameters_configurable(self):
        """SEARCH_K and RERANK_TOP_N must be named constants in service.py."""
        svc = self.svc
        self.assertTrue(hasattr(svc, "SEARCH_K"), "SEARCH_K constant must exist")
        self.assertTrue(hasattr(svc, "RERANK_TOP_N"), "RERANK_TOP_N constant must exist")
        self.assertIsInstance(svc.SEARCH_K, int)
        self.assertIsInstance(svc.RERANK_TOP_N, int)

    def test_reranker_model_env_override(self):
        """KV_RERANKER_MODEL env var controls which reranker model is tried."""
        import importlib
        with mock.patch.dict(os.environ, {"KV_RERANKER_MODEL": "custom-reranker"}):
            spec = importlib.util.spec_from_file_location("svc_p2_rr_env", _SVC_PATH)
            mod = importlib.util.module_from_spec(spec)
            try:
                spec.loader.exec_module(mod)
            except Exception:
                pass
        self.assertEqual(mod.RERANKER_MODEL, "custom-reranker",
                         "RERANKER_MODEL must read from KV_RERANKER_MODEL env")


# ===========================================================================
# entrypoint
# ===========================================================================

if __name__ == "__main__":
    unittest.main(verbosity=2)

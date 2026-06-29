#!/usr/bin/env python3
"""KnowVault search service — the Searcher worker as an HTTP backend.

The node (which owns permissions and journaling) POSTs /search with
{question, scope_ids}; this service does the heavy, untrusted work: embed the
question, vector-search ONLY the given workspace collections, answer with the
chat model over retrieved context. It never decides who may see what — that
boundary already happened in the node.

BUG-01: the embedding model is NOT configured here independently.  It is read
from the sentinel point that the indexer wrote into each Qdrant collection —
by construction, the same model that built the index is used for every query.

Env: KV_QDRANT, KV_LM, KV_CHAT_MODEL, KV_LISTEN (default :8200)
Zero dependencies: stdlib only.
"""
import json
import os
import uuid
import urllib.request
import urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

QDRANT = os.environ.get("KV_QDRANT", "http://192.168.1.4:6333")
LM = os.environ.get("KV_LM", "http://localhost:1234")
# Use the small model that fits in memory alongside the embedder + reranker.
CHAT_MODEL = os.environ.get("KV_CHAT_MODEL", "google/gemma-4-e4b")
LISTEN = os.environ.get("KV_LISTEN", "127.0.0.1:8200")

# BUG-05: relevance threshold — hits below this score are discarded before LLM.
SCORE_THRESHOLD = float(os.environ.get("KV_SCORE_THRESHOLD", "0.30"))

# BUG-07: wide candidate pool per workspace, optional reranker, final top-N.
SEARCH_K = int(os.environ.get("KV_SEARCH_K", "20"))        # candidates per workspace
RERANK_TOP_N = int(os.environ.get("KV_RERANK_TOP_N", "5")) # hits sent to LLM
RERANKER_MODEL = os.environ.get("KV_RERANKER_MODEL", "qwen3-reranker-0.6b")

# BUG-07 degraded: LM Studio does not expose /v1/rerank — flag it once on first
# failure and skip all subsequent attempts. Avoids ERROR spam on every search.
# Reset requires a worker restart (acceptable — endpoint appears via new LM Studio).
_rerank_unavailable = False

# Sentinel UUID shared with the indexer — must stay in sync with _META_ID there.
_META_ID = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))

# BUG-13: per-model embedding format table (mirrored from indexer.py).
# Source of truth for the active model: sentinel point in the collection (BUG-01).
# Symmetric models (bge family): same representation for query and passage — no prefix.
# Asymmetric e5 models: "query: " prefix on the question improves retrieval accuracy.
_EMBED_PREFIX = {
    "text-embedding-bge-m3":                         ("", ""),
    "text-embedding-e5-small":                       ("query: ", "passage: "),
    "text-embedding-e5-base":                        ("query: ", "passage: "),
    "text-embedding-e5-large":                       ("query: ", "passage: "),
    "text-embedding-multilingual-e5-large-instruct": ("query: ", "passage: "),
    "text-embedding-e5-mistral-7b-instruct":         ("Instruct: Retrieve relevant documents\nQuery: ", "passage: "),
}


def _fmt_query(model, text):
    """Apply query prefix for the given model; no-op for symmetric models (e.g. bge-m3)."""
    prefix = _EMBED_PREFIX.get(model, ("", ""))[0]
    return (prefix + text) if prefix else text


def http(url, body=None, method=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=180) as resp:
        return json.load(resp)


def get_collection_model(collection):
    """BUG-01: read the embedding model from the sentinel point the indexer
    stored in this collection.  Returns None if the collection is empty or
    has not been indexed yet."""
    try:
        r = http(f"{QDRANT}/collections/{collection}/points/{_META_ID}")
        pl = (r.get("result") or {}).get("payload") or {}
        if pl.get("_meta"):
            return pl.get("model")
    except urllib.error.HTTPError:
        pass
    return None


def rerank(question, candidates):
    """BUG-07: attempt cross-encoder reranking via LM Studio /v1/rerank.

    LM Studio (as of the version used during Phase-2 development) does NOT
    expose a /v1/rerank endpoint — it returns
    {"error": "Unexpected endpoint or method. (POST /v1/rerank)"} and the
    qwen3-reranker-0.6b model is downloaded but not served via any
    embeddings or chat endpoint either.

    On the first failure _rerank_unavailable is set to True and all subsequent
    calls return immediately without touching the network — no ERROR spam.
    If a future LM Studio version adds /v1/rerank, restart the worker to try again.

    Returns: (reranked_candidates, used_reranker: bool)
      reranked_candidates — list of (score, payload) sorted by relevance desc
      used_reranker       — True if the reranker endpoint actually scored them
    """
    global _rerank_unavailable
    if not candidates or _rerank_unavailable:
        return candidates, False
    docs = [p["text"] for _s, p in candidates]
    try:
        resp = http(LM + "/v1/rerank", body={
            "model": RERANKER_MODEL,
            "query": question,
            "documents": docs,
            "top_n": len(docs),
            "return_documents": False,
        }, method="POST")
        results = resp.get("results") or []
        if not results:
            return candidates, False
        # results: [{"index": i, "relevance_score": f}, …]
        ranked = sorted(results, key=lambda r: -r["relevance_score"])
        reranked = [(r["relevance_score"], candidates[r["index"]][1]) for r in ranked]
        return reranked, True
    except Exception:
        # Endpoint absent — flag once; degrade to cosine order permanently.
        _rerank_unavailable = True
        return candidates, False


def answer_question(question, scope_ids):
    # BUG-01: derive the embedding model from the collection itself — not from
    # an independent env var.  VaultSettings is a singleton, so all workspace
    # collections share the same model; we take the first one we can read.
    embed_model = None
    for ws_id in scope_ids:
        embed_model = get_collection_model(f"kv_{ws_id}")
        if embed_model:
            break
    if not embed_model:
        return {"answer": "No indexed content found — run an index first.", "sources": []}

    # BUG-13: apply query prefix for asymmetric models (no-op for bge-m3 and unknown models)
    qvec = http(LM + "/v1/embeddings", {"model": embed_model,
                                         "input": [_fmt_query(embed_model, question)]})["data"][0]["embedding"]

    # BUG-07: wider candidate pool (SEARCH_K per workspace, was hardcoded 4).
    hits = []
    for ws_id in scope_ids:
        col = f"kv_{ws_id}"
        try:
            res = http(f"{QDRANT}/collections/{col}/points/search", method="POST",
                       body={"vector": qvec, "limit": SEARCH_K, "with_payload": True,
                             # exclude the sentinel meta point from results
                             "filter": {"must_not": [{"key": "_meta",
                                                      "match": {"value": True}}]}})
            for p in res.get("result", []):
                hits.append((p["score"], p["payload"]))
        except urllib.error.HTTPError:
            continue
    hits.sort(key=lambda h: -h[0])

    # BUG-07: rerank the wider pool; falls back to raw scores if reranker absent.
    hits, _used_reranker = rerank(question, hits)

    # BUG-05: discard hits below the relevance threshold; never fabricate from noise.
    above_threshold = [(s, p) for s, p in hits if s >= SCORE_THRESHOLD]
    top = [p for _s, p in above_threshold[:RERANK_TOP_N]]
    if not top:
        return {
            "answer": (
                "No relevant content found in the indexed documents — "
                "the question may be outside the scope of the indexed material."
            ),
            "sources": [],
        }

    context = "\n\n---\n\n".join(f"[{p['file']}]\n{p['text']}" for p in top)
    reply = http(LM + "/v1/chat/completions", {
        "model": CHAT_MODEL,
        "messages": [
            {"role": "system", "content":
             "You are a company document search assistant. Answer briefly and only based on the provided context. "
             "If the answer is not in the context, say so."},
            {"role": "user", "content": f"Context:\n{context}\n\nQuestion: {question}"},
        ],
        "temperature": 0.1,
    })["choices"][0]["message"]["content"]
    return {"answer": reply, "sources": sorted({p["file"] for p in top})}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        if self.path != "/search":
            self.send_error(404)
            return
        try:
            payload = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))))
            result = answer_question(payload["question"], payload.get("scope_ids", []))
            body = json.dumps(result, ensure_ascii=False).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(body)
        except Exception as e:  # noqa: BLE001
            # BUG-10: return HTTP 500 with a distinct error flag so the node can tell
            # a search failure apart from a valid empty result (results: 0, sources: []).
            # HTTP 200 + "Search error: …" in the answer body was indistinguishable
            # from an honest "nothing found" response in node logs.
            body = json.dumps({"error": True, "message": f"Search error: {e}",
                               "sources": []}).encode()
            self.send_response(500)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(body)


def main():
    host, port = LISTEN.rsplit(":", 1)
    srv = ThreadingHTTPServer((host, int(port)), Handler)
    print(f"knowvault search service on {LISTEN} -> qdrant {QDRANT}, chat {CHAT_MODEL}")
    srv.serve_forever()


if __name__ == "__main__":
    main()

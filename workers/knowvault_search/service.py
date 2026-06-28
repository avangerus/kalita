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

# Sentinel UUID shared with the indexer — must stay in sync with _META_ID there.
_META_ID = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))


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

    qvec = http(LM + "/v1/embeddings", {"model": embed_model, "input": [question]})["data"][0]["embedding"]
    hits = []
    for ws_id in scope_ids:
        col = f"kv_{ws_id}"
        try:
            res = http(f"{QDRANT}/collections/{col}/points/search", method="POST",
                       body={"vector": qvec, "limit": 4, "with_payload": True,
                             # exclude the sentinel meta point from results
                             "filter": {"must_not": [{"key": "_meta",
                                                      "match": {"value": True}}]}})
            for p in res.get("result", []):
                hits.append((p["score"], p["payload"]))
        except urllib.error.HTTPError:
            continue
    hits.sort(key=lambda h: -h[0])
    top = [p for _s, p in hits[:5]]
    if not top:
        return {"answer": "Nothing found in indexed documents.", "sources": []}

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
        except Exception as e:  # noqa: BLE001 — return the error to the node
            body = json.dumps({"answer": f"Search error: {e}", "sources": []}).encode()
            self.send_response(200)
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

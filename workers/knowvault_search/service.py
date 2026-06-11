#!/usr/bin/env python3
"""KnowVault search service — the Searcher worker as an HTTP backend.

The node (which owns permissions and journaling) POSTs /search with
{question, scope_ids}; this service does the heavy, untrusted work: embed the
question, vector-search ONLY the given workspace collections, answer with the
chat model over retrieved context. It never decides who may see what — that
boundary already happened in the node.

Env: KV_QDRANT, KV_LM, KV_CHAT_MODEL, KV_EMBED_MODEL, KV_LISTEN (default :8200)
Zero dependencies: stdlib only.
"""
import json
import os
import urllib.request
import urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

QDRANT = os.environ.get("KV_QDRANT", "http://192.168.1.4:6333")
LM = os.environ.get("KV_LM", "http://localhost:1234")
CHAT_MODEL = os.environ.get("KV_CHAT_MODEL", "openai/gpt-oss-20b")
EMBED_MODEL = os.environ.get("KV_EMBED_MODEL", "text-embedding-nomic-embed-text-v1.5")
LISTEN = os.environ.get("KV_LISTEN", "127.0.0.1:8200")


def http(url, body=None, method=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=180) as resp:
        return json.load(resp)


def answer_question(question, scope_ids):
    qvec = http(LM + "/v1/embeddings", {"model": EMBED_MODEL, "input": [question]})["data"][0]["embedding"]
    hits = []
    for ws_id in scope_ids:
        col = f"kv_{ws_id}"
        try:
            res = http(f"{QDRANT}/collections/{col}/points/search", method="POST",
                       body={"vector": qvec, "limit": 4, "with_payload": True})
            for p in res.get("result", []):
                hits.append((p["score"], p["payload"]))
        except urllib.error.HTTPError:
            continue
    hits.sort(key=lambda h: -h[0])
    top = [p for _s, p in hits[:5]]
    if not top:
        return {"answer": "В проиндексированных документах ничего не найдено.", "sources": []}

    context = "\n\n---\n\n".join(f"[{p['file']}]\n{p['text']}" for p in top)
    reply = http(LM + "/v1/chat/completions", {
        "model": CHAT_MODEL,
        "messages": [
            {"role": "system", "content":
             "Ты — поиск по документам компании. Отвечай кратко и только по приведённому контексту. "
             "Если ответа в контексте нет — так и скажи."},
            {"role": "user", "content": f"Контекст:\n{context}\n\nВопрос: {question}"},
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
            body = json.dumps({"answer": f"Ошибка поиска: {e}", "sources": []}).encode()
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

#!/usr/bin/env python3
"""KnowVault Q&A — the Searcher worker: ask a question over perimeter documents.

Flow: embed the question (LM Studio) -> vector search across the Qdrant
collections of every workspace this actor may read -> answer with the chat
model using retrieved context -> journal the query as a SearchQuery record.

Usage:  py ask.py "Какая сумма в договоре с Вектором?"
Env:    KALITA_URL, KALITA_TOKEN (role Searcher), KV_QDRANT, KV_LM, KV_CHAT_MODEL
"""
import json
import os
import sys
import urllib.request

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
TOKEN = os.environ.get("KALITA_TOKEN") or sys.exit("KALITA_TOKEN is required")
QDRANT = os.environ.get("KV_QDRANT", "http://192.168.1.4:6333")
LM = os.environ.get("KV_LM", "http://localhost:1234")
CHAT_MODEL = os.environ.get("KV_CHAT_MODEL", "openai/gpt-oss-20b")
EMBED_MODEL = os.environ.get("KV_EMBED_MODEL", "text-embedding-nomic-embed-text-v1.5")


def http(url, body=None, headers=None, method=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method,
                                 headers={"Content-Type": "application/json", **(headers or {})})
    with urllib.request.urlopen(req, timeout=180) as resp:
        return json.load(resp)


def tool(name, args=None):
    rpc = http(NODE + "/mcp", {"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                               "params": {"name": name, "arguments": args or {}}},
               {"Authorization": f"Bearer {TOKEN}"})
    result = rpc["result"]
    decoded = json.loads(result["content"][0]["text"]) if result.get("content") else {}
    if result.get("isError"):
        raise SystemExit(f"node error: {decoded}")
    return decoded


def main():
    if len(sys.argv) < 2:
        sys.exit('usage: py ask.py "ваш вопрос"')
    question = sys.argv[1]

    # permission boundary: only workspaces this actor can read exist for it
    workspaces = tool("query", {"entity": "Workspace"}).get("records") or []
    if not workspaces:
        sys.exit("no workspaces visible to this actor")

    qvec = http(LM + "/v1/embeddings", {"model": EMBED_MODEL, "input": [question]})["data"][0]["embedding"]

    hits = []
    for ws in workspaces:
        col = f"kv_{ws['id']}"
        try:
            res = http(f"{QDRANT}/collections/{col}/points/search", method="POST",
                       body={"vector": qvec, "limit": 4, "with_payload": True})
            for p in res.get("result", []):
                hits.append((p["score"], ws, p["payload"]))
        except urllib.error.HTTPError:
            continue  # workspace not indexed yet
    hits.sort(key=lambda h: -h[0])
    top = hits[:5]
    if not top:
        sys.exit("nothing indexed yet — point the indexer at a source first")

    context = "\n\n---\n\n".join(f"[{p['file']}]\n{p['text']}" for _s, _w, p in top)
    answer = http(LM + "/v1/chat/completions", {
        "model": CHAT_MODEL,
        "messages": [
            {"role": "system", "content":
             "Ты — поиск по документам компании. Отвечай кратко и только по приведённому контексту. "
             "Если ответа в контексте нет — так и скажи. В конце укажи файлы-источники."},
            {"role": "user", "content": f"Контекст:\n{context}\n\nВопрос: {question}"},
        ],
        "temperature": 0.1,
    })["choices"][0]["message"]["content"]

    # the query is journaled — "кто что искал" is a record like any other
    tool("create_record", {"entity": "SearchQuery", "basis": {"type": "human", "id": "searcher"},
                           "values": {"workspace": top[0][1]["id"], "query": question,
                                      "actor_role": "Searcher", "results": len(top)}})

    print(f"\nQ: {question}\n")
    print(answer)
    print("\nsources:", ", ".join(sorted({p["file"] for _s, _w, p in top})))


if __name__ == "__main__":
    main()

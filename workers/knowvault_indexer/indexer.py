#!/usr/bin/env python3
"""KnowVault indexer — a kalita worker agent (v2: real RAG ingest).

Pipeline per source: walk files -> extract text -> chunk -> embed (LM Studio,
model name comes from the VaultSettings singleton ON THE NODE — humans manage
it in the UI, every change is journaled) -> upsert to Qdrant (one collection
per workspace: kv_<workspace_id>).

Env (worker-level config, the secrets tier):
  KALITA_URL   (default http://127.0.0.1:8095)
  KALITA_TOKEN (required, role Indexer)
  KV_QDRANT    (default http://192.168.1.4:6333)
  KV_LM        (default http://localhost:1234)

Zero dependencies: stdlib only.
"""
import json
import os
import sys
import time
import urllib.request
import urllib.error
import uuid
from datetime import datetime, timezone

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
TOKEN = os.environ.get("KALITA_TOKEN") or sys.exit("KALITA_TOKEN is required")
QDRANT = os.environ.get("KV_QDRANT", "http://192.168.1.4:6333")
LM = os.environ.get("KV_LM", "http://localhost:1234")

TEXT_EXT = {".txt", ".md", ".rst", ".csv", ".log"}


class ToolError(Exception):
    def __init__(self, payload):
        super().__init__(payload.get("message", str(payload)))
        self.payload = payload


def http(url, body=None, headers=None, method=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method,
                                 headers={"Content-Type": "application/json", **(headers or {})})
    with urllib.request.urlopen(req, timeout=120) as resp:
        return json.load(resp)


def tool(name, args=None):
    rpc = http(NODE + "/mcp", {
        "jsonrpc": "2.0", "id": 1, "method": "tools/call",
        "params": {"name": name, "arguments": args or {}},
    }, {"Authorization": f"Bearer {TOKEN}"})
    result = rpc["result"]
    decoded = json.loads(result["content"][0]["text"]) if result.get("content") else {}
    if result.get("isError"):
        raise ToolError(decoded)
    return decoded


def settings():
    """Business settings live on the node, not in the worker."""
    rows = tool("query", {"entity": "VaultSettings"}).get("records") or []
    if rows:
        v = rows[0]["values"]
        return v.get("embedding_model", "text-embedding-nomic-embed-text-v1.5"), int(v.get("chunk_size") or 512)
    return "text-embedding-nomic-embed-text-v1.5", 512


def chunk(text, size):
    """Paragraph-aware chunking with a hard size cap."""
    out, cur = [], ""
    for para in text.split("\n\n"):
        if len(cur) + len(para) + 2 <= size:
            cur = (cur + "\n\n" + para).strip()
            continue
        if cur:
            out.append(cur)
        while len(para) > size:
            out.append(para[:size])
            para = para[size:]
        cur = para
    if cur.strip():
        out.append(cur.strip())
    return out


def embed(model, texts):
    resp = http(LM + "/v1/embeddings", {"model": model, "input": texts})
    return [d["embedding"] for d in resp["data"]]


def ensure_collection(name, dim):
    try:
        http(f"{QDRANT}/collections/{name}", method="PUT",
             body={"vectors": {"size": dim, "distance": "Cosine"}})
    except urllib.error.HTTPError as e:
        if e.code != 409:  # already exists
            raise


def index_source(task):
    tid, sid = task["id"], task["record_id"]
    basis = {"type": "task", "id": tid}
    src = tool("get_record", {"entity": "Source", "id": sid})
    status = src["values"].get("status")
    if status in ("New", "Failed"):
        tool("act", {"entity": "Source", "id": sid,
                     "action": "start_index" if status == "New" else "retry_index", "basis": basis})
        status = "Indexing"
    if status != "Indexing":
        tool("complete_task", {"task_id": tid, "result": f"stale: source is {status}"})
        return

    path = src["values"].get("path", "")
    workspace = src["values"].get("workspace", "")
    collection = f"kv_{workspace}"
    model, chunk_size = settings()

    try:
        docs, chunks_total = 0, 0
        for root, _dirs, files in os.walk(path):
            for name in files:
                ext = os.path.splitext(name)[1].lower()
                if ext not in TEXT_EXT:
                    continue
                fp = os.path.join(root, name)
                try:
                    with open(fp, encoding="utf-8", errors="ignore") as f:
                        text = f.read()
                except OSError:
                    continue
                pieces = chunk(text, chunk_size)
                if not pieces:
                    continue
                vectors = embed(model, pieces)
                ensure_collection(collection, len(vectors[0]))
                points = [{
                    # stable ids: re-indexing overwrites instead of duplicating
                    "id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"{sid}|{fp}|{i}")),
                    "vector": vec,
                    "payload": {"source": sid, "file": name, "path": fp, "text": piece},
                } for i, (piece, vec) in enumerate(zip(pieces, vectors))]
                http(f"{QDRANT}/collections/{collection}/points?wait=true", method="PUT",
                     body={"points": points})
                docs += 1
                chunks_total += len(points)

        tool("report_progress", {"task_id": tid,
             "note": f"{docs} docs -> {chunks_total} chunks -> {collection} (model {model})"})
        tool("update_record", {"entity": "Source", "id": sid, "basis": basis, "values": {
            "documents": docs,
            "last_indexed": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }})
        tool("act", {"entity": "Source", "id": sid, "action": "finish_index", "basis": basis})
        tool("complete_task", {"task_id": tid, "result": f"indexed {docs} docs, {chunks_total} chunks"})
        print(f"[ok] {sid[:8]}: {docs} docs, {chunks_total} chunks -> {collection}")
    except (OSError, urllib.error.URLError, urllib.error.HTTPError) as e:
        tool("act", {"entity": "Source", "id": sid, "action": "fail_index", "basis": basis})
        tool("fail_task", {"task_id": tid, "reason": str(e)})
        print(f"[fail] {sid[:8]}: {e}")


def main():
    me = tool("describe_system")
    print(f"indexer up: pack={me.get('pack')} role={me.get('your_role')} id={me.get('your_id')}")
    while True:
        try:
            tasks = tool("wait_for_task", {"timeout_sec": 25}).get("tasks") or []
            for task in tasks:
                try:
                    tool("take_task", {"task_id": task["id"]})
                except ToolError:
                    continue
                if task.get("entity") == "Source":
                    index_source(task)
                else:
                    tool("complete_task", {"task_id": task["id"], "result": "not an indexer task"})
        except (ToolError, urllib.error.URLError) as e:
            print(f"[warn] {e}; retrying in 5s")
            time.sleep(5)


if __name__ == "__main__":
    main()

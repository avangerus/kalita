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

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from bootstrap import get_token  # noqa: E402

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
TOKEN = get_token()
QDRANT = os.environ.get("KV_QDRANT", "http://192.168.1.4:6333")
LM = os.environ.get("KV_LM", "http://localhost:1234")

# What counts as indexable text — prose, plus source code and config so a whole
# repository can be crawled and questioned. PDFs/Office docs need an extractor
# (a separate step; this worker stays stdlib-only).
TEXT_EXT = {
    ".txt", ".md", ".rst", ".csv", ".log", ".adoc", ".org", ".tex",
    ".py", ".go", ".js", ".ts", ".jsx", ".tsx", ".java", ".kt", ".scala",
    ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".rb", ".php", ".rs", ".swift",
    ".sh", ".bash", ".ps1", ".sql", ".r", ".lua", ".dart", ".ex", ".exs",
    ".json", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".env",
    ".xml", ".html", ".htm", ".css", ".scss", ".proto", ".gradle", ".dsl",
}
# Directories never worth crawling — VCS, dependencies, build output, caches.
SKIP_DIRS = {".git", ".hg", ".svn", "node_modules", "vendor", "dist", "build",
             "out", "target", "__pycache__", ".venv", "venv", ".idea", ".vscode",
             ".next", ".cache", "coverage", "bin", "obj"}
MAX_FILE_BYTES = 1_000_000  # skip anything bigger — generated, minified, or binary


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


def http_bytes(url):
    """Download raw bytes (uploaded document) with the worker's bearer token."""
    req = urllib.request.Request(url, headers={"Authorization": f"Bearer {TOKEN}"})
    with urllib.request.urlopen(req, timeout=120) as resp:
        return resp.read()


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

    workspace = src["values"].get("workspace", "")
    collection = f"kv_{workspace}"
    model, chunk_size = settings()

    # an indexable unit is (filename, text): either a single uploaded document
    # or every text file under a crawled path
    def units():
        doc = src["values"].get("document")
        if src["values"].get("kind") == "Upload" and doc:
            raw = http_bytes(f"{NODE}/api/files/{doc['hash']}")
            yield doc.get("name", "document"), raw.decode("utf-8", "ignore")
            return
        base = src["values"].get("path", "")
        for root, dirs, files in os.walk(base):
            dirs[:] = [d for d in dirs if d not in SKIP_DIRS]  # prune noise in place
            for name in files:
                if os.path.splitext(name)[1].lower() not in TEXT_EXT:
                    continue
                full = os.path.join(root, name)
                try:
                    if os.path.getsize(full) > MAX_FILE_BYTES:
                        continue
                    with open(full, encoding="utf-8", errors="ignore") as f:
                        # the relative path is the answer to "where is it?"
                        yield os.path.relpath(full, base).replace("\\", "/"), f.read()
                except OSError:
                    continue

    try:
        docs, chunks_total = 0, 0
        for fname, text in units():
            pieces = chunk(text, chunk_size)
            if not pieces:
                continue
            print(f"  {fname}: {len(pieces)} chunks — embedding…", flush=True)
            # embed in batches so a big file makes steady progress and one slow
            # request can't stall the whole source
            ensure_collection_done = False
            for off in range(0, len(pieces), 32):
                batch = pieces[off:off + 32]
                vectors = embed(model, batch)
                if not ensure_collection_done:
                    ensure_collection(collection, len(vectors[0]))
                    ensure_collection_done = True
                points = [{
                    "id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"{sid}|{fname}|{off + i}")),
                    "vector": vec,
                    "payload": {"source": sid, "file": fname, "text": piece},
                } for i, (piece, vec) in enumerate(zip(batch, vectors))]
                http(f"{QDRANT}/collections/{collection}/points?wait=true", method="PUT",
                     body={"points": points})
                chunks_total += len(points)
            docs += 1
            print(f"  {fname}: done ({chunks_total} total)", flush=True)

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

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

sys.path.insert(0, os.path.dirname(__file__))            # local extract module
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from bootstrap import get_token  # noqa: E402
import extract  # noqa: E402  (text extraction for the document zoo)

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
TOKEN = get_token()
QDRANT = os.environ.get("KV_QDRANT", "http://192.168.1.4:6333")
LM = os.environ.get("KV_LM", "http://localhost:1234")

# Anything extract.py can turn into text: prose, source code, config, AND
# docx/xlsx/pptx/rtf/pdf — so a folder of real documents indexes, not just .txt.
TEXT_EXT = extract.SUPPORTED_EXT
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


def indexed_fingerprints(collection, sid):
    """{file: fingerprint} already in the index for this source — so re-indexing
    skips unchanged files and only re-embeds what you actually edited. The index
    is the record of what's indexed; KnowVault keeps no copy of the files."""
    out, offset = {}, None
    while True:
        body = {"filter": {"must": [{"key": "source", "match": {"value": sid}}]},
                "with_payload": ["file", "fp"], "limit": 512}
        if offset:
            body["offset"] = offset
        try:
            r = http(f"{QDRANT}/collections/{collection}/points/scroll", method="POST", body=body)
        except urllib.error.HTTPError as e:
            if e.code == 404:  # collection not created yet — nothing indexed
                return {}
            raise
        result = r.get("result", {})
        for p in result.get("points", []):
            pl = p.get("payload", {})
            if pl.get("file"):
                out[pl["file"]] = pl.get("fp")
        offset = result.get("next_page_offset")
        if not offset:
            return out


def drop_file(collection, sid, fname):
    """Remove a file's chunks from the index (it changed or was deleted)."""
    http(f"{QDRANT}/collections/{collection}/points/delete?wait=true", method="POST",
         body={"filter": {"must": [{"key": "source", "match": {"value": sid}},
                                   {"key": "file", "match": {"value": fname}}]}})


def sync_source(sid, src, basis, tid):
    """The pure incremental ingest step: skip unchanged files, re-embed changed,
    drop deleted. No workflow transitions — so it serves BOTH first-index (inside
    the Indexing->Indexed flow) and resync (the source stays Indexed). Returns
    the human note; raises on I/O failure for the caller to translate."""
    workspace = src["values"].get("workspace", "")
    collection = f"kv_{workspace}"
    model, chunk_size = settings()

    # an indexable unit is (relpath, fingerprint, loader): a single uploaded
    # document, or every supported file under a crawled path. The fingerprint
    # (mtime:size) is read WITHOUT opening the file, so unchanged files are
    # skipped cheaply; the loader pulls the text only when we actually re-index.
    def units():
        doc = src["values"].get("document")
        if src["values"].get("kind") == "Upload" and doc:
            yield doc.get("name", "document"), doc.get("hash", ""), \
                lambda: http_bytes(f"{NODE}/api/files/{doc['hash']}").decode("utf-8", "ignore")
            return
        base = src["values"].get("path", "")
        for root, dirs, files in os.walk(base):
            dirs[:] = [d for d in dirs if d not in SKIP_DIRS]  # prune noise in place
            for name in files:
                if os.path.splitext(name)[1].lower() not in TEXT_EXT:
                    continue
                full = os.path.join(root, name)
                try:
                    size = os.path.getsize(full)
                    if size > MAX_FILE_BYTES:
                        continue
                    fp = f"{int(os.path.getmtime(full))}:{size}"
                except OSError:
                    continue
                rel = os.path.relpath(full, base).replace("\\", "/")  # "where is it?"
                yield rel, fp, (lambda p=full: extract.extract_text(p))

    # what's already in the index: skip unchanged, re-embed changed, drop gone
    prior = indexed_fingerprints(collection, sid)
    seen, changed = set(), 0
    for fname, fp, load in units():
        seen.add(fname)
        if prior.get(fname) == fp:
            continue  # unchanged since last index — leave it
        text = load()
        if not text or not text.strip():
            continue
        pieces = chunk(text, chunk_size)
        if not pieces:
            continue
        if fname in prior:
            drop_file(collection, sid, fname)  # changed — replace its chunks
        print(f"  {fname}: {len(pieces)} chunks — embedding…", flush=True)
        for off in range(0, len(pieces), 32):
            batch = pieces[off:off + 32]
            vectors = embed(model, batch)
            ensure_collection(collection, len(vectors[0]))
            points = [{
                "id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"{sid}|{fname}|{off + i}")),
                "vector": vec,
                "payload": {"source": sid, "file": fname, "fp": fp, "text": piece},
            } for i, (piece, vec) in enumerate(zip(batch, vectors))]
            http(f"{QDRANT}/collections/{collection}/points?wait=true", method="PUT",
                 body={"points": points})
        changed += 1
    # files that vanished from the source drop out of the index too
    removed = 0
    for fname in set(prior) - seen:
        drop_file(collection, sid, fname)
        removed += 1

    note = f"{len(seen)} files ({changed} re-embedded, {removed} removed) -> {collection}"
    tool("report_progress", {"task_id": tid, "note": note})
    tool("update_record", {"entity": "Source", "id": sid, "basis": basis, "values": {
        "documents": len(seen),
        "last_indexed": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    }})
    return note


def index_source(task):
    """First index / retry: drive New|Failed -> Indexing -> Indexed around the
    incremental sync."""
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
    try:
        note = sync_source(sid, src, basis, tid)
        tool("act", {"entity": "Source", "id": sid, "action": "finish_index", "basis": basis})
        tool("complete_task", {"task_id": tid, "result": note})
        print(f"[ok] {sid[:8]}: {note}")
    except (OSError, urllib.error.URLError, urllib.error.HTTPError) as e:
        tool("act", {"entity": "Source", "id": sid, "action": "fail_index", "basis": basis})
        tool("fail_task", {"task_id": tid, "reason": str(e)})
        print(f"[fail] {sid[:8]}: {e}")


def resync_source(task):
    """Scheduled freshness pass: the `on schedule every Nm … agent Indexer:
    resync()` automation fires this per indexed source. Re-syncs in place — the
    source stays Indexed; a transient failure fails the task but never knocks a
    good source into Failed, so the next schedule tick simply retries."""
    tid, sid = task["id"], task["record_id"]
    basis = {"type": "task", "id": tid}
    src = tool("get_record", {"entity": "Source", "id": sid})
    status = src["values"].get("status")
    if status != "Indexed":
        tool("complete_task", {"task_id": tid, "result": f"skip resync: source is {status}"})
        return
    try:
        note = sync_source(sid, src, basis, tid)
        tool("complete_task", {"task_id": tid, "result": f"resync: {note}"})
        print(f"[resync] {sid[:8]}: {note}")
    except (OSError, urllib.error.URLError, urllib.error.HTTPError) as e:
        tool("fail_task", {"task_id": tid, "reason": str(e)})
        print(f"[resync-fail] {sid[:8]}: {e}")


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
                    if task.get("action") == "resync":
                        resync_source(task)
                    else:
                        index_source(task)
                else:
                    tool("complete_task", {"task_id": task["id"], "result": "not an indexer task"})
        except (ToolError, urllib.error.URLError) as e:
            print(f"[warn] {e}; retrying in 5s")
            time.sleep(5)


if __name__ == "__main__":
    main()

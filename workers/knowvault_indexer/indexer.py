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
import hashlib
import json
import os
import sys
import tempfile
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
        return v.get("embedding_model", "text-embedding-bge-m3"), int(v.get("chunk_size") or 512)
    return "text-embedding-bge-m3", 512


def _has_alnum(s):
    """True if *s* carries any real content (letters/digits, Unicode-aware)."""
    return any(c.isalnum() for c in s)


def _is_noise_line(line):
    """A line that is pure structure/formatting — empty, or only table-drawing
    glyphs and punctuation (`+---+`, `|---|---|`, `=====`, `| | |`, rules).
    Such a line must NEVER become a chunk on its own (BUG-04 regression)."""
    return not _has_alnum(line)


def _looks_like_table(para):
    """Heuristic: a multi-line block that is mostly pipe-delimited rows and/or
    ruler/separator lines is a table — chunk it as a coherent unit, not row-by-row."""
    lines = [ln for ln in para.split("\n") if ln.strip()]
    if len(lines) < 2:
        return False
    tableish = sum(1 for ln in lines if ("|" in ln) or _is_noise_line(ln))
    return tableish >= max(2, (len(lines) * 3 + 4) // 5)  # ~60% of lines


def chunk(text, size):
    """BUG-04: structure-aware chunking that keeps tables coherent.

    Strategy:
      * Primary split is by paragraph (\\n\\n); paragraphs are coalesced up to
        *size* so a small table stays whole and keeps any adjacent caption.
      * A paragraph that LOOKS LIKE A TABLE and exceeds *size* is split by
        ROW GROUPS, never by single row and never by formatting: its header
        (first content row + the separator that follows) is repeated on each
        group so every chunk is a self-describing, coherent table fragment.
      * A non-table paragraph that exceeds *size* is split on line boundaries,
        and an over-long line on the last word boundary (only a single
        space-less token is ever hard-cut — prevents an infinite loop).

    Invariants (the contract):
      * No chunk is cut in the middle of a word or a line by a raw char index.
      * No chunk consists solely of table markup / punctuation / whitespace —
        a formatting-only line is always carried with real content or dropped,
        never emitted alone.
    """
    out = []
    cur = ""

    def _emit(s):
        s = s.strip()
        if s and _has_alnum(s):
            out.append(s)

    def _flush():
        nonlocal cur
        _emit(cur)
        cur = ""

    def _split_table(para):
        """Row-group split for an oversized table, header repeated per group."""
        lines = para.split("\n")
        # Header = the first line plus any immediately-following separator lines
        # (e.g. a markdown `|---|` ruler), so each group is self-describing.
        hdr_end = 1
        while hdr_end < len(lines) and _is_noise_line(lines[hdr_end]):
            hdr_end += 1
        header_txt = "\n".join(lines[:hdr_end]).rstrip()
        body = lines[hdr_end:]
        if not body:
            _emit(para)
            return
        base = len(header_txt)
        group, glen = [], base

        def _emit_group():
            nonlocal group, glen
            if group:
                _emit(header_txt + "\n" + "\n".join(group))
                group, glen = [], base

        for line in body:
            add = 1 + len(line)
            group_has_content = any(_has_alnum(g) for g in group)
            # Flush only when the current group already holds real content AND
            # the incoming line is content too — so a separator/blank row is
            # always carried alongside the data rows, never isolated.
            if group and glen + add > size and group_has_content and _has_alnum(line):
                _emit_group()
                add = 1 + len(line)
            group.append(line)
            glen += add
        _emit_group()

    def _split_prose(para):
        """Line/word split for an oversized non-table paragraph."""
        nonlocal cur
        for line in para.split("\n"):
            if _is_noise_line(line):
                # Carry a stray rule line with the current text if it fits;
                # otherwise drop it — it must never stand alone.
                if cur and len(cur) + 1 + len(line) <= size:
                    cur = cur + "\n" + line
                continue
            while len(line) > size:
                cut = line.rfind(" ", 0, size)
                if cut <= 0:
                    cut = size  # space-less token — forced hard cut
                seg = line[:cut].rstrip()
                if seg:
                    if cur:
                        _flush()
                    _emit(seg)
                line = line[cut:].lstrip()
            if not line:
                continue
            if cur and len(cur) + 1 + len(line) > size:
                _flush()
            cur = (cur + "\n" + line) if cur else line

    for para in text.split("\n\n"):
        if not para.strip():
            continue
        # Happy path: paragraph fits alongside the current accumulator.
        if len(cur) + (2 if cur else 0) + len(para) <= size:
            cur = (cur + "\n\n" + para).strip() if cur else para.strip()
            continue
        # Fits on its own — flush and start fresh (keeps a small table whole).
        if len(para) <= size:
            _flush()
            cur = para.strip()
            continue
        # Oversized paragraph: route table vs prose.
        _flush()
        if _looks_like_table(para):
            _split_table(para)
        else:
            _split_prose(para)

    _flush()
    return out


def embed(model, texts):
    resp = http(LM + "/v1/embeddings", {"model": model, "input": texts})
    return [d["embedding"] for d in resp["data"]]


# BUG-01/02: sentinel point — a well-known UUID stored in every collection so
# both indexer and search worker always agree on which model built the index.
# The search worker reads this; it has no independent EMBED_MODEL config.
_META_ID = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))

# BUG-13: per-model embedding format table.
# Tuple: (query_prefix, passage_prefix).
# Symmetric models (bge family) are used the same way for both query and passage —
# no prefix needed; adding one degrades quality.
# Asymmetric e5 models require distinct "query: " / "passage: " prefixes to separate
# the roles of query and document; omitting them measurably reduces retrieval quality.
# Source of truth for the active model: sentinel point / VaultSettings (BUG-01).
_EMBED_PREFIX = {
    # symmetric — no prefix for either role
    "text-embedding-bge-m3":                         ("", ""),
    # asymmetric e5 — query/passage prefixes required
    "text-embedding-e5-small":                       ("query: ", "passage: "),
    "text-embedding-e5-base":                        ("query: ", "passage: "),
    "text-embedding-e5-large":                       ("query: ", "passage: "),
    "text-embedding-multilingual-e5-large-instruct": ("query: ", "passage: "),
    "text-embedding-e5-mistral-7b-instruct":         ("Instruct: Retrieve relevant documents\nQuery: ", "passage: "),
}


def _fmt_passage(model, text):
    """Apply passage prefix for the given model; no-op for symmetric models (e.g. bge-m3)."""
    prefix = _EMBED_PREFIX.get(model, ("", ""))[1]
    return (prefix + text) if prefix else text


def _fmt_query(model, text):
    """Apply query prefix for the given model; no-op for symmetric models (e.g. bge-m3)."""
    prefix = _EMBED_PREFIX.get(model, ("", ""))[0]
    return (prefix + text) if prefix else text


def _file_fp(path, use_content_hash=False):
    """Compute a fingerprint for *path*.

    Default (use_content_hash=False): mtime:size — O(1), zero I/O, sufficient
    for the common case where filesystem timestamps are reliable.

    With use_content_hash=True: SHA-256 of the file contents — detects edits
    that produce the same file size on the same second (e.g. in-place patching,
    test fixtures, or mounted filesystems with coarse timestamp resolution).
    Enable per-Source via ``use_content_hash=true`` in the Source record.
    """
    if use_content_hash:
        h = hashlib.sha256()
        with open(path, "rb") as fh:
            for blk in iter(lambda: fh.read(65536), b""):
                h.update(blk)
        return f"sha256:{h.hexdigest()}"
    return f"{int(os.path.getmtime(path))}:{os.path.getsize(path)}"


def get_collection_meta(name):
    """Return (model, dim) from the sentinel point, or (None, None) if absent."""
    try:
        r = http(f"{QDRANT}/collections/{name}/points/{_META_ID}")
        pl = (r.get("result") or {}).get("payload") or {}
        if pl.get("_meta"):
            return pl.get("model"), pl.get("dim")
    except urllib.error.HTTPError:
        pass
    return None, None


def upsert_collection_meta(name, model, dim):
    """Write (or refresh) the sentinel meta point that records model+dim."""
    http(f"{QDRANT}/collections/{name}/points?wait=true", method="PUT",
         body={"points": [{"id": _META_ID, "vector": [0.0] * dim,
                           "payload": {"_meta": True, "model": model, "dim": dim}}]})


def ensure_collection(name, dim, model):
    """Create collection if it doesn't exist; write/refresh the model sentinel.
    Called on the first batch of every file — idempotent."""
    try:
        http(f"{QDRANT}/collections/{name}", method="PUT",
             body={"vectors": {"size": dim, "distance": "Cosine"}})
        upsert_collection_meta(name, model, dim)
    except urllib.error.HTTPError as e:
        if e.code != 409:  # already exists
            raise
        # Collection exists: write sentinel if it's missing (upgrade path).
        existing_model, _ = get_collection_meta(name)
        if existing_model is None:
            upsert_collection_meta(name, model, dim)


def collection_dim(name):
    """The configured vector size of an existing collection, or None if it does
    not exist. Read from Qdrant's collection config — independent of the sentinel,
    so it works even for old collections that predate the sentinel."""
    try:
        r = http(f"{QDRANT}/collections/{name}")
    except urllib.error.HTTPError as e:
        if e.code == 404:
            return None
        raise
    vec = ((((r.get("result") or {}).get("config") or {}).get("params") or {}).get("vectors") or {})
    return vec.get("size")


def reconcile_collection(name, target_model, target_dim):
    """BUG-02: bring an existing collection in line with the TARGET model+dim
    BEFORE any vector is written — independent of force_rebuild.

    Drops (for a clean recreate by ensure_collection) when the existing collection
    differs from the target in EITHER model OR dimension. This covers:
      - model swap (sentinel model != target)              -> rebuild
      - dimension change after a model swap                -> rebuild
      - pre-sentinel collections (model unknown)           -> rebuild, because we
        cannot prove the old model matches; rebuilding is the only safe choice and
        guarantees no dim-mismatch upsert can occur.
    A non-existent collection is a no-op. Returns True iff it was dropped (the
    caller must then treat the index as empty)."""
    dim = collection_dim(name)
    if dim is None:
        return False  # nothing there yet — ensure_collection will create it fresh
    model, _ = get_collection_meta(name)
    if model == target_model and dim == target_dim:
        return False  # already consistent with the target
    try:
        http(f"{QDRANT}/collections/{name}", method="DELETE")
    except urllib.error.HTTPError as e:
        if e.code != 404:
            raise
    return True


def _extract_upload(url, name):
    """BUG-03: download an uploaded document and extract its text via the same
    extract.extract_text path used for crawled files.  Avoids raw .decode() of
    binary formats (docx, pdf, …) which produces garbage."""
    data = http_bytes(url)
    ext = os.path.splitext(name)[1].lower() or ".bin"
    fd, tmp = tempfile.mkstemp(suffix=ext)
    try:
        os.write(fd, data)
        os.close(fd)
        return extract.extract_text(tmp) or ""
    finally:
        try:
            os.unlink(tmp)
        except OSError:
            pass


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


def sync_source(sid, src, basis, tid, force_rebuild=False):
    """The pure incremental ingest step: skip unchanged files, re-embed changed,
    drop deleted. No workflow transitions — so it serves BOTH first-index (inside
    the Indexing->Indexed flow) and resync (the source stays Indexed). Returns
    the human note; raises on I/O failure for the caller to translate.

    force_rebuild=True ignores all cached fingerprints and re-embeds every file
    (used when the user explicitly triggers reindex). Regardless of force_rebuild,
    a collection whose model OR dimension does not match the target is always
    recreated first, so a dim-mismatch upsert can never happen."""
    workspace = src["values"].get("workspace", "")
    collection = f"kv_{workspace}"
    model, chunk_size = settings()

    # BUG-02: reconcile the collection with the TARGET model+dim BEFORE reading
    # fingerprints or writing any vector — UNCONDITIONALLY (force_rebuild does not
    # skip this; that was the live-acceptance defect). We learn the target dim by
    # probing the model once: a model swap changes the dim, and a dim mismatch on
    # upsert must be impossible. If the existing collection's model OR dim differs
    # (including pre-sentinel collections with unknown model), it is dropped here
    # and recreated fresh by ensure_collection at the correct dimension.
    target_dim = len(embed(model, ["probe"])[0])
    rebuilt = reconcile_collection(collection, model, target_dim)
    if rebuilt:
        force_rebuild = True  # collection was wiped — every file is new

    # BUG-09: per-source fingerprint strategy.
    # Default (use_content_hash=False): mtime:size — fast, zero I/O.
    # Optional (use_content_hash=True): SHA-256 — catches same-size/same-mtime edits.
    use_content_hash = bool(src["values"].get("use_content_hash", False))

    # an indexable unit is (relpath, fingerprint, loader): a single uploaded
    # document, or every supported file under a crawled path. The fingerprint
    # is read WITHOUT opening the file (for mtime:size), so unchanged files are
    # skipped cheaply; the loader pulls the text only when we actually re-index.
    def units():
        doc = src["values"].get("document")
        if src["values"].get("kind") == "Upload" and doc:
            # BUG-03: route through extract path, not raw .decode() of bytes
            name = doc.get("name", "document")
            url = f"{NODE}/api/files/{doc['hash']}"
            yield name, doc.get("hash", ""), lambda u=url, n=name: _extract_upload(u, n)
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
                    # BUG-09: use content hash when enabled, mtime:size otherwise
                    fp = _file_fp(full, use_content_hash)
                except OSError:
                    continue
                rel = os.path.relpath(full, base).replace("\\", "/")  # "where is it?"
                yield rel, fp, (lambda p=full: extract.extract_text(p))

    # what's already in the index: skip unchanged, re-embed changed, drop gone.
    # force_rebuild=True: prior={} → every file is treated as new.
    prior = {} if force_rebuild else indexed_fingerprints(collection, sid)
    seen, changed = set(), 0
    for fname, fp, load in units():
        seen.add(fname)
        if not force_rebuild and prior.get(fname) == fp:
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
            # BUG-13: apply passage prefix for asymmetric models (no-op for bge-m3)
            vectors = embed(model, [_fmt_passage(model, t) for t in batch])
            # BUG-01/02: pass model so sentinel is written on first creation
            ensure_collection(collection, len(vectors[0]), model)
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
    # BUG-08: write update_record (journal event) only when something actually changed.
    # A no-op resync (0 changed, 0 removed) must not produce a journal entry every 2 min.
    if changed or removed:
        tool("update_record", {"entity": "Source", "id": sid, "basis": basis, "values": {
            "documents": len(seen),
            "last_indexed": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }})
    return note


def index_source(task):
    """First index / retry / explicit rebuild: drive New|Failed -> Indexing ->
    Indexed around the incremental sync.

    BUG-02 rebuild path: the DSL workflow exposes `reindex` (Indexed -> New),
    which the user triggers from the UI.  When we see status=New AND last_indexed
    is already set, the source was previously Indexed — the user explicitly asked
    for a full rebuild, so we skip fingerprints entirely.  A Failed->retry still
    uses incremental logic (it failed mid-index, partial data may be valid)."""
    tid, sid = task["id"], task["record_id"]
    basis = {"type": "task", "id": tid}
    src = tool("get_record", {"entity": "Source", "id": sid})
    status = src["values"].get("status")

    # Explicit rebuild: previously-indexed source moved back to New via `reindex`.
    force_rebuild = (status == "New" and bool(src["values"].get("last_indexed")))

    if status in ("New", "Failed"):
        # BUG-12: validate the embedding model BEFORE transitioning Source to Indexing.
        # Real model IDs are dynamic LM Studio identifiers ("text-embedding-bge-m3", etc.),
        # not a fixed enum — a probe-embed is the only reliable check.
        # On failure: fail the task without touching Source state; the source stays in
        # New/Failed (no workflow transition), giving the user a clear task-level error.
        model_probe, _ = settings()
        try:
            embed(model_probe, ["probe"])
        except (OSError, urllib.error.URLError, urllib.error.HTTPError) as probe_err:
            tool("fail_task", {"task_id": tid,
                               "reason": f"embedding_model '{model_probe}' rejected by LM Studio: {probe_err}"})
            return
        tool("act", {"entity": "Source", "id": sid,
                     "action": "start_index" if status == "New" else "retry_index", "basis": basis})
        status = "Indexing"
    if status != "Indexing":
        tool("complete_task", {"task_id": tid, "result": f"stale: source is {status}"})
        return
    try:
        note = sync_source(sid, src, basis, tid, force_rebuild=force_rebuild)
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

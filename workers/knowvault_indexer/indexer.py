#!/usr/bin/env python3
"""KnowVault indexer — a kalita worker agent.

The pattern every kalita worker follows:
  wait_for_task (long-poll) -> take_task (TTL lease) -> act within the
  workflow -> do the heavy lifting -> update records -> report with facts ->
  complete. Identity, permissions, audit and recovery come from the node;
  this file is ONLY the lifting.

v0 scope: walks a Files source, counts and extracts text from documents
(extraction is a stub until the Qdrant/embedding stage is wired in).

Env: KALITA_URL (default http://127.0.0.1:8096), KALITA_TOKEN (required).
Zero dependencies: stdlib only.
"""
import json
import os
import sys
import time
import urllib.request
import urllib.error
from datetime import datetime, timezone

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8096")
TOKEN = os.environ.get("KALITA_TOKEN") or sys.exit("KALITA_TOKEN is required")

TEXT_EXT = {".txt", ".md", ".rst", ".csv", ".log"}
BINARY_EXT = {".pdf", ".docx", ".xlsx", ".pptx", ".html"}  # counted, extraction TBD


class ToolError(Exception):
    def __init__(self, payload):
        super().__init__(payload.get("message", str(payload)))
        self.payload = payload


def tool(name, args=None):
    """Call one MCP tool, return the decoded result, raise ToolError on isError."""
    body = json.dumps({
        "jsonrpc": "2.0", "id": 1, "method": "tools/call",
        "params": {"name": name, "arguments": args or {}},
    }).encode()
    req = urllib.request.Request(
        NODE + "/mcp", data=body,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {TOKEN}"})
    with urllib.request.urlopen(req, timeout=70) as resp:
        rpc = json.load(resp)
    if "error" in rpc and rpc["error"]:
        raise ToolError({"message": rpc["error"].get("message", "rpc error")})
    result = rpc["result"]
    decoded = json.loads(result["content"][0]["text"]) if result.get("content") else {}
    if result.get("isError"):
        raise ToolError(decoded)
    return decoded


def extract(path):
    """Walk a directory: count documents, pull text from plain-text ones."""
    docs, chars = 0, 0
    for root, _dirs, files in os.walk(path):
        for name in files:
            ext = os.path.splitext(name)[1].lower()
            if ext in TEXT_EXT:
                docs += 1
                try:
                    with open(os.path.join(root, name), encoding="utf-8", errors="ignore") as f:
                        chars += len(f.read())
                except OSError:
                    pass
            elif ext in BINARY_EXT:
                docs += 1  # extraction for binary formats lands with embeddings
    return docs, chars


def index_source(task):
    tid, sid = task["id"], task["record_id"]
    basis = {"type": "task", "id": tid}
    src = tool("get_record", {"entity": "Source", "id": sid})
    status = src["values"].get("status")

    if status in ("New", "Failed"):
        tool("act", {"entity": "Source", "id": sid,
                     "action": "start_index" if status == "New" else "retry_index",
                     "basis": basis})
        status = "Indexing"
    if status != "Indexing":
        tool("complete_task", {"task_id": tid, "result": f"stale: source is {status}"})
        return

    path = src["values"].get("path", "")
    try:
        docs, chars = extract(path)
        tool("report_progress", {"task_id": tid,
                                 "note": f"extracted {docs} documents, {chars} chars from {path}"})
        tool("update_record", {"entity": "Source", "id": sid, "basis": basis, "values": {
            "documents": docs,
            "last_indexed": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }})
        tool("act", {"entity": "Source", "id": sid, "action": "finish_index", "basis": basis})
        tool("complete_task", {"task_id": tid, "result": f"indexed {docs} documents"})
        print(f"[ok] source {sid[:8]}: {docs} docs, {chars} chars")
    except OSError as e:
        # the honest way out: fail loudly, the workflow records it
        tool("act", {"entity": "Source", "id": sid, "action": "fail_index", "basis": basis})
        tool("fail_task", {"task_id": tid, "reason": str(e)})
        print(f"[fail] source {sid[:8]}: {e}")


def main():
    me = tool("describe_system")
    print(f"indexer up: node pack={me.get('pack')} role={me.get('your_role')} id={me.get('your_id')}")
    while True:
        try:
            tasks = tool("wait_for_task", {"timeout_sec": 25}).get("tasks") or []
            for task in tasks:
                try:
                    tool("take_task", {"task_id": task["id"]})
                except ToolError:
                    continue  # leased by another worker — the pool is shared
                if task.get("entity") == "Source":
                    index_source(task)
                else:
                    tool("complete_task", {"task_id": task["id"], "result": "not an indexer task"})
        except (ToolError, urllib.error.URLError) as e:
            print(f"[warn] {e}; retrying in 5s")
            time.sleep(5)


if __name__ == "__main__":
    main()

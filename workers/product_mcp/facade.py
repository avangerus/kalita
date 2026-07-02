#!/usr/bin/env python3
"""facade.py — the product MCP façade worker (LLD-06 §1, PK-2).

A GENERIC worker: it reads the `product.mcp:` section of a pack directory
(`manifest.py`) and serves whatever domain tools that pack declares
(`legal.submit_contract`, `kv.search`, ...) as its OWN MCP server, over HTTP
JSON-RPC — the same wire shape `internal/mcp/server.go` speaks, so any MCP
client that already understands the node also understands the façade. A
different product (a different `--pack-dir`) needs ZERO code changes here —
that genericity is the whole point of PK-2 (docs/lld/LLD-06-product-mcp-chat.md
§1: "Generic façade-воркер читает манифест — не кодируется под конкретный
продукт").

AUTH — read this before touching anything else in this file (LLD-06 §1, THE
non-negotiable decision, "ревью Fable" zone):

    The façade holds NO credential of its own for any write. Full stop. It
    never reads a worker-style service-credential environment variable and
    never imports the sibling worker-bootstrap helper every OTHER worker in
    this repo uses to mint its own agent identity (see workers/bootstrap.py —
    deliberately unused here) — search this file, neither appears anywhere.
    Every tool call this process makes to the node carries the EXACT bearer
    the INBOUND caller sent on ITS request to the façade (extracted once in
    `_handle_tools_call`, threaded through unchanged into `_call_node_factory`
    -> `_call_node` -> `_node_rpc`). Missing bearer on the inbound call =>
    AUTH_REQUIRED, refused BEFORE any node call is attempted. A caller who is
    denied by the node's own Permission Engine gets that exact denial back
    (`compose.ComposeError` -> the façade's tool-error response, payload
    passed through unchanged) — the façade does not retry with a "better"
    identity, because it HAS no other identity to retry with. This is what
    "strict pass-through" (LLD-06 §1) means operationally, not just in prose.

Zero non-stdlib dependencies, matching every other worker in this repo
(workers/connectors/inbound.py, workers/knowvault_search/service.py — same
BaseHTTPRequestHandler/ThreadingHTTPServer shape reused here).

Run:
    KALITA_URL=http://127.0.0.1:8095 \
    KALITA_PACK_DIR=../../packs/contracts \
    KALITA_FACADE_LISTEN=127.0.0.1:8200 \
    python facade.py
"""
import json
import os
import sys
import uuid
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
_WORKERS_DIR = os.path.dirname(_HERE)
if _WORKERS_DIR not in sys.path:
    sys.path.insert(0, _WORKERS_DIR)

import manifest  # noqa: E402
import compose  # noqa: E402
sys.path.insert(0, os.path.join(_WORKERS_DIR, "perimeter"))
import detector as perimeter  # noqa: E402  (H4 layer 3, ingest-detection tripwire, LLD-01 §6)

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095").rstrip("/")
PACK_DIR = os.environ.get("KALITA_PACK_DIR", "")
LISTEN = os.environ.get("KALITA_FACADE_LISTEN", "127.0.0.1:8200")
MAX_BODY = 256 * 1024

_TYPE_JSON = {
    "string": {"type": "string"}, "text": {"type": "string"}, "int": {"type": "integer"},
    "float": {"type": "number"}, "money": {"type": "number"}, "bool": {"type": "boolean"},
    "date": {"type": "string"}, "datetime": {"type": "string"}, "email": {"type": "string"},
}


def _field_schema(f):
    if f["type"].startswith("ref["):
        return {"type": "string", "description": "id of a " + f["type"][4:-1]}
    return _TYPE_JSON.get(f["type"], {"type": "string"})


def load_tools(pack_dir):
    """Load + FAIL-FAST validate every tool in pack_dir's product.mcp:
    section (TECHDEBT.md "P2 product.mcp's compose"). Any problem here is
    fatal to process startup — a broken tool must never come alive and fail
    silently on someone's first real call."""
    tools = manifest.parse_product_mcp(pack_dir)
    entities, actions = manifest.scan_entities_and_actions(pack_dir)
    for tool in tools:
        compose.validate_tool(tool, entities, actions)
    return {t["name"]: t for t in tools}


def _mcp_tool_defs(tools_by_name):
    defs = []
    for name, tool in tools_by_name.items():
        props = {f["name"]: _field_schema(f) for f in tool["in"]}
        required = [f["name"] for f in tool["in"] if f["required"]]
        schema = {"type": "object", "properties": props}
        if required:
            schema["required"] = required
        defs.append({"name": name, "description": tool["desc"], "inputSchema": schema})
    return defs


# --- node MCP client (pass-through — see module docstring) ------------------

class NodeUnreachable(Exception):
    pass


def _node_rpc(bearer, method, params):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    headers = {"Content-Type": "application/json"}
    if bearer:  # NEVER a façade-owned token — only what the caller sent us
        headers["Authorization"] = "Bearer " + bearer
    req = urllib.request.Request(NODE + "/mcp", data=body, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as ex:
        try:
            return json.loads(ex.read())
        except ValueError:
            raise NodeUnreachable("node HTTP %s" % ex.code)
    except urllib.error.URLError as ex:
        raise NodeUnreachable(str(ex))


def _call_node_factory(bearer):
    """Returns a `call_node(name, args, step_line)` bound to ONE inbound
    caller's bearer — compose.run_tool never sees the token itself, only
    this closure. A fresh closure per request; nothing here is cached or
    reused across callers."""

    def call_node(name, args, step_line):
        out = _node_rpc(bearer, "tools/call", {"name": name, "arguments": args})
        if "error" in out and out["error"] is not None:
            # a JSON-RPC PROTOCOL-level error (e.g. malformed params) — still
            # surface it unchanged, not swallowed.
            raise compose.ComposeError(step_line, out["error"])
        res = out.get("result") or {}
        content = res.get("content") or []
        payload = json.loads(content[0]["text"]) if content else {}
        if res.get("isError"):
            raise compose.ComposeError(step_line, payload)
        return payload

    return call_node


def _idem_key_fn(correlation):
    def fn(step_idx):
        return str(uuid.uuid5(uuid.NAMESPACE_URL, "%s|%d" % (correlation, step_idx)))
    return fn


# --- perimeter (H4 layer 3) --------------------------------------------------

def _flagged_text_inputs(tool, arguments):
    """Score every `in:` field typed exactly `text` (the type a pack author
    chooses for free-form/external content, as opposed to `string` for short
    labeled values) — a GENERIC, data-driven rule so the façade does not
    hardcode which tool/field is "the untrusted one" per product (LLD-06 §1:
    "Периметр: тулы, принимающие внешний текст... прогоняют его через
    promptbox/детектор"). Returns [(field_name, score, reasons), ...] for
    every field that cleared perimeter.THRESHOLD; empty if none did (the
    common case — this NEVER blocks, see perimeter/detector.py's own
    docstring: "This is a tripwire, not a filter")."""
    flagged = []
    for f in tool["in"]:
        if f["type"] != "text":
            continue
        val = arguments.get(f["name"])
        if not isinstance(val, str) or not val:
            continue
        score, reasons = perimeter.score(val)
        if score >= perimeter.THRESHOLD:
            flagged.append((f["name"], score, reasons))
    return flagged


def _attach_perimeter_flag(call_node, out_entity, out_value, flagged, correlation):
    """Best-effort: post an INTERNAL comment on the tool's out: record noting
    the perimeter flag — visible in the SAME audit trail a human reviewer
    already watches (I-3: a flag must be observable, not silent), exactly the
    pattern workers/agent_runner/runner.py's handle_legal already uses for
    the same signal on the agent-review side. Never raises: a flagging
    failure must not turn a successful, already-committed write into an
    error response — the record exists either way; only the annotation is
    best-effort."""
    if not (out_entity and isinstance(out_value, dict) and out_value.get("id")):
        return
    body = "perimeter: possible prompt injection in %s (%s)" % (
        ", ".join(f for f, _, _ in flagged),
        "; ".join("%s score=%.2f: %s" % (f, s, ", ".join(r) or "unspecified") for f, s, r in flagged))
    try:
        call_node("comment", {
            "entity": out_entity, "id": out_value["id"], "body": body, "internal": True,
            "basis": {"type": "human", "id": "product_mcp:perimeter"},
            "idempotency_key": str(uuid.uuid5(uuid.NAMESPACE_URL, "perimeter|" + correlation)),
        }, "<perimeter flag>")
    except compose.ComposeError:
        pass  # best-effort — see docstring above


# --- MCP-shaped HTTP server (mirrors internal/mcp/server.go's envelope) ----

PROTOCOL_VERSION = "2025-06-18"


def _rpc_result(rid, result):
    return {"jsonrpc": "2.0", "id": rid, "result": result}


def _tool_result(rid, payload, is_error):
    text = json.dumps(payload)
    return _rpc_result(rid, {"content": [{"type": "text", "text": text}], "isError": is_error})


def _facade_tool_error(rid, code, message, fix_hint=None):
    err = {"code": code, "message": message}
    if fix_hint:
        err["fix_hint"] = fix_hint
    return _tool_result(rid, err, True)


class Handler(BaseHTTPRequestHandler):
    tools_by_name = {}  # set once at startup by main()

    def _send(self, obj, code=200):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path in ("/healthz", "/readyz"):
            self._send({"ok": True, "tools": len(self.tools_by_name)})
            return
        self._send({"code": "NOT_FOUND"}, 404)

    def do_POST(self):
        if self.path != "/mcp":
            self._send({"code": "NOT_FOUND"}, 404)
            return
        length = int(self.headers.get("Content-Length") or 0)
        if length <= 0 or length > MAX_BODY:
            self._send({"code": "VALIDATION_ERROR", "message": "bad content-length"}, 400)
            return
        try:
            req = json.loads(self.rfile.read(length))
        except ValueError:
            self._send({"jsonrpc": "2.0", "error": {"code": -32700, "message": "parse error"}})
            return
        rid = req.get("id")
        method = req.get("method")
        if rid is None:
            self.send_response(202)
            self.end_headers()
            return
        if method == "initialize":
            self._send(_rpc_result(rid, {
                "protocolVersion": PROTOCOL_VERSION, "capabilities": {"tools": {}},
                "serverInfo": {"name": "kalita-product-mcp", "version": "0.1.0"},
            }))
        elif method == "ping":
            self._send(_rpc_result(rid, {}))
        elif method == "tools/list":
            self._send(_rpc_result(rid, {"tools": _mcp_tool_defs(self.tools_by_name)}))
        elif method == "tools/call":
            self._handle_tools_call(rid, req.get("params") or {})
        else:
            self._send({"jsonrpc": "2.0", "id": rid,
                        "error": {"code": -32601, "message": "method not found: " + str(method)}})

    def _handle_tools_call(self, rid, params):
        # --- AUTH: strict pass-through (LLD-06 §1) — read this file's module
        # docstring before changing ANYTHING below this line. ---
        auth = self.headers.get("Authorization") or ""
        bearer = auth[len("Bearer "):] if auth.startswith("Bearer ") else ""
        if not bearer:
            self._send(_facade_tool_error(rid, "AUTH_REQUIRED", "missing bearer token",
                                           "pass Authorization: Bearer <your own kalita token> — "
                                           "the façade has none of its own to use instead"))
            return

        name = params.get("name")
        arguments = params.get("arguments") or {}
        tool = self.tools_by_name.get(name)
        if tool is None:
            self._send(_facade_tool_error(rid, "VALIDATION_ERROR", "unknown tool " + str(name),
                                           "call tools/list for the tools this façade serves"))
            return
        missing = [f["name"] for f in tool["in"] if f["required"] and f["name"] not in arguments]
        if missing:
            self._send(_facade_tool_error(rid, "INVALID_PARAMS", "missing required input(s): " + ", ".join(missing)))
            return

        flagged = _flagged_text_inputs(tool, arguments)
        correlation = str(uuid.uuid4())
        call_node = _call_node_factory(bearer)
        try:
            value, out_entity = compose.run_tool(tool, arguments, call_node, _idem_key_fn(correlation))
        except compose.ComposeError as ex:
            # pass the node's OWN error through unchanged (LLD-06 §3 test 2:
            # façade "ничего не расширяет" — including not softening a denial).
            self._send(_tool_result(rid, ex.node_error, True))
            return
        except NodeUnreachable as ex:
            self._send(_facade_tool_error(rid, "UPSTREAM_UNAVAILABLE", str(ex)))
            return

        if flagged:
            _attach_perimeter_flag(call_node, out_entity, value, flagged, correlation)
        self._send(_tool_result(rid, value, False))

    def log_message(self, *_args):
        pass  # silence the default stderr access log (matches inbound.py)


def main():
    if not PACK_DIR:
        print("KALITA_PACK_DIR is required (the pack directory to read product.mcp from)", file=sys.stderr)
        sys.exit(2)
    try:
        tools_by_name = load_tools(PACK_DIR)
    except manifest.ManifestError as ex:
        # fail FAST at startup — never come alive serving a tool whose
        # compose is broken (TECHDEBT.md "P2 product.mcp's compose").
        print("product_mcp façade: manifest validation failed: %s" % ex, file=sys.stderr)
        sys.exit(1)
    Handler.tools_by_name = tools_by_name
    host, _, port = LISTEN.partition(":")
    srv = ThreadingHTTPServer((host, int(port)), Handler)
    print("product_mcp façade online at %s, pack %s, %d tool(s): %s"
          % (LISTEN, PACK_DIR, len(tools_by_name), ", ".join(sorted(tools_by_name))))
    srv.serve_forever()


if __name__ == "__main__":
    main()

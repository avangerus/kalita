#!/usr/bin/env python3
"""chat_runner.py — the operator chat worker (LLD-06 §2, PK-3, KChat).

Local LLM + a (bounded, one-tool-per-turn) MCP loop, driven UNDER THE ACTOR
OF THE HUMAN who is chatting — pass-through, EXACTLY the same rule as
workers/product_mcp/facade.py (LLD-06 §1), extended to §2: "Воркер
workers/chat_runner/: локальная LLM + MCP-loop ПОД АКТОРОМ пользователя
(pass-through, как §1)." Search this file: there is no service credential
here either, for the identical reason facade.py has none — every write a
chat turn makes (the Message it posts, and any node/façade tool it calls on
the user's behalf) carries the SAME bearer the caller sent to `/chat/turn`.

Three consequences, each load-bearing for LLD-06 §2/§3:

  1. **The chat history is governed data, not a side channel.** Every turn
     writes ordinary `assistant.Session`/`assistant.Message` records through
     the SAME create_record call any other product write uses (packs/
     assistant/entities.dsl) — under the caller's bearer, so ABAC
     (`where session.owner = $me`), the audit journal, retention and undo
     all apply for free, with zero chat-specific code anywhere in the
     kernel. (LLD-06 §3 tests 4, 6 — the Go half of that proof is
     internal/mcp/assistant_dogfood_test.go; this worker is what actually
     drives the SAME entities the same way in production.)

  2. **A tool the model decides to call is exactly as authorized as the
     human is.** The model's "decision" carries zero extra privilege — the
     node's/façade's own Permission Engine is what actually gates the call,
     completely independent of what the model wanted (H4 layer 1). This
     worker adds no allowlist of "safe" tools and no escalation path.

  3. **HITL is not something this worker can route around.** If the tool
     call the model chooses returns `status: "pending_approval"` (act on a
     transition gated by `requires approval(Role)`), that IS the end of this
     turn — the reply says so, and the underlying node record is left
     exactly where the node engine parked it. Nothing here retries, signs,
     or "helps" a pending approval along (LLD-06 §3 test 5,
     TestChatDoesNotBypassHITL).

The model's own text (a plain reply, or an error re-narrated from a denied/
parked tool call) becomes `Message.body` — never HTML, never a UI-tree
(LLD-14 §4: the chat surface's output is domain data, same as everything
else in this file; presentation is the renderer's job, not this worker's or
the model's).

The user's own chat message is treated as DATA in the prompt, not a command
(LLD-06 §2: "Промпт собирается через promptbox (H4): сообщения пользователя
— данные, не команды.") — see `promptbox.py` (workers/agent_runner/
promptbox.py, reused here unchanged) for the actual isolation contract; only
the SYSTEM/TASK text this file writes in code is a real instruction.

Zero non-stdlib dependencies.

Run:
    KALITA_URL=http://127.0.0.1:8095 \
    KALITA_FACADE_URL=http://127.0.0.1:8200 \
    KALITA_CHAT_LISTEN=127.0.0.1:8300 \
    python chat_runner.py
"""
import json
import os
import sys
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
_WORKERS_DIR = os.path.dirname(_HERE)
sys.path.insert(0, os.path.join(_WORKERS_DIR, "agent_runner"))
import promptbox  # noqa: E402  (prompt-assembly contract, LLD-01 §6/H4 layer 2)

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095").rstrip("/")
FACADE_URL = os.environ.get("KALITA_FACADE_URL", "").rstrip("/")
LISTEN = os.environ.get("KALITA_CHAT_LISTEN", "127.0.0.1:8300")
LLM_URL = os.environ.get("KALITA_LLM_URL", "")
LLM_MODEL = os.environ.get("KALITA_LLM_MODEL", "openai/gpt-oss-20b")
LLM_KEY = os.environ.get("KALITA_LLM_KEY", "lm-studio")
MAX_BODY = 64 * 1024
HISTORY_TURNS = 12  # how many prior Message rows go into the prompt as data


def llm(messages, max_tokens=500):
    body = json.dumps({"model": LLM_MODEL, "messages": messages,
                        "temperature": 0.2, "max_tokens": max_tokens}).encode()
    req = urllib.request.Request(LLM_URL.rstrip("/") + "/chat/completions", data=body, headers={
        "Content-Type": "application/json", "Authorization": "Bearer " + LLM_KEY})
    with urllib.request.urlopen(req, timeout=180) as resp:
        out = json.load(resp)
    return out["choices"][0]["message"]["content"].strip()


def _extract_obj(text):
    """Pull one JSON object out of a model reply, tolerant of fences/prose —
    same convention as workers/agent_runner/runner.py's `_extract_obj`
    (duplicated, not imported: each worker here is self-contained stdlib,
    no cross-worker runtime dependency)."""
    t = (text or "").strip()
    if "```" in t:
        for part in t.split("```"):
            p = part.strip()
            if p[:4].lower() == "json":
                p = p[4:].strip()
            if p.startswith("{"):
                t = p
                break
    i, j = t.find("{"), t.rfind("}")
    if i >= 0 and j > i:
        try:
            o = json.loads(t[i:j + 1])
            return o if isinstance(o, dict) else None
        except ValueError:
            return None
    return None


# --- node/façade MCP client (pass-through — see module docstring) ----------

class ChatError(Exception):
    pass


def mcp_call(base_url, bearer, tool, arguments):
    """One MCP tools/call against `base_url` (the node OR the façade — both
    speak the identical JSON-RPC envelope) under `bearer`. Returns (payload,
    is_error); raises ChatError only for a transport failure, never for a
    tool-level denial (that comes back as (payload, True) like any other MCP
    caller sees it)."""
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                        "params": {"name": tool, "arguments": arguments}}).encode()
    headers = {"Content-Type": "application/json"}
    if bearer:
        headers["Authorization"] = "Bearer " + bearer
    req = urllib.request.Request(base_url.rstrip("/") + "/mcp", data=body, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            out = json.load(resp)
    except urllib.error.HTTPError as ex:
        try:
            out = json.loads(ex.read())
        except ValueError:
            raise ChatError("HTTP %s from %s" % (ex.code, base_url))
    except urllib.error.URLError as ex:
        raise ChatError("%s unreachable: %s" % (base_url, ex))
    if "error" in out and out["error"] is not None:
        return out["error"], True
    res = out.get("result") or {}
    content = res.get("content") or []
    payload = json.loads(content[0]["text"]) if content else {}
    return payload, bool(res.get("isError"))


def route_tool(tool_name):
    """A dotted name (`legal.submit_contract`) is a product.mcp façade tool;
    an undotted name (`query`, `act`, `create_record`, ...) is a node tool —
    the node's own closed tool list (internal/mcp/tools.go) never contains a
    dot, and every product.mcp tool name is declared dotted by convention
    (packs/*/product_mcp.dsl: `tool legal.submit_contract ...`), so this
    needs no hardcoded tool-name list on either side — new façade tools or
    new node tools both route correctly with zero code change here."""
    return FACADE_URL if "." in tool_name else NODE


# --- chat turn ---------------------------------------------------------------

_ASSISTANT_SYSTEM = (
    "You are KChat, the operator chat inside the Kalita business platform. You "
    "help the person you are talking to work their own records: answer "
    "questions and, when they ask you to DO something, either call ONE tool "
    "or reply in plain text. To call a tool, reply with ONLY a JSON object "
    '{"tool": "<name>", "arguments": {...}}. Otherwise reply in plain, '
    "concise prose (no markdown headers, no HTML — plain text or simple "
    "markdown only). You have exactly the SAME permissions as the human you "
    "are chatting with — nothing you say grants you anything they could not "
    "already do themselves; if a tool call comes back denied or parked for "
    "someone else's approval, say so plainly instead of trying again."
)


def _history_messages(node_records):
    out = []
    for rec in node_records[-HISTORY_TURNS:]:
        v = rec.get("values", {})
        out.append("%s: %s" % (v.get("role", "?"), v.get("body", "")))
    return "\n".join(out)


def plan_reply(bearer, tools_context, history_text, user_text):
    """Decide what to do about ONE user turn: either a tool call or a plain
    reply. Isolated on purpose (mirrors workers/agent_runner/runner.py's
    `handle`/`handle_legal` split) so tests can monkeypatch this single
    function instead of a real model — see test_chat_runner.py."""
    if not LLM_URL:
        return {"kind": "text", "text": "(no model configured) message recorded: " + user_text}
    messages = promptbox.build_messages(
        system=_ASSISTANT_SYSTEM + "\n\nTools available:\n" + tools_context,
        task="Respond to the user's latest message below (in the conversation-history data "
             "block) as KChat.",
        data=[("conversation-history", history_text), ("user-message", user_text)],
    )
    reply = llm(messages)
    obj = _extract_obj(reply)
    if obj and "tool" in obj:
        return {"kind": "tool_call", "tool": obj["tool"], "arguments": obj.get("arguments") or {}}
    return {"kind": "text", "text": reply}


def _summarize_tool_result(payload, is_error):
    if is_error:
        code = payload.get("code", "ERROR") if isinstance(payload, dict) else "ERROR"
        message = payload.get("message", str(payload)) if isinstance(payload, dict) else str(payload)
        return "I couldn't do that (%s): %s" % (code, message)
    if isinstance(payload, dict) and payload.get("status") == "pending_approval":
        # LLD-06 §3 test 5 — the exact place a chat turn meets HITL and stops.
        return ("That action needs a human's sign-off and has been parked for approval — "
                "I can't push it through myself.")
    if isinstance(payload, dict) and "id" in payload:
        return "Done — record %s." % payload["id"]
    return "Done."


def handle_turn(bearer, session_id, user_text, tools_context=""):
    """Drive one chat turn end-to-end. Returns (assistant_text, session_id,
    pending_approval: bool) — the third value is a STRUCTURED signal (not a
    string the UI has to sniff for keywords) that this turn's tool call, if
    any, was parked for a human's approval (LLD-06 §3 test 5); ChatFace
    (packages/kalita-ui/faces.js) renders it as a distinct pending-approval
    state. EVERY node/façade call here uses `bearer` — no other identity
    exists in this function's closure."""
    if not session_id:
        rec, is_err = mcp_call(NODE, bearer, "create_record", {
            "entity": "Session", "values": {"title": user_text[:60]},
            "basis": {"type": "human", "id": "kchat"},
            "idempotency_key": _idem("session", user_text)})
        if is_err:
            raise ChatError("could not open a chat session: %r" % rec)
        session_id = rec["id"]

    _post_message(bearer, session_id, "user", user_text)

    hist, is_err = mcp_call(NODE, bearer, "query", {
        "entity": "Message", "where": 'session = "%s"' % session_id, "sort": ["created_at"]})
    history_text = _history_messages(hist.get("records", [])) if not is_err else ""

    plan = plan_reply(bearer, tools_context, history_text, user_text)
    pending_approval = False
    if plan["kind"] == "tool_call":
        target = route_tool(plan["tool"])
        payload, is_err = mcp_call(target, bearer, plan["tool"], plan["arguments"])
        pending_approval = (not is_err and isinstance(payload, dict)
                             and payload.get("status") == "pending_approval")
        reply_text = _summarize_tool_result(payload, is_err)
    else:
        reply_text = plan["text"]

    _post_message(bearer, session_id, "assistant", reply_text)
    return reply_text, session_id, pending_approval


def _post_message(bearer, session_id, role, body):
    _, is_err = mcp_call(NODE, bearer, "create_record", {
        "entity": "Message", "values": {"session": session_id, "role": role, "body": body},
        "basis": {"type": "human", "id": "kchat"},
        "idempotency_key": _idem("message", session_id, role, body)})
    if is_err:
        raise ChatError("could not record the %s message" % role)


def _idem(*parts):
    import uuid
    return str(uuid.uuid5(uuid.NAMESPACE_URL, "|".join(str(p) for p in parts) + "|" + uuid.uuid4().hex))


# --- façade tool discovery (unauthenticated tools/list, cached at startup) --

def discover_facade_tools():
    if not FACADE_URL:
        return "(no product façade configured — only the node's own tools are available)"
    try:
        body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}}).encode()
        req = urllib.request.Request(FACADE_URL.rstrip("/") + "/mcp", data=body,
                                      headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=10) as resp:
            out = json.load(resp)
        tools = out.get("result", {}).get("tools", [])
        return "\n".join("- %s: %s" % (t["name"], t.get("description", "")) for t in tools) or "(none)"
    except (urllib.error.URLError, ValueError):
        return "(product façade unreachable at startup)"


# --- HTTP surface -------------------------------------------------------------

class Handler(BaseHTTPRequestHandler):
    tools_context = ""

    def _send(self, obj, code=200):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path in ("/healthz", "/readyz"):
            self._send({"ok": True})
            return
        self._send({"code": "NOT_FOUND"}, 404)

    def do_POST(self):
        if self.path != "/chat/turn":
            self._send({"code": "NOT_FOUND"}, 404)
            return
        auth = self.headers.get("Authorization") or ""
        bearer = auth[len("Bearer "):] if auth.startswith("Bearer ") else ""
        if not bearer:
            self._send({"code": "AUTH_REQUIRED",
                        "message": "missing bearer token — KChat has none of its own"}, 401)
            return
        length = int(self.headers.get("Content-Length") or 0)
        if length <= 0 or length > MAX_BODY:
            self._send({"code": "VALIDATION_ERROR", "message": "bad content-length"}, 400)
            return
        try:
            req = json.loads(self.rfile.read(length))
        except ValueError:
            self._send({"code": "VALIDATION_ERROR", "message": "bad json"}, 400)
            return
        text = (req.get("message") or "").strip()
        if not text:
            self._send({"code": "VALIDATION_ERROR", "message": "message is empty"}, 400)
            return
        try:
            reply, session_id, pending_approval = handle_turn(bearer, req.get("session_id"), text, self.tools_context)
        except ChatError as ex:
            self._send({"code": "UPSTREAM_ERROR", "message": str(ex)}, 502)
            return
        self._send({"session_id": session_id, "reply": reply, "pending_approval": pending_approval})

    def log_message(self, *_args):
        pass


def main():
    Handler.tools_context = discover_facade_tools()
    host, _, port = LISTEN.partition(":")
    srv = ThreadingHTTPServer((host, int(port)), Handler)
    print("chat_runner online at %s, node=%s, facade=%s%s"
          % (LISTEN, NODE, FACADE_URL or "(none)", " with model " + LLM_MODEL if LLM_URL else " (deterministic)"))
    srv.serve_forever()


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Tests for chat_runner.py against a FAKE node + FAKE façade (no live stack
— same convention as workers/product_mcp/test_facade.py). Covers LLD-06 §3
test 4 (chat write journals as the human), test 5 (chat does not bypass
HITL), and the routing/pass-through plumbing specific to this worker.

Run: python -m pytest test_chat_runner.py -v
"""
import json
import os
import sys
import threading
import unittest
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

import chat_runner  # noqa: E402


class FakeServer(ThreadingHTTPServer):
    def __init__(self, *a, **kw):
        super().__init__(*a, **kw)
        self.calls = []          # [(bearer, tool, args), ...]
        self.records = {}        # (entity, id) -> values
        self.act_result = {"status": "applied"}  # override per test
        self.tools_list = []     # for façade tools/list


class FakeHandler(BaseHTTPRequestHandler):
    def log_message(self, *_a):
        pass

    def _send(self, obj):
        body = json.dumps(obj).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _result(self, rid, payload, is_error=False):
        self._send({"jsonrpc": "2.0", "id": rid,
                     "result": {"content": [{"type": "text", "text": json.dumps(payload)}], "isError": is_error}})

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        req = json.loads(self.rfile.read(length))
        rid = req.get("id")
        method = req.get("method")
        if method == "tools/list":
            self._send({"jsonrpc": "2.0", "id": rid, "result": {"tools": self.server.tools_list}})
            return
        auth = self.headers.get("Authorization") or ""
        bearer = auth[len("Bearer "):] if auth.startswith("Bearer ") else ""
        params = req.get("params") or {}
        name = params.get("name")
        args = params.get("arguments") or {}
        self.server.calls.append((bearer, name, dict(args)))

        if name == "create_record":
            entity = args["entity"]
            new_id = str(uuid.uuid4())
            values = dict(args["values"])
            self.server.records[(entity, new_id)] = values
            self._result(rid, {"id": new_id, "values": values})
        elif name == "query":
            entity = args["entity"]
            recs = [{"id": rid_, "values": v} for (e, rid_), v in self.server.records.items() if e == entity]
            self._result(rid, {"records": recs})
        elif name == "act":
            self._result(rid, self.server.act_result, self.server.act_result.get("code") is not None)
        else:
            self._result(rid, {"code": "VALIDATION_ERROR", "message": "unhandled " + str(name)}, True)


class ChatRunnerTestCase(unittest.TestCase):
    def setUp(self):
        self.node = FakeServer(("127.0.0.1", 0), FakeHandler)
        threading.Thread(target=self.node.serve_forever, daemon=True).start()
        self.facade = FakeServer(("127.0.0.1", 0), FakeHandler)
        threading.Thread(target=self.facade.serve_forever, daemon=True).start()

        self._orig_node = chat_runner.NODE
        self._orig_facade = chat_runner.FACADE_URL
        chat_runner.NODE = "http://127.0.0.1:%d" % self.node.server_address[1]
        chat_runner.FACADE_URL = "http://127.0.0.1:%d" % self.facade.server_address[1]

    def tearDown(self):
        self.node.shutdown()
        self.facade.shutdown()
        chat_runner.NODE = self._orig_node
        chat_runner.FACADE_URL = self._orig_facade


class TestChatActionIsJournaledAsUser(ChatRunnerTestCase):
    """LLD-06 §3 test 4 (worker-level reinforcement of the assistant_dogfood_
    test's Go proof): every node call a chat turn makes carries the CALLER's
    bearer, never a chat_runner-owned credential."""

    def test_no_model_turn_writes_under_callers_bearer_only(self):
        reply, session_id, pending = chat_runner.handle_turn("alice-token", None, "hello there")
        self.assertIn("hello there", reply)
        self.assertFalse(pending)
        bearers = {b for b, _, _ in self.node.calls}
        self.assertEqual(bearers, {"alice-token"})
        # two Message creates (user + assistant) plus the Session create plus the history query
        kinds = [n for _, n, _ in self.node.calls]
        self.assertEqual(kinds.count("create_record"), 3)  # Session, Message(user), Message(assistant)

    def test_second_turn_reuses_the_given_session(self):
        _, sid, _ = chat_runner.handle_turn("alice-token", None, "first")
        _, sid2, _ = chat_runner.handle_turn("alice-token", sid, "second")
        self.assertEqual(sid, sid2)
        session_creates = [a for b, n, a in self.node.calls if n == "create_record" and a["entity"] == "Session"]
        self.assertEqual(len(session_creates), 1, "a session_id was supplied — must not open a second Session")


class TestChatDoesNotBypassHITL(ChatRunnerTestCase):
    """LLD-06 §3 test 5 — the load-bearing test for this worker: a tool call
    the model decides to make, that the node parks pending_approval, must
    end the turn there — no retry, no second attempt, no silent success."""

    def test_pending_approval_is_surfaced_not_retried(self):
        self.node.act_result = {"status": "pending_approval", "approval_id": "ap-1"}
        chat_runner.plan_reply = lambda *a, **kw: {
            "kind": "tool_call", "tool": "act",
            "arguments": {"entity": "Contract", "id": "c-1", "action": "clear_legal"}}
        try:
            reply, _, pending = chat_runner.handle_turn("law-token", None, "approve this contract for me")
        finally:
            del chat_runner.plan_reply
        self.assertIn("approval", reply.lower())
        self.assertTrue(pending, "handle_turn must report pending_approval=True structurally, "
                                  "not just in the reply's prose (ChatFace renders this flag directly)")
        act_calls = [(b, a) for b, n, a in self.node.calls if n == "act"]
        self.assertEqual(len(act_calls), 1, "must attempt the act exactly once, never retry past a parked approval")
        bearer, args = act_calls[0]
        self.assertEqual(bearer, "law-token")
        # the assistant's own reply message records the parked outcome honestly
        msg_bodies = [a["values"]["body"] for _, n, a in self.node.calls
                      if n == "create_record" and a["entity"] == "Message" and a["values"]["role"] == "assistant"]
        self.assertTrue(any("approval" in b.lower() for b in msg_bodies))

    def test_denied_tool_call_is_reported_not_hidden(self):
        chat_runner.plan_reply = lambda *a, **kw: {
            "kind": "tool_call", "tool": "act",
            "arguments": {"entity": "Contract", "id": "c-1", "action": "activate"}}
        self.node.act_result = {"code": "PERMISSION_DENIED", "message": "no grant"}
        try:
            reply, _, pending = chat_runner.handle_turn("law-token", None, "activate it")
        finally:
            del chat_runner.plan_reply
        self.assertIn("PERMISSION_DENIED", reply)
        self.assertFalse(pending)


class TestToolRouting(ChatRunnerTestCase):
    """A dotted tool name goes to the façade; an undotted name goes to the
    node — no hardcoded tool-name list on either side."""

    def test_dotted_tool_routes_to_facade(self):
        self.facade.records = self.node.records  # share state for id lookups, irrelevant here
        chat_runner.plan_reply = lambda *a, **kw: {
            "kind": "tool_call", "tool": "legal.submit_contract",
            "arguments": {"title": "x", "counterparty": "y", "terms": "z"}}
        try:
            chat_runner.handle_turn("alice-token", None, "submit a contract")
        finally:
            del chat_runner.plan_reply
        self.assertEqual(len(self.facade.calls), 1)
        self.assertEqual(self.facade.calls[0][1], "legal.submit_contract")
        self.assertEqual([n for _, n, _ in self.node.calls if n not in ("create_record", "query")], [])

    def test_plain_tool_routes_to_node(self):
        chat_runner.plan_reply = lambda *a, **kw: {
            "kind": "tool_call", "tool": "query", "arguments": {"entity": "Message"}}
        try:
            chat_runner.handle_turn("alice-token", None, "list my messages")
        finally:
            del chat_runner.plan_reply
        self.assertEqual(len(self.facade.calls), 0)


class TestNoBearerHandledAtHTTPLayer(unittest.TestCase):
    def test_missing_bearer_returns_401_without_calling_mcp(self):
        import urllib.error
        srv = ThreadingHTTPServer(("127.0.0.1", 0), chat_runner.Handler)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        try:
            url = "http://127.0.0.1:%d/chat/turn" % srv.server_address[1]
            req = urllib.request.Request(url, data=json.dumps({"message": "hi"}).encode(),
                                          headers={"Content-Type": "application/json"})
            with self.assertRaises(urllib.error.HTTPError) as cm:
                urllib.request.urlopen(req, timeout=5)
            self.assertEqual(cm.exception.code, 401)
        finally:
            srv.shutdown()


if __name__ == "__main__":
    unittest.main()

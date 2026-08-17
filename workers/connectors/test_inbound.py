#!/usr/bin/env python3
"""Tests for workers/connectors/inbound.py (TST-1, Cycle-F2): the inbound
connector's trust boundary — shared-secret auth, body size/UTF-8 validation,
field-whitelist mapping (no field injection), and a single idempotent create.

Run:  python -m pytest test_inbound.py -v
  or: python test_inbound.py
Zero dependencies: stdlib unittest only. No network — the MCP call is stubbed.
"""
import json
import os
import sys
import threading
import unittest
import urllib.request
import urllib.error
from http.server import ThreadingHTTPServer

# Import-safe: an explicit token means bootstrap never touches the network.
os.environ["KALITA_TOKEN"] = "test-token"
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import inbound  # noqa: E402


class TestMapPayload(unittest.TestCase):
    def setUp(self):
        self.orig_cfg = inbound.CFG
        inbound.CFG = {
            "entity": "Lead",
            "secret": "",
            "field_map": {"name": "name", "company": "company", "email": "email"},
            "static": {"source": "Web"},
        }

    def tearDown(self):
        inbound.CFG = self.orig_cfg

    def test_whitelist_only(self):
        values, err = inbound.map_payload(
            {"name": "Ada", "company": "Vector", "email": "a@v.ru", "evil": "x", "is_admin": True})
        self.assertIsNone(err)
        self.assertEqual(values, {"source": "Web", "name": "Ada", "company": "Vector", "email": "a@v.ru"})
        self.assertNotIn("evil", values)          # unknown keys are dropped, not injected
        self.assertNotIn("is_admin", values)

    def test_rejects_nested(self):
        _, err = inbound.map_payload({"name": {"x": 1}})
        self.assertEqual(err, "field name must be a scalar")
        _, err = inbound.map_payload({"name": ["a", "b"]})
        self.assertEqual(err, "field name must be a scalar")

    def test_non_object(self):
        _, err = inbound.map_payload(["not", "a", "dict"])
        self.assertEqual(err, "body must be a JSON object")


class TestConstantEq(unittest.TestCase):
    def test_equal(self):
        self.assertTrue(inbound.constant_eq("abc", "abc"))

    def test_different_same_len(self):
        self.assertFalse(inbound.constant_eq("abc", "abd"))

    def test_different_len(self):
        self.assertFalse(inbound.constant_eq("abc", "ab"))  # no crash, constant-time shape


class TestIntakeHandler(unittest.TestCase):
    """End-to-end over a real HTTP socket, with the MCP create stubbed."""

    @classmethod
    def setUpClass(cls):
        cls.orig_cfg = inbound.CFG
        cls.orig_create = inbound.create_with_retry
        inbound.CFG = {
            "entity": "Lead",
            "secret": "form-shared-secret",
            "field_map": {"name": "name", "company": "company", "email": "email"},
            "static": {"source": "Web"},
        }
        cls.calls = []
        # capture through the stub so the field-injection assertion sees the payload
        def capture(entity, basis, values, idem_key):
            cls.calls.append((entity, basis, values, idem_key))
            return {"id": "lead-1", "entity": entity}, False
        inbound.create_with_retry = capture
        cls.srv = ThreadingHTTPServer(("127.0.0.1", 0), inbound.Handler)
        cls.port = cls.srv.server_address[1]
        cls.thread = threading.Thread(target=cls.srv.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.srv.shutdown()
        inbound.CFG = cls.orig_cfg
        inbound.create_with_retry = cls.orig_create

    def post(self, path, body=None, secret="form-shared-secret", raw=None):
        url = f"http://127.0.0.1:{self.port}{path}"
        data = raw if raw is not None else (json.dumps(body).encode() if body is not None else b"")
        req = urllib.request.Request(url, data=data, method="POST")
        if secret is not None:
            req.add_header("X-Intake-Secret", secret)
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                return resp.status, json.load(resp)
        except urllib.error.HTTPError as e:
            return e.code, json.loads(e.read() or b"{}")

    def get(self, path):
        with urllib.request.urlopen(f"http://127.0.0.1:{self.port}{path}", timeout=10) as resp:
            return resp.status, json.load(resp)

    def test_wrong_secret_rejected(self):
        status, body = self.post("/intake", {"name": "Ada"}, secret="wrong")
        self.assertEqual(status, 401)
        self.assertIn("bad or missing", body["error"])

    def test_oversized_body_rejected(self):
        status, _ = self.post("/intake", raw=b"x" * (inbound.MAX_BODY + 1))
        self.assertEqual(status, 413)

    def test_invalid_json_rejected(self):
        status, _ = self.post("/intake", raw=b"not-json")
        self.assertEqual(status, 400)

    def test_healthz(self):
        status, body = self.get("/healthz")
        self.assertEqual(status, 200)
        self.assertTrue(body["ok"])

    def test_valid_payload_field_injection_guarded(self):
        before = len(self.calls)
        status, body = self.post("/intake", {
            "name": "Ada", "company": "Vector", "email": "a@v.ru",
            "is_admin": True, "role": "Owner", "owner": "someone-else"})
        self.assertEqual(status, 201)
        self.assertEqual(body["entity"], "Lead")
        self.assertEqual(len(self.calls), before + 1)
        _, _, values, _ = self.calls[-1]
        # only whitelisted + static fields reach the node; every other key is dropped
        self.assertEqual(values, {"source": "Web", "name": "Ada", "company": "Vector", "email": "a@v.ru"})


if __name__ == "__main__":
    unittest.main(verbosity=2)

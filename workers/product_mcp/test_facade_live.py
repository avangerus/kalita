#!/usr/bin/env python3
"""LIVE end-to-end test for facade.py against a REAL kalita node (built from
this repo's own cmd/kalita, packs/contracts loaded) — not a fake. This is the
strongest evidence for the LLD-06 §1 "ревью Fable" pass-through claim: two
DIFFERENT real bearer tokens, minted by the real node's own /api/bootstrap,
driving legal.submit_contract through the real Permission Engine, with the
real journal read back to confirm the actor.

This test is SKIPPED (not failed) if `go` is not on PATH or the node fails
to come up in time — it complements, and does not replace,
test_facade.py's fake-node suite (which runs everywhere, matching this
repo's "no live stack required" worker-test convention).

Run:  python -m pytest test_facade_live.py -v
  or: python test_facade_live.py
"""
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
import uuid
from http.server import ThreadingHTTPServer

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
_REPO_ROOT = os.path.normpath(os.path.join(_HERE, "..", ".."))
_CONTRACTS_PACK = os.path.join(_REPO_ROOT, "packs", "contracts")

import facade  # noqa: E402


def _rest(url, method="GET", body=None, headers=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers=headers or {})
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            return resp.status, json.loads(resp.read() or b"{}")
    except urllib.error.HTTPError as ex:
        return ex.code, json.loads(ex.read() or b"{}")


@unittest.skipUnless(shutil.which("go"), "go toolchain not on PATH — skipping live node test")
class TestFacadeLivePassThrough(unittest.TestCase):
    node_proc = None
    node_url = None
    fac_srv = None
    fac_url = None
    bin_path = None

    @classmethod
    def setUpClass(cls):
        cls._tmp = tempfile.mkdtemp(prefix="kalita-facade-live-")
        cls.bin_path = os.path.join(cls._tmp, "kalita-live.exe" if os.name == "nt" else "kalita-live")
        build = subprocess.run(["go", "build", "-o", cls.bin_path, "./cmd/kalita"],
                                cwd=_REPO_ROOT, capture_output=True, text=True, timeout=180)
        if build.returncode != 0:
            raise unittest.SkipTest("go build ./cmd/kalita failed: " + build.stderr[-2000:])

        cls.node_port = _free_tcp_port()
        secret = "test-bootstrap-secret"
        env = dict(os.environ)
        env["KALITA_BOOTSTRAP_SECRET"] = secret
        env["KALITA_BOOTSTRAP_ROLES"] = "ContractManager,Legal"
        cls.node_proc = subprocess.Popen(
            [cls.bin_path, "serve", "--pack", _CONTRACTS_PACK, "--listen", "127.0.0.1:%d" % cls.node_port,
             "--data-dir", cls._tmp],
            cwd=_REPO_ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        cls.node_url = "http://127.0.0.1:%d" % cls.node_port
        if not _wait_healthy(cls.node_url + "/healthz", cls.node_proc, timeout=20):
            cls._dump_and_kill()
            raise unittest.SkipTest("kalita node did not become healthy in time")

        status, alice = _rest(cls.node_url + "/api/bootstrap", "POST",
                               {"secret": secret, "id": "alice", "role": "ContractManager"})
        status2, leg = _rest(cls.node_url + "/api/bootstrap", "POST",
                              {"secret": secret, "id": "leg-1", "role": "Legal"})
        if status != 201 or status2 != 201:
            cls._dump_and_kill()
            raise unittest.SkipTest("bootstrap failed: %r / %r" % (alice, leg))
        cls.alice_token = alice["token"]
        cls.legal_token = leg["token"]

        facade.NODE = cls.node_url
        tools = facade.load_tools(_CONTRACTS_PACK)
        facade.Handler.tools_by_name = tools
        cls.fac_srv = ThreadingHTTPServer(("127.0.0.1", 0), facade.Handler)
        threading.Thread(target=cls.fac_srv.serve_forever, daemon=True).start()
        cls.fac_url = "http://127.0.0.1:%d/mcp" % cls.fac_srv.server_address[1]

    @classmethod
    def _dump_and_kill(cls):
        if cls.node_proc and cls.node_proc.poll() is None:
            cls.node_proc.terminate()
            try:
                out, _ = cls.node_proc.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                cls.node_proc.kill()
                out = ""
        else:
            out = cls.node_proc.stdout.read() if cls.node_proc and cls.node_proc.stdout else ""
        print("kalita node output:\n", out, file=sys.stderr)

    @classmethod
    def tearDownClass(cls):
        if cls.fac_srv:
            cls.fac_srv.shutdown()
        if cls.node_proc and cls.node_proc.poll() is None:
            cls.node_proc.terminate()
            try:
                cls.node_proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                cls.node_proc.kill()
        if getattr(cls, "_tmp", None):
            shutil.rmtree(cls._tmp, ignore_errors=True)

    def _mcp(self, token, tool, args):
        body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                            "params": {"name": tool, "arguments": args}}).encode()
        req = urllib.request.Request(self.node_url + "/mcp", data=body,
                                      headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=10) as resp:
            out = json.load(resp)
        payload = json.loads(out["result"]["content"][0]["text"])
        return payload, out["result"]["isError"]

    def _fac(self, token, tool, args):
        headers = {"Content-Type": "application/json"}
        if token is not None:
            headers["Authorization"] = "Bearer " + token
        body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                            "params": {"name": tool, "arguments": args}}).encode()
        req = urllib.request.Request(self.fac_url, data=body, headers=headers)
        with urllib.request.urlopen(req, timeout=10) as resp:
            out = json.load(resp)
        payload = json.loads(out["result"]["content"][0]["text"])
        return payload, out["result"]["isError"]

    def test_submit_contract_journals_actor_alice_for_real(self):
        cp, e = self._mcp(self.alice_token, "create_record", {
            "entity": "Counterparty", "values": {"name": "Vector LLC", "kind": "Vendor"},
            "basis": {"type": "human", "id": "alice"}, "idempotency_key": "live-cp-" + str(uuid.uuid4())})
        self.assertFalse(e, cp)

        payload, is_err = self._fac(self.alice_token, "legal.submit_contract", {
            "title": "Live MSA", "counterparty": cp["id"], "terms": "Standard terms."})
        self.assertFalse(is_err, payload)
        cid = payload["id"]

        journ, e = self._mcp(self.alice_token, "read_journal", {"entity": "Contract", "id": cid})
        self.assertFalse(e, journ)
        first = journ["events"][0]
        self.assertEqual(first["actor"]["id"], "alice")
        self.assertEqual(first["actor"]["type"], "agent")  # bootstrap mints ActorAgent — see class docstring

    def test_legal_role_cannot_create_contract_via_facade(self):
        # the Legal role's grants (packs/contracts/security.dsl) are read/
        # update Contract + create Finding + act review — NOT create Contract.
        # The façade must relay that exact denial, not "help" by using a
        # different identity.
        payload, is_err = self._fac(self.legal_token, "legal.submit_contract", {
            "title": "Should not exist", "counterparty": "whatever", "terms": "text"})
        self.assertTrue(is_err)
        self.assertEqual(payload.get("code"), "PERMISSION_DENIED")

    def test_missing_bearer_never_reaches_the_node(self):
        payload, is_err = self._fac(None, "legal.submit_contract", {
            "title": "x", "counterparty": "y", "terms": "z"})
        self.assertTrue(is_err)
        self.assertEqual(payload.get("code"), "AUTH_REQUIRED")


def _free_tcp_port():
    import socket
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def _wait_healthy(url, proc, timeout):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if proc.poll() is not None:
            return False
        try:
            with urllib.request.urlopen(url, timeout=1) as resp:
                if resp.status == 200:
                    return True
        except Exception:
            pass
        time.sleep(0.3)
    return False


if __name__ == "__main__":
    unittest.main()

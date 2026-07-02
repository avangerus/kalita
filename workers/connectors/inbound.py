#!/usr/bin/env python3
"""Inbound connector — the first member of kalita's connector layer, and the
scaffold the others (payment webhook, Bitrix24 event, calendar) copy.

The shape of every inbound connector is the same and lives here: receive an
UNTRUSTED external payload over HTTP, authenticate it with a shared secret, map
ONLY whitelisted fields onto a kalita entity, and create the record over MCP
under a least-privilege connector identity — so the node's permissions and audit
apply, and untrusted parsing never touches the core.

This instance bridges "a website form -> a CRM Lead": the site and the CRM can
live on different machines (even different kalita nodes) and still meet in one
governed journal. Point a form's POST at /intake with the shared secret.

Config (env KALITA_INTAKE_CONFIG = path to JSON), e.g.:
  {
    "entity": "Lead",
    "secret": "form-shared-secret",
    "field_map": {"name": "name", "company": "company", "email": "email"},
    "static":    {"source": "Web"}
  }
field_map maps INCOMING json key -> entity field; only these keys are accepted,
everything else in the payload is ignored (no field injection). `static` is set
on every record. With no config file a Lead default is used.

Identity: bootstrap as the Intake role (KALITA_BOOTSTRAP_SECRET + KALITA_WORKER_ID
+ KALITA_WORKER_ROLE=Intake) or pass KALITA_TOKEN. Listen: KALITA_INTAKE_LISTEN
(default 127.0.0.1:8300). Zero dependencies: stdlib only.
"""
import json
import os
import sys
import time
import urllib.request
import urllib.error
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from bootstrap import get_token  # noqa: E402

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
TOKEN = get_token()
LISTEN = os.environ.get("KALITA_INTAKE_LISTEN", "127.0.0.1:8300")
MAX_BODY = 64 * 1024  # an intake form is small; cap untrusted input hard

DEFAULT_CFG = {
    "entity": "Lead",
    "secret": os.environ.get("KALITA_INTAKE_SECRET", ""),
    "field_map": {"name": "name", "company": "company", "email": "email"},
    "static": {"source": "Web"},
}


def load_config():
    path = os.environ.get("KALITA_INTAKE_CONFIG")
    if path:
        with open(path, encoding="utf-8") as f:
            cfg = json.load(f)
        return {**DEFAULT_CFG, **cfg}
    return DEFAULT_CFG


CFG = load_config()


def mcp(name, arguments):
    """Call one MCP tool; return (payload, is_error)."""
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                       "params": {"name": name, "arguments": arguments}}).encode()
    req = urllib.request.Request(NODE + "/mcp", data=body, headers={
        "Authorization": "Bearer " + TOKEN, "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        out = json.load(resp)
    res = out.get("result", {})
    content = res.get("content", [])
    payload = json.loads(content[0]["text"]) if content else {}
    return payload, bool(res.get("isError"))


def create_with_retry(entity, basis, values, idem_key, attempts=3):
    """create_record, retrying a transient network failure under the SAME
    idempotency_key (LLD-01 §5, PR-5): one inbound POST is one task attempt —
    the key is generated once per request (see do_POST) and reused across
    these retries, so a retried Append dedups to the record already created
    on an earlier attempt instead of creating a duplicate."""
    last_err = None
    for i in range(attempts):
        try:
            return mcp("create_record", {"entity": entity, "basis": basis, "values": values,
                                         "idempotency_key": idem_key})
        except (urllib.error.URLError, TimeoutError) as ex:
            last_err = ex
            if i < attempts - 1:
                time.sleep(0.5 * (i + 1))
    raise last_err


def map_payload(raw):
    """Map an untrusted payload to entity values using the whitelist. Only keys in
    field_map are accepted, only primitive scalar values pass, statics are applied.
    Returns (values, error_message)."""
    if not isinstance(raw, dict):
        return None, "body must be a JSON object"
    values = dict(CFG.get("static") or {})
    for in_key, field in (CFG.get("field_map") or {}).items():
        if field is None or in_key not in raw:
            continue
        v = raw[in_key]
        if isinstance(v, (dict, list)):
            return None, f"field {in_key} must be a scalar"
        if v is None or v == "":
            continue
        values[field] = v
    return values, None


def constant_eq(a, b):
    if len(a) != len(b):
        return False
    r = 0
    for x, y in zip(a, b):
        r |= ord(x) ^ ord(y)
    return r == 0


class Handler(BaseHTTPRequestHandler):
    def _json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            return self._json(200, {"ok": True, "entity": CFG["entity"]})
        self._json(404, {"error": "not found"})

    def do_POST(self):
        if self.path.rstrip("/") != "/intake":
            return self._json(404, {"error": "not found"})
        secret = CFG.get("secret") or ""
        if secret and not constant_eq(self.headers.get("X-Intake-Secret", ""), secret):
            return self._json(401, {"error": "bad or missing X-Intake-Secret"})
        length = int(self.headers.get("Content-Length") or 0)
        if length <= 0 or length > MAX_BODY:
            return self._json(413, {"error": "empty or oversized body"})
        try:
            raw = json.loads(self.rfile.read(length).decode("utf-8", "strict"))
        except (ValueError, UnicodeDecodeError):
            return self._json(400, {"error": "body is not valid UTF-8 JSON"})

        values, err = map_payload(raw)
        if err:
            return self._json(400, {"error": err})

        basis = {"type": "rule", "id": "intake/" + CFG["entity"].lower()}
        # one idempotency_key per inbound attempt (this POST), reused across the
        # retries inside create_with_retry — LLD-01 §5 / PR-5.
        idem_key = str(uuid.uuid4())
        try:
            res, is_err = create_with_retry(CFG["entity"], basis, values, idem_key)
        except (urllib.error.URLError, TimeoutError) as ex:
            return self._json(502, {"error": "UPSTREAM_UNAVAILABLE", "message": str(ex)})
        if is_err:  # surface the node's governed validation verdict, nothing more
            return self._json(400, {"error": res.get("code", "REJECTED"),
                                    "message": res.get("message", "create rejected")})
        rid = res.get("id", "")
        print(f"[intake] {CFG['entity']} {rid[:8]} <- {list(values)}", flush=True)
        self._json(201, {"id": rid, "entity": CFG["entity"]})

    def log_message(self, *_):  # quiet the default stderr access log
        pass


def main():
    host, _, port = LISTEN.partition(":")
    srv = ThreadingHTTPServer((host, int(port)), Handler)
    print(f"intake connector up: POST http://{LISTEN}/intake -> {CFG['entity']} on {NODE}"
          + ("  (secret required)" if CFG.get("secret") else "  (no secret — dev only)"))
    srv.serve_forever()


if __name__ == "__main__":
    main()

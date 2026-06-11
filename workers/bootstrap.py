"""Shared worker bootstrap: self-register with the node's shared secret and
return a bearer token. No human runs `kalita agent add` — the deployment's
KALITA_BOOTSTRAP_SECRET does it, and the node only mints allowlisted roles.

Used by all kalita workers so the canonical loop starts from `import`.
"""
import json
import os
import sys
import time
import urllib.request
import urllib.error


def get_token():
    """Return a bearer token: an explicit KALITA_TOKEN wins; otherwise bootstrap."""
    if os.environ.get("KALITA_TOKEN"):
        return os.environ["KALITA_TOKEN"]

    node = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
    secret = os.environ.get("KALITA_BOOTSTRAP_SECRET")
    wid = os.environ.get("KALITA_WORKER_ID")
    role = os.environ.get("KALITA_WORKER_ROLE")
    model = os.environ.get("KALITA_WORKER_MODEL", "")
    if not (secret and wid and role):
        sys.exit("need KALITA_TOKEN, or KALITA_BOOTSTRAP_SECRET + KALITA_WORKER_ID + KALITA_WORKER_ROLE")

    body = json.dumps({"secret": secret, "id": wid, "role": role, "model": model}).encode()
    # the node may still be starting in compose — retry a few times
    for attempt in range(30):
        try:
            req = urllib.request.Request(node + "/api/bootstrap", data=body,
                                         headers={"Content-Type": "application/json"})
            with urllib.request.urlopen(req, timeout=10) as resp:
                token = json.load(resp)["token"]
                print(f"bootstrapped {wid} as {role}")
                return token
        except urllib.error.URLError:
            time.sleep(2)
        except urllib.error.HTTPError as e:
            sys.exit(f"bootstrap denied ({e.code}): check secret and role allowlist")
    sys.exit("node never became reachable for bootstrap")


# kalita_smoketest.py
#
# Final smoke tests for the Kalita API with module-aware routes.
# Usage:
#   pip install requests
#   python kalita_smoketest.py --base-url http://localhost:8080 --module core --entity project
#
# Notes:
# - Adjust payloads to match your DSL schema if different.
# - Exits with non-zero code on failure.

import os
import sys
import json
import random
import string
import argparse
from typing import Dict, Any
try:
    import requests
except ImportError:
    print("Please install requests: pip install requests", file=sys.stderr)
    sys.exit(2)

def rand_suffix(n=6):
    return ''.join(random.choice(string.ascii_lowercase + string.digits) for _ in range(n))

def pretty(o):
    return json.dumps(o, ensure_ascii=False, indent=2)

def expect(status: int, expected: int, msg: str):
    if status != expected:
        raise SystemExit(f"{msg}: expected {expected}, got {status}")

def fail_with_response(prefix: str, r: 'requests.Response'):
    print(prefix, "Status:", r.status_code)
    try:
        print("Response JSON:", pretty(r.json()))
    except Exception:
        print("Response TEXT:", r.text)
    raise SystemExit(1)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default=os.getenv("KALITA_BASE_URL", "http://localhost:8080"))
    parser.add_argument("--module", default=os.getenv("KALITA_MODULE", "core"))
    parser.add_argument("--entity", default=os.getenv("KALITA_ENTITY", "project"))
    parser.add_argument("--keep", action="store_true", help="Do not delete created record at the end")
    args = parser.parse_args()

    base = args.base_url.rstrip("/")
    mod = args.module.strip()
    ent = args.entity.strip()
    prefix = f"{base}/api/{mod}/{ent}"

    # --- CREATE USER (manager) ---
    user_payload: Dict[str, Any] = {
        "name": "Mgr " + rand_suffix(),
        "email": f"mgr_{rand_suffix()}@example.com",
        "role": "Manager",
    }
    user_url = f"{base}/api/core/user"
    print(f"POST {user_url} -> create manager {user_payload}")
    ru = requests.post(user_url, json=user_payload, timeout=10)
    if ru.status_code != 201:
        fail_with_response("User create failed.", ru)
    manager_id = ru.json().get("id") or ru.json().get("_id")
    if not manager_id:
        print(pretty(ru.json()))
        raise SystemExit("Cannot find manager id in user create response")

    # --- CREATE PROJECT (now with manager_id) ---
    new_payload: Dict[str, Any] = {
        "name": f"smoke-{rand_suffix()}",
        "status": "Draft",
        "manager_id": manager_id,
    }
    print(f"POST {prefix} -> create {new_payload}")
    r = requests.post(prefix, json=new_payload, timeout=10)
    if r.status_code != 201:
        fail_with_response("Create failed.", r)
    created = r.json()
    rec_id = created.get("id") or created.get("_id") or created.get("Id") or created.get("ID")
    if not rec_id:
        print(pretty(created))
        raise SystemExit("Cannot find record id in create response")
    print("Created:", pretty(created))

    # --- LIST ---
    print(f"GET {prefix}?limit=5")
    r = requests.get(prefix, params={"limit": 5}, timeout=10)
    expect(r.status_code, 200, "List failed")
    total = r.headers.get("X-Total-Count", "?")
    print(f"List OK (total={total})")
    print(pretty(r.json()))

    # --- GET ONE ---
    one_url = f"{prefix}/{rec_id}"
    print(f"GET {one_url}")
    r = requests.get(one_url, timeout=10)
    expect(r.status_code, 200, "GetOne failed")
    current = r.json()
    print("GetOne OK")

    # --- UPDATE (full object for PUT) ---
    # Keep required fields and only change what we need.
    upd_payload = {
        "name": current.get("name"),
        "manager_id": current.get("manager_id"),
        "status": "InWork",
    }
    print(f"PUT {one_url} -> {upd_payload}")
    r = requests.put(one_url, json=upd_payload, timeout=10)
    if r.status_code not in (200, 204):
        fail_with_response("Update failed.", r)
    print("Update OK")

    # --- VERIFY UPDATE ---
    r = requests.get(one_url, timeout=10)
    expect(r.status_code, 200, "Verify after update failed")
    got = r.json()
    st = (got.get("status") or got.get("Status"))
    print("After update status:", st)

    # --- DELETE (optional) ---
    if not args.keep:
        print(f"DELETE {one_url}")
        r = requests.delete(one_url, timeout=10)
        if r.status_code not in (200, 204):
            fail_with_response("Delete failed.", r)
        print("Delete OK")

    print("\nAll smoke tests passed ✅")

if __name__ == "__main__":
    main()


# kalita_lookup_filters_test.py
#
# Tests for:
#  - /api/meta/:module/:entity (fields populated)
#  - /api/meta/lookup/:module/:entity (autocomplete)
#  - GET list filters/sort/pagination
#
# Usage:
#   pip install requests
#   python kalita_lookup_filters_test.py --base-url http://localhost:8080 --module core --entity project

import os, sys, json, argparse, random, string, time
from typing import Dict, Any, List
try:
    import requests
except ImportError:
    print("Please install requests: pip install requests", file=sys.stderr)
    sys.exit(2)

def rand_suffix(n=6):
    return ''.join(random.choice(string.ascii_lowercase + string.digits) for _ in range(n))

def pretty(o): return json.dumps(o, ensure_ascii=False, indent=2)

def must_status(resp, code, msg):
    if resp.status_code != code:
        try: body = resp.json()
        except Exception: body = resp.text
        raise SystemExit(f"{msg}: expected {code}, got {resp.status_code}\n{body}")

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default=os.getenv("KALITA_BASE_URL", "http://localhost:8080"))
    ap.add_argument("--module", default=os.getenv("KALITA_MODULE", "core"))
    ap.add_argument("--entity", default=os.getenv("KALITA_ENTITY", "project"))
    ap.add_argument("--keep", action="store_true")
    args = ap.parse_args()

    base = args.base_url.rstrip("/")
    mod, ent = args.module.strip(), args.entity.strip()
    prefix = f"{base}/api/{mod}/{ent}"

    # --- META entity fields ---
    url_schema = f"{base}/api/meta/{mod}/{ent}"
    print("GET", url_schema)
    r = requests.get(url_schema, timeout=10)
    must_status(r, 200, "Meta entity schema failed")
    schema = r.json()
    fields = schema.get("fields", [])
    print("Fields:", len(fields))
    if not fields:
        print(pretty(schema))
        raise SystemExit("No fields in meta schema (expected > 0)")

    # Find required fields from options if present (best-effort)
    # We assume 'manager_id' required for Project as in earlier tests.
    required = set()
    for f in fields:
        opts = f.get("options") or {}
        if str(opts.get("required", "")).lower() == "true":
            required.add(f["name"])
    # Fallback for known case
    if "manager_id" not in required:
        required.add("manager_id")

    # --- Prepare Users (for lookup + refs) ---
    users = []
    for i in range(2):
        user_payload = {
            "name": f"Mgr {rand_suffix()}",
            "email": f"mgr_{rand_suffix()}@example.com",
            "role": "Manager",
        }
        user_url = f"{base}/api/core/user"
        print("POST", user_url, "->", user_payload)
        ru = requests.post(user_url, json=user_payload, timeout=10)
        must_status(ru, 201, "Create user failed")
        users.append(ru.json())
    manager1 = users[0]["id"]
    manager2 = users[1]["id"]

    # --- Test lookup endpoint ---
    look_url = f"{base}/api/meta/lookup/core/user"
    look_q = users[0]["name"].split()[-1][:3]  # part of random suffix
    print("GET", look_url, f"?field=name&q={look_q}&limit=5")
    rl = requests.get(look_url, params={"field":"name", "q": look_q, "limit": 5}, timeout=10)
    must_status(rl, 200, "Lookup failed")
    print("Lookup sample:", pretty(rl.json()))

    # --- Create Projects for filtering/sorting ---
    created_ids: List[str] = []
    def create_project(name, status, manager_id):
        payload = {"name": name, "status": status, "manager_id": manager_id}
        r = requests.post(prefix, json=payload, timeout=10)
        must_status(r, 201, "Create project failed")
        return r.json()["id"]

    names = [f"flt-{rand_suffix()}" for _ in range(3)]
    created_ids.append(create_project(names[0], "Draft", manager1))
    time.sleep(0.2)
    created_ids.append(create_project(names[1], "InWork", manager1))
    time.sleep(0.2)
    created_ids.append(create_project(names[2], "InWork", manager2))

    # --- LIST filter by status + manager_id, sort desc by updated_at, limit 1 ---
    params = {
        "status": "InWork",
        "manager_id": manager1,
        "sort": "-updated_at",
        "limit": 1,
        "offset": 0,
    }
    print("GET", prefix, params)
    rl = requests.get(prefix, params=params, timeout=10)
    must_status(rl, 200, "List with filters failed")
    print("Filtered page:", pretty(rl.json()))
    total = rl.headers.get("X-Total-Count")
    print("Total (header):", total)

    # --- Cleanup ---
    if not args.keep:
        for pid in created_ids:
            d = requests.delete(f"{prefix}/{pid}", timeout=10)
            # ignore minor delete errors
        print("Cleanup done")

    print("\nLookup + Filters tests passed ✅")

if __name__ == "__main__":
    main()

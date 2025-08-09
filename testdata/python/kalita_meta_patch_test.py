
# kalita_meta_patch_test.py
#
# Tests meta endpoints and PATCH (partial update) semantics.
#
# Usage:
#   pip install requests
#   python kalita_meta_patch_test.py --base-url http://localhost:8080 --module core --entity project
#

import os, sys, json, argparse, random, string
from typing import Dict, Any
try:
    import requests
except ImportError:
    print("Please install requests: pip install requests", file=sys.stderr)
    sys.exit(2)

def rand_suffix(n=6):
    import random, string
    return ''.join(random.choice(string.ascii_lowercase + string.digits) for _ in range(n))

def pretty(o): return json.dumps(o, ensure_ascii=False, indent=2)

def must_status(resp, code, msg):
    if resp.status_code != code:
        try:
            body = resp.json()
        except Exception:
            body = resp.text
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

    # --- META: list all entities
    url_list = f"{base}/api/meta/entities"
    print("GET", url_list)
    r = requests.get(url_list, timeout=10)
    must_status(r, 200, "Meta entities failed")
    entities_meta = r.json()
    print("meta.entities count:", len(entities_meta))
    # quick show first few
    print(pretty(entities_meta[: min(5, len(entities_meta)) ]))

    # --- META: schema for target entity
    url_schema = f"{base}/api/meta/{mod}/{ent}"
    print("GET", url_schema)
    r = requests.get(url_schema, timeout=10)
    must_status(r, 200, "Meta entity schema failed")
    schema = r.json()
    print("schema ok; fields:", len(schema.get("fields", [])))

    # --- CREATE supporting User (manager) ---
    user_payload = {
        "name": "Mgr " + rand_suffix(),
        "email": f"mgr_{rand_suffix()}@example.com",
        "role": "Manager",
    }
    user_url = f"{base}/api/core/user"
    print("POST", user_url, "->", user_payload)
    ru = requests.post(user_url, json=user_payload, timeout=10)
    must_status(ru, 201, "Create user failed")
    manager_id = ru.json().get("id")
    if not manager_id:
        print(pretty(ru.json())); raise SystemExit("no manager id")

    # --- CREATE entity record ---
    prefix = f"{base}/api/{mod}/{ent}"
    new_payload: Dict[str, Any] = {
        "name": f"meta-patch-{rand_suffix()}",
        "status": "Draft",
        "manager_id": manager_id,
    }
    print("POST", prefix, "->", new_payload)
    r = requests.post(prefix, json=new_payload, timeout=10)
    must_status(r, 201, "Create entity failed")
    created = r.json()
    rec_id = created.get("id")
    print("created id:", rec_id)

    # --- PATCH: change only status ---
    one_url = f"{prefix}/{rec_id}"
    patch_payload = {"status": "InWork"}
    print("PATCH", one_url, "->", patch_payload)
    r = requests.patch(one_url, json=patch_payload, timeout=10)
    must_status(r, 200, "PATCH failed")
    after = r.json()
    print("after.patch:", pretty(after))

    # --- VERIFY via GET ---
    r = requests.get(one_url, timeout=10)
    must_status(r, 200, "Get after patch failed")
    got = r.json()
    if got.get("status") != "InWork":
        raise SystemExit(f"PATCH not applied, status is {got.get('status')}")
    print("verify ok; status:", got.get("status"))

    # --- CLEANUP ---
    if not args.keep:
        print("DELETE", one_url)
        r = requests.delete(one_url, timeout=10)
        if r.status_code not in (200, 204):
            print("Delete failed:", r.status_code, r.text)
            sys.exit(1)
        print("deleted")

    print("\nMETA+PATCH tests passed ✅")

if __name__ == "__main__":
    main()

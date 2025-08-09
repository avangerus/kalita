
# kalita_list_count_restore_test.py
#
# Verifies: DELETE 204, RESTORE, _count, multi-sort, q search.
#
# Usage:
#   pip install requests
#   python kalita_list_count_restore_test.py --base-url http://localhost:8080 --module core --entity project
import os, sys, json, argparse, random, string, time
from typing import Dict, Any
try:
    import requests
except ImportError:
    print("Please install requests: pip install requests", file=sys.stderr)
    sys.exit(2)

def rands(n=6): return ''.join(random.choice(string.ascii_lowercase + string.digits) for _ in range(n))
def pretty(o): return json.dumps(o, ensure_ascii=False, indent=2)

def must(r, code, where):
    if r.status_code != code:
        try: body = r.json()
        except Exception: body = r.text
        raise SystemExit(f"{where}: expected {code}, got {r.status_code}\n{body}")

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default=os.getenv("KALITA_BASE_URL", "http://localhost:8080"))
    ap.add_argument("--module", default=os.getenv("KALITA_MODULE", "core"))
    ap.add_argument("--entity", default=os.getenv("KALITA_ENTITY", "project"))
    args = ap.parse_args()

    base = args.base_url.rstrip("/")
    mod, ent = args.module.strip(), args.entity.strip()
    prefix = f"{base}/api/{mod}/{ent}"
    count_url = f"{prefix}/_count"

    # ensure a manager user
    user_url = f"{base}/api/core/user"
    u = {"name":"Mgr "+rands(), "email": f"mgr_{rands()}@example.com", "role":"Manager"}
    ru = requests.post(user_url, json=u, timeout=10); must(ru, 201, "create user")
    manager_id = ru.json()["id"]

    # create three projects with names to test 'q' search and multi-sort
    p1 = {"name": "alpha-"+rands(), "status":"Draft", "manager_id": manager_id}
    p2 = {"name": "beta-"+rands(),  "status":"InWork","manager_id": manager_id}
    p3 = {"name": "beta-"+rands(),  "status":"InWork","manager_id": manager_id}
    r1 = requests.post(prefix, json=p1, timeout=10); must(r1, 201, "create p1")
    time.sleep(0.2)
    r2 = requests.post(prefix, json=p2, timeout=10); must(r2, 201, "create p2")
    time.sleep(0.2)
    r3 = requests.post(prefix, json=p3, timeout=10); must(r3, 201, "create p3")
    id1, id2, id3 = r1.json()["id"], r2.json()["id"], r3.json()["id"]

    # _count with filter status=InWork
    rc = requests.get(count_url, params={"status":"InWork"}, timeout=10)
    must(rc, 200, "_count")
    total = rc.json().get("total")
    print("COUNT InWork:", total)
    if total is None or total < 2:
        raise SystemExit("expected at least 2 InWork")

    # list with multi-sort: sort=-updated_at,name
    rl = requests.get(prefix, params={"status":"InWork", "sort":"-updated_at,name", "limit":10}, timeout=10)
    must(rl, 200, "list multi-sort")
    print("List multi-sort sample:", pretty(rl.json()))

    # q search: find by prefix 'alpha' in name
    rlq = requests.get(prefix, params={"q":"alpha"}, timeout=10)
    must(rlq, 200, "list q search")
    names = [x.get("name") for x in rlq.json()]
    print("q=alpha names:", names)
    if not any(n and n.startswith("alpha-") for n in names):
        raise SystemExit("q search didn't return alpha-*")

    # DELETE p1
    rd = requests.delete(f"{prefix}/{id1}", timeout=10); must(rd, 204, "delete p1")
    # get should 404
    rg = requests.get(f"{prefix}/{id1}", timeout=10)
    if rg.status_code != 404:
        raise SystemExit(f"get deleted expected 404, got {rg.status_code}")

    # RESTORE p1
    rr = requests.post(f"{prefix}/{id1}/restore", timeout=10); must(rr, 200, "restore p1")
    # get should 200 again
    rg2 = requests.get(f"{prefix}/{id1}", timeout=10); must(rg2, 200, "get after restore")

    print("\nList/Count/Restore tests passed ✅")

if __name__ == "__main__":
    main()

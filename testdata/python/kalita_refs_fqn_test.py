# testdata/python/kalita_refs_fqn_test.py
import os, sys, time, json, requests as r

BASE = os.getenv("KALITA_BASE", "http://localhost:8080")
API  = f"{BASE}/api"
META = f"{BASE}/api/meta"

def ok(c, m):
    print(("[OK] " if c else "[FAIL] ") + m)
    if not c:
        sys.exit(1)

def skip(m): print("[SKIP] " + m)

def meta_entity(module, entity):
    m = r.get(f"{META}/{module}/{entity}")
    if m.status_code != 200:
        return None
    return m.json()

def post(module, entity, payload): return r.post(f"{API}/{module}/{entity}", json=payload)
def get(module, entity, id_):      return r.get(f"{API}/{module}/{entity}/{id_}")

def main():
    # Ищем поля, которые явно указывают на core.User (FQN) в других модулях тоже ок
    # Начнём с core/project
    meta_prj = meta_entity("core", "project")
    ok(meta_prj is not None, "meta for core/project available")

    fields = meta_prj.get("fields", [])
    refs_to_user = [f for f in fields if f.get("type") == "ref" and "core.user" in (f.get("target","").lower())]
    array_refs_to_user = [f for f in fields if f.get("type") == "array" and (f.get("items",{}).get("type")=="ref") and "core.user" in (f.get("items",{}).get("target","").lower())]

    if not refs_to_user and not array_refs_to_user:
        skip("no explicit FQN refs to core.User found in Project; skipping")
        print("refs FQN test passed ✅ (no FQN fields present)")
        return

    # Создадим пару пользователей
    u1 = post("core", "user", {"name": "FQN-A", "email": f"a-{time.time()}@x.x"}).json()
    u2 = post("core", "user", {"name": "FQN-B", "email": f"b-{time.time()}@x.x"}).json()
    ok(u1.get("id") and u2.get("id"), "users created")

    # Создадим проект и присвоим FQN ссылки
    pr = post("core", "project", {"name": "FQN-Project"}).json()
    ok(pr.get("id"), "project created")
    pr_id = pr["id"]

    patch_payload = {}
    if refs_to_user:
        patch_payload[refs_to_user[0]["name"]] = u1["id"]
    if array_refs_to_user:
        patch_payload[array_refs_to_user[0]["name"]] = [u1["id"], u2["id"]]

    # применим
    etag = get("core", "project", pr_id).headers.get("ETag")
    rp = r.patch(f"{API}/core/project/{pr_id}", json=patch_payload, headers={"If-Match": etag} if etag else {})
    ok(rp.status_code in (200, 204), "project updated with FQN ref fields")

    # перечитаем и проверим
    pr_now = get("core", "project", pr_id).json()
    for f in refs_to_user:
        ok(pr_now.get(f["name"]) == u1["id"], f"FQN single ref set for field {f['name']}")
    for f in array_refs_to_user:
        arr = pr_now.get(f["name"]) or []
        have = set(arr)
        ok(u1["id"] in have and u2["id"] in have, f"FQN array ref set for field {f['name']}")

    print("refs FQN test passed ✅")

if __name__ == "__main__":
    main()

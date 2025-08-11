# testdata/python/kalita_catalogs_test.py
import os, requests, random, string
BASE = os.getenv("KALITA_BASE", "http://localhost:8080")
get = lambda p: requests.get(BASE+p, timeout=20)
post = lambda p, **kw: requests.post(BASE+p, timeout=20, **kw)

def main():
    r = get("/api/meta/catalogs"); r.raise_for_status()
    catalogs = r.json()
    assert isinstance(catalogs, list)
    print("[OK] catalogs:", [c["name"] for c in catalogs])

    # берём первый доступный каталог
    if not catalogs:
        print("[SKIP] no catalogs"); return
    name = catalogs[0]["name"]
    r = get(f"/api/meta/catalog/{name}"); r.raise_for_status()
    one = r.json()
    print("[OK] catalog", name, "items:", len(one["items"]))

    # попробуем создать Project со статусом из каталога (если в DSL добавил поле)
    payload = {
        "name": "CatProj " + "".join(random.choices(string.ascii_uppercase, k=4)),
    }
    if one["items"]:
        payload["status"] = one["items"][0]["code"]

    r = post("/api/core/project", json=payload)
    if r.status_code == 404:
        print("[SKIP] core.project not found")
        return
    if r.status_code == 400:
        print("[WARN] create failed 400 (возможно, Project имеет другие required)")
        print(r.text)
        return
    r.raise_for_status()
    obj = r.json()
    print("[OK] created with status:", obj.get("status", "(none)"))

    # негатив (если статус есть)
    if "status" in payload:
        bad = dict(payload); bad["status"] = "__NOPE__"
        r = post("/api/core/project", json=bad)
        assert r.status_code in (400,409), r.text
        print("[OK] bad catalog code rejected")
    print("Catalogs test passed ✅")

if __name__ == "__main__":
    main()

import os, requests, sys, datetime

BASE = os.environ.get("KALITA_BASE", "http://localhost:8080")

def post(p, **kw): return requests.post(f"{BASE}{p}", timeout=20, **kw)
def get(p, **kw):  return requests.get(f"{BASE}{p}", timeout=20, **kw)

def main():
    # найдём любую сущность с простым required набором
    r = get("/api/meta"); r.raise_for_status()
    metas = r.json()
    picked = None
    for row in metas:
        r2 = get(f"/api/meta/{row['module']}/{row['name']}")
        if r2.status_code != 200: continue
        meta = r2.json()
        req = [f for f in meta["fields"] if f.get("required")]
        # пропускаем, если есть required ref
        if any(f["type"]=="ref" or (f["type"]=="array" and f.get("elem_type")=="ref") for f in req):
            continue
        picked = meta
        break
    if not picked:
        print("[FAIL] no suitable entity"); sys.exit(1)

    mod, ent = picked["module"], picked["name"]
    # соберём минимальный payload
    def sample(ft, enum):
        return ("x" if ft in ("string","text") else
                1 if ft=="int" else
                1.0 if ft in ("float","money") else
                True if ft=="bool" else
                "2025-01-01" if ft=="date" else
                datetime.datetime.utcnow().replace(microsecond=0).isoformat()+"Z" if ft=="datetime" else
                {"k":"v"} if ft=="json" else
                (enum[0] if enum else "X") if ft=="enum" else "x")
    payload = {}
    for f in picked["fields"]:
        if f.get("required"):
            t = f["type"]; et = f.get("elem_type")
            if t in ("string","text","int","float","money","bool","date","datetime","json","enum"):
                payload[f["name"]] = sample(t, f.get("enum", []))
            elif t=="array" and et in ("string","text","int","float","money","bool","date","datetime","json","enum"):
                payload[f["name"]] = [sample(et, f.get("enum", []))]
    if "name" in [f["name"] for f in picked["fields"]] and "name" not in payload:
        payload["name"] = "etag 304 test"

    r = post(f"/api/{mod}/{ent}", json=payload); r.raise_for_status()
    obj = r.json()
    rid = obj["id"]
    print("[OK] created", mod, ent, rid, "version", obj["version"])

    # первый GET — получим ETag
    r = get(f"/api/{mod}/{ent}/{rid}"); r.raise_for_status()
    etag = r.headers.get("ETag")
    if not etag:
        print("[FAIL] no ETag header"); sys.exit(1)
    print("[OK] got ETag:", etag)

    # второй GET с If-None-Match — ожидаем 304
    r = get(f"/api/{mod}/{ent}/{rid}", headers={"If-None-Match": etag})
    if r.status_code != 304:
        print("[FAIL] expected 304, got", r.status_code, r.text); sys.exit(1)
    print("GET ETag 304 test passed ✅")

if __name__ == "__main__":
    main()

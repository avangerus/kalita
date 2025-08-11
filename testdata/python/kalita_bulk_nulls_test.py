import os, requests, datetime, sys, json

BASE = os.environ.get("KALITA_BASE", "http://localhost:8080")
SIMPLE = {"string","text","int","float","money","bool","date","datetime","json","enum"}

def get(p, **kw): return requests.get(f"{BASE}{p}", timeout=20, **kw)
def post(p, **kw): return requests.post(f"{BASE}{p}", timeout=20, **kw)
def patch(p, **kw): return requests.patch(f"{BASE}{p}", timeout=20, **kw)

def sample_value(ftype, enum_vals):
    if ftype in ("string","text"): return "x"
    if ftype == "int": return 1
    if ftype in ("float","money"): return 1.0
    if ftype == "bool": return True
    if ftype == "date": return "2025-01-01"
    if ftype == "datetime": return datetime.datetime.utcnow().replace(microsecond=0).isoformat()+"Z"
    if ftype == "json": return {"k":"v"}
    if ftype == "enum": return (enum_vals[0] if enum_vals else "X")
    return "x"

def normalize_fields(fields):
    out = []
    for f in fields:
        out.append({
            "name": f.get("name") or f.get("Name"),
            "type": f.get("type") or f.get("Type"),
            "required": f.get("required", f.get("Required", False)),
            "enum": f.get("enum", f.get("Enum", [])),
            "elem_type": f.get("elem_type", f.get("Elem", "")),
        })
    return out

def build_payload(meta, extra=None):
    data = {}
    for f in meta["fields"]:
        if f["required"]:
            t, et = f["type"], f.get("elem_type")
            if t in SIMPLE:
                data[f["name"]] = sample_value(t, f.get("enum", []))
            elif t == "array" and et in SIMPLE:
                data[f["name"]] = [sample_value(et, f.get("enum", []))]
            else:
                # required ref/array[ref] — эту сущность лучше не использовать
                raise ValueError("required non-simple field")
    if extra: data.update(extra)
    # часто есть name — добавим для удобства, если отсутствует
    if "name" in [f["name"] for f in meta["fields"]] and "name" not in data:
        data["name"] = "bulk nulls test"
    return data

def find_working_entity():
    # получаем список сущностей
    r = get("/api/meta"); r.raise_for_status()
    meta_list = r.json()
    for row in meta_list:
        mod, ent = row["module"], row["name"]
        r2 = get(f"/api/meta/{mod}/{ent}")
        if r2.status_code != 200: 
            continue
        meta = r2.json()
        meta["fields"] = normalize_fields(meta["fields"])

        # пропускаем сущности с обязательными ref
        skip = False
        for f in meta["fields"]:
            if f["required"] and (f["type"] == "ref" or (f["type"]=="array" and f.get("elem_type")=="ref")):
                skip = True; break
        if skip: 
            continue

        # ищем НЕобязательное простое поле
        cand = None
        for f in meta["fields"]:
            if (not f["required"]) and (f["type"] in SIMPLE) and f["name"] not in ("id","created_at","updated_at","version"):
                cand = f; break
        if not cand: 
            continue

        # соберём payload и попробуем создать 2 записи
        try:
            p1 = build_payload(meta, {cand["name"]: "A"})
            p2 = build_payload(meta, {cand["name"]: "B"})
        except ValueError:
            continue

        r = post(f"/api/{mod}/{ent}", json=p1)
        if r.status_code >= 400:
            # пробуем следующую сущность
            continue
        o1 = r.json()

        r = post(f"/api/{mod}/{ent}", json=p2)
        if r.status_code >= 400:
            continue
        o2 = r.json()

        return (mod, ent, cand, o1, o2)

    raise RuntimeError("Не удалось найти подходящую сущность для теста (нужна без required ref и с необязательным простым полем)")

def main():
    try:
        mod, ent, fld, o1, o2 = find_working_entity()
    except Exception as e:
        print("[FAIL] discovery:", e)
        sys.exit(1)

    field = fld["name"]
    print(f"[INFO] Using {mod}.{ent} field={field} type={fld['type']}")
    print("[OK] Created:", o1["id"], o2["id"])

    items = [
        {"id": o1["id"], "version": o1["version"], "patch": {field: None}},
        {"id": o2["id"], "version": o2["version"], "patch": {field: None}},
    ]
    r = patch(f"/api/{mod}/{ent}/_bulk", json=items)
    if r.status_code != 207:
        print("[FAIL] bulk assign expected 207, got", r.status_code, r.text); sys.exit(1)
    res = r.json()
    for i, it in enumerate(res):
        if it.get(field) is not None:
            print("[FAIL] bulk assign item", i, "expected", field, "= None; got", it.get(field)); sys.exit(1)
    print("[OK] bulk assign -> field is None for all")

    v1, v2 = res[0]["version"], res[1]["version"]
    items = [
        {"id": o1["id"], "version": v1, "patch": {field: None}},
        {"id": o2["id"], "version": v2, "patch": {field: None}},
    ]
    r = patch(f"/api/{mod}/{ent}/_bulk", params={"nulls": "delete"}, json=items)
    if r.status_code != 207:
        print("[FAIL] bulk delete expected 207, got", r.status_code, r.text); sys.exit(1)
    res = r.json()
    for i, it in enumerate(res):
        if field in it:
            print("[FAIL] bulk delete item", i, "expected no", field, "key; got", list(it.keys())); sys.exit(1)
    print("Bulk nulls mode test passed ✅")

if __name__ == "__main__":
    main()

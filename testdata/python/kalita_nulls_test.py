import os, requests, datetime, sys

BASE = os.environ.get("KALITA_BASE", "http://localhost:8080")

SIMPLE_TYPES = {"string","text","int","float","money","bool","date","datetime","json","enum"}

def get(path, **kw):
    r = requests.get(f"{BASE}{path}", timeout=20, **kw)
    return r

def post(path, **kw):
    r = requests.post(f"{BASE}{path}", timeout=20, **kw)
    return r

def patch(path, **kw):
    r = requests.patch(f"{BASE}{path}", timeout=20, **kw)
    return r

def pick_entity_and_field():
    # 1) список всех сущностей
    r = get("/api/meta")
    r.raise_for_status()
    metas = r.json()  # [{module,name,fields}]
    # 2) пройтись и найти сущность с НЕобязательным простым полем (для теста null)
    for m in metas:
        fqn = f"{m['module']}.{m['name']}"
        r2 = get(f"/api/meta/{m['module']}/{m['name']}")
        r2.raise_for_status()
        meta = r2.json()
        fields = meta["fields"]
        # required простые поля (чтобы уметь создать запись)
        req_simple = [f for f in fields if f.get("Required") or f.get("required")]
        # нормализуем ключи
        for f in fields:
            f["required"] = f.get("required", f.get("Required", False))
            f["type"] = f.get("type", f.get("Type"))
            f["enum"] = f.get("enum", f.get("Enum", []))
            f["elem_type"] = f.get("elem_type", f.get("Elem", ""))
        # Отфильтруем required, которые не простые (ref/array/…)
        can_create = True
        for f in fields:
            t = f["type"]
            if f["required"]:
                if t == "ref" or (t == "array" and f.get("elem_type") == "ref"):
                    can_create = False
                    break
                if (t not in SIMPLE_TYPES) and not (t=="array" and f.get("elem_type") in SIMPLE_TYPES):
                    can_create = False
                    break
        if not can_create:
            continue
        # Найти НЕобязательное простое поле (которое и будем занулять/удалять)
        candidate = None
        for f in fields:
            t = f["type"]
            if not f["required"] and (t in SIMPLE_TYPES) and f["name"] not in ("id","created_at","updated_at","version"):
                candidate = f
                break
        if not candidate:
            continue
        return meta, candidate
    raise RuntimeError("Не нашёл подходящую сущность/поле для теста (нужна без required ref и с необязательным простым полем).")

def sample_value(ftype, enum_vals):
    if ftype == "string" or ftype == "text":
        return "hello"
    if ftype == "int":
        return 1
    if ftype == "float" or ftype == "money":
        return 1.0
    if ftype == "bool":
        return True
    if ftype == "date":
        return "2025-01-01"
    if ftype == "datetime":
        return datetime.datetime.utcnow().replace(microsecond=0).isoformat() + "Z"
    if ftype == "json":
        return {"k":"v"}
    if ftype == "enum":
        return (enum_vals[0] if enum_vals else "Unknown")
    return None

def build_min_payload(meta):
    payload = {}
    for f in meta["fields"]:
        t = f["type"]
        f["required"] = f.get("required", f.get("Required", False))
        f["enum"] = f.get("enum", f.get("Enum", []))
        if f["required"]:
            if t == "ref" or (t == "array" and f.get("elem_type") == "ref"):
                # такие сущности пропускаем заранее в pick_entity_and_field
                pass
            elif t in SIMPLE_TYPES:
                payload[f["name"]] = sample_value(t, f["enum"])
            elif t == "array" and f.get("elem_type") in SIMPLE_TYPES:
                payload[f["name"]] = [sample_value(f.get("elem_type"), f.get("enum", []))]
    # всегда попробуем добавить name если есть и не задано
    if "name" in [f["name"] for f in meta["fields"]] and "name" not in payload:
        payload["name"] = "Nulls test"
    return payload

def main():
    try:
        meta, field = pick_entity_and_field()
    except Exception as e:
        print("[FAIL] meta discovery:", e)
        sys.exit(1)

    mod = meta["module"]
    ent = meta["name"]
    fld = field["name"]
    print(f"[INFO] Picked {mod}.{ent}, field '{fld}' (type={field['type']})")

    payload = build_min_payload(meta)
    payload.setdefault(fld, "temp")  # чтобы поле существовало

    r = post(f"/api/{mod}/{ent}", json=payload)
    if r.status_code >= 400:
        print("[FAIL] create failed:", r.status_code, r.text)
        sys.exit(1)
    obj = r.json()
    rid = obj["id"]; ver = obj["version"]
    print("[OK] Created", rid)

    # 1) assign (по умолчанию): поле станет null
    r = patch(f"/api/{mod}/{ent}/{rid}", json={"version": ver, fld: None})
    if r.status_code >= 400:
        print("[FAIL] patch(assign) failed:", r.status_code, r.text); sys.exit(1)
    obj = r.json()
    if obj.get(fld) is not None:
        print("[FAIL] expected", fld, "= None in assign mode; got:", obj.get(fld)); sys.exit(1)
    print("[OK] assign mode →", fld, "is None")
    ver = obj["version"]

    # 2) delete: ключ должен исчезнуть
    r = patch(f"/api/{mod}/{ent}/{rid}", params={"nulls":"delete"}, json={"version": ver, fld: None})
    if r.status_code >= 400:
        print("[FAIL] patch(delete) failed:", r.status_code, r.text); sys.exit(1)
    obj = r.json()
    if fld in obj:
        print("[FAIL] expected key", fld, "to be absent in delete mode; got keys:", list(obj.keys())); sys.exit(1)
    print("[OK] delete mode →", fld, "removed")

    print("Nulls mode test passed ✅")

if __name__ == "__main__":
    main()

import os, re, sys, requests, datetime

BASE = os.getenv("KALITA_BASE", "http://localhost:8080")
get  = lambda p, **kw: requests.get(BASE+p, timeout=20, **kw)
post = lambda p, **kw: requests.post(BASE+p, timeout=20, **kw)

SIMPLE = {"string","text","int","float","money","bool","date","datetime","json","enum"}

def sample(ft, enum):
    if ft in ("string","text"): return "ok"
    if ft=="int": return 1
    if ft in ("float","money"): return 1.0
    if ft=="bool": return True
    if ft=="date": return "2025-01-01"
    if ft=="datetime": return datetime.datetime.utcnow().replace(microsecond=0).isoformat()+"Z"
    if ft=="json": return {"k":"v"}
    if ft=="enum": return (enum[0] if enum else "X")
    return "ok"

def norm_fields(fields):
    out=[]
    for f in fields:
        opt = f.get("options") or f.get("Options") or {}
        out.append({
            "name": f.get("name") or f.get("Name"),
            "type": (f.get("type") or f.get("Type") or "").lower(),
            "elem_type": (f.get("elem_type") or f.get("Elem") or "").lower(),
            "required": f.get("required", f.get("Required", False)),
            "enum": f.get("enum", f.get("Enum", [])),
            "options": { str(k).lower(): str(v) for k,v in opt.items() },
        })
    return out

def build_payload(meta):
    data={}
    for f in meta["fields"]:
        if f["required"]:
            t,et = f["type"], f["elem_type"]
            # пропускаем сущности, где есть required ref
            if t=="ref" or (t=="array" and et=="ref"):
                raise ValueError("required ref")
            if t in SIMPLE:
                data[f["name"]]=sample(t, f["enum"])
            elif t=="array" and et in SIMPLE:
                data[f["name"]]=[sample(et, f["enum"])]
    # частый случай: name
    if "name" in [f["name"] for f in meta["fields"]] and "name" not in data:
        data["name"]="constraints test"
    return data

def find_field_with_constraints():
    r = get("/api/meta"); r.raise_for_status()
    for row in r.json():
        mod, ent = row["module"], row["name"]
        r2 = get(f"/api/meta/{mod}/{ent}")
        if r2.status_code != 200: continue
        meta = r2.json()
        meta["fields"]=norm_fields(meta["fields"])

        # пропустим сущности где есть required ref — тяжело собирать тестовые данные
        if any(f["required"] and (f["type"]=="ref" or (f["type"]=="array" and f["elem_type"]=="ref")) for f in meta["fields"]):
            continue

        # ищем строковое поле с max_len/pattern (не системное, необязательное — проще проверять)
        for f in meta["fields"]:
            if f["type"] in ("string","text") and not f["required"] and f["name"] not in ("id","created_at","updated_at","version"):
                opts=f["options"]
                has_len = "max_len" in opts or "min_len" in opts
                has_pat = "pattern" in opts and opts["pattern"]
                if has_len or has_pat:
                    return (mod, ent, meta, f)
    raise RuntimeError("не нашёл поле с max_len/pattern без required ref")

def main():
    try:
        mod, ent, meta, fld = find_field_with_constraints()
    except Exception as e:
        print("[FAIL]", e); sys.exit(1)

    print(f"[INFO] Picked {mod}.{ent}.{fld['name']} type={fld['type']} opts={fld['options']}")
    payload = build_payload(meta)

    # 1) положительный кейс (в пределах ограничений)
    ok_value = "ok"
    if "max_len" in fld["options"]:
        ml = int(fld["options"]["max_len"])
        ok_value = "x"*min(ml, 5)
    if "pattern" in fld["options"]:
        pat = fld["options"]["pattern"]
        # простая эвристика: если паттерн явно латиница/цифры/пробел/подчёркивание/дефис — возьмём 'OK_1'
        if re.fullmatch(r"^[\[\]\^\$\w\-\ \+\*\.\?\(\)\|\\]+$", pat):
            ok_value = "OK_1"
    payload[fld["name"]] = ok_value
    r = post(f"/api/{mod}/{ent}", json=payload); r.raise_for_status()
    obj = r.json()
    print("[OK] create within constraints:", obj.get(fld["name"]))

    # 2) превышение max_len (если есть)
    if "max_len" in fld["options"]:
        bad = build_payload(meta)
        bad[fld["name"]] = "x"*(int(fld["options"]["max_len"])+10)
        r = post(f"/api/{mod}/{ent}", json=bad)
        assert r.status_code in (400,409), r.text
        print("[OK] max_len rejected:", r.json().get("errors"))

    # 3) нарушение pattern (если есть)
    if "pattern" in fld["options"]:
        bad = build_payload(meta)
        bad[fld["name"]] = "русские буквы"  # заведомо не латиница
        r = post(f"/api/{mod}/{ent}", json=bad)
        assert r.status_code in (400,409), r.text
        print("[OK] pattern rejected:", r.json().get("errors"))

    print("Constraints test passed ✅")

if __name__ == "__main__":
    main()

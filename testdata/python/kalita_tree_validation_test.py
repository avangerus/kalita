import os, sys, time, requests as r, random, string

BASE = os.getenv("KALITA_BASE", "http://localhost:8080")
API  = f"{BASE}/api"
META = f"{BASE}/api/meta"

def ok(c,m): print(("[OK] " if c else "[FAIL] ")+m); sys.exit(1) if not c else None

def meta_list():
    res = r.get(f"{META}")
    res.raise_for_status()
    data = res.json()
    # ожидаем массив [{module,name,...}]
    return [(x["module"], x["name"]) for x in data if isinstance(x, dict) and x.get("module") and x.get("name")]

def meta_entity(mod, ent):
    res = r.get(f"{META}/{mod}/{ent}")
    if res.status_code != 200:
        return None
    return res.json()

def pick_tree_entity():
    """
    Ищем сущность с полем parent_id, которое ref на ту же сущность.
    Отбрасываем кандидатов, у которых есть обязательные ref-поля (кроме parent_id),
    чтобы можно было создать запись без предварительных зависимостей.
    """
    for mod, ent in meta_list():
        me = meta_entity(mod, ent)
        if not me: 
            continue
        fields = me.get("fields", [])
        byname = {f["name"]: f for f in fields}
        f_parent = byname.get("parent_id")
        if not f_parent:
            continue
        # parent_id должен быть ref на ту же сущность
        is_ref = (f_parent.get("type") == "ref")
        target = (f_parent.get("ref") or f_parent.get("target") or "").lower()
        if not (is_ref and target == f"{mod}.{ent}".lower()):
            continue

        # проверяем, что нет других обязательных ссылок
        hard_refs = []
        for f in fields:
            if f["name"] == "parent_id":
                continue
            if f.get("required") is True and f.get("type") == "ref":
                hard_refs.append(f["name"])
        if hard_refs:
            # пропустим таких, тест заточен на простые деревья
            continue

        return mod, ent, me
    return None, None, None

def gen_val(f):
    t = f.get("type")
    nm = f.get("name")
    # подгенерим уникальные значения для string
    if t == "string":
        if nm == "code":
            return f"A-{int(time.time()*1e6)}"
        return f"{nm}-{int(time.time()*1e6)}"
    if t == "int":
        return random.randint(1, 10**6)
    if t == "float":
        return random.random() * 1000
    if t == "money":
        return round(random.random() * 1000, 2)
    if t == "bool":
        return True
    if t == "date":
        return "2025-01-01"
    if t == "datetime":
        return "2025-01-01T12:00:00Z"
    if t == "enum":
        # берём первое значение из списка
        opts = f.get("enum") or f.get("values") or []
        if isinstance(opts, list) and opts:
            return opts[0] if isinstance(opts[0], str) else (opts[0].get("value") or opts[0].get("code") or opts[0].get("name"))
    # всё остальное пропускаем
    return None

def build_min_payload(me):
    payload = {}
    for f in me.get("fields", []):
        if f.get("required") is True:
            if f.get("type") in ("ref","array"):
                # в этом тесте не удовлетворяем обязательные ссылки (кроме parent_id, которую зададим позже)
                if f["name"] != "parent_id":
                    return None
            else:
                v = gen_val(f)
                if v is None:
                    return None
                payload[f["name"]] = v
    # name/code очень часто обязательные — если не требуются, можно всё равно подставить
    if "name" in {f["name"] for f in me.get("fields", [])} and "name" not in payload:
        payload["name"] = f"Node-{int(time.time()*1e6)}"
    if "code" in {f["name"] for f in me.get("fields", [])} and "code" not in payload:
        payload["code"] = f"C-{int(time.time()*1e6)}"
    return payload

def create(mod, ent, payload):
    # делаем копию и гарантируем уникальные поля на каждый вызов
    j = dict(payload)
    ts = str(int(time.time()*1e6))
    if "code" in j:
        j["code"] = f"{j['code']}-{ts}"
    if "name" in j:
        j["name"] = f"{j['name']}-{ts}"

    res = r.post(f"{API}/{mod}/{ent}", json=j)
    try:
        data = res.json()
    except Exception:
        data = {"_raw": res.text}
    if "id" not in data:
        print(f"[POST /{mod}/{ent}] status={res.status_code} body={data}")
        sys.exit(1)
    return data["id"]

def get(mod, ent, id_):
    return r.get(f"{API}/{mod}/{ent}/{id_}")

def patch(mod, ent, id_, j, etag=None):
    h = {"If-Match": etag} if etag else {}
    return r.patch(f"{API}/{mod}/{ent}/{id_}", json=j, headers=h)

def main():
    mod, ent, me = pick_tree_entity()
    if not me:
        print("[SKIP] No simple tree entity with parent_id self-ref found in meta")
        print("       Add an entity like 'Account' with parent_id: ref[Account] and rerun.")
        sys.exit(0)

    print(f"[INFO] Using {mod}.{ent} for tree test")

    # минимальный payload для создания узлов без parent_id
    payload = build_min_payload(me)
    if payload is None:
        print(f"[SKIP] {mod}.{ent} has hard required references other than parent_id; skipping")
        sys.exit(0)

    # создаём A, затем B -> A, затем C -> B
    A = create(mod, ent, dict(payload))
    B = create(mod, ent, dict(payload, **{"parent_id": A}))
    C = create(mod, ent, dict(payload, **{"parent_id": B}))
    ok(A and B and C, "created A,B,C chain")

    # self-parent запрет
    e = get(mod, ent, A)
    etag = e.headers.get("ETag")
    resp = patch(mod, ent, A, {"parent_id": A}, etag)
    ok(resp.status_code in (400, 409), "self-parent is rejected")

    # цикл: A <- B <- C, пробуем A.parent = C
    e = get(mod, ent, A); etag = e.headers.get("ETag")
    resp = patch(mod, ent, A, {"parent_id": C}, etag)
    ok(resp.status_code in (400, 409), "cycle is rejected")

    print("tree validation test passed ✅")

if __name__ == "__main__":
    main()

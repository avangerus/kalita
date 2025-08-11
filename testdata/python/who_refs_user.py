import requests as r

BASE = "http://localhost:8080"

def safe_json(resp):
    try:
        return resp.json()
    except Exception:
        print(f"[warn] {resp.url} non-JSON, status={resp.status_code}, snippet={resp.text[:200]!r}")
        return None

def list_entities():
    # ваш эндпоинт возвращает массив [{module,name,fields}, ...]
    resp = r.get(f"{BASE}/api/meta")
    data = safe_json(resp)
    out = []
    if isinstance(data, list):
        for row in data:
            mod = row.get("module")
            name = row.get("name")
            if mod and name:
                out.append((mod, name))
    return out

def meta_entity(mod, ent):
    return safe_json(r.get(f"{BASE}/api/meta/{mod}/{ent}"))

def main():
    pairs = list_entities()
    if not pairs:
        print("[FAIL] cannot list entities from /api/meta")
        return

    hits = []
    for mod, ent in pairs:
        me = meta_entity(mod, ent)
        if not me:
            continue
        # ожидаем формат как в твоём MetaEntityHandler: resp.Fields = [] с json-ключами
        for f in me.get("fields", []):
            # одиночная ссылка
            if f.get("type") == "ref" and str(f.get("ref","")).lower() == "core.user":
                hits.append((f"{mod}.{ent}", f["name"], "ref", f.get("onDelete")))
            # массив ссылок
            it = f.get("items") or {}
            if f.get("type") == "array" and it.get("type") == "ref" and str(f.get("ref","") or it.get("ref","")).lower() == "core.user":
                hits.append((f"{mod}.{ent}", f["name"], "array[ref]", f.get("onDelete")))

    if not hits:
        print("Refs to core.User: none found.")
        return

    print("Refs to core.User:")
    for modent, field, kind, pol in hits:
        print(f" - {modent} field: {field} | {kind} | onDelete: {pol}")

if __name__ == "__main__":
    main()

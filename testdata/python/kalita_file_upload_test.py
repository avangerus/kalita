#!/usr/bin/env python3
import io
import sys
import json
import requests
from datetime import date, datetime, timezone

BASE = "http://localhost:8080"
S = requests.Session()

def url(p: str) -> str:
    if not p.startswith("/"):
        p = "/" + p
    return BASE + p

def fail(msg: str):
    print(msg)
    sys.exit(1)

def rq_get(path: str, expect: int = 200):
    r = S.get(url(path), timeout=20)
    if r.status_code != expect:
        fail(f"[FAIL] GET {path} expected {expect}, got {r.status_code}: {r.text}")
    return r

def rq_post(path: str, **kwargs):
    return S.post(url(path), timeout=30, **kwargs)

def find_entity_with_attachment_field():
    r = rq_get("/api/meta")
    items = r.json()
    for it in items:
        mod = it.get("module")
        ent = it.get("name")
        m = rq_get(f"/api/meta/{mod}/{ent}").json()
        for f in m.get("fields", []):
            if f.get("type") == "ref" and f.get("ref") == "core.Attachment":
                return (mod, ent, f["name"], False, m)
            if f.get("type") == "array" and f.get("elem_type") == "ref" and f.get("ref") == "core.Attachment":
                return (mod, ent, f["name"], True, m)
    return None

def list_one_id(mod, ent):
    r = rq_get(f"/api/{mod}/{ent}?limit=1")
    arr = r.json()
    if isinstance(arr, list) and arr:
        return arr[0]["id"]
    return None

def build_minimal_value(field):
    t = field.get("type")
    if t in ("string", "text", "markdown", "richtext"):
        return "Temp"
    if t == "enum":
        vals = field.get("enum") or []
        return vals[0] if vals else "Value"
    if t in ("int", "float", "money", "decimal", "percent", "rate"):
        return 1
    if t == "bool":
        return True
    if t == "date":
        return date.today().isoformat()
    if t == "datetime":
        return datetime.now(timezone.utc).isoformat().replace("+00:00","Z")
    if t == "array":
        elem = field.get("elem_type")
        if elem in ("string", "text"):
            return ["Temp"]
        return []
    # fallback
    return "Temp"

def build_minimal_payload(meta):
    payload = {}
    required_refs = []
    for f in meta.get("fields", []):
        if not f.get("required"):
            continue
        name = f["name"]
        t = f.get("type")
        # пропускаем обязательные ref — создадим отдельно
        if t == "ref" and f.get("ref"):
            required_refs.append((name, f.get("ref")))
            continue
        if t == "array" and f.get("elem_type") == "ref":
            required_refs.append((name, f.get("ref")))
            continue
        payload[name] = build_minimal_value(f)
    return payload, required_refs

def ensure_record_exists(mod, ent, meta, depth=0, seen=None):
    """Возвращает id существующей/созданной записи для сущности."""
    if seen is None:
        seen = set()
    key = f"{mod}.{ent}"
    if key in seen or depth > 3:
        return None  # защита от циклов
    seen.add(key)

    rid = list_one_id(mod, ent)
    if rid:
        return rid

    payload, req_refs = build_minimal_payload(meta)

    # сначала обеспечим ref'ы
    for fname, refFQN in req_refs:
        # refFQN должен быть в формате module.Entity (наш /api/meta так отдаёт)
        if "." not in refFQN:
            # на всякий случай, но по нашему MetaEntityHandler тут уже FQN
            return None
        rmod, rent = refFQN.split(".", 1)
        # найдём мету целевой сущности
        rmeta = rq_get(f"/api/meta/{rmod}/{rent}").json()
        ref_id = ensure_record_exists(rmod, rent, rmeta, depth+1, seen)
        if not ref_id:
            return None
        # подставим в payload (для array ref вставим список)
        # если поле объявлено как массив ссылок — оставим массив
        field_def = next((f for f in meta.get("fields", []) if f["name"] == fname), None)
        if field_def and field_def.get("type") == "array":
            payload[fname] = [ref_id]
        else:
            payload[fname] = ref_id

    cr = rq_post(f"/api/{mod}/{ent}", json=payload)
    if cr.status_code not in (200, 201):
        print(f"[SKIP] Auto-create {mod}.{ent} failed: {cr.status_code} {cr.text}")
        return None
    obj = cr.json()
    return obj.get("id")

def main():
    found = find_entity_with_attachment_field()
    if not found:
        print("[SKIP] No entity with ref[core.Attachment] field found in meta.")
        sys.exit(0)
    mod, ent, field, is_array, meta = found
    print(f"[INFO] Using {mod}.{ent}.{field} (array={is_array})")

    rec_id = list_one_id(mod, ent)
    if not rec_id:
        # создадим с учётом обязательных ссылок (Project, Currency, status, name и т.д.)
        rec_id = ensure_record_exists(mod, ent, meta)
        if not rec_id:
            print(f"[SKIP] Can't auto-create {mod}.{ent} with required refs")
            sys.exit(0)
        print(f"[INFO] Auto-created {mod}.{ent} id={rec_id}")
    else:
        print(f"[INFO] Using existing {mod}.{ent} id={rec_id}")

    content = b"hello kalita file test"
    files = {"file": ("test.txt", io.BytesIO(content), "text/plain")}

    up = rq_post(f"/api/{mod}/{ent}/{rec_id}/_file/{field}", files=files)
    if up.status_code != 200:
        fail(f"[FAIL] upload failed {up.status_code}: {up.text}")
    uj = up.json()
    att_id = uj.get("attachment_id")
    if not att_id:
        fail(f"[FAIL] upload response missing attachment_id: {uj}")
    print(f"[OK] Upload succeeded: attachment_id={att_id}")

    dl = rq_get(f"/api/core/attachment/{att_id}/download")
    body = dl.content
    if body != content:
        fail("[FAIL] downloaded content does not match uploaded content")
    ct = dl.headers.get("Content-Type", "")
    if ct and not ct.startswith("text/plain"):
        fail(f"[FAIL] expected Content-Type 'text/plain', got '{ct}'")
    print("[OK] Download content and Content-Type verified")

    print("\nFile upload/download test passed ✅")

if __name__ == "__main__":
    main()


#!/usr/bin/env python3
import requests, sys, datetime, json

BASE = "http://localhost:8080"

def jbody(r):
    try:
        return r.json()
    except Exception:
        return r.text

def get_meta_list():
    return requests.get(f"{BASE}/api/meta")

def get_meta_entity(mod, ent):
    return requests.get(f"{BASE}/api/meta/{mod}/{ent}")

def get_catalog(name):
    return requests.get(f"{BASE}/api/meta/catalog/{name}")

def choose_entity(meta):
    for item in meta:
        mod, ent = item.get("module"), item.get("entity")
        m = get_meta_entity(mod, ent)
        if m.status_code != 200:
            continue
        schema = m.json()
        fields = schema.get("fields", [])
        has_patchable = any(
            (f.get("type") == "string" and not (f.get("options") or {}).get("readonly"))
            for f in fields
        )
        feasible = True
        for f in fields:
            if str((f.get("options") or {}).get("required", "")).lower() == "true":
                if f.get("type") == "ref":
                    feasible = False
                    break
        if has_patchable and feasible:
            return (mod, ent, schema)
    return None

def sample_value_for_field(f):
    t = f.get("type")
    opts = f.get("options") or {}
    enum = f.get("enum") or []
    et = f.get("elemType")
    if "default" in opts and opts["default"] is not None:
        return opts["default"]
    if t == "string": return "x"
    if t == "int": return 1
    if t in ("float","money"): return 1.0
    if t == "bool": return True
    if t == "date": return datetime.date.today().isoformat()
    if t == "datetime": return datetime.datetime.utcnow().replace(microsecond=0).isoformat() + "Z"
    if t == "enum":
        if enum: return enum[0]
        cat = opts.get("catalog")
        if cat:
            rc = get_catalog(cat)
            if rc.status_code == 200:
                items = (rc.json() or {}).get("items", []) if isinstance(rc.json(), dict) else []
                if items:
                    return items[0].get("code") or items[0].get("id") or items[0].get("name") or "X"
        return "X"
    if t == "array":
        if et in ("string","int","float","bool","money","date","datetime"):
            fake = {"type": et, "options": {}}
            return [sample_value_for_field(fake)]
        if et == "enum" and enum:
            return [enum[0]]
        return []
    return None

def build_min_payload(schema):
    out = {}
    for f in schema.get("fields", []):
        name = f.get("name")
        opts = f.get("options") or {}
        if opts.get("readonly"): continue
        if str(opts.get("required","")).lower() == "true":
            v = sample_value_for_field(f)
            if v is None: return None
            out[name] = v
        else:
            if f.get("type") == "string" and name not in out:
                out[name] = "base"
    return out or None

def post_entity(mod, ent, payload):
    return requests.post(f"{BASE}/api/{mod}/{ent}", json=payload)

def patch_entity(mod, ent, id_, payload, etag=None):
    h = {"Content-Type":"application/json"}
    if etag is not None:
        h["If-Match"] = f'"{etag}"'
    return requests.patch(f"{BASE}/api/{mod}/{ent}/{id_}", json=payload, headers=h)

def get_one(mod, ent, id_):
    return requests.get(f"{BASE}/api/{mod}/{ent}/{id_}")

def run_flow(mod, ent, schema):
    print(f"[i] Using {mod}.{ent}")
    payload = build_min_payload(schema)
    if payload is None:
        print("[!] Can't build minimal payload (required ref?)")
        return 2
    r = post_entity(mod, ent, payload)
    if not (200 <= r.status_code < 300):
        print("[!] CREATE failed", r.status_code, jbody(r)); return 2
    created = r.json(); print("[OK] CREATE", created)
    id_ = created.get("id"); v1 = created.get("version")

    # patch no If-Match
    patch_field = None
    for f in schema.get("fields", []):
        if f.get("type") == "string" and not (f.get("options") or {}).get("readonly"):
            patch_field = f.get("name"); break
    r = patch_entity(mod, ent, id_, {patch_field:"patched"})
    if not (200 <= r.status_code < 300):
        if r.status_code == 404:
            print("[-] PATCH 404. Проверь роутер: внутри apiGroup должен быть путь '/:module/:entity/:id'")
        print("[!] PATCH failed", r.status_code, jbody(r)); return 2
    patched = r.json(); print("[OK] PATCH", patched)
    v2 = patched.get("version")

    # conflict
    r = patch_entity(mod, ent, id_, {patch_field:"conflict"}, etag=v1)
    print("[Conflict expected]", r.status_code, jbody(r))

    # readonly
    r = patch_entity(mod, ent, id_, {"id":"zzz","version":999})
    print("[Readonly expected]", r.status_code, jbody(r))

    # unique (если есть явный уник по одному из полей)
    uniq_field = None
    for f in schema.get("fields", []):
        if str((f.get("options") or {}).get("unique","")).lower() == "true":
            uniq_field = f.get("name"); break
    if uniq_field:
        r2 = post_entity(mod, ent, payload | {"name":"second"} if "name" in payload else payload)
        if 200 <= r2.status_code < 300:
            second = r2.json()
            r3 = patch_entity(mod, ent, second["id"], {uniq_field: created[uniq_field]})
            print("[Unique expected]", r3.status_code, jbody(r3))
        else:
            print("[i] Skip unique — second create failed:", r2.status_code, jbody(r2))
    return 0

def main():
    # Plan A: discover via meta
    r = get_meta_list()
    if 200 <= r.status_code < 300:
        meta = r.json()
        picked = choose_entity(meta)
        if picked:
            mod, ent, schema = picked
            sys.exit(run_flow(mod, ent, schema))
    print("[i] Meta not available or no suitable entity — fallback to test.Item")

    # Plan B: fallback to test.Item
    # схему опишем вручную (минимум) — code:string required [unique], name:string
    mod, ent = "test", "item"
    schema = {
        "fields":[
            {"name":"code", "type":"string", "options":{"required":"true"}},
            {"name":"name", "type":"string", "options":{}}
        ]
    }
    # пробуем сразу
    r = requests.get(f"{BASE}/api/{mod}/{ent}?limit=1")
    if r.status_code == 404:
        print("[!] Fallback entity test.item не найдена. Добавь DSL:"
              "module test\n\nentity Item:\n  code: string required\n  name: string")
        sys.exit(3)
    sys.exit(run_flow(mod, ent, schema))

if __name__ == "__main__":
    main()

#!/usr/bin/env python3
import requests, json

BASE = "http://localhost:8080"

def pp(j):
    print(json.dumps(j, ensure_ascii=False, indent=2))

# список всех сущностей
r = requests.get(f"{BASE}/api/meta")
print("[/api/meta]", r.status_code)
pp(r.json())

# пройтись по каждой сущности и вывести её мету
for ent in r.json():
    mod, e = ent["module"], ent["entity"]
    url = f"{BASE}/api/meta/{mod}/{e}"
    rr = requests.get(url)
    print(f"\n=== {mod}.{e} === {rr.status_code}")
    meta = rr.json()
    for f in meta.get("fields", []):
        if f.get("type") == "ref" or f.get("elemType") == "ref":
            print("  REF FIELD:", f["name"], "->", f.get("ref"))
    # можно раскомментировать, чтобы смотреть всю схему
    # pp(meta)

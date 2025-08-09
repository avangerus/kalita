#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import sys, time, requests
BASE="http://localhost:8080"
H={"Content-Type":"application/json"}

def u(*p): return "/".join(s.strip("/") for s in p)

def make_company(name):
    r=requests.post(u(BASE,"api","olga","company"), json={"name":name}, headers=H, timeout=30)
    if r.status_code not in (200,201): print("CREATE fail", r.status_code, r.text); sys.exit(1)
    return r.json()

def bulk_patch(items):
    r=requests.patch(u(BASE,"api","olga","company","_bulk"), json=items, headers=H, timeout=30)
    return r

def main():
    # создадим 3 компании
    c1 = make_company(f"BPV {int(time.time())}-1")
    c2 = make_company(f"BPV {int(time.time())}-2")
    c3 = make_company(f"BPV {int(time.time())}-3")

    # 1) без версий (должны вернуться 409 на все)
    r = bulk_patch([
        {"id": c1["id"], "patch": {"name": "X1"}},
        {"id": c2["id"], "patch": {"name": "X2"}},
    ])
    if r.status_code != 207:
        print("Expected 207, got", r.status_code, r.text); sys.exit(1)
    body = r.json()
    bad = [it for it in body if "errors" not in it or it["errors"][0]["code"]!="version_conflict"]
    if bad:
        print("Expected version_conflict for all, got", body); sys.exit(1)
    print("[OK] bulk PATCH without versions rejected with 409 for each item")

    # 2) смешанный запрос: один с правильной версией, один с неправильной
    ok_ver = c1["version"]
    wrong_ver = c2["version"] - 1 if c2["version"]>0 else 0
    r = bulk_patch([
        {"id": c1["id"], "patch": {"name": "OK1"}, "version": ok_ver},
        {"id": c2["id"], "patch": {"name": "BAD"}, "version": wrong_ver},
        # поддержка legacy-формата (наложится поверх, если у тебя реализована):
        # {"ids":[c3["id"]], "patch":{"name":"LEGACY"}}
    ])
    if r.status_code != 207:
        print("Expected 207, got", r.status_code, r.text); sys.exit(1)
    body = r.json()

    # найдём по id
    bmap = {}
    for it in body:
        if isinstance(it, dict) and it.get("id"):
            bmap[it["id"]] = it

    ok_item = bmap.get(c1["id"])
    bad_item = bmap.get(c2["id"])

    if not ok_item or ok_item.get("name")!="OK1":
        print("First item should be updated", ok_item); sys.exit(1)
    if not bad_item or "errors" not in bad_item or bad_item["errors"][0]["code"]!="version_conflict":
        print("Second item should be version_conflict", bad_item); sys.exit(1)
    print("[OK] bulk PATCH handled per-item version checks (one success, one conflict)")

    print("\nBulk PATCH versions test passed ✅")

if __name__=="__main__":
    main()

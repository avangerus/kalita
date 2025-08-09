#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import sys, time, requests
BASE="http://localhost:8080"
H={"Content-Type":"application/json"}

def u(*p): return "/".join(s.strip("/") for s in p)

def create_company(name):
    r=requests.post(u(BASE,"api","olga","company"), json={"name":name}, headers=H, timeout=30)
    if r.status_code not in (200,201): raise SystemExit(f"CREATE fail {r.status_code} {r.text}")
    return r.json()["id"]

def get_company(cid):
    return requests.get(u(BASE,"api","olga","company",cid), timeout=30)

def bulk_delete(ids):
    return requests.post(u(BASE,"api","olga","company","_bulk_delete"), json={"ids":ids}, headers=H, timeout=30)

def bulk_restore(ids):
    return requests.post(u(BASE,"api","olga","company","_bulk_restore"), json={"ids":ids}, headers=H, timeout=30)

def main():
    # создаём 3 компании
    ids=[create_company(f"BDR-{int(time.time())}-{i}") for i in range(3)]
    print("[OK] created:", ids)

    # bulk delete
    r=bulk_delete(ids)
    if r.status_code!=207: print("Bulk delete expected 207, got", r.status_code, r.text); sys.exit(1)
    print("[OK] bulk delete returned 207")

    # убедимся, что они не находятся обычным GET
    for cid in ids:
        g=get_company(cid)
        if g.status_code!=404:
            print("Expected 404 after delete for", cid, "got", g.status_code, g.text); sys.exit(1)
    print("[OK] individual GETs return 404 after delete")

    # bulk restore
    r=bulk_restore(ids)
    if r.status_code!=207: print("Bulk restore expected 207, got", r.status_code, r.text); sys.exit(1)
    print("[OK] bulk restore returned 207")

    # убедимся, что снова доступны
    for cid in ids:
        g=get_company(cid)
        if g.status_code!=200:
            print("Expected 200 after restore for", cid, "got", g.status_code, g.text); sys.exit(1)
    print("[OK] individual GETs return 200 after restore")

    print("\nBulk delete/restore test passed ✅")

if __name__=="__main__":
    main()

#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import sys, time, requests
BASE="http://localhost:8080"
H={"Content-Type":"application/json"}

def u(*p): return "/".join(s.strip("/") for s in p)

def create_company(name):
    r=requests.post(u(BASE,"api","olga","company"), json={"name":name}, headers=H, timeout=30)
    if r.status_code not in (200,201): print("CREATE fail", r.status_code, r.text); sys.exit(1)
    return r.json()

def get_company(cid):
    return requests.get(u(BASE,"api","olga","company",cid), timeout=30)

def patch_company(cid, patch, if_match=None):
    hh=H.copy()
    if if_match is not None:
        hh["If-Match"]=str(if_match)
    return requests.patch(u(BASE,"api","olga","company",cid), json=patch, headers=hh, timeout=30)

def put_company(cid, body, if_match=None):
    hh=H.copy()
    if if_match is not None:
        hh["If-Match"]=str(if_match)
    return requests.put(u(BASE,"api","olga","company",cid), json=body, headers=hh, timeout=30)

def main():
    # 1) create
    c=create_company(f"VerCo {int(time.time())}")
    cid=c["id"]; ver=c["version"]
    print("[OK] created:", cid, "version", ver)

    # 2) PATCH без версии → 409
    r=patch_company(cid, {"name":"X1"})
    if r.status_code!=409:
        print("Expected 409 on PATCH without version, got", r.status_code, r.text); sys.exit(1)
    print("[OK] PATCH without version rejected")

    # 3) PATCH с неправильной версией → 409
    r=patch_company(cid, {"name":"X2","version":ver-1})
    if r.status_code!=409:
        print("Expected 409 on PATCH wrong version, got", r.status_code, r.text); sys.exit(1)
    print("[OK] PATCH wrong version rejected")

    # 4) PATCH с правильной версией → 200 и ver+1
    r=patch_company(cid, {"name":"X3","version":ver})
    if r.status_code!=200:
        print("PATCH with correct version failed", r.status_code, r.text); sys.exit(1)
    c=r.json()
    if c["version"]!=ver+1:
        print("Expected version", ver+1, "got", c["version"]); sys.exit(1)
    print("[OK] PATCH with correct version accepted, version", c["version"])

    # 5) GET и PUT с If-Match
    g=get_company(cid)
    if g.status_code!=200: print("GET fail", g.status_code, g.text); sys.exit(1)
    cur=g.json(); v=cur["version"]

    # PUT без If-Match/без version → 409
    r=put_company(cid, {"name":"X4"})
    if r.status_code!=409:
        print("Expected 409 on PUT without version, got", r.status_code, r.text); sys.exit(1)
    print("[OK] PUT without version rejected")

    # PUT с If-Match → 200
    r=put_company(cid, {"name":"X5"}, if_match=v)
    if r.status_code!=200:
        print("PUT with If-Match failed", r.status_code, r.text); sys.exit(1)
    cur=r.json()
    print("[OK] PUT with If-Match accepted, new version", cur["version"])

    print("\nVersion conflict test passed ✅")

if __name__=="__main__":
    main()

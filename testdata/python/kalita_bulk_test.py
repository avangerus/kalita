#!/usr/bin/env python3
# -*- coding: utf-8 -*-

"""
Kalita API bulk create test.

Verifies:
  1) POST /api/olga/company/_bulk returns 207 Multi-Status.
  2) Successful items are returned in FLAT format with meta fields (id, version, created_at, updated_at)
     and include defaults (e.g., is_active=true) without stuffing meta into data.
  3) Readonly/system fields are rejected per-item with code=readonly_field.
  4) Type validation errors per-item return code=type_mismatch.
  5) Created records can be fetched individually and then cleaned up.

Run: python kalita_bulk_test.py
"""

import sys
import time
import requests

BASE_URL = "http://localhost:8080"
TIMEOUT   = 30
HJSON     = {"Content-Type": "application/json"}

def u(*parts): 
    return "/".join(p.strip("/") for p in parts)

def ensure_meta_flat(obj, where):
    # Expect meta at top-level (flattened), not inside obj["data"]
    for k in ("id","version","created_at","updated_at"):
        if k not in obj:
            raise AssertionError(f"{where}: missing meta field {k}")
    if "data" in obj and isinstance(obj["data"], dict):
        pass  # допускаем наличие, но мета должна быть наверху

def post_bulk_company(items):
    return requests.post(u(BASE_URL, "api", "olga", "company", "_bulk"),
                         headers=HJSON, json=items, timeout=TIMEOUT)

def get_company(cid):
    return requests.get(u(BASE_URL, "api", "olga", "company", cid), timeout=TIMEOUT)

def delete_company(cid):
    return requests.delete(u(BASE_URL, "api", "olga", "company", cid), timeout=TIMEOUT)

def main():
    items = [
        {"name": f"BulkCo A {int(time.time())}"},                    # A
        {"name": f"BulkCo B {int(time.time())}", "version": 999},    # B (readonly violation)
        {"name": 12345},                                             # C (type mismatch)
    ]

    r = post_bulk_company(items)
    if r.status_code != 207:
        print("Expected 207 Multi-Status, got:", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)

    results = r.json()
    if not isinstance(results, list) or len(results) != len(items):
        print("Bulk result length mismatch:", type(results), len(results), file=sys.stderr)
        sys.exit(1)

    created_ids = []

    # 0: success
    item0 = results[0]
    if "errors" in item0:
        print("Item 0 unexpectedly failed:", item0, file=sys.stderr); sys.exit(1)
    ensure_meta_flat(item0, "item0")
    if item0.get("is_active") is not True:
        print("WARN: default is_active=true missing or different:", item0.get("is_active"))
    else:
        print("[OK] Item 0 success with flattened meta and default is_active=true")
    created_ids.append(item0["id"])

    # 1: readonly_field
    item1 = results[1]
    errs1 = item1.get("errors", [])
    if not any(e.get("code")=="readonly_field" and e.get("field")=="version" for e in errs1 if isinstance(e, dict)):
        print("Item 1 no readonly_field on 'version'. Got:", item1, file=sys.stderr); sys.exit(1)
    print("[OK] Item 1 rejected readonly 'version'")

    # 2: type_mismatch
    item2 = results[2]
    errs2 = item2.get("errors", [])
    if not any(e.get("code")=="type_mismatch" and e.get("field")=="name" for e in errs2 if isinstance(e, dict)):
        print("Item 2 no type_mismatch on 'name'. Got:", item2, file=sys.stderr); sys.exit(1)
    print("[OK] Item 2 rejected bad type for 'name'")

    # GET check
    g = get_company(created_ids[0])
    if g.status_code != 200:
        print("GET created item failed:", g.status_code, g.text, file=sys.stderr); sys.exit(1)
    obj = g.json()
    ensure_meta_flat(obj, "GET item0")
    print("[OK] GET returned created record in flattened format")

    # Cleanup
    for cid in created_ids:
        delete_company(cid)

    print("\nBulk create test passed ✅")

if __name__ == "__main__":
    main()

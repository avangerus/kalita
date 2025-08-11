#!/usr/bin/env python3
import os
import sys
import json
import time
from typing import Any, Dict
import requests

BASE = os.getenv("KALITA_BASE", "http://localhost:8080")
S = requests.Session()

def url(p: str) -> str:
    if not p.startswith("/"):
        p = "/" + p
    return BASE + p

def pretty(o: Any) -> str:
    return json.dumps(o, ensure_ascii=False, indent=2, sort_keys=True)

def fail(msg: str):
    print(msg)
    sys.exit(1)

def get(path: str, expect: int = 200):
    r = S.get(url(path), timeout=10)
    if r.status_code != expect:
        fail(f"[FAIL] GET {path} expected {expect}, got {r.status_code}: {r.text}")
    return r

def post(path: str, body: Dict[str, Any], expect: int = 200):
    r = S.post(url(path), json=body, timeout=15)
    if r.status_code != expect:
        fail(f"[FAIL] POST {path} expected {expect}, got {r.status_code}: {r.text}")
    return r

def main():
    # 0) sanity: API meta works
    r0 = get("/api/meta")
    meta0 = r0.json()
    if not isinstance(meta0, list) or len(meta0) == 0:
        print("[WARN] /api/meta returned empty or unexpected payload")
    else:
        print(f"[OK] /api/meta returned {len(meta0)} entities (before reload)")

    # 1) happy path: reload with defaults
    r1 = post("/api/admin/reload", {"dsl_root": "dsl", "enums_root": "reference/enums"})
    j1 = r1.json()
    if not (j1.get("ok") is True and isinstance(j1.get("entities"), int)):
        fail(f"[FAIL] reload response shape unexpected:\n{pretty(j1)}")
    print(f"[OK] reload succeeded: entities={j1.get('entities')} enumGroups={j1.get('enumGroups')}")

    # 2) API still works after reload
    r2 = get("/api/meta")
    meta2 = r2.json()
    print(f"[OK] /api/meta still works after reload (entities={len(meta2) if isinstance(meta2, list) else 'n/a'})")

    # 3) negative: invalid path should yield 400
    r3 = S.post(url("/api/admin/reload"), json={"dsl_root": "__no_such_dir__"}, timeout=10)
    if r3.status_code != 400:
        fail(f"[FAIL] reload with bad path expected 400, got {r3.status_code}: {r3.text}")
    else:
        print("[OK] reload with bad dsl_root rejected with 400")

    print("\nAdmin reload test passed ✅")

if __name__ == "__main__":
    main()

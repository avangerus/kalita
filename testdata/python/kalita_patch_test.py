#!/usr/bin/env python3
# -*- coding: utf-8 -*-

# Kalita API PATCH test (point 3.3: UpdatePartialHandler)
# Just run: python kalita_patch_test.py
#
# What it checks:
#  1) POST creates a record and returns a flattened object (id/version/created_at/updated_at + data)
#  2) PATCH (normal fields) succeeds and bumps version/updated_at
#  3) PATCH with a readonly/system field returns 400 with code='readonly_field'
#  4) GET confirms that the normal PATCH persisted
#
# Adjust CONFIG below if needed.

import sys
import time
import requests

# ==== CONFIG ====
BASE_URL = "http://localhost:8080"
MODULE   = "core"
ENTITY   = "user"
CREATE_PAYLOAD = {
    "name": "Ivan",
    "email": "ivan@example.com",
    "role": "Manager"
}
PATCH_OK = {
    "name": "Ivan Updated"
}
PATCH_READONLY = {
    "created_at": "2025-01-01T00:00:00Z"
}
HEADERS  = {
    "Content-Type": "application/json"
}
REQUIRED_META = ["id", "version", "created_at", "updated_at"]
# ================

def ensure_flattened(obj, where):
    missing = [k for k in REQUIRED_META if k not in obj]
    if missing:
        raise AssertionError(f"{where}: Missing meta fields: {missing}. Got keys: {list(obj.keys())[:20]}")

def main():
    base = BASE_URL.rstrip("/")
    path = f"{base}/api/{MODULE}/{ENTITY}"

    # 1) Create
    r = requests.post(path, headers=HEADERS, json=CREATE_PAYLOAD, timeout=30)
    if r.status_code not in (200, 201):
        print("CREATE failed", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    created = r.json()
    ensure_flattened(created, "CREATE response")
    rec_id = created["id"]
    old_ver = created.get("version", 0)
    print("[OK] CREATE flattened. id:", rec_id, "version:", old_ver)

    # small sleep to make updated_at visibly change on some systems
    time.sleep(0.5)

    # 2) PATCH (ok)
    r = requests.patch(f"{path}/{rec_id}", headers=HEADERS, json=PATCH_OK, timeout=30)
    if r.status_code != 200:
        print("PATCH(OK) failed", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    patched = r.json()
    ensure_flattened(patched, "PATCH(OK) response")
    if patched.get("name") != PATCH_OK["name"]:
        print("PATCH(OK) did not update 'name'", file=sys.stderr)
        sys.exit(1)
    if patched.get("version", 0) <= old_ver:
        print("PATCH(OK) did not bump version", file=sys.stderr)
        sys.exit(1)
    print("[OK] PATCH(OK) updated name and bumped version to", patched.get("version"))

    # 3) PATCH (readonly/system) — expect 400 with code='readonly_field'
    r = requests.patch(f"{path}/{rec_id}", headers=HEADERS, json=PATCH_READONLY, timeout=30)
    if r.status_code != 400:
        print("PATCH(READONLY) expected 400, got", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    body = r.json()
    errs = body.get("errors") or []
    has_ro = any((e.get("code") == "readonly_field") for e in errs if isinstance(e, dict))
    if not has_ro:
        print("PATCH(READONLY) did not return code=readonly_field. Errors:", errs, file=sys.stderr)
        sys.exit(1)
    print("[OK] PATCH(READONLY) returned 400 with readonly_field")

    # 4) GET to confirm persisted OK patch
    r = requests.get(f"{path}/{rec_id}", headers=HEADERS, timeout=30)
    if r.status_code != 200:
        print("GET failed", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    got = r.json()
    ensure_flattened(got, "GET response")
    if got.get("name") != PATCH_OK["name"]:
        print("GET value mismatch for 'name' after PATCH(OK)", file=sys.stderr)
        sys.exit(1)
    print("[OK] GET after patches is consistent.")

    print("\nAll PATCH tests passed ✅")

if __name__ == "__main__":
    main()

#!/usr/bin/env python3
# -*- coding: utf-8 -*-

# Kalita API default-field test (uses DSL defaults)
# Just run: python kalita_default_test.py
#
# It will:
#  1) POST /api/olga/company with only required field(s)
#  2) Verify response is flattened and has is_active==true (default applied)
#  3) GET by id to confirm persisted default

import sys
import requests

BASE_URL = "http://localhost:8080"
MODULE   = "olga"
ENTITY   = "company"
HEADERS  = {"Content-Type": "application/json"}

REQUIRED_META = ["id", "version", "created_at", "updated_at"]

def ensure_flattened(obj, where):
    missing = [k for k in REQUIRED_META if k not in obj]
    if missing:
        raise AssertionError(f"{where}: Missing meta fields: {missing}")

def main():
    base = BASE_URL.rstrip("/")
    path = f"{base}/api/{MODULE}/{ENTITY}"

    # 1) Create with minimal payload (name is required, code optional)
    payload = {"name": "TestCo-"}
    r = requests.post(path, headers=HEADERS, json=payload, timeout=30)
    if r.status_code not in (200, 201):
        print("CREATE failed", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    created = r.json()
    ensure_flattened(created, "CREATE")
    rec_id = created["id"]
    print("[OK] CREATE:", rec_id)

    # 2) Expect default is_active == true
    if created.get("is_active") is not True:
        print("Default not applied: expected is_active=true, got:", created.get("is_active"), file=sys.stderr)
        sys.exit(1)
    print("[OK] default is_active==true applied at CREATE")

    # 3) GET and verify persisted
    r = requests.get(f"{path}/{rec_id}", headers=HEADERS, timeout=30)
    if r.status_code != 200:
        print("GET failed", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    got = r.json()
    ensure_flattened(got, "GET")
    if got.get("is_active") is not True:
        print("Persisted default mismatch on GET:", got.get("is_active"), file=sys.stderr)
        sys.exit(1)
    print("[OK] GET confirms default persisted")

    print("\nDefault test passed ✅")

if __name__ == "__main__":
    main()

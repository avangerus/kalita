#!/usr/bin/env python3
# -*- coding: utf-8 -*-

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
    path = f"{BASE_URL.rstrip('/')}/api/{MODULE}/{ENTITY}"

    # 1) POST с неверным типом
    bad_payload = {"name": "TypeTestCo", "is_active": "yes"}
    r = requests.post(path, headers=HEADERS, json=bad_payload, timeout=30)
    if r.status_code != 400:
        print("Expected 400 on bad type, got", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    body = r.json()
    errs = body.get("errors") or []
    if not any(e.get("code") == "type_mismatch" for e in errs if isinstance(e, dict)):
        print("No type_mismatch error for bad POST", errs, file=sys.stderr)
        sys.exit(1)
    print("[OK] Bad type POST correctly returned 400 type_mismatch")

    # 2) POST без is_active (default=true)
    good_payload = {"name": "TypeTestCoGood"}
    r = requests.post(path, headers=HEADERS, json=good_payload, timeout=30)
    if r.status_code not in (200, 201):
        print("Good POST failed", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    created = r.json()
    ensure_flattened(created, "Good POST")
    if created.get("is_active") is not True:
        print("Default not applied: expected is_active=true", created, file=sys.stderr)
        sys.exit(1)
    rec_id = created["id"]
    print("[OK] Good POST applied default is_active=true")

    # 3) PATCH с неверным типом
    bad_patch = {"is_active": 123}
    r = requests.patch(f"{path}/{rec_id}", headers=HEADERS, json=bad_patch, timeout=30)
    if r.status_code != 400:
        print("Expected 400 on bad type PATCH, got", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    errs = (r.json()).get("errors") or []
    if not any(e.get("code") == "type_mismatch" for e in errs if isinstance(e, dict)):
        print("No type_mismatch error for bad PATCH", errs, file=sys.stderr)
        sys.exit(1)
    print("[OK] Bad type PATCH correctly returned 400 type_mismatch")

    print("\nType validation test passed ✅")

if __name__ == "__main__":
    main()

#!/usr/bin/env python3
# -*- coding: utf-8 -*-

# Kalita API smoke test (point 1: unified flattened responses)
# Just run: python kalita_smoke.py

import json
import sys
import requests
from typing import Dict, Any

# ==== CONFIG ====
BASE_URL = "http://localhost:8080"
MODULE   = "core"
ENTITY   = "user"
PAYLOAD  = {
    "name": "Ivan",
    "email": "ivan@example.com",
    "role": "Manager"
}
HEADERS  = {
    "Content-Type": "application/json"
}
# ================

REQUIRED_META = ["id", "version", "created_at", "updated_at"]

def ensure_flattened(obj: Dict[str, Any], where: str):
    missing = [k for k in REQUIRED_META if k not in obj]
    if missing:
        raise AssertionError(f"{where}: Missing meta fields: {missing}. Got keys: {list(obj.keys())[:20]}")

def main():
    base = BASE_URL.rstrip("/")
    path = f"{base}/api/{MODULE}/{ENTITY}"

    # 1) Create
    r = requests.post(path, headers=HEADERS, json=PAYLOAD, timeout=30)
    if r.status_code not in (200, 201):
        print("CREATE failed", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    created = r.json()
    ensure_flattened(created, "CREATE response")
    rec_id = created["id"]
    print("[OK] CREATE returned flattened record with id:", rec_id)

    # 2) List
    r = requests.get(path, headers=HEADERS, timeout=30)
    if r.status_code != 200:
        print("LIST failed", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    arr = r.json()
    if not isinstance(arr, list):
        print("LIST did not return a list", file=sys.stderr)
        sys.exit(1)
    if not arr:
        print("LIST returned empty list, but we just created a record — check server", file=sys.stderr)
        sys.exit(1)
    ensure_flattened(arr[0], "LIST[0] response")
    print("[OK] LIST returned flattened records")

    # 3) Get by id
    r = requests.get(f"{path}/{rec_id}", headers=HEADERS, timeout=30)
    if r.status_code != 200:
        print("GET-by-id failed", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    one = r.json()
    ensure_flattened(one, "GET-by-id response")
    print("[OK] GET-by-id returned flattened record")

    print("\nAll checks passed ✅")
    print("Meta fields present:", ", ".join(REQUIRED_META))

if __name__ == "__main__":
    main()

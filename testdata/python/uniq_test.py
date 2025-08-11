#!/usr/bin/env python3
import requests, json

BASE = "http://localhost:8080/api/test/item"

def ok(flag, msg): print(("🟩 " if flag else "🟥 ") + msg)

# 1) create first
r1 = requests.post(BASE, json={"code":"DUP-1","name":"one"})
ok(r1.status_code in (200,201), f"create #1 HTTP {r1.status_code} -> {r1.text}")

# 2) create duplicate
r2 = requests.post(BASE, json={"code":"DUP-1","name":"two"})
ok(r2.status_code in (400,409), f"create duplicate HTTP {r2.status_code}")

body = r2.json() if r2.headers.get("content-type","").startswith("application/json") else {}
errs = body.get("errors", []) if isinstance(body, dict) else []
uv = [e for e in errs if e.get("code")=="unique_violation"]
ok(len(uv) == 1, f"unique_violation appears once (got {len(uv)})")
if len(uv) != 1:
    print("  errors:", json.dumps(errs, ensure_ascii=False))
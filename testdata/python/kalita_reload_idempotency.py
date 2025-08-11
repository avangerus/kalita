#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Idempotent DDL / reload tester for Kalita

- Repeatedly calls POST /api/admin/reload
- Ensures server stays responsive (GET /api/meta)
- (Optional) If --dsn provided, inspects pg_catalog for example FK names and prints presence

Usage:
  python kalita_reload_idempotency.py --base http://localhost:8080 --repeats 5 --wait 0.5
  python kalita_reload_idempotency.py --repeats 5 --dsn "postgres://USER:PASS@localhost:5432/DB"
"""

import argparse, json, sys, time
from typing import Any, Dict, Tuple

try:
    import requests
except ImportError:
    print("This script needs 'requests'. Install: pip install requests", file=sys.stderr)
    sys.exit(1)

def pretty(x: Any) -> str:
    try:
        return json.dumps(x, ensure_ascii=False, indent=2, sort_keys=True)
    except Exception:
        return str(x)

def get_json(url: str, params: Dict[str,Any]=None) -> Tuple[int, Any]:
    r = requests.get(url, params=params or {}, timeout=20)
    try:
        return r.status_code, r.json()
    except Exception:
        return r.status_code, r.text

def post_json(url: str, payload: Dict[str, Any]=None) -> Tuple[int, Any]:
    r = requests.post(url, json=(payload or {}), timeout=60)
    try:
        return r.status_code, r.json()
    except Exception:
        return r.status_code, r.text

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8080")
    ap.add_argument("--repeats", type=int, default=5)
    ap.add_argument("--wait", type=float, default=0.5, help="Seconds between reloads")
    ap.add_argument("--dsn", help="Optional Postgres DSN for catalog checks (psycopg2 required)")
    args = ap.parse_args()

    base = args.base.rstrip("/")
    reload_url = f"{base}/api/admin/reload"
    meta_url = f"{base}/api/meta"

    print(f"[i] Starting reload-idempotency test: base={base}, repeats={args.repeats}")

    # Initial meta check
    sc, body = get_json(meta_url)
    print(f"[GET /api/meta] status={sc}")
    if sc != 200:
        print(pretty(body)); return

    for i in range(1, args.repeats+1):
        sc, rb = post_json(reload_url, {})
        print(f"[{i}/{args.repeats}] POST /api/admin/reload -> {sc}")
        if sc != 200:
            print(pretty(rb))
            # don't stop immediately; continue to see stability
        # small wait
        if args.wait > 0: time.sleep(args.wait)
        # check meta again
        sc2, mb = get_json(meta_url)
        print(f"        GET /api/meta -> {sc2} (entities={len(mb) if isinstance(mb, list) else 'n/a'})")

    # Optional: DB catalog checks
    if args.dsn:
        try:
            import psycopg2
            conn = psycopg2.connect(args.dsn)
            cur = conn.cursor()
            print("\n[DB] Checking example FK existence (first 10)")
            cur.execute("""
                SELECT n.nspname AS schema, c.relname AS table, con.conname AS name, con.contype
                FROM pg_constraint con
                JOIN pg_class c ON c.oid = con.conrelid
                JOIN pg_namespace n ON n.oid = c.relnamespace
                WHERE con.contype = 'f'
                ORDER BY con.conname
                LIMIT 10;
            """)
            rows = cur.fetchall()
            for r in rows:
                print(f"  {r[0]}.{r[1]}  {r[2]}  (type={r[3]})")
            # Example: look up the FK seen in logs earlier if present
            cur.execute("""
                SELECT 1 FROM pg_constraint WHERE conname = 'project_manager_id_fk' LIMIT 1;
            """)
            ok = cur.fetchone() is not None
            print(f"[DB] project_manager_id_fk present: {ok}")
            cur.close(); conn.close()
        except Exception as e:
            print(f"[DB] catalog check skipped (error: {e}). Tip: pip install psycopg2-binary")
    print("\n[i] Done. If all statuses were 200 and server stayed responsive, DDL apply is idempotent.")
if __name__ == "__main__":
    main()

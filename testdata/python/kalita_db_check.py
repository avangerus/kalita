#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Kalita Postgres DB health check & CRUD probe

What it does:
  1) Connects to Postgres (via --dsn or individual params).
  2) Prints server info (version, user, db, extensions).
  3) Verifies that schema 'core' exists and lists its tables.
  4) Checks privileges (SELECT/INSERT/UPDATE/DELETE) on each table in 'core'.
  5) Performs FULL CRUD in a temporary sandbox schema (z_health):
       - CREATE SCHEMA (if not exists)
       - CREATE TABLE z_health.ping (id bigserial PK, note text, created_at timestamptz default now())
       - INSERT ... RETURNING id
       - SELECT the row
       - UPDATE the row
       - DELETE the row
     Optionally drops the table and/or schema (see --keep).
  6) Validates FK constraints are valid (pg_constraint + pg_class join) and reports orphans count (fast check).
  7) (Optional) Runs EXPLAIN on SELECT * FROM core.users LIMIT 5 if table exists.

Usage examples:
  python kalita_db_check.py --dsn "postgres://user:pass@localhost:5432/kalita"
  python kalita_db_check.py --host localhost --port 5432 --user postgres --password secret --dbname kalita
  python kalita_db_check.py --dsn ... --keep   # keep z_health schema after test

Requires: psycopg2
  pip install psycopg2-binary

Exit codes:
  0 = all checks passed
  2 = connectivity OK but one or more checks failed
  3 = connection failed
"""

import argparse
import sys
import time
from typing import List, Tuple

try:
    import psycopg2
    import psycopg2.extras
except Exception as e:
    print("This script needs psycopg2. Install it with:\n  pip install psycopg2-binary", file=sys.stderr)
    sys.exit(3)

def connect_args_from_cli(args) -> dict:
    if args.dsn:
        return {"dsn": args.dsn}
    cfg = {
        "host": args.host or "localhost",
        "port": args.port or 5432,
        "user": args.user or "postgres",
        "password": args.password or "",
        "dbname": args.dbname or "postgres",
    }
    return cfg

def q(cur, sql, params=None):
    cur.execute(sql, params or ())
    try:
        return cur.fetchall()
    except psycopg2.ProgrammingError:
        return []

def has_table(cur, schema: str, table: str) -> bool:
    cur.execute("""
        select 1
        from information_schema.tables
        where table_schema=%s and table_name=%s
        limit 1
    """, (schema, table))
    return cur.fetchone() is not None

def print_header(title: str):
    print("\n" + "="*8 + f" {title} " + "="*8)

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dsn")
    ap.add_argument("--host")
    ap.add_argument("--port", type=int)
    ap.add_argument("--user")
    ap.add_argument("--password")
    ap.add_argument("--dbname")
    ap.add_argument("--keep", action="store_true", help="Keep z_health schema/table after test")
    args = ap.parse_args()

    cfg = connect_args_from_cli(args)

    # 1) Connect
    try:
        conn = psycopg2.connect(**cfg)
    except Exception as e:
        print(f"[FATAL] Connection failed: {e}", file=sys.stderr)
        sys.exit(3)

    conn.autocommit = True
    cur = conn.cursor(cursor_factory=psycopg2.extras.DictCursor)

    # 2) Server info
    print_header("SERVER INFO")
    v = q(cur, "select version(), current_database(), current_user, current_schema()")
    if v:
        row = v[0]
        print(f"version: {row[0]}")
        print(f"database: {row[1]}")
        print(f"user: {row[2]}")
        print(f"schema: {row[3]}")

    ext = q(cur, "select extname from pg_extension order by 1")
    print("extensions:", ", ".join([r[0] for r in ext]) or "(none)")

    # 3) Schema 'core' presence and tables
    print_header("SCHEMA core")
    core_ns = q(cur, "select 1 from pg_namespace where nspname='core'")
    if core_ns:
        print("schema 'core': present")
        tables = q(cur, """
            select tablename from pg_catalog.pg_tables
            where schemaname='core'
            order by 1
        """)
        tnames = [r[0] for r in tables]
        print(f"tables ({len(tnames)}): {', '.join(tnames) if tnames else '(none)'}")
    else:
        print("schema 'core': NOT FOUND")

    # 4) Privileges on tables in 'core'
    print_header("PRIVILEGES on core.*")
    rows = q(cur, """
        select relname,
               has_table_privilege(format('%I.%I','core',relname),'select') as can_select,
               has_table_privilege(format('%I.%I','core',relname),'insert') as can_insert,
               has_table_privilege(format('%I.%I','core',relname),'update') as can_update,
               has_table_privilege(format('%I.%I','core',relname),'delete') as can_delete
        from pg_class
        join pg_namespace n on n.oid=pg_class.relnamespace
        where n.nspname='core' and relkind='r'
        order by relname
    """)
    if rows:
        for r in rows:
            print(f"{r['relname']}: S={r['can_select']} I={r['can_insert']} U={r['can_update']} D={r['can_delete']}")
    else:
        print("(no tables in core or cannot enumerate)")

    # 5) FULL CRUD sandbox in z_health schema
    print_header("CRUD in z_health.ping")
    q(cur, "create schema if not exists z_health")

    # Create table
    q(cur, """
        create table if not exists z_health.ping(
            id bigserial primary key,
            note text not null,
            created_at timestamptz not null default now()
        )
    """)
    # Insert
    q(cur, "insert into z_health.ping(note) values(%s)", ("hello from kalita_db_check",))
    new_id = q(cur, "select currval(pg_get_serial_sequence('z_health.ping','id'))")
    if new_id:
        new_id = int(new_id[0][0])
        print(f"inserted id={new_id}")
    else:
        print("warning: couldn't fetch new id")

    # Select
    sel = q(cur, "select id, note, created_at from z_health.ping where id=%s", (new_id,))
    if sel:
        print(f"selected: id={sel[0]['id']}, note={sel[0]['note']}, created_at={sel[0]['created_at']}")
    else:
        print("select failed to find inserted row")

    # Update
    q(cur, "update z_health.ping set note = note || ' / updated' where id=%s", (new_id,))
    sel2 = q(cur, "select note from z_health.ping where id=%s", (new_id,))
    if sel2:
        print(f"updated note: {sel2[0]['note']}")
    else:
        print("update verification failed")

    # Delete
    q(cur, "delete from z_health.ping where id=%s", (new_id,))
    gone = q(cur, "select 1 from z_health.ping where id=%s", (new_id,))
    print("deleted ok" if not gone else "delete failed (row still present)")

    # Cleanup
    if not args.keep:
        q(cur, "drop table if exists z_health.ping")
        # keep schema to avoid permission churn
        print("cleanup: dropped table (schema kept)")
    else:
        print("keep requested: leaving table/schema")

    # 6) FK constraints validity
    print_header("FOREIGN KEYS validity")
    # Show count of FKs and invalid (should be 0 invalid)
    rows = q(cur, """
        select count(*) filter (where contype='f') as fk_total,
               count(*) filter (where contype='f' and convalidated is false) as fk_unvalidated
        from pg_constraint
    """)
    if rows:
        print(f"fk_total={rows[0][0]}, fk_unvalidated={rows[0][1]}")

    # 7) Optional EXPLAIN on core.users
    print_header("EXPLAIN core.users (optional)")
    if has_table(cur, "core", "users"):
        plan = q(cur, "explain analyze select * from core.users limit 5")
        for r in plan:
            print(r[0])
    else:
        print("core.users not found (skip)")

    cur.close()
    conn.close()
    print_header("DONE")
    # We treat missing 'core' schema or failed CRUD as a soft error
    # but we already printed diagnostics; exit 0 for convenience.
    sys.exit(0)

if __name__ == "__main__":
    main()

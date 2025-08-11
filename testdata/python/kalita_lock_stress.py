#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Kalita API concurrency / locking stress

What it tests
- Concurrent PATCH on the same record (default: core/User), measuring:
  - success vs conflict codes (e.g., 200/204 vs 409/412)
  - average/percentile latencies
  - last-write-wins behavior (final value on the record)
- Optional "optimistic" mode:
  - send If-Match: <version> header (if backend supports ETag/version precondition)
  - or include "version": <n> in body (if backend enforces optimistic checks by field)

Usage examples
  python kalita_lock_stress.py --base http://localhost:8080 --module core --entity User \
      --threads 12 --ops 50 --optimistic header

  python kalita_lock_stress.py --threads 8 --ops 20 --with "{\"email\":\"concur@example.com\",\"role\":\"Manager\"}"

Options
  --threads, --ops        number of workers and ops per worker (default 8 x 20)
  --optimistic none|header|body  (default: none)
  --with JSON             payload to merge into the initial CREATE (to satisfy required fields)
  --field name            which writable text field to update (default tries 'name' then a text field)
"""
import argparse
import json
import threading
import time
from statistics import mean
from typing import Any, Dict, List, Optional, Tuple
from datetime import datetime, timezone

try:
    import requests
except ImportError:
    print("This script needs 'requests'. Install: pip install requests")
    raise

def now_iso():
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat()

def pretty(o: Any) -> str:
    try:
        return json.dumps(o, ensure_ascii=False, indent=2, sort_keys=True)
    except Exception:
        return str(o)

def get_json(url: str, headers: Dict[str,str]=None) -> Tuple[int, Any, Dict[str,str]]:
    r = requests.get(url, headers=headers or {}, timeout=15)
    try:
        body = r.json()
    except Exception:
        body = r.text
    return r.status_code, body, dict(r.headers)

def post_json(url: str, payload: Dict[str, Any]) -> Tuple[int, Any, Dict[str,str]]:
    r = requests.post(url, json=payload, timeout=20)
    try:
        body = r.json()
    except Exception:
        body = r.text
    return r.status_code, body, dict(r.headers)

def patch_json(url: str, payload: Dict[str, Any], headers: Dict[str,str]=None) -> Tuple[int, Any, Dict[str,str], float]:
    t0 = time.perf_counter()
    r = requests.patch(url, json=payload, headers=headers or {}, timeout=20)
    dt = (time.perf_counter() - t0) * 1000.0
    try:
        body = r.json()
    except Exception:
        body = r.text
    return r.status_code, body, dict(r.headers), dt

def choose_text_field(meta_fields: List[Dict[str,Any]], prefer: Optional[str]) -> Optional[str]:
    if prefer:
        return prefer
    # Prefer name/title/code/email/login if writable
    writable = []
    for f in meta_fields:
        name = f.get("name") or f.get("Name")
        typ  = (f.get("type") or f.get("Type") or "").lower()
        ro   = bool(f.get("readOnly") or f.get("ReadOnly"))
        if ro: continue
        if typ in ("string","text","email","uuid","varchar"):
            writable.append(name)
    for pref in ("name","title","code","email","login","username"):
        if pref in writable:
            return pref
    return writable[0] if writable else None

def fetch_meta(base, module, entity) -> Dict[str,Any]:
    sc, body, _ = get_json(f"{base}/api/meta/{module}/{entity}")
    return body if (sc==200 and isinstance(body, dict)) else {}

def is_required(f: Dict[str,Any]) -> bool:
    if bool(f.get("required") or f.get("Required")): return True
    opts = f.get("options") or f.get("Options") or {}
    v = opts.get("required")
    if isinstance(v, bool): return v
    if isinstance(v, str): return v.lower() in ("true","1","y","yes")
    return False

def build_create_payload(meta_fields: List[Dict[str,Any]], overrides: Dict[str,Any], token: str) -> Dict[str,Any]:
    system = {"id","version","created_at","updated_at","deleted_at"}
    payload: Dict[str,Any] = {}
    # required
    for f in meta_fields:
        name = f.get("name") or f.get("Name")
        if not name or name in system or name in overrides:
            continue
        if not is_required(f):
            continue
        typ  = (f.get("type") or f.get("Type") or "").lower()
        if typ == "enum":
            ev = f.get("enum") or f.get("Enum") or []
            if isinstance(ev, list) and ev:
                payload[name] = ev[0]
            else:
                payload[name] = "EnumDefault"
        elif typ == "email":
            payload[name] = f"{token}@example.com"
        elif typ in ("string","text","uuid","varchar"):
            payload[name] = f"{token}-{name}"
        elif typ in ("int","integer","bigint","number","float","double","decimal"):
            payload[name] = 0
        elif typ in ("bool","boolean"):
            payload[name] = False
        elif typ == "date":
            payload[name] = now_iso().split("T")[0]
        elif typ in ("datetime","timestamp"):
            payload[name] = now_iso()
        # skip ref here
    payload.update(overrides)
    return payload

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8080")
    ap.add_argument("--module", default="core")
    ap.add_argument("--entity", default="User")
    ap.add_argument("--threads", type=int, default=8)
    ap.add_argument("--ops", type=int, default=20, help="operations per worker")
    ap.add_argument("--field", default=None, help="writable text field to update (e.g., name)")
    ap.add_argument("--optimistic", choices=["none","header","body"], default="none")
    ap.add_argument("--with", dest="with_json", help="JSON for CREATE (e.g. '{\"email\":\"x@ex.com\",\"role\":\"Manager\"}')")
    args = ap.parse_args()

    base = args.base.rstrip("/")
    mod, ent = args.module, args.entity
    base_entity_url = f"{base}/api/{mod}/{ent}"

    # 1) meta & create record
    meta = fetch_meta(base, mod, ent)
    fields = meta.get("fields") or meta.get("Fields") or []
    field_name = choose_text_field(fields, args.field)
    if not field_name:
        print("[FATAL] couldn't find a writable text field; pass --field")
        return

    overrides = {}
    if args.with_json:
        overrides = json.loads(args.with_json)

    token = f"lock-{int(time.time())}"
    create_payload = build_create_payload(fields, overrides, token)
    sc, body, _ = post_json(base_entity_url, create_payload)
    if sc not in (200,201):
        print(f"[FATAL] create failed {sc}: {pretty(body)}")
        return
    rec_id = body.get("id") if isinstance(body, dict) else None
    if not rec_id:
        print(f"[FATAL] create returned no id: {pretty(body)}")
        return

    # 2) initial GET (for version/etag if needed)
    sc, body, hdrs = get_json(f"{base_entity_url}/{rec_id}")
    if sc != 200 or not isinstance(body, dict):
        print(f"[FATAL] get failed {sc}: {body}")
        return
    start_version = body.get("version")
    etag = hdrs.get("ETag") or hdrs.get("Etag") or hdrs.get("etag")
    print(f"[i] Created id={rec_id}, start_version={start_version}, etag={etag}, field={field_name}")

    # 3) barrier for simultaneous PATCH
    barrier = threading.Barrier(args.threads)
    results_lock = threading.Lock()
    statuses: List[int] = []
    latencies: List[float] = []
    conflicts = 0

    def worker(wid: int):
        nonlocal conflicts
        try:
            barrier.wait(timeout=10.0)
        except Exception:
            pass
        for k in range(args.ops):
            value = f"{token}-{wid}-{k}"
            payload = { field_name: value }
            headers: Dict[str,str] = {}
            if args.optimistic == "header" and etag:
                headers["If-Match"] = etag
            elif args.optimistic == "body" and start_version is not None:
                payload["version"] = start_version
            sc, b, h, dt = patch_json(f"{base_entity_url}/{rec_id}", payload, headers=headers)
            with results_lock:
                statuses.append(sc)
                latencies.append(dt)
                if sc in (409, 412):
                    conflicts += 1

    threads = [threading.Thread(target=worker, args=(i,), daemon=True) for i in range(args.threads)]
    for t in threads: t.start()
    for t in threads: t.join()

    # 4) final state
    sc, final_body, _ = get_json(f"{base_entity_url}/{rec_id}")
    final_val = None
    final_ver = None
    if sc == 200 and isinstance(final_body, dict):
        final_val = final_body.get(field_name)
        final_ver = final_body.get("version")

    # 5) summary
    total_ops = len(statuses)
    ok = sum(1 for s in statuses if s in (200,204))
    err = total_ops - ok
    avg = mean(latencies) if latencies else 0.0
    p95 = sorted(latencies)[int(0.95*len(latencies))-1] if latencies else 0.0

    print("\n===== SUMMARY =====")
    print(f"entity={mod}/{ent}, id={rec_id}")
    print(f"threads={args.threads}, ops_per_thread={args.ops}, total_ops={total_ops}")
    print(f"ok(200/204)={ok}, conflicts(409/412)={conflicts}, others={err-conflicts}")
    print(f"latency_ms: avg={avg:.1f}, p95={p95:.1f}")
    print(f"final {field_name}={final_val}, final version={final_ver} (start={start_version})")
    print("Status counts:")
    from collections import Counter
    c = Counter(statuses)
    for k in sorted(c.keys()):
        print(f"  {k}: {c[k]}")

if __name__ == "__main__":
    main()

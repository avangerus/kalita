#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Kalita API perf runner (read/write/mixed)
- Warmup
- Duration run with concurrency and optional target RPS
- Percentiles + throughput
- Works on Windows / PowerShell

Examples:
  # Read-heavy test for User listing/search/sort for 60s with 16 workers
  python kalita_perf.py --scenario read --module core --entity User --duration 60 --concurrency 16

  # Write-heavy test for User (creates/patches/deletes), cleans up after
  python kalita_perf.py --scenario write --module core --entity User --duration 30 --concurrency 8 --cleanup

  # Mixed test with a modest target rate
  python kalita_perf.py --scenario mixed --duration 45 --concurrency 12 --target-rps 200

Notes:
- For write/mixed, script will seed required fields from meta (enum -> first value, email unique).
- Use --with to force required business values if needed.
"""

import argparse
import json
import threading
import time
from collections import Counter, defaultdict
from datetime import datetime, timezone
from statistics import mean
from typing import Any, Dict, List, Optional, Tuple

try:
    import requests
except ImportError:
    print("This script needs 'requests'. Install with: pip install requests")
    raise

def now_iso():
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat()

def pretty(o: Any) -> str:
    try:
        return json.dumps(o, ensure_ascii=False, indent=2, sort_keys=True)
    except Exception:
        return str(o)

def get_json(url: str, params: Dict[str, Any]=None, headers: Dict[str,str]=None):
    r = requests.get(url, params=params or {}, headers=headers or {}, timeout=20)
    try:
        body = r.json()
    except Exception:
        body = r.text
    return r.status_code, body

def post_json(url: str, payload: Dict[str, Any], headers: Dict[str,str]=None):
    r = requests.post(url, json=payload, headers=headers or {}, timeout=20)
    try:
        body = r.json()
    except Exception:
        body = r.text
    return r.status_code, body

def patch_json(url: str, payload: Dict[str, Any], headers: Dict[str,str]=None):
    r = requests.patch(url, json=payload, headers=headers or {}, timeout=20)
    try:
        body = r.json()
    except Exception:
        body = r.text
    return r.status_code, body

def delete(url: str, headers: Dict[str,str]=None):
    r = requests.delete(url, headers=headers or {}, timeout=20)
    try:
        body = r.json()
    except Exception:
        body = r.text
    return r.status_code, body

def fetch_meta(base, module, entity) -> Dict[str,Any]:
    sc, body = get_json(f"{base}/api/meta/{module}/{entity}")
    return body if (sc==200 and isinstance(body, dict)) else {}

def parse_fields(meta: Dict[str,Any]) -> List[Dict[str,Any]]:
    arr = meta.get("fields") or meta.get("Fields") or []
    # attach options
    for f in arr:
        if isinstance(f, dict):
            f["_opts"] = f.get("options") or f.get("Options") or {}
    return arr

def is_required(f: Dict[str,Any]) -> bool:
    if bool(f.get("required") or f.get("Required")):
        return True
    v = (f.get("_opts") or {}).get("required")
    if isinstance(v, bool): return v
    if isinstance(v, str): return v.lower() in ("true","1","y","yes")
    return False

def choose_text_field(fields: List[Dict[str,Any]]) -> Optional[str]:
    writable = []
    for f in fields:
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

def build_required_payload(fields: List[Dict[str,Any]], overrides: Dict[str,Any], token: str) -> Dict[str,Any]:
    system = {"id","version","created_at","updated_at","deleted_at"}
    p: Dict[str,Any] = {}
    for f in fields:
        name = f.get("name") or f.get("Name")
        if not name or name in system or name in overrides:
            continue
        if not is_required(f):
            continue
        typ  = (f.get("type") or f.get("Type") or "").lower()
        if typ == "enum":
            ev = f.get("enum") or f.get("Enum") or []
            p[name] = (ev[0] if (isinstance(ev, list) and ev) else "EnumDefault")
        elif typ == "email":
            p[name] = f"{token}@example.com"
        elif typ in ("string","text","uuid","varchar"):
            p[name] = f"{token}-{name}"
        elif typ in ("int","integer","bigint","number","float","double","decimal"):
            p[name] = 0
        elif typ in ("bool","boolean"):
            p[name] = False
        elif typ == "date":
            p[name] = now_iso().split("T")[0]
        elif typ in ("datetime","timestamp"):
            p[name] = now_iso()
    p.update(overrides)
    return p

def percentile(vals: List[float], p: float) -> float:
    if not vals: return 0.0
    s = sorted(vals)
    k = int(max(0, min(len(s)-1, round(p * (len(s)-1)))))
    return s[k]

class Runner:
    def __init__(self, base, module, entity, scenario, duration, concurrency, target_rps, overrides, cleanup):
        self.base = base.rstrip("/")
        self.module, self.entity = module, entity
        self.scenario = scenario
        self.duration = duration
        self.concurrency = concurrency
        self.target_rps = target_rps
        self.overrides = overrides
        self.cleanup = cleanup

        self.base_entity_url = f"{self.base}/api/{self.module}/{self.entity}"
        self.stop_at = time.perf_counter() + duration
        self.lat = defaultdict(list)
        self.codes = Counter()
        self.created_ids = []
        self.lock = threading.Lock()
        self.token = f"perf-{int(time.time())}"

        # meta
        meta = fetch_meta(self.base, self.module, self.entity)
        self.fields = parse_fields(meta)
        self.text_field = choose_text_field(self.fields)

    def record(self, op: str, code: int, dt_ms: float):
        with self.lock:
            self.codes[(op, code)] += 1
            self.lat[op].append(dt_ms)

    def run_op(self, op: str):
        t0 = time.perf_counter()
        try:
            if op == "LIST":
                sc, _ = get_json(self.base_entity_url, params={"limit": 20, "sort": (f"+{self.text_field}" if self.text_field else None)})
            elif op == "COUNT":
                sc, _ = get_json(f"{self.base_entity_url}/count")
            elif op == "SEARCH":
                sc, _ = get_json(self.base_entity_url, params={"q": "perf-"})
            elif op == "CREATE":
                payload = build_required_payload(self.fields, self.overrides, f"{self.token}.{int(t0*1000)}")
                sc, body = post_json(self.base_entity_url, payload)
                if sc in (200,201) and isinstance(body, dict):
                    rid = body.get("id")
                    if rid:
                        with self.lock:
                            self.created_ids.append(rid)
            elif op == "PATCH":
                rid = None
                with self.lock:
                    if self.created_ids:
                        rid = self.created_ids[-1]
                sc = 0
                if rid:
                    payload = {}
                    if self.text_field:
                        payload[self.text_field] = f"{self.token}-upd-{int(t0*1000)}"
                    sc, _ = patch_json(f"{self.base_entity_url}/{rid}", payload)
                else:
                    sc, _ = get_json(self.base_entity_url)  # noop, keeps loop alive
            elif op == "DELETE":
                rid = None
                with self.lock:
                    if self.created_ids:
                        rid = self.created_ids.pop(0)
                sc = 0
                if rid:
                    sc, _ = delete(f"{self.base_entity_url}/{rid}")
                else:
                    sc, _ = get_json(self.base_entity_url)
            else:
                sc = 0
        except Exception:
            sc = 0
        dt = (time.perf_counter() - t0) * 1000.0
        self.record(op, sc, dt)

    def worker(self, ops_cycle: List[str]):
        sleep_between = 0.0
        if self.target_rps and self.concurrency > 0:
            sleep_between = self.concurrency / max(1.0, self.target_rps)
        while time.perf_counter() < self.stop_at:
            for op in ops_cycle:
                self.run_op(op)
                if sleep_between > 0:
                    time.sleep(sleep_between)

    def start(self):
        # Warmup: small sequential run
        for _ in range(3):
            self.run_op("LIST")
            self.run_op("COUNT")

        # Scenario definition
        if self.scenario == "read":
            ops_cycle = ["LIST","COUNT","SEARCH","LIST"]
        elif self.scenario == "write":
            ops_cycle = ["CREATE","PATCH","DELETE"]
        else:  # mixed
            ops_cycle = ["LIST","CREATE","PATCH","COUNT","DELETE","LIST","SEARCH"]

        threads = [threading.Thread(target=self.worker, args=(ops_cycle,), daemon=True) for _ in range(self.concurrency)]
        for t in threads: t.start()
        for t in threads: t.join()

        # Cleanup created
        if self.cleanup and self.created_ids:
            for rid in list(self.created_ids):
                delete(f"{self.base_entity_url}/{rid}")

    def report(self):
        total = sum(self.codes.values())
        elapsed = self.duration  # approximate
        rps = total / elapsed if elapsed > 0 else 0.0
        print("\n===== PERF SUMMARY =====")
        print(f"scenario={self.scenario}, entity={self.module}/{self.entity}")
        print(f"concurrency={self.concurrency}, duration={self.duration}s, target_rps={self.target_rps or 'n/a'}")
        print(f"total_ops={total}, approx_throughput={rps:.1f} ops/s")
        # per-op percentiles
        for op in ("LIST","COUNT","SEARCH","CREATE","PATCH","DELETE"):
            vals = self.lat.get(op, [])
            if not vals: continue
            print(f"{op}: n={len(vals)} p50={percentile(vals,0.50):.1f}ms p90={percentile(vals,0.90):.1f}ms p95={percentile(vals,0.95):.1f}ms p99={percentile(vals,0.99):.1f}ms avg={mean(vals):.1f}ms")
        # status breakdown
        print("Status breakdown: (op, status) -> count")
        for (op, code) in sorted(self.codes.keys()):
            print(f"  ({op},{code}): {self.codes[(op,code)]}")

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8080")
    ap.add_argument("--module", default="core")
    ap.add_argument("--entity", default="User")
    ap.add_argument("--scenario", choices=["read","write","mixed"], default="read")
    ap.add_argument("--duration", type=int, default=30)
    ap.add_argument("--concurrency", type=int, default=8)
    ap.add_argument("--target-rps", type=float, help="Best-effort total RPS target (optional)")
    ap.add_argument("--with", dest="with_json", help="JSON for required fields on CREATE (e.g. '{\"email\":\"x@ex.com\",\"role\":\"Manager\"}')")
    ap.add_argument("--cleanup", action="store_true", help="DELETE created records at the end")
    args = ap.parse_args()

    overrides = {}
    if args.with_json:
        try:
            obj = json.loads(args.with_json)
            if isinstance(obj, dict):
                overrides.update(obj)
        except Exception as e:
            print(f"[warn] --with parse error: {e}")

    runner = Runner(
        base=args.base,
        module=args.module,
        entity=args.entity,
        scenario=args.scenario,
        duration=args.duration,
        concurrency=args.concurrency,
        target_rps=args.target_rps,
        overrides=overrides,
        cleanup=args.cleanup,
    )
    print(f"[i] Starting perf run: {args.scenario} {args.module}/{args.entity} for {args.duration}s, {args.concurrency} workers")
    runner.start()
    runner.report()

if __name__ == "__main__":
    main()

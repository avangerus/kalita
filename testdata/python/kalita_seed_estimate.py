#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Verbose seeder for Estimate tree:
Estimate -> EstimateBlock -> EstimateSection -> EstimateLine -> Subline (optional)

It prints every request/response and shows required fields from meta.
"""

import argparse, json, sys, time
from typing import Any, Dict, List, Optional, Tuple

try:
    import requests
except ImportError:
    print("Please install requests: pip install requests", file=sys.stderr); sys.exit(1)

def pretty(x: Any)->str:
    try: return json.dumps(x, ensure_ascii=False, indent=2, sort_keys=True)
    except Exception: return str(x)

def get_json(url: str, params: Dict[str,Any]=None)->Tuple[int,Any]:
    print(f"[HTTP] GET {url} params={params}")
    r = requests.get(url, params=params or {}, timeout=30)
    try:
        body = r.json()
    except Exception:
        body = r.text
    print(f"[HTTP] -> {r.status_code}")
    if isinstance(body, (dict, list)): print(pretty(body))
    else: print(str(body)[:500])
    return r.status_code, body

def post_json(url: str, payload: Dict[str,Any])->Tuple[int,Any]:
    print(f"[HTTP] POST {url}\n{pretty(payload)}")
    r = requests.post(url, json=payload, timeout=30)
    try:
        body = r.json()
    except Exception:
        body = r.text
    print(f"[HTTP] -> {r.status_code}")
    if isinstance(body, (dict, list)): print(pretty(body))
    else: print(str(body)[:500])
    return r.status_code, body

def fields_from_meta(meta: Dict[str,Any]) -> List[Dict[str,Any]]:
    arr = meta.get("fields") or []
    for f in arr:
        if isinstance(f, dict):
            f["_opts"] = f.get("options") or {}
    return arr

def is_required(f: Dict[str,Any]) -> bool:
    if f.get("required") or f.get("Required"): return True
    v = (f.get("_opts") or {}).get("required")
    if isinstance(v, bool): return v
    if isinstance(v, str): return v.lower() in ("true","1","y","yes")
    return False

def build_payload(fields: List[Dict[str,Any]], token: str) -> Dict[str,Any]:
    system = {"id","version","created_at","updated_at","deleted_at"}
    p: Dict[str,Any] = {}
    for f in fields:
        name = f.get("name") or f.get("Name")
        if not name or name in system:
            continue
        if not is_required(f):
            continue
        typ  = (f.get("type") or "").lower()
        if typ == "enum":
            ev = f.get("enum") or []
            p[name] = (ev[0] if (isinstance(ev, list) and ev) else "EnumDefault")
        elif typ == "email":
            p[name] = f"{token}@example.com"
        elif typ in ("string","text","uuid","varchar"):
            p[name] = f"{token}-{name}"
        elif typ in ("int","integer","bigint","number","float","double","decimal"):
            p[name] = 1
        elif typ in ("bool","boolean"):
            p[name] = True
        elif typ == "date":
            p[name] = "2025-01-01"
        elif typ in ("datetime","timestamp"):
            p[name] = "2025-01-01T00:00:00Z"
    return p

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8080")
    ap.add_argument("--module", default="olga")
    ap.add_argument("--estimate", default="Estimate")
    ap.add_argument("--block", default="EstimateBlock")
    ap.add_argument("--section", default="EstimateSection")
    ap.add_argument("--line", default="EstimateLine")
    ap.add_argument("--subline", default="Subline")
    args = ap.parse_args()

    base = args.base.rstrip("/")
    token = f"seedv-{int(time.time())}"

    # list meta
    sc, meta_list = get_json(f"{base}/api/meta")
    if sc!=200 or not isinstance(meta_list, list):
        print("[FATAL] /api/meta failed"); return

    def exists(module, entity):
        return any(isinstance(e, dict) and e.get("module")==module and e.get("entity")==entity for e in meta_list)

    for ent in (args.estimate, args.block, args.section, args.line):
        if not exists(args.module, ent):
            print(f"[FATAL] Entity {args.module}/{ent} not found in /api/meta"); return

    # fetch metas
    def fetch_m(module, entity):
        sc, m = get_json(f"{base}/api/meta/{module}/{entity}")
        if sc!=200 or not isinstance(m, dict):
            print(f"[FATAL] meta for {module}/{entity} failed"); sys.exit(1)
        return m

    m_est = fetch_m(args.module, args.estimate)
    m_blk = fetch_m(args.module, args.block)
    m_sec = fetch_m(args.module, args.section)
    m_lin = fetch_m(args.module, args.line)
    m_sub = fetch_m(args.module, args.subline)

    f_est = fields_from_meta(m_est)
    f_blk = fields_from_meta(m_blk)
    f_sec = fields_from_meta(m_sec)
    f_lin = fields_from_meta(m_lin)
    f_sub = fields_from_meta(m_sub)

    print("\n[INFO] Required fields per entity (from meta):")
    for name, ff in [(args.estimate,f_est),(args.block,f_blk),(args.section,f_sec),(args.line,f_lin),(args.subline,f_sub)]:
        req = [ (f.get('name') or f.get('Name'), f.get('type')) for f in ff if is_required(f) ]
        print(f"  {args.module}/{name}: {req}")

    # Create Estimate
    p_est = build_payload(f_est, token)
    sc, b = post_json(f"{base}/api/{args.module}/{args.estimate}", p_est)
    if sc not in (200,201) or not isinstance(b, dict):
        print(f"[FATAL] CREATE {args.module}/{args.estimate} -> {sc}"); print(pretty(b)); return
    est_id = b.get("id"); print(f"[OK] Estimate id={est_id}")

    # Try create Block with naive ref guesses
    # look for ref fields likely pointing to Estimate
    blk_ref = None
    for f in f_blk:
        if (f.get("type") or "").lower() == "ref":
            opts = f.get("_opts") or {}
            ref = opts.get("ref") or opts.get("refTarget") or ""
            if ref:
                if "." not in ref: ref = f"{args.module}.{ref}"
                if ref.split(".")[-1].lower() == args.estimate.lower():
                    blk_ref = f.get("name") or f.get("Name")
                    break
    p_blk = build_payload(f_blk, token)
    if blk_ref: p_blk[blk_ref] = est_id
    sc, b = post_json(f"{base}/api/{args.module}/{args.block}", p_blk)
    if sc not in (200,201) or not isinstance(b, dict):
        print(f"[FATAL] CREATE {args.module}/{args.block} -> {sc}"); print(pretty(b)); return
    blk_id = b.get("id"); print(f"[OK] Block id={blk_id} (ref={blk_ref})")

    # Section -> Block
    sec_ref = None
    for f in f_sec:
        if (f.get("type") or "").lower() == "ref":
            opts = f.get("_opts") or {}
            ref = opts.get("ref") or opts.get("refTarget") or ""
            if ref:
                if "." not in ref: ref = f"{args.module}.{ref}"
                if ref.split(".")[-1].lower() == args.block.lower():
                    sec_ref = f.get("name") or f.get("Name")
                    break
    p_sec = build_payload(f_sec, token)
    if sec_ref: p_sec[sec_ref] = blk_id
    sc, b = post_json(f"{base}/api/{args.module}/{args.section}", p_sec)
    if sc not in (200,201) or not isinstance(b, dict):
        print(f"[FATAL] CREATE {args.module}/{args.section} -> {sc}"); print(pretty(b)); return
    sec_id = b.get("id"); print(f"[OK] Section id={sec_id} (ref={sec_ref})")

    # Line -> Section
    lin_ref = None
    for f in f_lin:
        if (f.get("type") or "").lower() == "ref":
            opts = f.get("_opts") or {}
            ref = opts.get("ref") or opts.get("refTarget") or ""
            if ref:
                if "." not in ref: ref = f"{args.module}.{ref}"
                if ref.split(".")[-1].lower() == args.section.lower():
                    lin_ref = f.get("name") or f.get("Name")
                    break
    p_lin1 = build_payload(f_lin, token)
    if lin_ref: p_lin1[lin_ref] = sec_id
    sc, b = post_json(f"{base}/api/{args.module}/{args.line}", p_lin1)
    if sc not in (200,201) or not isinstance(b, dict):
        print(f"[FATAL] CREATE {args.module}/{args.line} -> {sc}"); print(pretty(b)); return
    line1_id = b.get("id"); print(f"[OK] Line1 id={line1_id} (ref={lin_ref})")

    # Subline -> Line (optional)
    if exists(args.module, args.subline):
        sub_ref = None
        for f in f_sub:
            if (f.get("type") or "").lower() == "ref":
                opts = f.get("_opts") or {}
                ref = opts.get("ref") or opts.get("refTarget") or ""
                if ref:
                    if "." not in ref: ref = f"{args.module}.{ref}"
                    if ref.split(".")[-1].lower() == args.line.lower():
                        sub_ref = f.get("name") or f.get("Name")
                        break
        p_sub = build_payload(f_sub, token)
        if sub_ref: p_sub[sub_ref] = line1_id
        sc, b = post_json(f"{base}/api/{args.module}/{args.subline}", p_sub)
        if sc in (200,201) and isinstance(b, dict):
            print(f"[OK] Subline id={b.get('id')} (ref={sub_ref})")
        else:
            print(f"[WARN] Subline create failed: {sc}")
            print(pretty(b))

    print("\n[i] Done. Now run:")
    print(f"  python kalita_expand_test.py --module {args.module} --entity {args.estimate} --id {est_id} --full")

if __name__ == "__main__":
    main()

#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Recursive seeder for Kalita to create a minimal olga/Estimate tree:
- Creates required referenced parents first (based on meta), recursively (depth-capped)
- Then creates Estimate, Block, Section, Line, Subline (optional)
- Works generically off /api/meta; picks first enum value; generates plausible values for types

Usage:
  python kalita_seed_estimate_recursive.py --base http://localhost:8080
"""

import argparse, json, sys, time
from typing import Any, Dict, List, Optional, Tuple, Set

try:
    import requests
except ImportError:
    print("Please install requests: pip install requests", file=sys.stderr); sys.exit(1)

def pretty(x: Any)->str:
    try: return json.dumps(x, ensure_ascii=False, indent=2, sort_keys=True)
    except Exception: return str(x)

def get_json(url: str, params: Dict[str,Any]=None)->Tuple[int,Any]:
    r = requests.get(url, params=params or {}, timeout=30)
    try:
        return r.status_code, r.json()
    except Exception:
        return r.status_code, r.text

def post_json(url: str, payload: Dict[str,Any])->Tuple[int,Any]:
    r = requests.post(url, json=payload, timeout=30)
    try:
        return r.status_code, r.json()
    except Exception:
        return r.status_code, r.text

def meta_for(base: str, module: str, entity: str) -> Dict[str,Any]:
    sc, body = get_json(f"{base}/api/meta/{module}/{entity}")
    if sc==200 and isinstance(body, dict):
        return body
    raise RuntimeError(f"meta {module}/{entity} failed: {sc}")

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

def ref_target_fqn(module: str, f: Dict[str,Any]) -> Optional[str]:
    # support both refFQN and options.ref
    rfqn = f.get("refFQN") or f.get("ref") or (f.get("_opts") or {}).get("ref") or (f.get("_opts") or {}).get("refFQN")
    if not rfqn or not isinstance(rfqn, str):
        return None
    if "." not in rfqn:
        rfqn = f"{module}.{rfqn}"
    return rfqn

def build_minimal_payload(token: str, fields: List[Dict[str,Any]], overrides: Dict[str,Any]) -> Dict[str,Any]:
    sys_fields = {"id","version","created_at","updated_at","deleted_at"}
    p: Dict[str,Any] = {}
    for f in fields:
        name = f.get("name") or f.get("Name")
        if not name or name in sys_fields: continue
        if name in (overrides or {}): continue
        if not is_required(f): continue
        typ = (f.get("type") or "").lower()
        if typ == "enum":
            ev = f.get("enum") or []
            p[name] = (ev[0] if isinstance(ev, list) and ev else "EnumDefault")
        elif typ == "email":
            p[name] = f"{token}-{name}@example.com"
        elif typ in ("string","text","uuid","varchar"):
            p[name] = f"{token}-{name}"
        elif typ in ("int","integer","bigint","number"):
            p[name] = 1
        elif typ in ("float","double","decimal","money"):
            p[name] = 1.0
        elif typ in ("bool","boolean"):
            p[name] = False
        elif typ == "date":
            p[name] = "2025-01-01"
        elif typ in ("datetime","timestamp"):
            p[name] = "2025-01-01T00:00:00Z"
        # ref fields are handled separately
    if overrides: p.update(overrides)
    return p

class Seeder:
    def __init__(self, base: str, token: str, max_depth: int = 6):
        self.base = base.rstrip("/")
        self.token = token
        self.cache_id: Dict[str,str] = {}   # FQN -> created id (first)
        self.in_progress: Set[str] = set()
        self.max_depth = max_depth

    def create_minimal(self, module: str, entity: str, depth: int = 0) -> str:
        fqn = f"{module}.{entity}"
        if fqn in self.cache_id:
            return self.cache_id[fqn]
        if depth > self.max_depth:
            raise RuntimeError(f"Max recursion depth when creating {fqn}")
        if fqn in self.in_progress:
            raise RuntimeError(f"Cycle detected while creating {fqn}")
        self.in_progress.add(fqn)

        meta = meta_for(self.base, module, entity)
        fields = fields_from_meta(meta)

        # Prepare overrides for required ref fields: ensure parents exist first
        overrides: Dict[str,Any] = {}
        for f in fields:
            if (f.get("type") or "").lower() != "ref": 
                continue
            if not is_required(f): 
                continue
            name = f.get("name") or f.get("Name")
            tgt = ref_target_fqn(module, f)
            if not tgt: 
                continue
            tgt_mod, tgt_ent = tgt.split(".", 1)
            parent_id = self.create_minimal(tgt_mod, tgt_ent, depth+1)
            overrides[name] = parent_id

        payload = build_minimal_payload(self.token, fields, overrides)
        # POST
        sc, body = post_json(f"{self.base}/api/{module}/{entity}", payload)
        if sc not in (200,201) or not isinstance(body, dict):
            raise RuntimeError(f"CREATE {module}/{entity} -> {sc}\n{pretty(body)}")
        rid = body.get("id")
        if not rid:
            raise RuntimeError(f"CREATE {module}/{entity} returned no id")
        self.cache_id[fqn] = rid
        self.in_progress.discard(fqn)
        return rid

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

    token = f"seedr-{int(time.time())}"
    s = Seeder(args.base, token)

    # Create parents recursively for Estimate (Project, Currency, and their parents)
    est_id = s.create_minimal(args.module, args.estimate)
    print(f"[OK] {args.module}/{args.estimate} id={est_id}")

    # Create a Block -> ref estimate_id (required)
    # Pull meta to find the exact ref field name
    m_blk = meta_for(args.base, args.module, args.block); f_blk = fields_from_meta(m_blk)
    blk_ref_name = None
    for f in f_blk:
        if (f.get("type") or "").lower()=="ref" and ((f.get("refFQN") or f.get("ref") or (f.get("_opts") or {}).get("ref")) in (args.estimate, f"{args.module}.{args.estimate}")):
            blk_ref_name = f.get("name") or f.get("Name")
            break
        # fallback by suffix
        if (f.get("name") or "").lower() == "estimate_id":
            blk_ref_name = f.get("name")
            break
    p_blk = build_minimal_payload(token, f_blk, {blk_ref_name: est_id} if blk_ref_name else {})
    sc, body = post_json(f"{args.base}/api/{args.module}/{args.block}", p_blk)
    if sc not in (200,201) or not isinstance(body, dict):
        raise RuntimeError(f"CREATE block -> {sc}\n{pretty(body)}")
    blk_id = body.get("id")
    print(f"[OK] {args.module}/{args.block} id={blk_id}")

    # Section -> needs estimate_id (required), block_id (optional but useful)
    m_sec = meta_for(args.base, args.module, args.section); f_sec = fields_from_meta(m_sec)
    sec_payload = build_minimal_payload(token, f_sec, {})
    # fill ref fields if present
    for f in f_sec:
        if (f.get("type") or "").lower()!="ref": continue
        name = f.get("name") or f.get("Name")
        if name.lower()=="estimate_id":
            sec_payload[name] = est_id
        if name.lower()=="block_id":
            sec_payload[name] = blk_id
    sc, body = post_json(f"{args.base}/api/{args.module}/{args.section}", sec_payload)
    if sc not in (200,201) or not isinstance(body, dict):
        raise RuntimeError(f"CREATE section -> {sc}\n{pretty(body)}")
    sec_id = body.get("id")
    print(f"[OK] {args.module}/{args.section} id={sec_id}")

    # Line -> required: estimate_id, currency_id, item, status
    m_lin = meta_for(args.base, args.module, args.line); f_lin = fields_from_meta(m_lin)
    line_payload = build_minimal_payload(token, f_lin, {})
    # inherit estimate_id and currency_id from Estimate if present
    for f in f_lin:
        if (f.get("type") or "").lower()!="ref": continue
        name = f.get("name") or f.get("Name")
        if name.lower()=="estimate_id":
            line_payload[name] = est_id
        elif name.lower()=="section_id":
            line_payload[name] = sec_id
        elif name.lower()=="currency_id":
            # try to take same currency as estimate: s.cache_id has only ids by FQN;
            # fetch the estimate to read currency_id
            sc0, est_obj = get_json(f"{args.base}/api/{args.module}/{args.estimate}/{est_id}")
            if sc0==200 and isinstance(est_obj, dict) and "currency_id" in est_obj:
                line_payload[name] = est_obj["currency_id"]
    # ensure status has a valid enum (first value if not already filled by required)
    if "status" not in line_payload:
        for f in f_lin:
            if (f.get("name") or "").lower()=="status" and (f.get("type") or "").lower()=="enum":
                ev = f.get("enum") or []
                if ev: line_payload["status"] = ev[0]
    # ensure item
    if "item" not in line_payload:
        line_payload["item"] = f"{token}-item"
    sc, body = post_json(f"{args.base}/api/{args.module}/{args.line}", line_payload)
    if sc not in (200,201) or not isinstance(body, dict):
        raise RuntimeError(f"CREATE line -> {sc}\n{pretty(body)}")
    line_id = body.get("id")
    print(f"[OK] {args.module}/{args.line} id={line_id}")

    # Optional Subline -> requires line_id, type(enum), amount, currency_id
    try:
        m_sub = meta_for(args.base, args.module, "Subline"); f_sub = fields_from_meta(m_sub)
        sub_payload = build_minimal_payload(token, f_sub, {})
        sub_payload["line_id"] = line_id
        if "amount" not in sub_payload: sub_payload["amount"] = 1.0
        # reuse estimate currency if needed
        if "currency_id" not in sub_payload:
            sc0, est_obj = get_json(f"{args.base}/api/{args.module}/{args.estimate}/{est_id}")
            if sc0==200 and isinstance(est_obj, dict) and "currency_id" in est_obj:
                sub_payload["currency_id"] = est_obj["currency_id"]
        sc, body = post_json(f"{args.base}/api/{args.module}/Subline", sub_payload)
        if sc in (200,201) and isinstance(body, dict):
            print(f"[OK] {args.module}/Subline id={body.get('id')}")
        else:
            print(f"[WARN] Subline create failed -> {sc}\n{pretty(body)}")
    except Exception as e:
        print(f"[INFO] Subline skipped: {e}")

    print("\n[i] Done. Expand check:")
    print(f"  python kalita_expand_test.py --module {args.module} --entity {args.estimate} --id {est_id} --full")

if __name__ == "__main__":
    main()

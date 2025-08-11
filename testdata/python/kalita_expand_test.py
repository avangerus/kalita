#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Kalita expand/full/depth validator (generic)
"""
import argparse, json, sys
from typing import Any, Dict, Tuple
try:
    import requests
except ImportError:
    print("Needs 'requests' (pip install requests)", file=sys.stderr); sys.exit(1)

def pretty(x: Any)->str:
    try: return json.dumps(x, ensure_ascii=False, indent=2, sort_keys=True)
    except Exception: return str(x)

def get_json(url: str, params: Dict[str,Any]=None)->Tuple[int,Any]:
    r = requests.get(url, params=params or {}, timeout=20)
    try: return r.status_code, r.json()
    except Exception: return r.status_code, r.text

def summarize_children(children: Dict[str, Any]):
    out = {}
    if isinstance(children, dict):
        for fqn, arr in children.items():
            if isinstance(arr, list): out[fqn] = len(arr)
    return out

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8080")
    ap.add_argument("--module", required=True)
    ap.add_argument("--entity", required=True)
    ap.add_argument("--id")
    ap.add_argument("--depth", type=int, default=2)
    ap.add_argument("--expand", default=None)
    ap.add_argument("--full", action="store_true")
    ap.add_argument("--show-json", dest="show_json", action="store_true")
    args = ap.parse_args()

    base = args.base.rstrip("/")
    base_entity_url = f"{base}/api/{args.module}/{args.entity}"
    # choose id if not provided
    rid = args.id
    if not rid:
        sc, body = get_json(base_entity_url, params={"limit":10})
        if sc!=200 or not isinstance(body, list) or not body:
            print(f"[FATAL] list failed: {sc}"); print(pretty(body)); return
        rid = body[0].get("id") or body[0].get("ID")
        print(f"[i] Picked id={rid}")
    # plain
    sc, plain = get_json(f"{base_entity_url}/{rid}")
    print(f"[plain] status={sc}")
    if sc!=200: print(pretty(plain)); return
    # expand
    if args.full:
        params = {"full":"1"}
        label = "full=1"
    else:
        depth = max(0, min(5, args.depth))
        params = {"_depth": depth}
        if args.expand: params["_expand"] = args.expand
        else: params["_expand"] = "*"
        label = f"{params}"
    sc, obj = get_json(f"{base_entity_url}/{rid}", params=params)
    print(f"[expand {label}] status={sc}")
    if sc!=200 or not isinstance(obj, dict):
        print(pretty(obj)); return
    if args.show_json: print(pretty(obj))
    children = obj.get("_children"); counts = summarize_children(children)
    total = sum(counts.values())
    print(f"  _expand_depth={obj.get('_expand_depth')}  _truncated={obj.get('_truncated', False)}  children_total={total}")
    for k in sorted(counts.keys()):
        print(f"    {k}: {counts[k]}")

if __name__ == "__main__":
    main()

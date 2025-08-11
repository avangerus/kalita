#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Kalita API helper
- Lists entities (/api/meta)
- Shows meta for a specific entity (/api/meta/:module/:entity)
- Optionally attempts CREATE/GET/LIST/COUNT/PATCH/DELETE with safe defaults
- Can fetch a catalog by name (/api/meta/catalog/:name)

Examples:
  python kalita_helper.py --base http://localhost:8080 --module core --entity User
  python kalita_helper.py --module core --entity User --create --autofill
  python kalita_helper.py --module core --entity User --create --data '{"email":"a@b.c"}'
  python kalita_helper.py --catalog currencies
"""
import argparse
import json
import sys
import time
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional, Tuple

try:
    import requests
except ImportError:
    print("This script needs the 'requests' package. Install it with: pip install requests", file=sys.stderr)
    sys.exit(1)

def pretty(obj: Any) -> str:
    try:
        return json.dumps(obj, ensure_ascii=False, indent=2, sort_keys=True)
    except Exception:
        return str(obj)

def get_json(url: str) -> Tuple[int, Any, Dict[str, str]]:
    try:
        r = requests.get(url, timeout=10)
        ct = r.headers.get("Content-Type", "")
        try:
            body = r.json()
        except Exception:
            body = None
        return r.status_code, body, {"Content-Type": ct}
    except Exception as e:
        print(f"GET {url} failed: {e}", file=sys.stderr)
        return 0, None, {}

def post_json(url: str, payload: Dict[str, Any]) -> Tuple[int, Any]:
    try:
        r = requests.post(url, json=payload, timeout=15)
        try:
            return r.status_code, r.json()
        except Exception:
            return r.status_code, r.text
    except Exception as e:
        return 0, str(e)

def put_json(url: str, payload: Dict[str, Any]) -> Tuple[int, Any]:
    try:
        r = requests.put(url, json=payload, timeout=15)
        try:
            return r.status_code, r.json()
        except Exception:
            return r.status_code, r.text
    except Exception as e:
        return 0, str(e)

def patch_json(url: str, payload: Dict[str, Any]) -> Tuple[int, Any]:
    try:
        r = requests.patch(url, json=payload, timeout=15)
        try:
            return r.status_code, r.json()
        except Exception:
            return r.status_code, r.text
    except Exception as e:
        return 0, str(e)

def delete(url: str) -> int:
    try:
        r = requests.delete(url, timeout=10)
        return r.status_code
    except Exception as e:
        print(f"DELETE {url} failed: {e}", file=sys.stderr)
        return 0

def now_iso() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat()

def parse_entity_meta(meta_entity: Any) -> Dict[str, Dict[str, Any]]:
    """
    Expecting meta like:
    {
      "module":"core",
      "entity":"User",
      "fields":[
         {"name":"email","type":"string","required":true,"readOnly":false, ...},
         ...
      ]
    }
    Returns dict[name] -> info
    """
    fields = {}
    if isinstance(meta_entity, dict):
        arr = meta_entity.get("fields") or meta_entity.get("Fields") or []
        if isinstance(arr, list):
            for f in arr:
                if isinstance(f, dict) and "name" in f:
                    name = f.get("name") or f.get("Name")
                    info = {
                        "type": f.get("type") or f.get("Type"),
                        "required": bool(f.get("required") or f.get("Required")),
                        "readOnly": bool(f.get("readOnly") or f.get("ReadOnly")),
                        "options": f.get("options") or f.get("Options") or {},
                    }
                    fields[name] = info
    return fields

def build_autofill_payload(fields: Dict[str, Dict[str, Any]]) -> Dict[str, Any]:
    """Skip readOnly/system fields. Provide minimal best-effort values for required fields."""
    payload: Dict[str, Any] = {}
    system = {"id","version","created_at","updated_at","deleted_at"}
    for name, info in fields.items():
        if name in system: 
            continue
        if info.get("readOnly"):
            continue
        typ = (info.get("type") or "").lower()
        required = info.get("required", False)
        if not required:
            # skip optional fields; backend likely has defaults
            continue
        # best-effort value by type
        if typ in ("string","text","email","uuid"):
            payload[name] = f"autofill-{name}"
        elif typ in ("int","integer","bigint","number","float","double","decimal"):
            payload[name] = 0
        elif typ in ("bool","boolean"):
            payload[name] = False
        elif typ == "date":
            payload[name] = now_iso().split("T")[0]
        elif typ in ("datetime","timestamp"):
            payload[name] = now_iso()
        elif typ in ("ref","reference"):
            # we can't guess an id here; leave empty and let server validate
            # caller can override via --data
            continue
        else:
            # unknown -> string
            payload[name] = f"autofill-{name}"
    return payload

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8080", help="Base URL")
    ap.add_argument("--module", help="Module (e.g., core)")
    ap.add_argument("--entity", help="Entity (e.g., User)")
    ap.add_argument("--id", help="Record id (optional; server may auto-generate)")
    ap.add_argument("--create", action="store_true", help="Attempt a CREATE/GET/LIST/COUNT/PATCH/DELETE sequence")
    ap.add_argument("--data", help="JSON to merge into the create payload")
    ap.add_argument("--autofill", action="store_true", help="Best-effort fill required fields (skips read-only)")
    ap.add_argument("--catalog", help="Fetch /api/meta/catalog/<name> and print")
    args = ap.parse_args()

    base = args.base.rstrip("/")
    print(f"[i] Base URL: {base}")

    # List all entities
    status, meta_all, _ = get_json(f"{base}/api/meta")
    print(f"[GET /api/meta] status={status}")
    if meta_all is not None:
        print(pretty(meta_all))

    # Catalog fetch if requested
    if args.catalog:
        sc, body, _ = get_json(f"{base}/api/meta/catalog/{args.catalog}")
        print(f"[GET /api/meta/catalog/{args.catalog}] status={sc}")
        if body is not None:
            print(pretty(body))
        return

    mod = args.module or "core"
    ent = args.entity or "User"
    print(f"[i] Target entity: {mod}/{ent}")

    # Fetch entity meta
    sc, meta_ent, _ = get_json(f"{base}/api/meta/{mod}/{ent}")
    print(f"[GET /api/meta/{mod}/{ent}] status={sc}")
    if meta_ent is not None:
        print(pretty(meta_ent))
    fields = parse_entity_meta(meta_ent)
    if not fields:
        print("[!] Couldn't parse fields from meta; CREATE may fail without explicit --data", file=sys.stderr)

    if not args.create:
        return

    # Build payload
    payload: Dict[str, Any] = {}
    if args.autofill and fields:
        payload.update(build_autofill_payload(fields))

    if args.data:
        try:
            user_data = json.loads(args.data)
            if not isinstance(user_data, dict):
                raise ValueError("data is not a JSON object")
            payload.update(user_data)
        except Exception as e:
            print(f"[!] --data parse error: {e}", file=sys.stderr)
            return

    rid = args.id
    if rid:
        payload["id"] = rid  # only if user asked; some backends allow client ids

    base_entity_url = f"{base}/api/{mod}/{ent}"

    print("\n[CREATE] payload:")
    print(pretty(payload))
    sc, body = post_json(base_entity_url, payload)
    print(f"POST {base_entity_url} -> {sc}")
    if body is not None:
        print(pretty(body))

    # Try to extract id from response if present
    new_id = None
    if isinstance(body, dict):
        new_id = body.get("id") or body.get("ID") or rid
    new_id = new_id or rid

    # Follow-up operations only if we have an id
    if new_id:
        print("\n[GET ONE]")
        sc, gbody, _ = get_json(f"{base_entity_url}/{new_id}")
        print(f"GET {base_entity_url}/{new_id} -> {sc}")
        if gbody is not None:
            print(pretty(gbody))

        print("\n[PATCH] (best-effort)")
        sc, pbody = patch_json(f"{base_entity_url}/{new_id}", {})
        print(f"PATCH {base_entity_url}/{new_id} -> {sc}")
        if pbody is not None:
            print(pretty(pbody))
    else:
        print("\n[i] No id to follow-up with GET/PATCH (CREATE likely failed or server auto-generated and didn't return id).")

    print("\n[LIST]")
    sc, lbody, _ = get_json(base_entity_url)
    print(f"GET {base_entity_url} -> {sc}")
    if lbody is not None:
        out = pretty(lbody)
        print(out[:4000])

    print("\n[COUNT]")
    sc, cbody, _ = get_json(f"{base}/api/{mod}/{ent}/count")
    print(f"GET /api/{mod}/{ent}/count -> {sc}")
    if cbody is not None:
        print(pretty(cbody))

if __name__ == "__main__":
    main()

#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Kalita API + DB integration test (CRUD, list, sorting, pagination, search, count)

Enhancements:
- Detects required via field.options.required (string "true" or boolean).
- Auto-fills enum fields using the first enum value from meta.
- Auto-fills email fields with unique values (token-based).
- Still supports --with and --catalog-for.

Usage examples:
  python kalita_api_db_test.py --module core --entity User --records 3
  python kalita_api_db_test.py --module core --entity User --records 3 --with "{\"role\":\"Admin\"}"
"""
import argparse
import json
import random
import string
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

def get_json(url: str, params: Dict[str, Any]=None) -> Tuple[int, Any]:
    try:
        r = requests.get(url, params=params or {}, timeout=15)
        try:
            return r.status_code, r.json()
        except Exception:
            return r.status_code, r.text
    except Exception as e:
        return 0, str(e)

def post_json(url: str, payload: Dict[str, Any]) -> Tuple[int, Any]:
    try:
        r = requests.post(url, json=payload, timeout=20)
        try:
            return r.status_code, r.json()
        except Exception:
            return r.status_code, r.text
    except Exception as e:
        return 0, str(e)

def patch_json(url: str, payload: Dict[str, Any]) -> Tuple[int, Any]:
    try:
        r = requests.patch(url, json=payload, timeout=20)
        try:
            return r.status_code, r.json()
        except Exception:
            return r.status_code, r.text
    except Exception as e:
        return 0, str(e)

def delete(url: str) -> int:
    try:
        r = requests.delete(url, timeout=15)
        return r.status_code
    except Exception as e:
        return 0

def now_iso() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat()

def rand_token(n=6) -> str:
    alphabet = string.ascii_lowercase + string.digits
    return ''.join(random.choice(alphabet) for _ in range(n))

def fetch_meta(base: str, module: str, entity: str) -> Dict[str, Any]:
    sc, body = get_json(f"{base}/api/meta/{module}/{entity}")
    if sc != 200 or not isinstance(body, dict):
        print(f"[WARN] Could not fetch meta for {module}/{entity}, status={sc}")
        return {}
    return body

def parse_fields(meta_ent: Dict[str, Any]) -> List[Dict[str, Any]]:
    arr = meta_ent.get("fields") or meta_ent.get("Fields") or []
    if isinstance(arr, list):
        # normalize options to dict
        for f in arr:
            if isinstance(f, dict):
                opts = f.get("options") or f.get("Options") or {}
                f["_opts"] = opts
        return [f for f in arr if isinstance(f, dict)]
    return []

def is_required(f: Dict[str, Any]) -> bool:
    # direct flags
    if bool(f.get("required") or f.get("Required")):
        return True
    # options.required may be "true" or True
    opts = f.get("_opts") or {}
    val = opts.get("required")
    if isinstance(val, bool):
        return val
    if isinstance(val, str):
        return val.lower() in ("true","1","yes","y")
    return False

def choose_text_field(fields: List[Dict[str, Any]]) -> Optional[str]:
    candidates = []
    for f in fields:
        name = f.get("name") or f.get("Name")
        typ  = (f.get("type") or f.get("Type") or "").lower()
        ro   = bool(f.get("readOnly") or f.get("ReadOnly"))
        if ro: 
            continue
        if typ in ("string","text","email","uuid","varchar"):
            candidates.append(name)
    for pref in ("name","title","code","email","login","username"):
        if pref in candidates:
            return pref
    return candidates[0] if candidates else None

def build_payloads(module: str, entity: str, fields: List[Dict[str, Any]], n: int, token: str, text_field: Optional[str], overrides: Dict[str, Any]) -> List[Dict[str, Any]]:
    payloads: List[Dict[str, Any]] = []
    ro_names = { (f.get("name") or f.get("Name")) for f in fields if bool(f.get("readOnly") or f.get("ReadOnly")) }
    system = {"id","version","created_at","updated_at","deleted_at"}

    # pick enum defaults from meta
    enum_defaults: Dict[str, Any] = {}
    for f in fields:
        name = f.get("name") or f.get("Name")
        if not name:
            continue
        if name in overrides:
            continue
        typ  = (f.get("type") or f.get("Type") or "").lower()
        if typ == "enum":
            enum_vals = f.get("enum") or f.get("Enum") or []
            if isinstance(enum_vals, list) and enum_vals:
                enum_defaults[name] = enum_vals[0]

    for i in range(n):
        p: Dict[str, Any] = {}
        # required fields first
        for f in fields:
            name = f.get("name") or f.get("Name")
            if not name or name in system or name in ro_names:
                continue
            if name in overrides:
                continue
            if not is_required(f):
                continue
            typ  = (f.get("type") or f.get("Type") or "").lower()
            if typ in ("ref","reference"):
                continue
            if typ in ("string","text","uuid","varchar"):
                p[name] = f"{token}-{name}-{i}"
            elif typ == "email":
                p[name] = f"{token}.{i}@example.com"
            elif typ in ("int","integer","bigint","number","float","double","decimal"):
                p[name] = i
            elif typ in ("bool","boolean"):
                p[name] = (i % 2 == 0)
            elif typ == "date":
                p[name] = now_iso().split("T")[0]
            elif typ in ("datetime","timestamp"):
                p[name] = now_iso()
            elif typ == "enum":
                if name in enum_defaults:
                    p[name] = enum_defaults[name]
                else:
                    p[name] = f"{token}-{name}-enum"
            else:
                p[name] = f"{token}-{name}-{i}"
        # ensure a text field is present for sorting/search (if optional)
        if text_field and text_field not in ro_names and text_field not in overrides and text_field not in p:
            p[text_field] = f"{token}-{text_field}-{i:02d}"
        # merge enum defaults for optional enums if not set
        for k,v in enum_defaults.items():
            p.setdefault(k, v)
        # finally overrides
        p.update(overrides)
        payloads.append(p)
    return payloads

def validate_sorted(values: List[str], asc=True) -> bool:
    comp = sorted(values)
    if not asc:
        comp = list(reversed(comp))
    return values == comp

def try_sort_list(base_url: str, field: str) -> Tuple[Optional[str], bool, bool]:
    styles = [
        ("style1", lambda asc: {"sort": f"{field}:{'asc' if asc else 'desc'}"}),
        ("style2", lambda asc: {"sort": (f"+{field}" if asc else f"-{field}")}),
        ("style3a", lambda asc: {"orderBy": field, "order": ("asc" if asc else "desc")}),
    ]
    ok_style = None
    ok_asc = False
    ok_desc = False
    for name, build_params in styles:
        for asc in (True, False):
            params = build_params(asc)
            sc, body = get_json(base_url, params=params)
            if sc != 200 or not isinstance(body, list) or len(body) == 0:
                continue
            vals = []
            for item in body:
                if isinstance(item, dict) and field in item and isinstance(item[field], (str,int,float)):
                    vals.append(str(item[field]))
                else:
                    vals = []
                    break
            if not vals:
                continue
            ordered = validate_sorted(vals, asc=asc)
            if asc and ordered:
                ok_asc = True
                ok_style = ok_style or name
            if (not asc) and ordered:
                ok_desc = True
                ok_style = ok_style or name
        if ok_asc and ok_desc:
            return ok_style, ok_asc, ok_desc
    return ok_style, ok_asc, ok_desc

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8080", help="Base URL")
    ap.add_argument("--module", default="core", help="Module")
    ap.add_argument("--entity", default="User", help="Entity")
    ap.add_argument("--records", type=int, default=5, help="How many records to create")
    ap.add_argument("--clean", action="store_true", help="Only delete records previously created by this script (by token)")
    ap.add_argument("--with", dest="with_json", help="JSON object to merge into each create payload (e.g. '{\"email\":\"u@ex.com\",\"role\":\"User\"}')")
    ap.add_argument("--catalog-for", dest="catalog_for", help="FIELD:CatalogName -> fetch first code and use as FIELD value")
    args = ap.parse_args()

    base = args.base.rstrip("/")
    mod = args.module
    ent = args.entity
    base_entity_url = f"{base}/api/{mod}/{ent}"
    token = f"apitest-{int(time.time())}-{rand_token()}"

    print(f"[i] Target: {mod}/{ent}  base={base}  token={token}")

    # Parse overrides
    overrides: Dict[str, Any] = {}
    if args.with_json:
        try:
            obj = json.loads(args.with_json)
            if isinstance(obj, dict):
                overrides.update(obj)
            else:
                print("[!] --with must be a JSON object", file=sys.stderr)
        except Exception as e:
            print(f"[!] --with parse error: {e}", file=sys.stderr)

    # Catalog for field
    if args.catalog_for:
        try:
            field, cat = args.catalog_for.split(":", 1)
            sc, body = get_json(f"{base}/api/meta/catalog/{cat}")
            if sc == 200 and isinstance(body, dict):
                items = body.get("items") or body.get("Items") or []
                if isinstance(items, list) and items:
                    first = items[0]
                    code = None
                    for k in ("Code","code","id","ID","value","Value","key","Key","Name","name"):
                        if isinstance(first, dict) and k in first:
                            code = first[k]
                            break
                    if code is not None:
                        overrides[field] = code
                        print(f"[i] Using catalog '{cat}' for field '{field}': {code}")
        except Exception as e:
            print(f"[WARN] --catalog-for parse/fetch failed: {e}", file=sys.stderr)

    # Fetch meta to understand fields
    meta = fetch_meta(base, mod, ent)
    fields = parse_fields(meta)
    if not fields:
        print("[WARN] No fields meta; proceeding blind. You may need to supply required fields via --with.")
    text_field = choose_text_field(fields)
    if text_field:
        print(f"[i] Using text field for sort/search: {text_field}")
    else:
        print("[i] No obvious text field found; sorting/search tests may be limited.")

    # Clean-only mode
    if args.clean and text_field:
        print("[CLEAN] Attempting to find and delete previous test records")
        for qname in ("q","search","query"):
            sc, body = get_json(base_entity_url, params={qname: "apitest-"})
            if sc==200 and isinstance(body, list) and body:
                for item in body:
                    rid = item.get("id") or item.get("ID")
                    if not rid: continue
                    dsc = delete(f"{base_entity_url}/{rid}")
                    print(f"DELETE {rid} -> {dsc}")
        print("[CLEAN] Done.")
        return

    # Create N records
    payloads = build_payloads(mod, ent, fields, args.records, token, text_field, overrides)
    ids: List[str] = []
    for p in payloads:
        sc, body = post_json(base_entity_url, p)
        if sc != 200:
            print(f"[CREATE] status={sc}\n{pretty(body)}")
        rid = None
        if isinstance(body, dict):
            rid = body.get("id") or body.get("ID")
        if rid:
            ids.append(rid)
        else:
            print("[WARN] Could not capture created id; some follow-up tests may be limited")

    print(f"[i] Created {len(ids)} / {args.records} records.")

    # LIST
    sc, body = get_json(base_entity_url)
    print(f"[LIST] status={sc}, items={len(body) if isinstance(body, list) else 'n/a'}")

    # COUNT (two endpoints)
    for suffix in ("count","_count"):
        sc, body = get_json(f"{base_entity_url}/{suffix}")
        if isinstance(body, dict) and "total" in body:
            print(f"[COUNT via /{suffix}] total={body.get('total')} (status={sc})")
        else:
            print(f"[COUNT via /{suffix}] status={sc} body={str(body)[:200]}")

    # Pagination (limit/offset)
    sc, body = get_json(base_entity_url, params={"limit": 2, "offset": 0})
    if sc==200 and isinstance(body, list):
        print(f"[PAGE] limit=2 offset=0 -> {len(body)} items")
    else:
        print(f"[PAGE] pagination with limit/offset may not be supported (status={sc})")

    # Sorting test
    if text_field:
        style, okAsc, okDesc = try_sort_list(base_entity_url, text_field)
        if style:
            print(f"[SORT] {style}: asc={okAsc} desc={okDesc}  (field={text_field})")
        else:
            print("[SORT] Unable to validate sorting; API may use a different parameter name or requires explicit whitelists.")

    # Search test
    found = False
    for qname in ("q","search","query"):
        sc, body = get_json(base_entity_url, params={qname: "apitest-"})
        if sc==200 and isinstance(body, list):
            cnt = len(body)
            print(f"[SEARCH] param '{qname}' -> {cnt} items (status={sc})")
            if cnt>0: found = True
            break
    if not found:
        print("[SEARCH] No items found via q/search/query — search might require specific fields or param name.")

    # Update first id
    if ids and text_field:
        rid = ids[0]
        sc, body = patch_json(f"{base_entity_url}/{rid}", {text_field: f"{token}-{text_field}-upd"})
        print(f"[PATCH] id={rid} status={sc}")
        if isinstance(body, dict):
            print(pretty(body))

    # Delete last id
    if ids:
        rid = ids[-1]
        dsc = delete(f"{base_entity_url}/{rid}")
        print(f"[DELETE] id={rid} -> {dsc}")

    print("\n[i] Test run complete. Tip: you can force specific values via --with if needed.")

if __name__ == "__main__":
    main()

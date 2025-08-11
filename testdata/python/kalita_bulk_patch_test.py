#!/usr/bin/env python3
import requests, json, uuid

BASE = "http://localhost:8080/api/test/item"

def ok(flag, msg):
    print(("🟩 " if flag else "🟥 ") + msg)

def j(r):
    if r.headers.get("content-type","").startswith("application/json"):
        try:
            return r.json()
        except Exception:
            return None
    return None

def create_unique(name_prefix):
    for _ in range(3):
        code_val = f"B{uuid.uuid4().hex[:6].upper()}"
        r = requests.post(BASE, json={"code": code_val, "name": f"{name_prefix}-{code_val}"})
        if r.status_code in (200,201):
            return r
    return r

def get_one(id_):
    r = requests.get(f"{BASE}/{id_}")
    etag = r.headers.get("ETag","").strip('"')
    return r, etag

def patch_one(id_, payload, etag=None):
    h = {"Content-Type":"application/json"}
    if etag is not None:
        h["If-Match"] = f'"{etag}"'
    return requests.patch(f"{BASE}/{id_}", json=payload, headers=h)

def bulk_try(items, mode):
    # mode: 'array' | 'wrapped' | 'wrappedIfMatch'
    if mode == "array":
        return requests.patch(f"{BASE}/_bulk", json=items)
    if mode == "wrapped":
        return requests.patch(f"{BASE}/_bulk", json={"items": items})
    if mode == "wrappedIfMatch":
        conv = []
        for it in items:
            it2 = {"id": it["id"], "patch": it["patch"]}
            if "if_match" in it:
                it2["ifMatch"] = it["if_match"]
            conv.append(it2)
        return requests.patch(f"{BASE}/_bulk", json={"items": conv})
    raise ValueError("bad mode")

def normalize_bulk_response(r):
    body = j(r)
    if isinstance(body, list):
        return body
    if isinstance(body, dict) and "results" in body and isinstance(body["results"], list):
        return body["results"]
    return None

def main():
    # 1) create two
    r1 = create_unique("Alpha"); r2 = create_unique("Beta")
    ok(r1.status_code in (200,201) and r2.status_code in (200,201), f"created 2 items (codes {r1.status_code}, {r2.status_code})")
    a, b = j(r1), j(r2)
    if not isinstance(a, dict) or not isinstance(b, dict) or "id" not in a or "id" not in b:
        print("    create#1 body:", r1.text); print("    create#2 body:", r2.text); return
    a_id, b_id = a["id"], b["id"]

    # 2) stale If-Match for A
    rA, etag = get_one(a_id)
    ok(rA.status_code == 200 and etag, f"got ETag for A (code {rA.status_code})")
    patch_one(a_id, {"name":"Alpha1"})  # bump version

    items = [
        {"id": a_id, "patch": {"name": "AlphaBulk"}, "if_match": etag},
        {"id": b_id, "patch": {"name": "BetaBulk"}}
    ]

    # 3) try array → wrapped → wrappedIfMatch
    modes = ["array", "wrapped", "wrappedIfMatch"]
    bulk_ok = False; used_mode = None; results = None; http_code = None; raw_text = None
    for m in modes:
        rb = bulk_try(items, m)
        http_code = rb.status_code
        results = normalize_bulk_response(rb)
        if results is not None and len(results) >= 2:
            bulk_ok = True; used_mode = m; break
        raw_text = rb.text

    ok(bulk_ok and http_code in (200,207), f"bulk patch ({used_mode}) HTTP {http_code}")
    if not bulk_ok:
        print("    bulk body:", raw_text); return

    rAres, rBres = results[0], results[1]
    ok(rAres.get("status") in (409, 412), "A version conflict as expected")
    ok(rBres.get("status") == 200, "B updated in bulk")

    # 4) unique violation on B.code ← A.code
    #    reuse the detected working mode
    uniq_items = [{"id": b_id, "patch": {"code": a.get("code")}}]
    rb2 = bulk_try(uniq_items, used_mode or "array")
    results2 = normalize_bulk_response(rb2)
    good = False
    if isinstance(results2, list) and results2:
        r = results2[0]
        errs = r.get("errors", []) or []
        good = r.get("status") in (400,409) and any((e.get("code") or "")=="unique_violation" for e in errs)
    ok(good, "unique violation detected (if 'code' is unique)")
    if not good:
        print("    bulk-unique raw:", rb2.text)

if __name__ == "__main__":
    main()

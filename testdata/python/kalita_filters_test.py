#!/usr/bin/env python3
# -*- coding: utf-8 -*-

"""
Kalita API filters test (List/Count with operators).

Checks:
  1. Creates a core.user (manager) and an olga.company.
  2. Creates three olga.project with statuses: Draft, InWork, Closed.
  3. GET /api/olga/project?status__in=Draft,Closed -> expect only Draft & Closed.
  4. GET /api/olga/project?status__in=in:Draft,Closed (alternative syntax) -> same.
  5. GET /api/olga/project/count?status__in=Draft,Closed -> total matches.
  6. Cleanup: delete created projects, company, user.

Run: python kalita_filters_test.py
"""

import sys
import time
import requests

BASE_URL = "http://localhost:8080"
HEADERS  = {"Content-Type": "application/json"}

USER_MOD, USER_ENT = "core", "user"
USER_ROLE = "Manager"  # adjust to your DSL enum if needed

COMP_MOD, COMP_ENT = "olga", "company"
PROJ_MOD, PROJ_ENT = "olga", "project"

REQ_TIMEOUT = 30

def u(*parts): return "/".join(p.strip("/") for p in parts)

def ensure_meta(obj, where):
    for k in ("id","version","created_at","updated_at"):
        if k not in obj:
            raise AssertionError(f"{where}: missing {k}")

def post(mod, ent, payload):
    return requests.post(u(BASE_URL,"api",mod,ent), headers=HEADERS, json=payload, timeout=REQ_TIMEOUT)

def get(mod, ent, params=None, path_suffix=""):
    return requests.get(u(BASE_URL,"api",mod,ent, path_suffix), params=params or {}, timeout=REQ_TIMEOUT)

def delete(mod, ent, rec_id):
    return requests.delete(u(BASE_URL,"api",mod,ent,rec_id), timeout=REQ_TIMEOUT)

def main():
    created_ids = {"projects": []}

    # 1) manager
    r = post(USER_MOD, USER_ENT, {
        "name":  f"Filters Manager {int(time.time())}",
        "email": f"filters_manager_{int(time.time())}@example.com",
        "role":  USER_ROLE,
    })
    if r.status_code not in (200,201):
        print("User CREATE failed:", r.status_code, r.text, file=sys.stderr); sys.exit(1)
    user = r.json(); ensure_meta(user, "User CREATE")
    manager_id = user["id"]
    created_ids["user"] = manager_id
    print(f"[OK] User created: {manager_id}")

    # 2) company
    r = post(COMP_MOD, COMP_ENT, {"name": "Filters Co"})
    if r.status_code not in (200,201):
        delete(USER_MOD, USER_ENT, manager_id)
        print("Company CREATE failed:", r.status_code, r.text, file=sys.stderr); sys.exit(1)
    comp = r.json(); ensure_meta(comp, "Company CREATE")
    company_id = comp["id"]
    created_ids["company"] = company_id
    print(f"[OK] Company created: {company_id}")

    # 3) projects: Draft, InWork, Closed
    statuses = ["Draft", "InWork", "Closed"]
    for st in statuses:
        pr = post(PROJ_MOD, PROJ_ENT, {
            "name":       f"Proj {st} {int(time.time())}",
            "company_id": company_id,
            "manager_id": manager_id,
            "status":     st,
        })
        if pr.status_code not in (200,201):
            print("Project CREATE failed:", st, pr.status_code, pr.text, file=sys.stderr)
            # cleanup
            for pid in created_ids["projects"]:
                delete(PROJ_MOD, PROJ_ENT, pid)
            delete(COMP_MOD, COMP_ENT, company_id)
            delete(USER_MOD, USER_ENT, manager_id)
            sys.exit(1)
        pj = pr.json(); ensure_meta(pj, "Project CREATE")
        created_ids["projects"].append(pj["id"])
    print(f"[OK] Projects created:", created_ids["projects"])

    # 4) List with status__in=Draft,Closed
    r = get(PROJ_MOD, PROJ_ENT, params={"status__in": "Draft,Closed"})
    if r.status_code != 200:
        print("List with status__in failed:", r.status_code, r.text, file=sys.stderr); sys.exit(1)
    arr = r.json()
    if not isinstance(arr, list):
        print("List did not return a list", file=sys.stderr); sys.exit(1)
    got_statuses = {x.get("status") for x in arr}
    if not got_statuses.issubset({"Draft","Closed"}) or not got_statuses:
        print("List status__in mismatch, got statuses:", got_statuses, file=sys.stderr); sys.exit(1)
    print("[OK] List status__in=Draft,Closed returned only Draft/Closed:", got_statuses)

    # 5) Alternative syntax: status__in=in:Draft,Closed
    r = get(PROJ_MOD, PROJ_ENT, params={"status__in": "in:Draft,Closed"})
    if r.status_code != 200:
        print("List with status__in=in:... failed:", r.status_code, r.text, file=sys.stderr); sys.exit(1)
    arr2 = r.json()
    got2 = {x.get("status") for x in arr2}
    if got2 != got_statuses:
        print("Alternative in: syntax mismatch:", got2, "vs", got_statuses, file=sys.stderr); sys.exit(1)
    print("[OK] List status__in using 'in:' prefix works as expected")

    # 6) Count endpoint
    rc = get(PROJ_MOD, PROJ_ENT, params={"status__in": "Draft,Closed"}, path_suffix="count")
    if rc.status_code != 200:
        print("Count failed:", rc.status_code, rc.text, file=sys.stderr); sys.exit(1)
    total = rc.json().get("total")
    if total != len(arr):
        print("Count mismatch: total", total, "!= len(list)", len(arr), file=sys.stderr); sys.exit(1)
    print("[OK] Count matches filtered list:", total)

    print("\nFilters test passed ✅")

    # Cleanup
    for pid in created_ids["projects"]:
        delete(PROJ_MOD, PROJ_ENT, pid)
    time.sleep(0.1)
    delete(COMP_MOD, COMP_ENT, company_id)
    time.sleep(0.1)
    delete(USER_MOD, USER_ENT, manager_id)

if __name__ == "__main__":
    main()

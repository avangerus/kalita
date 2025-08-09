#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import sys, time, requests

BASE_URL = "http://localhost:8080"
HEADERS  = {"Content-Type": "application/json"}

# ---- adjust if your core.user differs ----
USER_MODULE, USER_ENTITY = "core", "user"
ROLE_VALUE = "Manager"   # поменяй, если у тебя иной enum набора ролей
# ------------------------------------------

PARENT_MODULE, PARENT_ENTITY = "olga", "company"
CHILD_MODULE,  CHILD_ENTITY  = "olga", "project"
CHILD_STATUS = "Draft"       # enum из DSL для Project

def u(*parts): return "/".join(p.strip("/") for p in parts)

def ensure_meta(obj, where):
    for k in ("id","version","created_at","updated_at"):
        if k not in obj: raise AssertionError(f"{where}: missing {k}")

def post(mod, ent, payload):
    return requests.post(u(BASE_URL,"api",mod,ent), headers=HEADERS, json=payload, timeout=30)

def delete(mod, ent, rec_id):
    return requests.delete(u(BASE_URL,"api",mod,ent,rec_id), timeout=30)

def main():
    # 1) core.user (manager stub)
    user_payload = {
        "name":  "FK Manager",
        "email": f"fk_manager_{int(time.time())}@example.com",
        "role":  ROLE_VALUE
    }
    r = post(USER_MODULE, USER_ENTITY, user_payload)
    if r.status_code not in (200,201):
        print("User CREATE failed:", r.status_code, r.text, file=sys.stderr); sys.exit(1)
    user = r.json(); ensure_meta(user, "User CREATE")
    manager_id = user["id"]
    print(f"[OK] Manager(core.user) created: {manager_id}")

    # 2) olga.company
    r = post(PARENT_MODULE, PARENT_ENTITY, {"name":"FK Co"})
    if r.status_code not in (200,201):
        print("Company CREATE failed:", r.status_code, r.text, file=sys.stderr); sys.exit(1)
    company = r.json(); ensure_meta(company, "Company CREATE")
    company_id = company["id"]
    print(f"[OK] Company created: {company_id}")

    # 3) olga.project (company_id + manager_id + status)
    proj_payload = {
        "name":       "FK Project",
        "company_id": company_id,
        "manager_id": manager_id,
        "status":     CHILD_STATUS
    }
    r = post(CHILD_MODULE, CHILD_ENTITY, proj_payload)
    if r.status_code not in (200,201):
        # cleanup parent+user
        delete(PARENT_MODULE, PARENT_ENTITY, company_id)
        print("Project CREATE failed:", r.status_code, r.text, file=sys.stderr); sys.exit(1)
    proj = r.json(); ensure_meta(proj, "Project CREATE")
    project_id = proj["id"]
    print(f"[OK] Project created: {project_id}")

    # 4) DELETE company -> expect 409 fk_in_use
    r = delete(PARENT_MODULE, PARENT_ENTITY, company_id)
    if r.status_code != 409:
        # cleanup
        delete(CHILD_MODULE, CHILD_ENTITY, project_id)
        time.sleep(0.2)
        delete(PARENT_MODULE, PARENT_ENTITY, company_id)
        print("Expected 409 on company DELETE, got:", r.status_code, r.text, file=sys.stderr)
        sys.exit(1)
    body = {}
    try: body = r.json()
    except: pass
    errs = (body.get("errors") or []) if isinstance(body, dict) else []
    if not any(isinstance(e, dict) and e.get("code")=="fk_in_use" for e in errs):
        # cleanup
        delete(CHILD_MODULE, CHILD_ENTITY, project_id)
        time.sleep(0.2)
        delete(PARENT_MODULE, PARENT_ENTITY, company_id)
        print("409 but no code=fk_in_use. Body:", body, file=sys.stderr)
        sys.exit(1)
    print("[OK] Company DELETE returned 409 fk_in_use")

    # 5) delete project -> 204; then company -> 204
    r1 = delete(CHILD_MODULE, CHILD_ENTITY, project_id)
    if r1.status_code != 204:
        print("Project DELETE expected 204, got:", r1.status_code, r1.text, file=sys.stderr); sys.exit(1)
    time.sleep(0.2)
    r2 = delete(PARENT_MODULE, PARENT_ENTITY, company_id)
    if r2.status_code != 204:
        print("Company DELETE expected 204, got:", r2.status_code, r2.text, file=sys.stderr); sys.exit(1)

    print("\nFK protection test passed ✅")

if __name__ == "__main__":
    main()

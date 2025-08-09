#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import sys, time, requests

BASE = "http://localhost:8080"
H = {"Content-Type":"application/json"}

def u(*p): return "/".join(s.strip("/") for s in p)

def meta_list():
    r = requests.get(u(BASE, "api", "meta"), timeout=30)
    r.raise_for_status()
    return r.json()  # [{module,name,fields}, ...]

def meta(mod, ent):
    r = requests.get(u(BASE, "api", "meta", mod, ent), timeout=30)
    r.raise_for_status()
    return r.json()

def create(mod, ent, payload):
    return requests.post(u(BASE,"api",mod,ent), json=payload, headers=H, timeout=30)

def get_one(mod, ent, rid):
    return requests.get(u(BASE,"api",mod,ent,rid), timeout=30)

def delete(mod, ent, rid):
    return requests.delete(u(BASE,"api",mod,ent,rid), timeout=30)

def pick_entity(meta_rows, name, prefer_module=None):
    cand = [r for r in meta_rows if r["name"].lower()==name.lower()]
    if not cand: return None
    if prefer_module:
        for r in cand:
            if r["module"].lower()==prefer_module.lower():
                return r
    return cand[0]

def first_enum(field):
    vals = field.get("enum") or []
    return vals[0] if vals else None

def synth_value(ftype, fname):
    ts = int(time.time())
    if ftype == "string":
        if "email" in fname.lower():
            return f"usr{ts}@example.com"
        return f"{fname}_{ts}"
    if ftype == "int": return 1
    if ftype == "float": return 1.0
    if ftype == "bool": return True
    if ftype == "date": return "2025-01-01"
    if ftype == "datetime": return "2025-01-01T00:00:00Z"
    return None

def build_required_payload(m):
    out = {}
    for f in m["fields"]:
        if not f.get("required"): continue
        t = f.get("type")
        n = f.get("name")
        if t == "enum":
            val = first_enum(f)
            if val is None: raise RuntimeError(f"required enum {n} has no values")
            out[n] = val
        elif t == "ref" or (t=="array" and f.get("elem_type")=="ref"):
            # ссылки проставим вручную позже
            continue
        else:
            v = synth_value(t, n)
            if v is not None: out[n] = v
    return out

def main():
    # 1) Получим список сущностей и выберем user/company/project
    rows = meta_list()

    user_row = pick_entity(rows, "user", prefer_module="core") or pick_entity(rows, "user")
    comp_row = pick_entity(rows, "company", prefer_module="olga") or pick_entity(rows, "company")
    proj_row = pick_entity(rows, "project", prefer_module="olga") or pick_entity(rows, "project")

    if not user_row or not comp_row or not proj_row:
        print("Не нашёл нужные сущности. Есть:", [(r["module"], r["name"]) for r in rows])
        sys.exit(1)

    user_m = meta(user_row["module"], user_row["name"])
    comp_m = meta(comp_row["module"], comp_row["name"])
    proj_m = meta(proj_row["module"], proj_row["name"])

    # 2) В проекте найдём:
    #   - поле ref на company с onDelete=restrict (или без onDelete — считаем restrict по умолчанию)
    #   - поле ref на user с onDelete=set_null
    company_ref_field = None
    manager_ref_field = None

    proj_fields = proj_m["fields"]
    for f in proj_fields:
        if f.get("type") == "ref" and f.get("ref"):
            # ref хранится FQN в meta
            if f["ref"].lower() == f'{comp_row["module"]}.{comp_row["name"]}'.lower():
                od = (f.get("onDelete") or "").lower()
                if od in ("", "restrict"):  # default считаем restrict
                    company_ref_field = f["name"]
            if f["ref"].lower() == f'{user_row["module"]}.{user_row["name"]}'.lower():
                od = (f.get("onDelete") or "").lower()
                if od == "set_null":
                    manager_ref_field = f["name"]

    if not company_ref_field:
        print("В проекте нет ref на company с on_delete=restrict (или он не распознан). Поля:", 
              [(f["name"], f.get("ref"), f.get("onDelete")) for f in proj_fields])
        sys.exit(1)

    if not manager_ref_field:
        print("В проекте нет ref на user с on_delete=set_null. Поля:", 
              [(f["name"], f.get("ref"), f.get("onDelete")) for f in proj_fields])
        sys.exit(1)

    # 3) Соберём валидные payload'ы по метаданным
    user_payload = build_required_payload(user_m)
    user_payload.setdefault("name", f"Mgr {int(time.time())}")
    if "email" in [f["name"] for f in user_m["fields"]]:
        user_payload.setdefault("email", f"usr{int(time.time())}@example.com")

    comp_payload = build_required_payload(comp_m)
    comp_payload.setdefault("name", f"Comp {int(time.time())}")

    proj_payload = build_required_payload(proj_m)
    proj_payload.setdefault("name", f"Prj {int(time.time())}")

    # 4) Создадим user и company
    r = create(user_row["module"], user_row["name"], user_payload)
    if r.status_code not in (200,201):
        print("create user fail", r.status_code, r.text); sys.exit(1)
    user = r.json(); user_id = user["id"]

    r = create(comp_row["module"], comp_row["name"], comp_payload)
    if r.status_code not in (200,201):
        print("create company fail", r.status_code, r.text); sys.exit(1)
    comp = r.json(); comp_id = comp["id"]

    # 5) Создадим проект с обоими ref'ами
    proj_payload[company_ref_field] = comp_id   # restrict
    proj_payload[manager_ref_field] = user_id   # set_null

    r = create(proj_row["module"], proj_row["name"], proj_payload)
    if r.status_code not in (200,201):
        delete(user_row["module"], user_row["name"], user_id)
        delete(comp_row["module"], comp_row["name"], comp_id)
        print("create project fail", r.status_code, r.text); sys.exit(1)
    prj = r.json(); prj_id = prj["id"]

    print("[OK] created user, company, project:",
          user_id, comp_id, prj_id,
          f"(fields: {manager_ref_field}=set_null, {company_ref_field}=restrict)")

    # 6) Удаляем user → поле set_null в проекте должно занулиться
    r = delete(user_row["module"], user_row["name"], user_id)
    if r.status_code not in (204,200): print("delete user failed", r.status_code, r.text); sys.exit(1)

    g = get_one(proj_row["module"], proj_row["name"], prj_id)
    if g.status_code != 200: print("get project after set_null failed", g.status_code, g.text); sys.exit(1)
    pj = g.json()
    if pj.get(manager_ref_field) not in (None, "", []):
        print("on_delete=set_null НЕ сработал, поле:", manager_ref_field, "=", pj.get(manager_ref_field)); sys.exit(1)
    print("[OK] on_delete=set_null worked: field nulled")

    # 7) Пытаемся удалить company → ожидаем 409 (restrict)
    r = delete(comp_row["module"], comp_row["name"], comp_id)
    if r.status_code != 409:
        print("Expected 409 on company delete (restrict), got", r.status_code, r.text); sys.exit(1)
    print("[OK] on_delete=restrict worked: delete blocked")

    print("\nOn-Delete policies test passed ✅")

    # cleanup
    delete(proj_row["module"], proj_row["name"], prj_id)
    delete(comp_row["module"], comp_row["name"], comp_id)

if __name__ == "__main__":
    main()

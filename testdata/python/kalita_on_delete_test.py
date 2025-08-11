import os, sys, time, requests as r

BASE = os.getenv("KALITA_BASE", "http://localhost:8080")
API  = f"{BASE}/api"
META = f"{BASE}/api/meta"

def ok(cond, msg):
    print(("[OK] " if cond else "[FAIL] ") + msg)
    if not cond:
        sys.exit(1)

def skip(msg): print("[SKIP] " + msg)

def meta_entity(module, entity):
    m = r.get(f"{META}/{module}/{entity}")
    if m.status_code != 200:
        return None
    return m.json()

def get(module, entity, id_):      return r.get(f"{API}/{module}/{entity}/{id_}")
def delete(module, entity, id_):   return r.delete(f"{API}/{module}/{entity}/{id_}")

def post(module, entity, payload):
    res = r.post(f"{API}/{module}/{entity}", json=payload)
    try:    j = res.json()
    except: j = {"_raw": res.text}
    return res.status_code, res.headers, j

def patch(module, entity, id_, payload, etag=None):
    headers = {}
    if etag: headers["If-Match"] = etag
    res = r.patch(f"{API}/{module}/{entity}/{id_}", json=payload, headers=headers)
    try:    j = res.json()
    except: j = {"_raw": res.text}
    return res.status_code, res.headers, j

def ensure_user(suffix):
    # читаем meta core/user и достаём первый допустимый enum для role
    m = r.get(f"{META}/core/user")
    role_val = None
    if m.status_code == 200:
        meta_u = m.json()
        # поле role
        for f in meta_u.get("fields", []):
            if f.get("name") == "role":
                # приоритет: enum (список строк), иначе values/options/items[…]
                enum_list = None
                if isinstance(f.get("enum"), list) and f["enum"]:
                    enum_list = f["enum"]
                elif isinstance(f.get("values"), list) and f["values"]:
                    enum_list = f["values"]
                elif isinstance(f.get("options"), list) and f["options"]:
                    enum_list = [x.get("value") or x.get("code") or x.get("name") for x in f["options"] if isinstance(x, dict)]
                elif isinstance(f.get("items"), list) and f["items"]:
                    enum_list = f["items"]
                if enum_list and isinstance(enum_list[0], str):
                    role_val = enum_list[0]
                break

    # всегда делаем уникальный email
    ts = str(int(time.time() * 1e6))  # микросекунды
    payload = {
        "name": f"U-{suffix}",
        "email": f"u-{suffix}-{ts}@x.x",
    }
    if role_val is not None:
        payload["role"] = role_val

    res = r.post(f"{API}/core/user", json=payload)
    try:
        j = res.json()
    except Exception:
        j = {"_raw": res.text}
    if "id" not in j:
        print("[POST /core/user] failed", res.status_code, j)
        print("[meta core/user role] =", {"role_meta": role_val, "raw_field": f if m.status_code==200 else None})
        sys.exit(1)
    return j["id"]




def ensure_company():
    sc, _, c = post("core", "company", {"name": f"ACME-{int(time.time())}"})
    if "id" not in c:
        print("[POST /core/company] failed", sc, c); sys.exit(1)
    return c["id"]

def main():
    meta = meta_entity("core", "project")
    ok(meta is not None, "meta for core/project available")
    fields = {f["name"]: f for f in meta.get("fields", [])}

    has_manager = "manager_id" in fields
    has_members = "member_ids" in fields
    has_company = "company_id" in fields

    # Выясняем обязательность ссылок
    req_manager = has_manager and fields["manager_id"].get("required") is True
    req_members = has_members and fields["member_ids"].get("required") is True
    req_company = has_company and fields["company_id"].get("required") is True

    # Готовим обязательные референсы для создания проекта
    payload = {"name": "OD-Test", "status": "Draft"}

    user_for_manager = None
    if req_manager:
        user_for_manager = ensure_user("mgr-req")
        payload["manager_id"] = user_for_manager

    members_list = None
    if req_members:
        u1 = ensure_user("mem1-req")
        u2 = ensure_user("mem2-req")
        members_list = [u1, u2]
        payload["member_ids"] = members_list

    company_id = None
    if req_company:
        company_id = ensure_company()
        payload["company_id"] = company_id

    # 1) Создаём проект с учётом required полей
    sc, headers, prj = post("core", "project", payload)
    if "id" not in prj:
        print("[POST /core/project] status=", sc, "body=", prj); sys.exit(1)
    prj_id = prj["id"]
    ok(True, "project created")

    # A) set_null — делаем только если поля присутствуют (не обязательно required)
    if has_manager or has_members:
        # если manager_id не был обязательным — создадим сейчас
        if has_manager and not req_manager:
            user_for_manager = ensure_user("mgr-opt")
        # если member_ids не были обязательными — создадим сейчас
        if has_members and not req_members:
            u1 = ensure_user("mem1-opt"); u2 = ensure_user("mem2-opt"); members_list = [u1, u2]

        patch_payload = {}
        if has_manager:
            patch_payload["manager_id"] = user_for_manager
        if has_members:
            patch_payload["member_ids"] = members_list

        if patch_payload:
            etag = get("core", "project", prj_id).headers.get("ETag")
            sc, _, resp = patch("core", "project", prj_id, patch_payload, etag=etag)
            if sc not in (200, 204):
                print("[PATCH /core/project] status=", sc, "body=", resp); sys.exit(1)
            ok(True, "project updated with refs for set_null test")

            # удаляем user_for_manager → должен сработать set_null и из массива исчезнуть
            if has_manager and user_for_manager:
                d = delete("core", "user", user_for_manager)
                ok(d.status_code == 204, "set_null: delete user returns 204")
                prj_now = get("core", "project", prj_id).json()
                ok(prj_now.get("manager_id") in (None, ""), "set_null: manager_id cleared")
                if has_members and members_list:
                    arr = prj_now.get("member_ids") or []
                    ok(user_for_manager not in arr, "set_null: removed deleted id from member_ids")
        else:
            skip("no manager_id/member_ids fields; skipping set_null")
    else:
        skip("no set_null-capable fields in Project")

    # B) restrict — проверяем, если есть company_id (обычно restrict)
    if has_company:
        # если company_id ещё не стоял — поставим
        if not req_company:
            company_id = ensure_company()
            etag = get("core", "project", prj_id).headers.get("ETag")
            sc, _, resp = patch("core", "project", prj_id, {"company_id": company_id}, etag=etag)
            if sc not in (200, 204):
                print("[PATCH project company_id] status=", sc, "body=", resp); sys.exit(1)

        d2 = delete("core", "company", company_id)
        ok(d2.status_code in (400, 409), "restrict: deletion blocked when referenced")
    else:
        skip("no company_id in Project; restrict case skipped")

    print("on_delete test passed ✅")

if __name__ == "__main__":
    main()

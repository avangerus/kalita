#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import sys, time, requests

BASE = "http://localhost:8080"
H = {"Content-Type": "application/json"}

def u(*p): return "/".join(s.strip("/") for s in p)

def must_ok(r, *codes):
    if r.status_code not in codes:
        print("HTTP", r.status_code, r.text)
        sys.exit(1)

def create_company(name):
    r = requests.post(u(BASE, "api", "olga", "company"), json={"name": name}, headers=H, timeout=30)
    must_ok(r, 200, 201)
    return r.json()["id"]

def create_project(name, company_id):
    payload = {"name": name, "company_id": company_id}
    r = requests.post(u(BASE, "api", "olga", "project"), json=payload, headers=H, timeout=30)
    must_ok(r, 200, 201)
    return r.json()

def list_projects(params=None):
    r = requests.get(u(BASE, "api", "olga", "project"), params=params or {}, timeout=30)
    must_ok(r, 200)
    total = int(r.headers.get("X-Total-Count", "-1"))
    return total, r.json()

def count_projects(params=None):
    r = requests.get(u(BASE, "api", "olga", "project", "_count"), params=params or {}, timeout=30)
    must_ok(r, 200)
    return r.json().get("total")

def expect_names(objs):
    return [o.get("name") for o in objs]

def main():
    # 1) Создаём компанию и 12 проектов: PAG-000..PAG-011
    comp = create_company(f"PAG-COMP-{int(time.time())}")
    names = [f"PAG-{i:03d}" for i in range(12)]
    for n in names:
        create_project(n, comp)
    print("[OK] created 12 projects")

    # 2) Список по возрастанию name, первые 5
    total, page = list_projects({"sort":"name", "limit":"5", "offset":"0"})
    got = expect_names(page)
    if got != names[:5]:
        print("Ascending sort/page mismatch:", got, "expected", names[:5]); sys.exit(1)
    if total != 12:
        print("X-Total-Count expected 12, got", total); sys.exit(1)
    print("[OK] sort=name asc + limit/offset works; X-Total-Count=", total)

    # 3) Отступ 5, ещё 5
    _, page = list_projects({"sort":"name", "limit":"5", "offset":"5"})
    got = expect_names(page)
    if got != names[5:10]:
        print("Page 2 mismatch:", got, "expected", names[5:10]); sys.exit(1)
    print("[OK] pagination offset works")

    # 4) Убывание name, первые 5 (ожидаем 011..007)
    total2, page = list_projects({"sort":"-name", "limit":"5", "offset":"0"})
    got = expect_names(page)
    expected_desc = list(reversed(names))[:5]
    if got != expected_desc:
        print("Descending sort mismatch:", got, "expected", expected_desc); sys.exit(1)
    if total2 != 12:
        print("X-Total-Count (desc) expected 12, got", total2); sys.exit(1)
    print("[OK] sort=-name desc works")

    # 5) Offset за пределами
    _, page = list_projects({"sort":"name", "limit":"5", "offset":"999"})
    if page != []:
        print("Expected empty page for big offset, got", page); sys.exit(1)
    print("[OK] big offset returns empty list")

    # 6) /_count без фильтров совпадает с total
    cnt = count_projects({})
    if cnt != 12:
        print("Count mismatch:", cnt, "expected 12"); sys.exit(1)
    print("[OK] _count matches 12")

    # 7) Небольшой sanity для q-фильтра (если поддерживается)
    # q=PAG должен вернуть все наши записи
    cnt_q = count_projects({"q":"PAG"})
    if cnt_q not in (12, None):
        print("q-count expected 12 or None (если q не поддержан в /_count), got", cnt_q); sys.exit(1)
    print("[OK] q filter (if supported) looks fine")

    print("\nPagination & sort test passed ✅")

if __name__ == "__main__":
    main()

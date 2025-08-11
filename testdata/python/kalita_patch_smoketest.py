#!/usr/bin/env python3
import os
import sys
import json
import requests

BASE = os.getenv("KALITA_BASE", "http://localhost:8080/api/test/item")
headers = {"Content-Type": "application/json"}

def print_err(prefix, r):
    try:
        body = r.json()
    except Exception:
        body = r.text
    print(f"{prefix} status={r.status_code} body={body}")
    return body

def must_ok(r, prefix):
    if 200 <= r.status_code < 300:
        return r
    print_err(prefix, r)
    sys.exit(1)

def get_list():
    r = requests.get(BASE + "?limit=1")
    return r

def create_item(payload):
    r = requests.post(BASE, json=payload, headers=headers)
    return r

def patch_item(id_, payload, etag=None):
    h = headers.copy()
    if etag is not None:
        h["If-Match"] = f'"{etag}"'
    r = requests.patch(f"{BASE}/{id_}", json=payload, headers=h)
    return r

def main():
    print("[i] BASE =", BASE)

    # 0) Быстрая проверка, что путь к сущности корректен
    r = get_list()
    if not (200 <= r.status_code < 300):
        print_err("[!] GET list failed — проверь путь BASE (модуль/сущность)", r)
        print("Подсказка: BASE должен быть вида http://host:port/api/<module>/<entity>")
        return

    # 1) Попробуем создать запись. ВАЖНО: адаптируйте payload под required-поля вашей схемы.
    # пример минимального payload-а (замените на ваши поля!)
    payload = json.loads(os.getenv("KALITA_CREATE_JSON", '{"name":"First","code":"A1"}'))
    r = create_item(payload)
    if not (200 <= r.status_code < 300):
        print_err("[!] Create failed — вероятно, не совпадает схема (required/type/enum) или сущности нет", r)
        print("Подсказка: задайте переменную окружения KALITA_CREATE_JSON с нужными полями.")
        return

    item = r.json()
    print("[OK] created:", item)
    etag_v1 = item.get("version")
    item_id = item.get("id")

    # 2) PATCH без If-Match
    r = patch_item(item_id, {"name": "FirstUpdated"})
    must_ok(r, "[!] Patch no If-Match failed")
    print("[OK] patched no If-Match:", r.json())

    # 3) Конфликт версии
    r = patch_item(item_id, {"name": "ShouldConflict"}, etag=etag_v1)
    print_err("[Conflict expected]", r)

    # 4) Readonly/system попытка
    r = patch_item(item_id, {"id": "zzz", "version": 999})
    print_err("[Readonly expected]", r)

    # 5) Уникальность: создаём вторую запись и ломаем уникальность
    payload2 = json.loads(os.getenv("KALITA_CREATE2_JSON", '{"name":"Second","code":"A2"}'))
    r = create_item(payload2)
    must_ok(r, "[!] Create second failed")
    item2 = r.json()

    r = patch_item(item2["id"], {"code": payload["code"]})
    print_err("[Unique violation expected]", r)

if __name__ == "__main__":
    main()

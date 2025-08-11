import requests
import random
import string

BASE = "http://localhost:8080/api/test/item"

def rnd_code():
    return "N" + "".join(random.choices(string.digits + string.ascii_uppercase, k=6))

def ok(cond, msg):
    print(f"🟩 {msg}" if cond else f"🟥 {msg}")

def main():
    # создаём 3 записи, где name = None кроме одной
    ids = []
    for name in [None, None, "zzz"]:
        code = rnd_code()
        r = requests.post(BASE, json={"code": code, "name": name})
        ok(r.status_code == 201, f"created {code} HTTP {r.status_code}")
        ids.append(code)

    # проверка nulls=last
    r_last = requests.get(f"{BASE}?_sort=name&nulls=last&_limit=100")
    ok(r_last.status_code == 200, f"list nulls=last HTTP {r_last.status_code}")
    if r_last.status_code == 200:
        names = [x.get("code") for x in r_last.json()]
        print("   nulls=last order:", names)

    # проверка nulls=first
    r_first = requests.get(f"{BASE}?_sort=name&nulls=first&_limit=100")
    ok(r_first.status_code == 200, f"list nulls=first HTTP {r_first.status_code}")
    if r_first.status_code == 200:
        names = [x.get("code") for x in r_first.json()]
        print("   nulls=first order:", names)

if __name__ == "__main__":
    main()

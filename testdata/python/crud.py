import requests, json, sys

BASE = "http://localhost:8080/api"
ENTITY = "user"

s = requests.Session()
TIMEOUT = 5

def show(resp):
    print(f"{resp.request.method} {resp.url} -> {resp.status_code}")
    ct = resp.headers.get("content-type","")
    if resp.content and "application/json" in ct:
        try:
            print(json.dumps(resp.json(), ensure_ascii=False, indent=2))
        except Exception as e:
            print("<non-json body>", resp.text[:200], f"... ({e})")
    else:
        if resp.content:
            print(resp.text[:200])
    print("-"*60)

# 1) Create
r = s.post(f"{BASE}/{ENTITY}", json={"name":"Иван","email":"ivan2@example.com","role":"Manager"}, timeout=TIMEOUT)
show(r)
rid = r.json()["id"]

# 2) Get by id
r = s.get(f"{BASE}/{ENTITY}/{rid}", timeout=TIMEOUT)
show(r)

# 3) Update
r = s.put(f"{BASE}/{ENTITY}/{rid}", json={"name":"Иван Петров","email":"ivan2@example.com","role":"Manager"}, timeout=TIMEOUT)
show(r)

# 4) List
r = s.get(f"{BASE}/{ENTITY}", timeout=TIMEOUT)
show(r)

# 5) Delete (204, без тела)
r = s.delete(f"{BASE}/{ENTITY}/{rid}", timeout=TIMEOUT)
show(r)

# 6) List (пусто)
r = s.get(f"{BASE}/{ENTITY}", timeout=TIMEOUT)
show(r)

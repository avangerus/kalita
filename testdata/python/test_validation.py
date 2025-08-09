import requests, json

BASE = "http://localhost:8080/api"

def show(r):
    print(r.status_code)
    try:
        print(json.dumps(r.json(), ensure_ascii=False, indent=2))
    except:
        print(r.text)
    print("-"*50)

# 1) создаём Users
u1 = requests.post(f"{BASE}/user", json={"name":"Alice","email":"alice@example.com","role":"Developer"})
u2 = requests.post(f"{BASE}/user", json={"name":"Bob","email":"bob@example.com","role":"Designer"})
show(u1); show(u2)

uid1 = u1.json()["id"]
uid2 = u2.json()["id"]

# 2) negative: ref на несуществующего
bad = requests.post(f"{BASE}/project", json={
    "name":"Proj X",
    "manager_id":"01ZZZZZZZZZZZZZZZZZZZZZZZZ",
    "member_ids":[uid1, uid2],
    "status":"Draft"
})
show(bad)  # ждём 400

# 3) ok
ok = requests.post(f"{BASE}/project", json={
    "name":"Proj OK",
    "manager_id":uid1,
    "member_ids":[uid1, uid2],
    "tags":["internal","urgent"],
    "budget":"12000.50",
    "start_date":"2025-08-10",
    "end_ts":"2025-08-10T12:00:00Z",
    "status":"InWork"
})
show(ok)   # ждём 201

# 4) фильтр по ссылке
lst = requests.get(f"{BASE}/project?manager_id={uid1}")
show(lst)

import requests
import json

BASE = "http://localhost:8080/api"
ENTITY = "user"

def pp(label, r):
    print(f"\n=== {label} ===")
    print(r.status_code)
    try:
        print(json.dumps(r.json(), ensure_ascii=False, indent=2))
    except:
        print(r.text)

# 1. Создаём несколько пользователей
users = [
    {"name": "Иван", "email": "ivan@example.com", "role": "Manager"},
    {"name": "Пётр", "email": "petr@example.com", "role": "Developer"},
    {"name": "Анна", "email": "anna@example.com", "role": "Manager"},
    {"name": "Олег", "email": "oleg@example.com", "role": "Designer"},
]

for u in users:
    r = requests.post(f"{BASE}/{ENTITY}", json=u)
    pp(f"Create {u['name']}", r)

# 2. Пагинация — первые 2
r = requests.get(f"{BASE}/{ENTITY}?limit=2&offset=0")
pp("Page 1 (limit=2)", r)

# 3. Пагинация — следующие 2
r = requests.get(f"{BASE}/{ENTITY}?limit=2&offset=2")
pp("Page 2 (limit=2)", r)

# 4. Сортировка по имени (возр)
r = requests.get(f"{BASE}/{ENTITY}?sort=name")
pp("Sort by name ASC", r)

# 5. Сортировка по имени (убыв)
r = requests.get(f"{BASE}/{ENTITY}?sort=-name")
pp("Sort by name DESC", r)

# 6. Фильтр по роли
r = requests.get(f"{BASE}/{ENTITY}?role=Manager")
pp("Filter role=Manager", r)

# 7. Поиск по подстроке q
r = requests.get(f"{BASE}/{ENTITY}?q=ol")
pp("Search q=ol", r)

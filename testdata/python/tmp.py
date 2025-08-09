import requests
import json

BASE_URL = "http://localhost:8080/api"

def pretty(resp):
    print(resp.status_code, json.dumps(resp.json(), ensure_ascii=False, indent=2))

# 1. Создаём User
print("\n=== Create User ===")
user_data = {
    "name": "Иван",
    "email": "ivan@example.com",
    "role": "Manager"
}
resp = requests.post(f"{BASE_URL}/user", json=user_data)
pretty(resp)

# 2. Получаем всех User
print("\n=== Get All Users ===")
resp = requests.get(f"{BASE_URL}/user")
pretty(resp)

# 3. Создаём User с ошибкой (нет email)
print("\n=== Create User (Bad) ===")
bad_data = {
    "name": "Петр",
    "role": "Manager"
}
resp = requests.post(f"{BASE_URL}/user", json=bad_data)
pretty(resp)

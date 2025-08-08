import requests

BASE = 'http://localhost:8080/api'

# 1. Создаём нового пользователя
user = {
    "name": "Иван",
    "email": "ivan@company.com",
    "role": "Manager"
}
r = requests.post(f"{BASE}/User", json=user)
print("Create User:", r.status_code, r.json())

# 2. Получаем список всех пользователей
r = requests.get(f"{BASE}/User")
print("All Users:", r.status_code, r.json())

# 3. Пробуем создать пользователя с ошибкой (нет email)
user_bad = {
    "name": "Петя"
}
r = requests.post(f"{BASE}/User", json=user_bad)
print("Create User (bad):", r.status_code, r.json())

# 4. Создаём проект
project = {
    "name": "Kalita MVP",
    "status": "Draft"
}
r = requests.post(f"{BASE}/Project", json=project)
print("Create Project:", r.status_code, r.json())

# 5. Получаем список проектов
r = requests.get(f"{BASE}/Project")
print("All Projects:", r.status_code, r.json())

# 6. Пробуем создать проект с недопустимым статусом
project_bad = {
    "name": "Kalita 2.0",
    "status": "Review"  # Такого значения нет в enum
}
r = requests.post(f"{BASE}/Project", json=project_bad)
print("Create Project (bad):", r.status_code, r.json())

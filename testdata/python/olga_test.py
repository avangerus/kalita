import requests

BASE = 'http://localhost:8080/api'

def pretty(r):
    try:
        print(r.status_code, r.json())
    except Exception as e:
        print(r.status_code, r.text)

print("\n=== Создаём Brief ===")
brief = {
    "number": 1001,
    "project_name": "Ольга-проект",
    "deadline": "2025-08-20",
    "client": "BigCorp",
    "brand": "BrandX",
    "period_from": "2025-08-01",
    "period_to": "2025-12-31",
    "no_tender": False,
    "comment": "Тестовый бриф",
    "companies": ["ООО Пример", "ЗАО Демо"],
    "profit_norm": [20.0, 15.0],
    "managers": ["Иванов Иван", "Петров Пётр"],
    "project_team": ["Менеджер 1", "Эксперт 2"]
}
r = requests.post(f"{BASE}/Brief", json=brief)
pretty(r)

print("\n=== Получаем все Brief ===")
r = requests.get(f"{BASE}/Brief")
pretty(r)

print("\n=== Создаём Project ===")
project = {
    "number": 12345,
    "name": "Проект Ольга",
    "client": "BigCorp",
    "brand": "BrandX",
    "period_from": "2025-08-01",
    "period_to": "2025-12-31",
    "brief_number": 1001,
    "no_tender": True,
    "companies": ["ООО Пример"],
    "profit_norm": [18.0],
    "project_managers": ["Иванов Иван"],
    "project_team": ["Эксперт 1", "Эксперт 2"],
    "status": "Draft"
}
r = requests.post(f"{BASE}/Project", json=project)
pretty(r)

print("\n=== Создаём Estimate (Смета) ===")
estimate = {
    "number": 50001,
    "name": "Смета #1",
    "project": "Проект Ольга",
    "period_from": "2025-08-01",
    "period_to": "2025-12-31",
    "client": "BigCorp",
    "company": "ООО Пример",
    "manager": "Иванов Иван",
    "currency": "RUB",
    "type": "Project",
    "status": "Draft",
    "comment": "Первая тестовая смета"
}
r = requests.post(f"{BASE}/Estimate", json=estimate)
pretty(r)

print("\n=== Создаём EstimateLine (Строка сметы) ===")
est_line = {
    "number": 1,
    "estimate": "Смета #1",
    "code": "001",
    "item": "Услуга тестовая",
    "qty": [{"unit": "час", "amount": 10}],
    "unit_cost": 500.0,
    "subtotal": 5000.0,
    "vat": 20.0,
    "pl_percent": 30.0,
    "gross_extra_net": 1500.0,
    "costs": 3500.0,
    "act_percent": 29.0,
    "rest": 500.0
}
r = requests.post(f"{BASE}/EstimateLine", json=est_line)
pretty(r)

print("\n=== Получаем все EstimateLine ===")
r = requests.get(f"{BASE}/EstimateLine")
pretty(r)

print("\n=== Создаём Expense (Расход) ===")
expense = {
    "number": 9001,
    "created_at": "2025-08-10",
    "creator": "Петров Пётр",
    "assignee": "Сидоров Сидор",
    "currency": "RUB",
    "payment_date": "2025-08-15",
    "status": "Draft",
    "in_subline_number": 1,
    "item": "Аванс за услуги",
    "qty": [{"unit": "шт", "amount": 1}]
}
r = requests.post(f"{BASE}/Expense", json=expense)
pretty(r)

print("\n=== Получаем все Expense ===")
r = requests.get(f"{BASE}/Expense")
pretty(r)

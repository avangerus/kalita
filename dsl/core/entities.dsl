# Пользователь
module core

entity User:
  name: string required
  email: string required unique
  role: enum[Manager, Admin, Developer, Designer] required

# Проект
entity Project:
  name: string required
  manager_id: ref[User] required
  member_ids: array[ref[User]]          # массив ссылок на User
  tags: array[string]                   # массив строк
  budget: float
  start_date: date
  end_ts: datetime
  status: enum[Draft, InWork, Closed] required
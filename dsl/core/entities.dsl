# Пользователь
entity User:
    name: string required
    email: string unique required
    role: enum[Admin, Manager, Employee] default=Employee

# Проект
entity Project:
    name: string required
    status: enum[Draft, InWork, Closed] default=Draft

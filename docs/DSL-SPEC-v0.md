# Kalita DSL — Specification v0 (MVP)

Статус: проект. Область: только то, что входит в 8-недельный MVP. Ключевые слова — английские (решение HLD п.10). Файлы: `*.kal`, кодировка UTF-8. Пак = каталог с `pack.kal` (манифест) + файлы модулей.

## 0. Философия грамматики

1. **Узость — источник гарантий.** Каждая конструкция либо полностью проверяема компилятором, либо не входит в грамматику.
2. **Один способ выразить одну вещь.** Синонимов и сахара нет — меньше поверхность дрифта при генерации агентами.
3. **Ошибки пишутся для агентов:** каждая ошибка компиляции = `{code, file:line, message, fix_hint}` — машиночитаемо, с подсказкой исправления (цикл самокоррекции агента).
4. Всё имена — `snake_case` для полей, `PascalCase` для сущностей/состояний/ролей.

## 1. Манифест пака

```
pack collections
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1        # базовый пак: User, attachments, notifications
```

## 2. Типы данных (закрытый список v0)

| Тип | Примечание |
|---|---|
| `string` | до 1 КБ, индексируемо |
| `text` | без лимита, полнотекст |
| `int`, `float`, `money`, `bool` | money хранит валюту |
| `date`, `datetime` | TZ-aware |
| `enum[A, B, C]` | закрытый список значений |
| `ref[Entity]` | + `on_delete = restrict \| set_null \| cascade` |
| `array[ref[Entity]]` | только ссылки в v0 |
| `file` | вложение (хранится ядром) |

Не в v0: `json`, geo, decimal-точность настраиваемая, вложенные структуры.

## 3. Сущности

```
entity Debtor:
    company: string required
    contract: ref[Contract] on_delete=restrict
    debt: money
    overdue_days: int computed = days_since(contract.due_date)
    status: enum[OnTime, Overdue, Claim, Legal, Settled] default=OnTime
    manager: ref[core.User] default=$me

constraints:
    unique(company, contract)
```

Модификаторы поля: `required`, `default=<expr>`, `unique`, `computed=<expr>` (read-only, пересчитывается ядром). Каждая сущность автоматически получает: `id`, `created_at/by`, `updated_at/by`, полную историю изменений (event store) — объявлять не нужно.

## 4. Workflow

```
workflow Debtor on status:
    OnTime  -> Overdue: auto when overdue_days > 0
    Overdue -> Claim:   send_claim assignee=agent(Collector)
    Claim   -> Legal:   escalate requires approval(FinDirector)
    any     -> Settled: auto when debt = 0
```

Семантика перехода: `Источник -> Цель: имя_действия [auto] [when <guard>] [assignee=<role|agent(Role)|auto>] [requires approval(<Role>)]`.

- `auto` — выполняется ядром при истинности guard;
- `assignee` — кому создаётся задача на выполнение перехода;
- `requires approval(Role)` — HITL: переход создаёт запись в очереди подписей; **без подписи перехода не существует**. Подпись пишется в журнал криптографически.
- Компилятор проверяет: достижимость всех состояний, отсутствие переходов из/в несуществующие состояния, конфликтов auto-guards.

## 5. Роли и права

```
roles:
    Accountant
    FinDirector
    Collector agent          # маркер agent: роль исполняется агентом

permissions:
    Collector:
        read  [Debtor, Contract]
        act   [send_claim]
        deny  [update Debtor.debt, delete *, read Contract where classified = true]
    Accountant:
        full  [Debtor]
        read  [Contract]
    FinDirector:
        approve [escalate]
        read    all
```

- Действия: `read | create | update | delete | act [имена_переходов] | approve [имена] | full`.
- Row-level: `where <expr>` (например `read Debtor where manager = $me`).
- Field-level: `deny update Debtor.debt`.
- **Deny сильнее allow. Для агентских ролей deny-блок обязателен** (компилятор требует явных запретов — агент без явных границ не компилируется).
- Субъект без правила = нет доступа (default deny).

## 6. Автоматизация

```
automation:
    on schedule daily at 09:00 for Debtor when status = Overdue and overdue_days in [3, 7, 14]:
        agent Collector: draft_reminder(tone = soft if overdue_days < 7 else firm)
        notify email(manager)

    on update Debtor when status changed to Legal:
        webhook out "https://legal.example.com/intake"

    on stuck Debtor in Claim for 10d:
        escalate_to FinDirector
```

Триггеры v0: `on schedule <расписание> [for <Entity>] [when ...]` (с `for` — правило прогоняется по записям сущности, без — глобальное действие), `on create|update|delete <Entity> [when ...]`, `on stuck <Entity> in <State> for <duration>`.
Действия v0 (закрытый список): `agent <Role>: <task>(args)` (создание задачи агенту), `notify email(...)`, `webhook out <url>` (только исходящие, объявленные = видимые в спеке), `create/update <Entity> set {...}`, `escalate_to <Role>`.

## 7. Выражения (намеренно бедные)

- Литералы, пути полей (`contract.due_date` — одна ступень ref), `$me`, `now()`, `today()`, `days_since(x)`;
- Сравнения, `in [...]`, `and/or/not`, инлайн `<a> if <cond> else <b>`;
- Закрытый список функций. **Нет:** циклов, агрегатов (v0), пользовательских функций, вызовов наружу из выражений.

## 8. UI

```
ui Debtor:
    list: [company, debt, overdue_days, status] sort=-overdue_days
        filters: [status, manager]
        view "My debtors": where manager = $me
    form:
        section "General": [company, contract, debt]
        section "Status":  [status, overdue_days]
    board: by status
```

Встроено без объявления: очередь подписей (approval inbox), журнал объекта, глобальный поиск. Не в v0: дашборды/графики, кастомные виджеты, темы.

## 9. Миграции (правила v0 — только аддитивные)

Разрешено диффом: новая сущность; новое поле (nullable или с default); новое значение enum (в конец); новое состояние/переход; новая роль/право; новая автоматизация; любые изменения ui.
Запрещено в v0 (только ручная процедура с экспортом): смена типа поля, удаление/переименование поля и сущности, удаление значения enum и состояния.
Каждый принятый дифф = новая версия SystemDefinition; применение атомарно; откат = обратный proposal через ту же очередь подписей.

## 10. Зарезервированные слова (не реализуются, но запрещены как идентификаторы)

`component` (escape hatch/WASM), `integration`, `simulate`, `dashboard`, `i18n`, `metric`, `system` (системные паки), `federation`/`remote` (межузловые ссылки) — чтобы паки v0 не конфликтовали с v1.

## 10a. Системные паки (наследие kalita-2024, план v1)

Ядро платформы максимально описывается на самой kalita: `core` — пак с
`core.User`, справочниками (master data), вложениями. Отличие от обычных
паков — маркер `system`: содержимое меняется только обновлением платформы,
обычный change pipeline его отклоняет (защита, а не приватность). Принцип:
всё, что МОЖЕТ быть паком, ОБЯЗАНО быть паком; в ядре остаётся только то, что
паком быть не может (журнал, права, компилятор, рантайм).

## 10b. Федерация узлов (план, слой 3)

Один узел = один контур, но узлы стыкуются: пак может объявлять внешнюю
ссылку на сущность другого инстанса (две доменные области — два узла — один
протокол). Зарезервировано: `remote[node.Entity]` как тип ссылки; обмен —
типизированные контракты через MCP-мост с identity обоих узлов и журналом с
обеих сторон. Не реализуется до слоя 3.

## 11. Полный мини-пример

См. `examples/collections/` (пак дебиторки из этого документа целиком) и `examples/dev_department/` (отдел разработки: Task, ADR, Defect, gates) — оба обязаны компилироваться к концу недели 4; это приёмочные тесты грамматики.

## 12. Чего сознательно нет в v0 — и почему

| Нет | Почему | Когда |
|---|---|---|
| Агрегаты/отчёты | тяжёлая семантика, не блокирует первые паки | v1 |
| Escape hatch (component) | контракт WASM требует отдельного дизайна | v1 |
| Входящие интеграции | безопасность контура; исходящие webhook достаточно | v1 |
| Вычисления между сущностями глубже 1 ref | взрывает сложность компилятора | v1 |
| Разрушающие миграции | риск №1 проекта (HLD п.9) | v2 |
```

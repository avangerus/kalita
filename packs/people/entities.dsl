# packs/people — учебный референс-пак. Не продуктовая витрина, а нарочито
# маленький пример, который в ОДНОМ месте показывает три уровня поведения
# kalita и попутно догфудит примитивы event / emit / on-event на свежем паке:
#
#   УРОВЕНЬ 1 (действие/переход) — workflows.dsl: Employee.status ходит по
#   workflow Active <-> Inactive; деактивация требует подписи HRAdmin (HITL).
#
#   УРОВЕНЬ 2 (событие) — event OutOfOffice ниже + automation в workflows.dsl:
#   факт "сотрудник в отпуске" НЕ является сменой Employee.status (это другое
#   измерение, не workflow-поле), поэтому выпускается не через `emits` на
#   переходе, а извне через MCP `emit_event` — см. security.dsl (permission
#   verb `emit`) и отчёт агента для точной цитаты синтаксиса. Автоматизация
#   на это событие реагирует: уведомляет менеджера и заводит Coverage-запись.
#
#   УРОВЕНЬ 3 (свой код в воркере) — workers/coverage_bot/bot.py: реагирует на
#   задачу, которую заводит automation (on create Coverage: agent CoverageBot:
#   ...), читает записи через MCP и пишет содержательный comment — код,
#   которого нет в декларативном automation-блоке.

entity Employee "Employee":
    include core.Named
    email:   email label="Email"
    status:  enum[Active, Inactive] default=Active label="Status"
    # самоссылка: руководитель — тоже Employee. on_delete=set_null — увольнение
    # руководителя не должно каскадно валить подчинённых записей.
    manager: ref[Employee] on_delete=set_null label="Manager"

# УРОВЕНЬ 2 — декларация события. Payload — застывший факт на момент выпуска:
# кто ушёл в отпуск и на какой период. Заметьте: намеренно НЕТ поля manager —
# это подчёркивает, что подписчик (on event) обязан сам сделать one-hop
# ref-path (employee.manager), а не получать его бесплатно в payload.
event OutOfOffice "Employee out of office":
    employee: ref[Employee]
    from:     date
    to:       date

# Coverage — рабочий продукт УРОВНЯ 2 (что заводит автоматизация) и контекст
# для УРОВНЯ 3 (что дальше берёт в работу CoverageBot). Отдельная сущность, а
# не поле на Employee — сотрудник может уходить в отпуск много раз, у каждого
# раза свой covering-менеджер и период.
entity Coverage "Coverage":
    employee: ref[Employee] on_delete=cascade label="Employee"
    manager:  ref[Employee] on_delete=set_null label="Manager"
    from:     date label="From"
    to:       date label="To"

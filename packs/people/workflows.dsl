# УРОВЕНЬ 1 — действие/переход. Деактивация сотрудника — HITL: перевод в
# Inactive паркуется в очереди подписи HRAdmin и не происходит без неё
# (requires approval, Ed25519, офлайн-проверяемо — DSL-SPEC-v0 §4). Обратный
# переход (activate) HITL не требует — включить обратно может тот же HRAdmin
# самостоятельным действием.
workflow Employee on status:
    Active   -> Inactive: deactivate requires approval(HRAdmin) label="Deactivate"
    Inactive -> Active:   activate label="Activate"

# УРОВЕНЬ 2 — реакция на событие, в два шага, тем же приёмом, что и в
# packs/crm/crm.dsl (on event LeadConverted: create Opportunity ...; on create
# Opportunity: agent SalesBot: ...):
#
#   1) on event OutOfOffice: реагирует на факт, выпущенный извне через MCP
#      emit_event (см. security.dsl: permission verb `emit [OutOfOffice]` у
#      роли Employee — сотрудник сам объявляет об отпуске). Уведомляет
#      руководителя (notify email(...) — как и в приведённом в
#      internal/dsl/parser_blocks.go грамматика для automation, аргумент
#      notify не резолвится движком в реальный e-mail, это декларативная
#      задача-уведомление в журнале) и заводит запись Coverage. Обе части
#      читают payload события напрямую (employee, from, to) и один ref-hop
#      от него (employee.manager) — контекст события, а не record.
#   2) on create Coverage: сама созданная запись Coverage — это уже
#      record-триггер (entity=Coverage, record_id=<новая запись>), поэтому
#      задача агента ниже получает РЕАЛЬНЫЙ record_id, который CoverageBot
#      может прочитать через get_record. Событие emit_event само по себе не
#      несёт record_id (оно вне транзакции над записью) — этот второй шаг и
#      есть мост к Уровню 3, тем же способом, каким это уже сделано в
#      packs/crm/crm.dsl.
automation:
    on event OutOfOffice:
        notify email(employee.manager)
        create Coverage(employee = employee, manager = employee.manager, from = from, to = to)
    on create Coverage:
        agent CoverageBot: arrange_coverage()

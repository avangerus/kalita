# Роли и права. HRAdmin управляет карточками сотрудников и подписывает
# деактивацию (Уровень 1). Employee — рядовой сотрудник: читает справочник и
# объявляет о своём отсутствии (Уровень 2, emit OutOfOffice). CoverageBot —
# агентская роль минимального доступа (Уровень 3): читает ровно то, что нужно
# её коду в bot.py, и не может НИЧЕГО из того, что относится к Уровню 1
# (в частности, не может деактивировать сотрудника).

roles:
    HRAdmin
    Employee
    # агентская роль — маркер `agent`; per DSL-SPEC-v0 §5 deny-блок для неё
    # обязателен (компилятор отклонит агентскую роль без явных границ).
    CoverageBot agent

permissions:
    # HRAdmin ведёт карточки, подписывает деактивацию и видит Coverage-записи
    # (кто кого прикрывает, пока сотрудник в отпуске).
    HRAdmin:
        full    [Employee, Coverage]
        act     [deactivate, activate]
        approve [deactivate]
    # рядовой сотрудник: читает справочник коллег, объявляет о своём отпуске
    # через emit_event — и НИЧЕГО больше. Деактивировать себя или кого-либо
    # employee не может ни прямо (нет act), ни в обход (deny update Employee.*).
    Employee:
        read [Employee]
        emit [OutOfOffice]
        deny [delete *, act [deactivate, activate], update Employee.*]
    # CoverageBot — свой код в воркере (workers/coverage_bot/bot.py). Задачу
    # получает через task-пул (Kind=agent, роль CoverageBot — не через
    # `permissions:`, это отдельный механизм), читает Employee/Coverage,
    # чтобы узнать имя менеджера, и оставляет comment на Coverage (комментарий
    # требует только `read` на хост-запись — engine/comments.go). Явный
    # deny-блок, как у BridgeReader/BridgeEmitter в packs/site/site.dsl —
    # ровно то, что нужно, и явно НЕ deactivate/activate Employee.
    CoverageBot:
        read [Employee, Coverage]
        deny [delete *, create *, update *, act [deactivate, activate], emit *]

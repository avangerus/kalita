# Адаптивный агент: ResponsibilityContract и durable intent loop

**Статус:** проект N1, не реализовано. Связи: [WORK_MODEL](WORK_MODEL.md), [EXECUTION](EXECUTION.md), [AUTHORITY_SECURITY](AUTHORITY_SECURITY.md), [EXPERIENCE](EXPERIENCE.md), [API](API_CONTRACTS.md). Этот протокол дополняет workflow шаги: надёжное выполнение статического workflow само по себе не доказывает автономную работу в выданной области.

## 1. Контракт ответственности и граница изменения плана

`ResponsibilityContract` содержит `contract_id/revision`, company, goal_ref/revision, scope кейсов/ресурсов, responsible_party, executor profile, критерии результата, ограничения метода, permitted operations, время/deadline, budget root/ceilings, approved delegation edges, required review points, stop conditions и basis. Принятие/передача ответственности — отдельное доменное решение. Grant реализует действующие полномочия этого контракта; contract сам не credential.

В простом case контракт — минимальный versioned блок внутри кейса. Агент может менять очередность разрешённых шагов, выбирать опубликованный применимый метод, создавать bounded children и запрашивать уточнение. Не может изменять принятую цель, критерии, grant, бюджет или обязанность человека собственным plan revision. Изменение этих условий требует amendment уполномоченного owner и, где нужно, пересогласования сторон.

`ExecutionPlanRevision` хранит contract_ref, parent plan revision, graph шагов/связей, method refs, input basis и причину изменения. Graph допускает только typed step kinds: model decision, context query, domain command, connector effect, child delegation, wait, review, result submission. Нет arbitrary code/string function. Validator проверяет budgets, node count/depth, циклы/iteration bounds, допустимые edges, visibility результатов и совместимость версий. План всегда ограничен, даже если изменяем.

## 2. Durable протокол одного решения

1. Scheduler выбирает следующий ready step по текущей PlanRevision. Claim выдаёт новую Attempt с fence и ограниченным lease.
2. Context broker проверяет активную grant lineage, выбирает минимум разрешённых refs/fields и записывает observation refs, source versions/freshness/redactions. Snapshot decisions не означают права на дальнейшее чтение без recheck.
3. Model broker атомарно резервирует bounded call cost, пишет `ModelCallIntent` с logical call ID, input digest, model/pricing/method revision, max input/output/duration и effect-like state. Сырые secrets/текст не помещаются в append audit; закрытый payload имеет отдельную retention/ACL.
4. Provider возвращает ответ; broker сохраняет receipt/usage и typed `DecisionProposal`, связанный с exact call/input. Proposal допустимых видов: `request_context`, `propose_plan_patch`, `request_tool`, `request_child`, `ask_human`, `submit_result`, `stop`. Модель не пишет domain facts/effect success.
5. Validator проверяет schema/размеры, версии, актуальный contract/grant/fence, limits, required review и basis. Зафиксированное принятие proposal и checkpoint следующего состояния записываются атомарно. Невалидное предложение не исполняется и расходует допустимый decision/retry budget.
6. Tool/domain/effect исполняется через broker по отдельному разрешённому intent. Observation/receipt становится входом следующего решения. Result submission создаёт Claim/Evidence refs, затем уполномоченная сторона выполняет Acceptance.

Step checkpoint содержит call/intent IDs, input/decision digest, текущую plan revision, выполненные command IDs, receipts, pending review/wait и termination counters. Progress текст executor не является checkpoint. После рестарта scheduler читает ledger; уже принятый decision не генерируется повторно как новый. Подтверждённый command/receipt не исполняется заново.

## 3. Неопределённые вызовы и пределы

Model call — платный внешний эффект, даже если не меняет бизнес-объект. Потерянный ответ не гарантирует отсутствие счёта. `unknown` удерживает worst-case reserve; retry может начать новый call только при свободном дополнительном бюджете и явной policy, не освобождая неизвестный предыдущий. Provider idempotency используется только при подтверждённом контракте. Каждый logical call имеет отдельный idempotency/settlement key, чтобы повторный usage receipt не списывал дважды.

Hard-cost mode допускает лишь provider с enforceable bound. Ограничения: max input bytes/tokens, max output tokens, number of tool/model requests, wall-clock/deadline, total calls/attempts, maximum plan nodes/child depth, concurrency и общий root cost. `Stop` при исчерпании — наблюдаемый исход `blocked`/`needs_review`, а не автоматический новый бюджет. Сбой provider caps/actual>bound — incident; данная pricing/adapter revision блокируется до расследования.

Один loop не продолжается бесконечно: лимит decision rounds, повтор одинакового proposal digest без нового observation и отсутствие прогресса переводят в review. Политика human wait указывает owner и срок эскалации. Ожидание не занимает worker lease. Отмена блокирует следующие decisions/dispatch, unknown эффекты продолжают независимую reconciliation.

## 4. Делегирование и изменение цели

Child grant хранит `parent_grant_id`, `root_authority_id` и scope/expiry. Эффективное право — пересечение всех активных звеньев lineage с текущей company policy, не snapshot ACL на момент рождения. Revoke/expiry родителя блокирует потомков до следующего context/tool/model/dispatch, независимо от задержки cache invalidation. Если company issuer выдаёт специалисту самостоятельное право вне lineage, создаётся **новый authority root** с явным decision и отдельными доступом/отзывом; нельзя маскировать обход отзыва как child grant.

Child work возвращает разрешённый typed summary; родитель не наследует PII/закрытые evidence. Дополнительные полномочия запрашиваются через issuer edge; отказ сохраняет `blocked` и reason. Все расходы children/retry — в общем budget root, если company decision явно не создал другой независимый contract; перенос бюджета не происходит автоматически.

Goal amendment создаёт новую contract revision, определяет поведение активных шагов и affected obligations. Без safe rebase текущие runs прекращают будущие действия; новые runs связаны с прежними. Нельзя переписать историю того, что было обещано. Handoff фиксирует offer/accept новым responsible_party, момент владения, остаток grants/лимитов и pending unknown effects; пока handoff не принят, система показывает текущего ответственного.

## 5. Наблюдаемость без раскрытия лишних данных

Человек видит цель, текущего владельца, план в доступном объёме, последние основания, состояние ожидания, расходы/резервы, следующий consequential action и способ остановки. Chain-of-thought не требуется и не хранится как audit proof. Короткое объяснение — Claim модели; проверяемыми остаются refs, решения broker и receipts.

Опубликованный MethodRevision определяет step schemas/selection rules и ограничения, но не выдаёт grant. Model/provider routing выбирается по разрешённой policy, sensitivity, verified method compatibility и cost bound. Дешёвая модель может исполнять low-risk структурирование; более сильная — консультировать/критиковать сложное решение. Это конфигурация метода, а не hard-coded название vendor model в domain kernel.

## 6. Проверки и этапность

R1 реализует bounded harness и checkpoint для фиксированного малого набора step kinds. R2 использует его для read/extract/confirm value probe. R4 проверяет adaptive plan patch, method change, specialist delegation, goal amendment/handoff/stop на одной области; не универсальное autonomous company управление.

Негативные сценарии: model предлагает неизвестный tool/саморасширение grant; injection из source меняет goal; повторный proposal без новых данных; provider timeout после billable acceptance; crash после tool commit до next decision; revoke родителя перед child read; child PII не видна parent; старый fence отправляет plan patch; goal change во время pending effect; handoff отклонён. Для каждого ожидаются typed отказ/blocked, сохранённая история, непревышенный hard bound и отсутствие blind external repeat.

Связанные tasks: KAL-009/KAL-015/KAL-019/KAL-020 (основа), KAL-046–KAL-050 (проверяемые методы и область ответственности). Конкретные names/routes новых agent commands уточняются contract revision в этих tasks; до этого они не существующие endpoints.

## 7. Неизменяемые начатые шаги и outstanding obligations

Plan patch может менять только ещё не начатые steps. Для leased/running/dispatching/unknown/completed step нельзя изменить logical identity, args/digest или заменить его новым ID как ту же операцию. Новый план ссылается на прежний step/effect; остановка оформляется cancel, затем receipt/reconcile/компенсация при необходимости. Дедуп logical operation key учитывает уже начатую работу независимо от текущей PlanRevision.

Все outstanding effects, approvals и reservations принадлежат Run/ResponsibilityContract ledger, а не текущему graph. Удаление узла из отображаемого плана их не закрывает и не освобождает reserve. Terminal success проверяет полный ledger: неизвестный существенный business effect блокирует success и окончательное принятие затронутого результата до reconciliation. Pure billing unknown может оставить business result отдельно submitted/accepted лишь если контракт допускает, но Run остаётся `blocked` с reason=`settlement_pending` и финансовый reserve не освобождается. Transfer/handoff переносит видимость outstanding работы в рамках ACL, не стирает её.

Контрпример для KAL-049/050: effect dispatched, receipt потерян; модель предлагает удалить узел, создать аналогичный с новым ID и submit_result. Validator отвергает patch/duplicate logical effect; прежний unknown/reserve остаётся и terminal success недоступен.

# Проверенное исходное состояние

**Срез:** 2026-10-07. HEAD `67f3587fbcebeb552be9f3debcb30efd7e494608`, branch `main`; изучен checkout с существующими локальными изменениями. Это code inspection, а не новый clean-room релиз или заявление о production readiness. [Цель N1](HLD.md) описана отдельно.

## Что есть и что предстоит

| Область | Наблюдаемая реализация | Разрыв до N1 |
|---|---|---|
| Запуск | [cmd/kalita/main.go](../../cmd/kalita/main.go): Go узел, REST/MCP, веб-интерфейс | Не готовый installer полного PG/executor/broker/backup lifecycle |
| DSL | [parser.go](../../internal/dsl/parser.go), [compile.go](../../internal/dsl/compile.go), [ast.go](../../internal/dsl/ast.go): entities, workflows, permissions, metadata | Canonical contract boundary и новые work/agent/view types ещё не закреплены |
| Journal | [pgstore.go](../../internal/eventstore/pgstore.go): append Tx, advisory lock, hash chain; [memstore.go](../../internal/eventstore/memstore.go) — dev | Новый composable Tx API с operational ledger не существует от факта наличия Append |
| Domain engine | [internal/engine](../../internal/engine): business projection из journal, named commands/guards | Operational SQL authority нельзя заменить RAM task state |
| Права | [permissions.go](../../internal/engine/permissions.go), API/MCP используют engine | Не все context/model/transitive effect/read audit paths ограничены run-scoped grants |
| Tasks | [tasks.go](../../internal/engine/tasks.go): take/report/complete/fail, leases, роли | Нет полноценного Attempt protocol с renewal/fence/cancel history и общей reservation семантикой |
| Runner | [runner.py](../../workers/agent_runner/runner.py): single completion/handlers, task polling | Не durable bounded intent loop; результат handler не доказывает committed completion |
| Egress | [egress_delivery.go](../../internal/engine/egress_delivery.go), [webhooks.go](../../cmd/kalita/webhooks.go): DeliveryID, retry/DLQ и receipts в текущем checkout | Не считать долговечность полностью отсутствующей; также не переносить частную гарантию webhook на произвольные tools/model calls |
| UI | [web/app.js](../../web/app.js), metadata DSL, каталог компонентов | Нет полного lifecycle генеративного ViewSpec, audience/pin/snapshot и trusted effect confirmation N1 |
| Интеграции | [workers/connectors](../../workers/connectors): inbound/excel/exact-match scaffolding | Нет доказанного готового mail/CRM/ERP/1C adapter suite для выбранного pack |
| Packs | [servicedesk](../../packs/servicedesk), [ap](../../packs/ap), [finance](../../packs/finance), [eshop](../../packs/eshop) и другие | ITSM/AP/финансовые и demo packs — полезные образцы; не готовый Client Delivery / wholesale продукт |

Существующая optional human signature enforcement не означает подпись каждого действия по умолчанию. SHA256 chain подтверждает связность сохранённой истории при заданной trust/anchor модели; не доказывает истинность внешнего факта, отсутствие всего непопавшего в журнал и защиту от администратора без отдельного anchoring.

## Конкретные проблемы для implementation backlog

1. `finishTask` проверяет `TakenBy == actor.ID`, но не новый token/fence конкретной Attempt. Старый ответ того же actor после перевыдачи нельзя отделить от нового. Нужен новый write protocol и drain старых workers — KAL-011/KAL-013/KAL-015.
2. `expireIfDue` игнорирует ошибку journal Append и открывает RAM task. Persistent truth и память могут разойтись. Нельзя исправить только отчёт: state transition должен зависеть от committed Tx — KAL-010/KAL-012.
3. `countActorEvents` считает исторические изменения actor, не действия конкретной текущей Attempt. Не использовать как evidence completion/authority — KAL-015/KAL-021.
4. `run_once` игнорирует ошибки report/complete/fail и печатает `done` по локальному handler result. Подтверждение executor должно отражать серверный исход; durable harness восстанавливается по checkpoint/receipts — KAL-019/KAL-020.
5. Durable webhook delivery в локальных изменениях уже есть. Её нужно исследовать и адаптировать к Effect/authority contract, а не писать вторую несвязанную delivery систему — KAL-001/KAL-043.

## Границы сохранения

Сохраняем существующие journal события, идентификаторы записей, опубликованные contract formats и читаемую историю. Миграция task writers не может обещать старый completion shape и новый fencing одновременно. Новые таблицы/события additive либо имеют явный migration plan. Любой cutover — один writer, reconciliation и операторский fallback.

Checkout содержит пользовательские tracked/untracked изменения, в том числе egress/webhook и builder. Этот документ не утверждает, что они committed или входят в tag release. При работе над N1 нужно заново снять diff/inventory; не откатывать их автоматически. Исторические forge отчёты читаются с датой: они могут не учитывать нынешнюю реализацию доставки.

## Проверки в этом задании

Для архитектурного набора проверяется согласованность документов, ссылок, task graph и release coverage. Runtime не изменён, новые API/гарантии не проверены исполнением. Ранее пройденные тесты не являются acceptance будущего N1. Перед implementation I00 заново фиксирует baseline tests и failure reproductions на сохраняемом checkout/изолированной рабочей копии.

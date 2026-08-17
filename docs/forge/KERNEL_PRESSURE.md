# KERNEL PRESSURE — когда pack требует изменения kernel

> Milestone C миссии. Каждый раз, когда pack не может выразить потребность без
> изменения ядра, записывается строка. Правило: одна и та же primitive, нужная
> НЕСКОЛЬКИМ независимым доменам → кандидат в core; только одному → pack/framework.

| # | Capability | Домен(ы) | Почему pack не может выразить | Универсальный enterprise construct? | Решение |
|---|---|---|---|---|---|
| 1 | Кросс-пак ref (`ref[contracts.Contract]`) | ap (счёт↔договор) | semantic.go допускает только core.*/mdg/свои сущности (H2 Cycle-001) | ДА — связи между доменами | G13 (gate владельца); workaround: contract_no + сверка агентом |
| 2 | Доставка webhook-эффектов | ap (send_to_payment), др. | automation `webhook out` журналирует намерение, раннера ноды нет (D-011) | ДА — egress любой интеграции | G2 (gate владельца); шов = событие, runner = инфраструктура доставки |
| 3 | Орг-иерархия/лестницы утверждений first-class | ap, finance, contracts | H1: лестница выразима guarded-переходами; Department-резолва нет | ДА (в перспективе) | Канонический DSL-приём зафиксирован; first-class — Later по спросу |

_Правило записи: строка появляется только после реальной попытки выразить в DSL
(а не «наверное не сможем»)._

## Milestone C evidence (exit criterion: 3 существенно разных системы без Go-кода)

Доказано dogfood-тестами (internal/mcp/*_dogfood_test.go, все зелёные, kernel не
менялся): **AP** (входящие счета + лестница + agent-check, ap_dogfood_test.go),
**Finance** (collections/долги, finance_dogfood_test.go), **Service Desk**
(incident/SLA/CAB, servicedesk_dogfood_test.go), **CRM** (crm_dogfood_test.go),
дополнительно HR/Eshop/Contracts/People/DevTrack/Dashboard. Лестница утверждений —
чистые guarded-переходы (H1), Department/first-class не понадобился.

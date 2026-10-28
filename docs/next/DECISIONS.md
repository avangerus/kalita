# Решения и замещение прежних правил — N1

**Дата:** 2026-10-07. Эти N-ADR принимают направление для планирования, не подтверждают реализацию. Изменять решение можно новым revision с причиной, последствиями и обновлением зависимых контрактов. Контекст: [HLD](HLD.md), [AS_IS](AS_IS.md), [первичная критика](REVIEW.md).

## N-ADR-01 — развивать Kalita с заменой слабых подсистем

**Статус:** accepted for planning. Варианты: полный rewrite; надстройка чата над существующим runtime; развитие контрактного ядра с ограниченными заменами. Выбран третий. Сохраняем типизированный domain engine, ABAC, журнал, transport parity; не обещаем сохранение task pool/runner/UI архитектуры без изменений. Разделяем compatibility для данных/API и возможность изменить внутреннюю реализацию.

**Почему:** полезная база уже есть, а главные недоказанные свойства лежат в execution, broker, продуктовых контрактах и эксплуатации. Rewrite не снимает эти проблемы. **Проверка:** I00 inventory и fault prototype. **Замещает:** запрет пересмотра ядра в старом [HLD-V1](../HLD-V1.md); прежний принцип не может блокировать необходимую смену ownership или протокола.

## N-ADR-02 — контракт работы первичен относительно чата и экрана

**Статус:** accepted for planning. WorkCase, цель, ответственность, обязательства, основания и принятие результата устойчивы. ResponsibilityContract задаёт допустимую область самостоятельной работы; execution graph — версия плана выполнения, а не контракт обещанного результата. Для простого кейса типы могут быть объединены в минимальное представление.

**Альтернативы:** переписка как единственное состояние; жёсткий workflow на каждый возможный ход; универсальная онтология. Они либо теряют ответственность, либо делают продукт непригодным для меняющейся работы. **Проверка:** handoff и goal-change experiment R4. **Замещает:** буквальное `agent=сотрудник` в старом HLD: actor, credential, AgentProfile и responsible human теперь явно различаются; метафора сохраняется лишь в объяснении продукта.

## N-ADR-03 — один процесс и доказанный покупатель до платформенного расширения

**Статус:** provisional market hypothesis. Default — Client Delivery для B2B услуг. Альтернативы — Document Close и Order Exceptions. I00 выбирает по реальному owner, доступу к двум источникам, измеримому дорогому повторяющемуся процессу, стоимости установки и штатной альтернативе. Sector ranking без этого не считается validation.

**Последствие:** pack — единый поддерживаемый процесс с эксплуатацией, а не набор DSL файлов. Отсутствие первого покупателя допускает синтетический technical spike, но не коммерческое подтверждение. При смене первой вертикали меняются её контракты/задачи, инварианты HLD остаются. **Stop:** нет owner/данных/проверяемого результата или штатная автоматизация выигрывает полную стоимость. См. [PRODUCT_CONTRACT](PRODUCT_CONTRACT.md).

## N-ADR-04 — DSL как authoring adapter к canonical contracts

**Статус:** accepted for planning. Существующая grammar сохраняет compatibility. Новый canonical domain/action contract имеет независимую version/digest; DSL compiler и другие будущие редакторы дают тот же проверяемый результат. Agent protocol и ViewSpec — отдельные contracts с согласованными refs, не расширение DSL до языка всего.

**Альтернатива:** заставить все процессы/UI/методы говорить одной DSL. Это делает любое развитие миграцией языка и ухудшает простоту. **Проверка:** equivalent contract roundtrip и negative schema tests. **Замещает:** исключительность DSL/spec-runtime в старых HLD/LANGUAGE для новых областей; named commands, versioning и запрет обхода admission gate сохраняются.

## N-ADR-05 — bounded PG execution ledger, обязательный I00 gate

**Статус:** provisional implementation choice. В одной PostgreSQL-транзакции Tx API пишет operational state, нужное canonical journal event и outbox. Execution SQL authoritative для Run/Attempt/grant/budget/effect; journal authoritative для business facts; RAM projections derived. Audit execution history не означает второй независимый writer. Durable aggregate/authority heads в той же Tx служат admission barrier: guards/If-Match/ABAC и protected reads не принимают старую RAM revision, при отставании — fail closed до catch-up.

**Альтернативы:** current task pool; собственная универсальная orchestration platform; Temporal. Default PG выбран ради локальной согласованности и поддерживаемой установки, но ещё не доказан по fault/ops стоимости. I00 сравнивает crash points, fencing, parallel budgets, unknown effects, recovery/operator complexity и объём поддержки. Не прошёл заранее объявленный предел — оформляем новый ADR для Temporal: scheduling/history принадлежат ему, PG остаётся authority budgets/effects, межсистемные промежуточные состояния явно reconciled. Никакого runtime dual-master/failover.

**Замещает:** старое правило «единственная истина любого состояния — business journal» применительно к operational state; [ADR-001](../adr/ADR-001-in-memory-projections.md) сохраняется для business projections, не распространяется на execution ledger. Физический journal формат не меняется автоматически этим ADR. **Гарантия:** ни старый Append, ни outbox в другой транзакции не могут быть объявлены атомарной заменой Tx API.

## N-ADR-06 — контроль через trusted broker и enforced boundary

**Статус:** accepted for planning. Context, model, tool и effect calls идут через broker; long-lived secrets недоступны executor. Enforcement требует подтверждённой process/network isolation. Trusted adapter и development поддерживаются с явно меньшими обещаниями.

**Альтернатива:** worker получает токены, но обещает соблюдать prompt/policy. Это может быть удобным dev-профилем, а не корпоративной гарантией. **Проверка:** попытки прямого socket/DB/secret access, redirect/rebinding, prompt injection, nested automation. **Замещает:** кооперативный egress как достаточную production границу в старом [LLD-09](../lld/LLD-09-deployment-product.md). Не меняет сеть компьютера пользователя: устанавливаем изоляцию только в явно выбранной среде приложения.

## N-ADR-07 — разрешения актуальны, approvals зависят от риска

**Статус:** accepted for planning. Actor права + company policy + run grant + typed arguments + ресурсные пределы проверяются при каждом защищённом шаге. Делегирование сужает scope; расширение возможно отдельным company issuer edge. Существенный effect имеет trusted digest/confirmation; ограниченные классы действий могут быть заранее авторизованы.

**Замещает:** безусловное HITL для любого необратимого действия из старого LANGUAGE, blanket manual signature любого изменения модели/brief/handler. Значимое изменение semantics/grant требует проверки/публикации и при необходимости human confirmation по policy; косметический view или low-risk method rollout не требует всеобщей ручной подписи. Auditability и подлинность human decision сохраняются. Risk class не выдаёт право сама.

Revocation локально сериализуется с dispatch permit. Отправка с уже принятым permit может завершиться после revoke; невозможно гарантировать отмену удалённого запроса. **Проверка:** revoke-before-permit блокирует; permit-before-revoke сохраняет trace и не запускает следующие действия.

## N-ADR-08 — неизвестный результат является состоянием

**Статус:** accepted for planning. Effect states содержат unknown/reconciling; intent durable до dispatch, receipt trusted; retry только по доказанному контракту endpoint. Cancel и rollback прекращают следующие действия, компенсация отдельна и не всегда существует.

**Замещает:** «эффект либо произошёл, либо явная ошибка» старого HLD-V1 I-3; универсальную обратимость в ARCHITECTURE/LANGUAGE; exactly-once произвольной federation из старого [LLD-07](../lld/LLD-07-federation-bridge.md). Сохраняем at-least-once доставки с dedup и конкретные endpoint guarantees, не делаем глобального exactly-once обещания. **Проверка:** crash после отправки до receipt, отмена и restore.

## N-ADR-09 — общий bounded resource ledger

**Статус:** accepted for planning. Root budget не обходится child/retry/model call; каждое reservation имеет stable logical key и один settlement. Delegated subtree ceiling — предел, не дополнительная прибавка к уже удержанным call reserves; ledger предотвращает двойной счёт. Денежный расход моделей, бизнес-сумма действия, rate/attempt/depth/time limits — разные величины.

Hard budget требует консервативной цены/объёма до call и технически обеспеченных caps. Unknown billing удерживает worst-case reserve. Provider без такого bound — только soft estimate, явно вне hard-budget guarantee. Actual больше гарантированного bound — инцидент контракта с блокировкой новых calls. **Проверка:** race children+retry, lost response и provider overrun fault.

## N-ADR-10 — выразительные генерируемые views в trusted runtime

**Статус:** accepted for planning. UI agent создаёт ViewSpec из versioned catalog. View может быть ephemeral, pinned live, shared; snapshot отдельный object. Генератор — логическая роль, не обязательная отдельная модель/сервис. Смена аудитории проверяет bindings и literals/derived private text. Действия имеют trusted confirmation по точным args.

**Замещает:** запрет «рантайм от агента» в старом [LLD-14](../lld/LLD-14-ui-as-data-embed.md), если он запрещает генерацию runtime-композиции вообще. **Сохраняет:** no arbitrary agent HTML/JS, закрытые schema/catalog, backend authority; старый I-6 уточняется до «доверенный runtime, недоверенная декларативная композиция». Выразительность не ограничена одной формой/таблицей. **Проверка:** generated mislabel, private literal publish, stale schema, revoke во время открытого view.

## N-ADR-11 — опыт только с применимостью и управлением

**Статус:** accepted for planning. MethodRevision содержит owner, source basis, условия применимости, evaluation cases (positive/negative/new), expiry/revoke и rollout. Model output предлагает revision; принятие идёт по policy. Передача между клиентами требует отдельного разрешения и проверки отсутствия чужих данных.

**Альтернативы:** общая embedding память всех чатов; «самообучение» от числа выполнений. Они закрепляют ошибки и смешивают права. **Проверка:** drift/new case, отзыв версии, cross-client extraction. **Последствие:** evidence подтверждает заявленный наблюдаемый факт/решение в своей области, а не объективную правовую/физическую истину. Риск определяет глубину проверки.

## N-ADR-12 — selective responsibility transition вместо поглощения всей базы

**Статус:** accepted for planning. SourceBinding отображает нужные поля конкретного процесса. Внешняя система остаётся writer до AuthorityTransition с scope, cutover, reconciliation, owner и fallback. Для области один authoritative writer; shadow не пишет.

**Альтернативы:** общий warehouse до пользы; сразу заместить ERP; постоянные два writers. **Проверка:** конфликт версий, рассинхронизация, отмена cutover и export. Будущий handover не даёт оснований обещать уже готовые 1C/ERP/mail adapters.

## N-ADR-13 — один поддерживаемый installation path

**Статус:** provisional delivery choice. Default — packaged self-host: Go control plane + PG + isolated executor/broker, понятная инструкция. Managed delivery выбирается вместо него, если первый покупатель показывает существенно более простой lifecycle и допускает data policy. Kubernetes, несколько поддерживаемых платформ и federation не являются предпосылкой первого pack.

**Замещает:** on-prem как универсальное следствие privacy и «single binary» как полную стоимость эксплуатации старого HLD. Секреты/модели/OAuth/backup остаются реальными dependencies. **Проверка:** install, first value, обновление, restore, support и exit второй компанией. Никаких изменений VPN/маршрутов пользователя в installer.

## N-ADR-14 — restore quarantine и новая execution epoch

**Статус:** accepted for planning. Recovery не включает dispatch автоматически. Восстановленная база может не содержать поздние effects, grants/revokes. Нужны установление lost tail по WAL/anchors/remote evidence, revalidation текущих полномочий, invalidate старых capabilities и новый epoch. Неопределённые области blocked до ручного решения.

**Альтернатива:** восстановить backup и сразу продолжить очередь. Это может повторить эффект и воскресить отозванное право. **Проверка:** effect+revoke после backup, затем restore; даже отсутствующий в backup effect не отправляется повторно без reconciliation.

## N-ADR-15 — несовместимая безопасность важнее legacy write shape

**Статус:** accepted for planning. Legacy read responses можно сохранять через façade; write clients обязаны передавать Attempt ID/token/fence. Старый completion от того же actor нельзя отличить от нового по actor ID. Legacy workers дренируются/останавливаются, не получают новый lease до обновления. Active ambiguous jobs — blocked/manual reconcile, один writer.

**Замещает:** обещание полной прозрачной совместимости task completion без смены протокола. Facade не синтезирует fence по текущему actor и не даёт старому ответу завершить новую попытку. **Проверка:** A истёк → A снова получил задачу → пришёл старый complete A; состояние не меняется. **Migration gate:** inventory, protocol handshake, drain, safe import, negative test, затем переключение.

## Реестр решений, ожидающих evidence

| Вопрос | Кто принимает | Задача / evidence | До решения |
|---|---|---|---|
| Первая компания/процесс и две системы | Product owner + process owner | KAL-002, KAL-004, selector/baseline/access | Только synthetic prototype |
| PG adapter против Temporal | Tech lead + Astra/high review | KAL-003, fault/ops comparison | Проектируем PG, не выдаём production гарантию |
| Self-host против managed | Product/operations owner + buyer | KAL-004, lifecycle/data policy | Один default prototype profile |
| Pilot thresholds, RPO/RTO, limits | Process/operations owner | KAL-002, KAL-053, измеренный gate | Значения не обещаны |
| Вторая вертикаль | Product owner | KAL-066, KAL-070 | R6 conditional |
| Federation / third-party extensibility | Tech/security lead | KAL-076–KAL-080 | R7 experimental, без commercial promises |

Номер решения не совпадает с историческими ADR-001/002: это отдельный namespace N-ADR. Исторические implementation/release документы остаются свидетельствами своих дат.

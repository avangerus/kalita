# DECISIONS — архитектурные решения Kalita

> Формат каждого решения: проблема → варианты → решение → почему → последствия → можно ли отменить.
> ADR в docs/adr/ (ADR-001..005) — первоисточник; здесь сводка + решения эпохи Forge.

## ADR-001..005 (приняты ранее, детали в docs/adr/)

| ADR | Проблема | Решение | Почему | Отмена |
|---|---|---|---|---|
| 001 in-memory проекции | проекции в БД — сложность синхронизации | проекции в памяти, реплей из журнала | журнал — единственная правда; один клиент = один узел | да, при смене масштаба (тогда и tiered storage) |
| 002 tiered storage | рост журнала | порог → холодное хранилище; не строить вслепую | преждевременная оптимизация | да |
| 003 UI SDK | как строить UI | notation-driven, без build-шага | модель = источник UI | да |
| 004 frontend layers | слои фронта | 4 слоя, без npm/JS-runner | buildless — инвариант продукта | сложно |
| 005 permitted-set index | 100k строк ABAC-скан | вторичные индексы permitted-set (sub-ms) | масштаб дашбордов | да |

## Эпоха Forge

### D-009 · Cycle-001: асимметрия reject vs approve (осознанный допуск)
- **Проблема**: в packs/ap `reject` — plain act для трёх утверждающих, `approve_*` — HITL с подписью. Симметрия или нет?
- **Решение**: оставить асимметрию. Отклонение обратимо (reopen) и не двигает деньги; одобрение — критичное направление и гейтится подписью. Зафиксировано в security.dsl и Skeptic-обзором.
- **Отмена**: да — добавить `requires approval` на reject, если появится сценарий злонамеренных отказов.

### D-010 · Cycle-001 (Skeptic REJECT → исправлено): park→decide окно — kernel-gap
- **Проблема**: guard перехода проверяется только при Act-парковке (engine/workflow.go:146-153); Decide (workflow.go:347-358) проверяет лишь `current != From`. Пока подпись висит, сумма может измениться, и подпись применится к другой сумме.
- **Решение сейчас (уровень пака)**: deny-строки APClerk запирают amount/vendor/contract_no/delete в ApprovalPending/Approved/SentToPayment — окно закрыто для этого пака. **Kernel-fix выполнен Cycle-003 (D-013); deny-строки остались defense-in-depth.**
- **Последствия**: до Cycle-003 каждый пак с HITL запирал критичные поля сам; после — защищены все паки ядром.
- **Отмена**: deny-строки можно упрощать; ядро — первая линия защиты.

### D-013 · Cycle-003: Decide перепроверяет guard HITL-перехода (kernel-fix D-010)
- **Проблема**: D-010 — окно park→decide; guard-релевантные данные (свои поля, ref-пути,
  агрегаты, computed, часы) могли измениться, пока висит подпись.
- **Варианты**: A) ре-оценка guard в Decide ДО журналирования решения — GUARD_FAILED,
  ничего не пишется, аппрувал остаётся pending, решение retryable, журнал честен
  («granted ⇒ переход применён»); B) ПОСЛЕ журналирования — решение записано даже если
  не применимо (аудит-полнота), но granted-без-apply в журнале и решение финально.
- **Решение**: A. Полная ре-оценка выражения (withComputed + ctxFor + ref-path resolve,
  internal/engine/workflow.go:321-349) на текущих данных, контекст **requester**
  (`$me = a.RequestedBy` — журналируется как ev.Actor с attrs, replay-консистентно),
  текущие часы `e.now()`. Reject-путь освобождён (D-009: «нет» можно всегда).
  Guard-текст не замораживается — перечитывается из декларации текущей модели
  (definitions — governed события). Код ошибки GUARD_FAILED переиспользован.
- **Почему A, а не B**: B заявляет «аппрувал остаётся pending» — фактически неверно
  (существующий `current != a.From` post-journal путь финализирует решение); A
  единственный даёт retry-семантику и не плодит granted-без-apply в журнале —
  консистентно с размещением checkConsistent (workflow.go:275-281).
- **Два architect-вердикта разошлись**: ранний прогон (файл proposals/cycle-003-architect-verdict.md,
  прерван на доставке) предлагал B и approver-контекст — неверно прочитал proposal
  (пункт C: proposal всегда говорил requester) и внутренне противоречив (пункт E).
  Управляющий вердикт (foreground): ACCEPT A+requester, 11/11. Ruling: skeptic_pro
  (ACCEPT-WITH-NOTES → заметки учтены) + review_security_pro (ACCEPT).
- **Edge**: findTransition==nil (переход удалён/переопределён из DSL между park и decide,
  governed change) — legacy fail-open synthetic-переход без guard; логируется +
  метрика DecideFallbackSynthetic. Fail-closed — отдельное gated-решение (кандидат в Next).
- **Последствия**: 8 регрессий internal/engine/decide_guard_test.go; журнал/реплей не
  тронуты (failed-decide ничего не пишет); контракт MCP не менялся; packs/ap deny-строки
  = defense-in-depth. Известная асимметрия: `current != a.From` остаётся post-journal —
  осознанно не переставлено (меняло бы журнальные семантики существующих журналов).
- **Отмена**: да — переставить проверку после журналирования или сменить контекст на
  approver, если появится сценарий.

### D-014 · Cycle-A2: identity admin boundary (закрытие impersonation/takeover-класса SEC-1)
- **Проблема**: v0 admin-модель гейтила identity-операции только `Type == Human` — любой
  аутентифицированный человек мог сменить пароль ЛЮБОГО актора (`/api/actors/{id}/password`
  → `/api/login` → чужой токен), заблокировать любого (`disableActor`), выписать себе инвайт
  любой роли и погасить через публичный `/api/register` (прямая эскалация), отозвать чужой
  WebAuthn-креденшал (`revokeCredential`). Evidence: login.go:23-42, password.go:42-74,
  handlers_ui.go:266-311, invites.go:37-63, credentials.go:245.
- **Варианты**: A) self-or-admin + admin-only + no-admin-invite; B) всё admin-only (ломает
  портал Manager→Client и self-service паролей); C) DSL role-grant модель (HUMAN_GATE, вне scope).
- **Решение**: A, на существующей конвенции `engine.defApprover` (default "Owner", флаг
  `--approver`, engine.go:198; тот же gate, что у definitions/shadow): setPassword и
  revokeCredential = self ИЛИ admin (403 ДО registry-вызова — журнал чистый); disableActor =
  admin-only; createInvite = любой человек → любая роль КРОМЕ admin-роли (admin — любую).
  Портал-флоу Manager→Client сохранён (portal_test).
- **Почему**: закрывает P1-класс минимальной поверхностью, не ломая онбординг; полная
  role-grant модель — отдельный gated-дизайн (C), staff→staff инвайты остаются v0.
- **Остаточные допущения (зафиксировано reviewers)**: (1) смена пароля НЕ отзывает уже
  выданный bearer-токен (LoginWithPassword минтит новый токен; старый живёт до rotate/disable)
  — remediation в admin playbook: «после сброса чужого пароля — disable→re-enable или
  key-rotate»; (2) defApprover — строковое сравнение; переименование роли в DSL без `--approver`
  = config-drift (операционно; кандидат в конфиг-гейт); (3) self-disable запрещён (не был
  фичей). Ничего из этого не блокирует пилот.
- **Последствия**: 5 новых тестов internal/api/admin_boundary_test.go; TECHDEBT SEC-1 → FIXED;
  RELEASE_BLOCKERS SEC-1 → закрыт. Журнал/DSL не тронуты.
- **Отмена**: да — вернуть v0-семантику или перейти на C (role-grant DSL) при масштабе.

### D-015 · Cycle-A3: operator trust surface (verify --file + TLS-дефолты; OPS-2, SEC-3)
- **Проблема**: (1) `kalita verify` читал журнал только из PG, а restore-совет backup
  указывал на `verify --pub` по файлу — drill невыполним; (2) `--insecure-skip-tls-verify`
  default true (молчаливое отключение TLS-проверки) + prometheus `insecure_skip_verify: true`.
- **Варианты**: A) минимальный честный фикс (verify --file через loadVerifiedBackup; флаг
  default false; prometheus ca_file); B) только OPS-2; C) полноценный CA-bootstrap (Later).
- **Решение**: A. `verify --file <dir>` переиспользует `loadVerifiedBackup` (sha256+цепочка+
  якорь) — без ослабленного пути; `--insecure-skip-tls-verify` default false (константа
  upgradeInsecureSkipTLSVerifyDefault); prometheus `ca_file` + `server_name: localhost`
  (сертификат ноды CN=localhost, scrape на hostname kalita — deploy-баг найден
  security-reviewer, исправлен).
- **Почему**: доверие оператора по умолчанию — false/verify; самоподписанный деплой
  опт-инится явно.
- **Последствия**: 3 теста (loadVerifyEvents file/tamper/missing-manifest; TLS-default const);
  runbook §2/§5/§8; сертификатный путь сверен с deploy/entrypoint.sh. Журнал/DSL не тронуты.
- **Отмена**: да.

### D-016 · Cycle-A4: identity registry atomicity + invite projection (LLD-10 PR-1/PR-2)
- **Проблема**: (1) `Registry.register`/`EnsureActor` — check-then-append БЕЗ mu: конкурентные
  регистрации одного id → двойной actor.registered и/или orphan-токен (hash не в журнале);
  (2) `findInvite` — полный скан журнала; (3) `Redeem` — двойной redeem одного кода
  регистрировал двух акторов.
- **Варианты**: A) mu-сериализация + инвайт-проекция; B) только PR-1; C) IdempotencyKey
  (orphan-токен при dedup — отклонено).
- **Решение**: A. register/EnsureActor держат r.mu поверх foldLocked+проверка+append;
  invites-проекция (map hex(CodeHash)→Invite) в foldLocked; findInvite O(1); Redeem атомарен
  (lock поверх lookup+registerLocked+redeemed-append). Тот же паттерн, что tokenIndex.
- **Почему**: закрывает TOCTOU-класс в identity; тот же журнал/ошибки, нулевое изменение
  наблюдаемого поведения.
- **Последствия**: 4 регрессии registry_atomic_test.go (concurrent register/EnsureActor/
  double-redeem/проекция-vs-скан). Остаточный TOCTOU RotateKey/Disable (idempotent-обновления,
  не создают identity) — POST-RELEASE (PR-6). Журнал/DSL не тронуты.
- **Отмена**: да.

### D-011 · Cycle-001 (Skeptic P1): egress-шов — событие + webhook-задача, доставка — kernel-gap
- **Проблема**: `when status changed to X` — предикат отсутствует в expr-грамматике (молча false); act-переходы не триггерят on-event правила; в cmd/kalita нет webhook-раннера — «интеграция Y» была мёртвым кодом.
- **Решение**: send_to_payment emits `InvoiceSentToPayment`; automation `on event InvoiceSentToPayment: webhook out ...` журналирует TaskWebhook (проверено тестом). Доставка (runner ноды, retries, egress-whitelist) — kernel-gap, ROADMAP Next (KALITA CORE/PLUGIN).
- **Отмена**: runner появится — пак менять не нужно (шов = событие).

### D-012 · maxCascadeDepth=8 vs auto-hops<10 — осознанное расхождение
- **Проблема**: два соседних механизма имеют разные лимиты глубины: op-каскады — `maxCascadeDepth=8` (internal/engine/op.go, константа LLD-01 §2 «глубина цепочки каскада ограничена 8»), авто-переходы — 10 хопов (internal/engine/workflow.go `runAutoTransitions`). Вопрос: свести к одному числу?
- **Решение**: поведение НЕ менять, зафиксировать как осознанное. 8 — фиксированная константа спеки каскадов, к которой подключится PR-4b (вложенные каскады); 10 — исторический лимит авто-переходов, предшествующий каскадам. Оба значения не пересекаются по назначению до тех пор, пока каскады не вкладываются. В коде добавлены комментарии-ссылки «D-012» в обоих местах.
- **Отмена**: да — PR-4b (вложенные каскады) сведёт лимиты к одному значению и обновит обе ссылки.

### D-006 · Kalita Forge как пресет DSH, а не код в репозитории Kalita
- **Проблема**: как реализовать режим непрерывной эволюции — кодом в Kalita или надстройкой над harness?
- **Варианты**: A) подсистема внутри Kalita; B) agent-пресет DeepSeek Harness (persona + скиллы + MCP-мост); C) отдельный сервис.
- **Решение**: B. Пресет `forge` в `${DSH_HOME}/.agent-presets/forge/`: persona-хартия, скилл `kalita-forge` (протокол A–H, роли, gates), MCP-мост к живой ноде, скиллы композиций для самоэволюции.
- **Почему**: harness — заменяемый нижний слой; режим = «как работает агент», а не «что умеет Kalita»; пресет переносим на любой совместимый harness; Kalita-репо остаётся чистым от harness-зависимостей.
- **Последствия**: docs/forge/ — единственная память режима внутри репо; сам пресет вне git Kalita (риск: потерять — митигируем копией/документированием в DECISIONS и экспортом при необходимости).
- **Отмена**: да — можно переложить протокол в docs + скрипты, если пресет неудобен.

### D-007 · В пресете forge нет `tool-cordis`
- **Проблема**: первый mount пресета упал: `tool-cordis` регистрирует Host-провайдеры инспекции процесс-глобально; в процессе уже стоит пресет `cordis` → «Service is already registered».
- **Варианты**: A) оставить tool-cordis в forge (пресет не монтируется рядом с cordis); B) убрать; C) сделать провайдеры scope-aware в DSH (правка harness).
- **Решение**: B — убрать. Инспекция рантайма и правки композиции — удел cordis-сессий (Creator Mode); forge-агент редактирует только свою папку пресета.
- **Почему**: провайдеры — легитимный процесс-глобальный синглтон harness; ломать его ради одного пресета — неверная граница; конфликт двух пресетов в одном процессе — реальный сценарий владельца.
- **Последствия**: forge-сессии не видят cordis_inspect_*/cordis_define; самоэволюция режима двухшаговая (cordis-сессия → проверка mount → forge-сессия).
- **Отмена**: да — добавить строку обратно, если сервер будет запускаться с единственным пресетом forge. (C — кандидат в upstream DSH.)

### D-008 · docs/forge — живая карта владельца, обновляется каждый цикл
- **Проблема**: владелец не может за 10–15 минут понять систему; доки разбросаны и отстают (выявлено 14+ расхождений).
- **Решение**: каталог docs/forge/ с 7 файлами (SYSTEM_MAP, BUSINESS_TO_RUNTIME, COMPONENTS, CURRENT_STATE, ROADMAP, DECISIONS, AGENT_STATUS) + proposals/; EXPLAIN-фаза каждого цикла обновляет их. Код+тесты > документация; расхождения фиксируются, а не замазываются.
- **Почему**: Definition of Done Forge: «владелец понимает» — отдельное условие успеха.
- **Последствия**: двойная документация (docs/forge vs docs/) — митигация: forge-карта ссылается на первоисточники (ADR/LLD), а не дублирует.
- **Отмена**: да.

## Открытые вопросы (ждут решения владельца или исследования)

1. **EVENT-STORE-v0 §4 «агент подписывает все мутации Ed25519»** — документ обещает, код не реализует (подписи есть только у approvals/definitions/checkpoints). Это изменение trust model → **gate владельца**: реализовать (ADR + цикл) или понизить документ? Рекомендация Forge: понизить документ сейчас, реализовать после пилота — подпись каждой мутации дорогая, а ценность HITL уже даёт WebAuthn.
2. **Кросс-пак ref** (счёт↔договор): **проверено в Cycle-001 (H2) — не выразимо**: semantic.go допускает только core.*/mdg/свои сущности; ref[contracts.Contract] не компилируется. Workaround v1 в packs/ap: contract_no + сверка агентом. Решение за владельцем: явный gap навсегда / цикл на ядро (кросс-пак ref или depends-разрешение) — кандидат в Next.
3. **Орг-иерархия/лестницы утверждений**: first-class в ядре или канонический DSL-приём? **H1 Cycle-001 подтвердила**: лестница «сумма→роль» выразима чистыми guarded-переходами (три ApprovalPending→Approved с разными action, requires approval(Role), when amount…), ядро не менялось. Канонический приём зафиксирован в packs/ap/workflows.dsl как образец; first-class (Department, резолв цепочки утверждающих) — Later, по спросу.

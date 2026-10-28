# DECISIONS — архитектурные решения Kalita

> Формат каждого решения: проблема → варианты → решение → почему → последствия → можно ли отменить.
> ADR в docs/adr/ (ADR-001..005) — первоисточник; здесь сводка + решения D-xxx по
> реализованному продукту. Решения целевой архитектуры N1 — docs/next/DECISIONS.md (N-ADR);
> при конфликте для новой разработки действует N-ADR.

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
  governed change) — был legacy fail-open synthetic-переход без guard; с D-020 — fail-closed.
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

### D-017 · Cycle-B1: webhook delivery runner (G2/D-011, Milestone B)
- **Проблема**: `webhook out` журналировал TaskWebhook, но нода не доставляла — egress мёртв
  (D-011); reference-сценарий X→approve→Y невозможен.
- **Решения владельца**: node-level KALITA_EGRESS_DOMAINS (enforcement) + egress-report
  (audit); доставка at-least-once, business effect обязан быть idempotent; Y = mock payment.
- **Решение**: in-process runner в `serve` (тик 10s, gated на непустом allowlist; пустой
  allowlist = runner выключен, задачи остаются pending). Enforcement fail-closed: host из
  URL нормализуется (lowercase/trailing-dot/port) и сверяется точно (без префикс-матча);
  redirect-клиент не следует (CheckRedirect=ErrUseLastResponse). Доставка с retry cap 5 +
  backoff; payload = {idempotency_key: "webhook:<taskID>" (детерминированный), entity,
  record_id, values}; успех → SettleWebhookTask(completed) + фолд в egress-report под
  WebhookRunnerActorID; постоянная ошибка → SettleWebhookTask(failed). Шатдаун оставляет
  задачу open (повтор при следующем старте с тем же ключом).
- **Почему**: at-least-once + детерминированный ключ = exactly-once ЭФФЕКТА на приёмнике
  (паттерн subscribe.py); whitelist enforcement — до любого сетевого вызова.
- **Последствия**: engine.OpenWebhookTasks/SettleWebhookTask/WebhookRecordValues;
  cmd/kalita/webhooks.go; egress-report фолдит доставки ноды. 8 тестов (engine 2 + cmd 6).
  Journal-схема не менялась (task.completed/failed + event.emitted).
- **Остаточное (reviewers, pilot-grade)**: (1) payload несёт живые values на момент
  доставки (не на момент задачи) — задокументировано; (2) IDN/punycode-гомограф и
  DNS-rebinding на allowlist — KNOWN LIMITATION (LIM-9); (3) PII в payload — sealed-шифртекст,
  приёмник — mock (доверенный).
- **Отмена**: да.

### D-011 · Cycle-001 (Skeptic P1): egress-шов — событие + webhook-задача, доставка — kernel-gap
- **Проблема**: `when status changed to X` — предикат отсутствует в expr-грамматике (молча false); act-переходы не триггерят on-event правила; в cmd/kalita нет webhook-раннера — «интеграция Y» была мёртвым кодом.
- **Решение**: send_to_payment emits `InvoiceSentToPayment`; automation `on event InvoiceSentToPayment: webhook out ...` журналирует TaskWebhook (проверено тестом). Доставка (runner ноды, retries, egress-whitelist) — kernel-gap, ROADMAP Next (KALITA CORE/PLUGIN).
- **Отмена**: runner появится — пак менять не нужно (шов = событие).

### D-012 · maxCascadeDepth=8 vs auto-hops<10 — осознанное расхождение
- **Проблема**: два соседних механизма имеют разные лимиты глубины: op-каскады — `maxCascadeDepth=8` (internal/engine/op.go, константа LLD-01 §2 «глубина цепочки каскада ограничена 8»), авто-переходы — 10 хопов (internal/engine/workflow.go `runAutoTransitions`). Вопрос: свести к одному числу?
- **Решение**: поведение НЕ менять, зафиксировать как осознанное. 8 — фиксированная константа спеки каскадов, к которой подключится PR-4b (вложенные каскады); 10 — исторический лимит авто-переходов, предшествующий каскадам. Оба значения не пересекаются по назначению до тех пор, пока каскады не вкладываются. В коде добавлены комментарии-ссылки «D-012» в обоих местах.
- **Отмена**: да — PR-4b (вложенные каскады) сведёт лимиты к одному значению и обновит обе ссылки.

### D-018 · v1.0.0 без пилота: скоуп первой релизной версии
- **Статус**: принято 2026-10-28 (решение владельца: «доделать продукт до первой релизной версии»).
- **Проблема**: критерии RC → 1.0 (POST_RELEASE, git 5447fc5) требуют пилота ≥ 2 недель на реальном
  процессе и egress с реальной системой Y — их нельзя выполнить без клиента, а продукт нужно выпустить.
- **Решение**: `v1.0.0` выпускается по критериям, проверяемым без клиента: закрыты все известные
  ограничения, закрываемые кодом в рамках текущей архитектуры; автоматическая приёмка на PG
  (`tests/acceptance`) и CI зелёные; есть release-пайплайн с подписанными артефактами.
  Критерии 1–3 (пилот, реальная Y, мониторинг без новых P0/P1) переносятся в эксплуатацию 1.0:
  нарушение = патч-релиз 1.0.x, а не блок выпуска.
- **Границы**: в скоуп не входит то, что принадлежит N1 (Attempt/fence, бюджеты, broker —
  KAL-011…), consumer-auth (SEC-5), федерация E2E (LIM-6), эпохи ключа узла. Порядок этапов и их
  состояние — `docs/STATUS.md` → «Путь к 1.0».
- **Отмена**: да — тег 1.0 можно отозвать до первой установки у клиента.

### D-019 · DLQ показывает только недоставленное
- **Статус**: принято 2026-10-28 (S2 пути к 1.0).
- **Проблема**: после успешного retry исходная упавшая задача доставки оставалась в DLQ навсегда,
  `kalita_egress_dead_lettered` её считал, а кнопка Retry повторно отправляла уже доставленное.
- **Решение**: задача доставки, у которой есть более поздняя задача с тем же `delivery_id`,
  считается замещённой: её нет в DLQ и в метрике, retry по ней — CONFLICT «already retried».
  Журнал не меняется — замещение выводится из уже записанных задач.
- **Отмена**: да, правило живёт в проекции (`internal/engine/egress_delivery.go`).

### D-020 · `on update` видит переходы; Decide fail-closed на удалённом переходе
- **Статус**: принято 2026-10-28 (S2 пути к 1.0; закрывает LIM-1 и LIM-7).
- **LIM-1**: `on update` срабатывал только на правку полей. Act-переход и одобренный HITL-переход
  его не вызывали, а `status changed to X` из DSL-SPEC §6 не был реализован: вычислитель отклонял
  такое условие, и правило молча не работало.
  **Решение**: после act и после granted-решения выполняются `on update`-правила; в их `when`
  работают `field changed` и `field changed to V` (сравнение с состоянием до изменения; вне
  `on update` — false). Узел отказывается загружать модель с неразбираемым `when` в automation
  (старт, ApplyAdditive, proposal).
- **LIM-7**: если переход удалён из DSL между park и decide, Decide применял синтетический переход
  без guard (fail-open, D-013). **Решение**: grant отклоняется CONFLICT до записи в журнал,
  аппрувал остаётся pending, reject по-прежнему доступен (D-009). Реплей не затронут: Decide не
  переигрывается, в старых журналах — обычные record.action.
- **Последствия**: правила `on update X when <условие>` теперь срабатывают и на переходах, если
  условие истинно; среди поставляемых такие правила есть только в примерах (dev_department, pangram).
- **Отмена**: да, это поведение рантайма; формат журнала не менялся.

### D-021 · Egress: канонические хосты, IDN только в punycode, resolve-and-pin
- **Статус**: принято 2026-10-28 (S3 пути к 1.0; закрывает LIM-9).
- **Проблема**: allowlist сравнивал имя хоста, но (1) голые IPv6 портились при нормализации —
  `fe80::1` и `fe80::2` давали одинаковую строку; (2) Unicode-хосты (гомографы) проходили как есть;
  (3) имя из allowlist могло резолвиться во внутренний адрес (DNS rebinding, 169.254.169.254).
- **Решение**: хосты сравниваются в канонической форме (`net.ParseIP` → `String()`, зоны IPv6
  отклоняются). Не-ASCII хосты отклоняются и в URL, и в `KALITA_EGRESS_DOMAINS` (квитанция
  `idn_host`; IDN указывается в punycode). Доставка идёт через свой dialer: имя резолвится один раз
  на соединение, соединение — на проверенный адрес. Разрешены публичные адреса и loopback;
  приватные, link-local, CGNAT и ULA — только с `KALITA_EGRESS_ALLOW_PRIVATE=1` (иначе квитанция
  `private_target`, без повторов). IP-литерал, явно указанный в allowlist, доступен как указан.
  Настроенный HTTP(S)_PROXY набирается как есть — тогда контроль на прокси.
- **Почему loopback разрешён**: байты не покидают хост (как и исключение для bearer по http://);
  на нём работают приёмка и локальные моки.
- **Отмена**: да; поведение узла, формат журнала не менялся.

### D-022 · Выпуск: GitHub Releases + GHCR, подпись cosign keyless
- **Статус**: принято 2026-10-28 (S5 пути к 1.0; закрывает OPS-3).
- **Решение**: тег `v*` запускает `.github/workflows/release.yml`: архивы linux/amd64, linux/arm64,
  windows/amd64, darwin/amd64, darwin/arm64 (бинарь `-tags embedui` + packs + examples), файл
  `SHA256SUMS` с подписью `SHA256SUMS.bundle`, GitHub Release (`-rc` → prerelease), образ
  `ghcr.io/avangerus/kalita:<тег>` для amd64 и arm64, подписанный по digest. `latest` двигается только
  финальным тегом. Подпись keyless (GitHub OIDC): ключа, который надо хранить и ротировать, нет;
  проверка — по идентичности `release.yml@refs/tags/v*` этого репозитория. `kalita upgrade` проверяет
  её по умолчанию; `--cosign-key` — для своих образов, подписанных ключом.
- **Гейт**: workflow собирает и подписывает, но не тестирует; тег ставится только на коммит с
  зелёным CI (тесты, race, python, приёмка на PG).
- **Отмена**: да — можно перейти на ключ (`--cosign-key`) без изменения формата выпуска.

### D-006 · Kalita Forge как пресет DSH, а не код в репозитории Kalita
- **Статус**: отменено 2026-10-28 — режим Forge и интеграция DSH удалены из репозитория (история: git 5447fc5).
- **Проблема**: как реализовать режим непрерывной эволюции — кодом в Kalita или надстройкой над harness?
- **Варианты**: A) подсистема внутри Kalita; B) agent-пресет DeepSeek Harness (persona + скиллы + MCP-мост); C) отдельный сервис.
- **Решение**: B. Пресет `forge` в `${DSH_HOME}/.agent-presets/forge/`: persona-хартия, скилл `kalita-forge` (протокол A–H, роли, gates), MCP-мост к живой ноде, скиллы композиций для самоэволюции.
- **Почему**: harness — заменяемый нижний слой; режим = «как работает агент», а не «что умеет Kalita»; пресет переносим на любой совместимый harness; Kalita-репо остаётся чистым от harness-зависимостей.
- **Последствия**: docs/forge/ — единственная память режима внутри репо; сам пресет вне git Kalita (риск: потерять — митигируем копией/документированием в DECISIONS и экспортом при необходимости).
- **Отмена**: да — можно переложить протокол в docs + скрипты, если пресет неудобен.

### D-007 · В пресете forge нет `tool-cordis`
- **Статус**: неактуально с 2026-10-28 (см. D-006).
- **Проблема**: первый mount пресета упал: `tool-cordis` регистрирует Host-провайдеры инспекции процесс-глобально; в процессе уже стоит пресет `cordis` → «Service is already registered».
- **Варианты**: A) оставить tool-cordis в forge (пресет не монтируется рядом с cordis); B) убрать; C) сделать провайдеры scope-aware в DSH (правка harness).
- **Решение**: B — убрать. Инспекция рантайма и правки композиции — удел cordis-сессий (Creator Mode); forge-агент редактирует только свою папку пресета.
- **Почему**: провайдеры — легитимный процесс-глобальный синглтон harness; ломать его ради одного пресета — неверная граница; конфликт двух пресетов в одном процессе — реальный сценарий владельца.
- **Последствия**: forge-сессии не видят cordis_inspect_*/cordis_define; самоэволюция режима двухшаговая (cordis-сессия → проверка mount → forge-сессия).
- **Отмена**: да — добавить строку обратно, если сервер будет запускаться с единственным пресетом forge. (C — кандидат в upstream DSH.)

### D-008 · docs/forge — живая карта владельца, обновляется каждый цикл
- **Статус**: отменено 2026-10-28 — docs/forge упразднён; единая карта — docs/README.md, текущее состояние — docs/STATUS.md.
- **Проблема**: владелец не может за 10–15 минут понять систему; доки разбросаны и отстают (выявлено 14+ расхождений).
- **Решение**: каталог docs/forge/ с 7 файлами (SYSTEM_MAP, BUSINESS_TO_RUNTIME, COMPONENTS, CURRENT_STATE, ROADMAP, DECISIONS, AGENT_STATUS) + proposals/; EXPLAIN-фаза каждого цикла обновляет их. Код+тесты > документация; расхождения фиксируются, а не замазываются.
- **Почему**: Definition of Done Forge: «владелец понимает» — отдельное условие успеха.
- **Последствия**: двойная документация (docs/forge vs docs/) — митигация: forge-карта ссылается на первоисточники (ADR/LLD), а не дублирует.
- **Отмена**: да.

## Открытые вопросы (ждут решения владельца или исследования)

1. **EVENT-STORE-v0 §4 «агент подписывает все мутации Ed25519»** — документ обещает, код не реализует (подписи есть только у approvals/definitions/checkpoints). Это изменение trust model → **gate владельца**: реализовать (ADR + цикл) или понизить документ? Рекомендация Forge: понизить документ сейчас, реализовать после пилота — подпись каждой мутации дорогая, а ценность HITL уже даёт WebAuthn.
2. **Кросс-пак ref** (счёт↔договор): **проверено в Cycle-001 (H2) — не выразимо**: semantic.go допускает только core.*/mdg/свои сущности; ref[contracts.Contract] не компилируется. Workaround v1 в packs/ap: contract_no + сверка агентом. Решение за владельцем: явный gap навсегда / цикл на ядро (кросс-пак ref или depends-разрешение) — кандидат в Next.
3. **Орг-иерархия/лестницы утверждений**: first-class в ядре или канонический DSL-приём? **H1 Cycle-001 подтвердила**: лестница «сумма→роль» выразима чистыми guarded-переходами (три ApprovalPending→Approved с разными action, requires approval(Role), when amount…), ядро не менялось. Канонический приём зафиксирован в packs/ap/workflows.dsl как образец; first-class (Department, резолв цепочки утверждающих) — Later, по спросу.

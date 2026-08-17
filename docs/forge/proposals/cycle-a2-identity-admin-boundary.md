# Cycle-A2 · PROPOSE — Identity admin boundary: закрыть impersonation/takeover-класс

> Milestone A fix-цикл #1 (I-1/I-2/I-3 из cycle-a1-gaps.md). Для ARCHITECT REVIEW.

## Проблема

Три REST-операции v0 admin-модели гейтятся только `actor.Type == Human` — ЛЮБОЙ
аутентифицированный человек может:

- **I-1** `POST /api/actors/{id}/password` — сменить пароль ЛЮБОГО актора
  (internal/api/login.go:23-42 → internal/identity/password.go:42-74: нет проверки
  self/admin), затем `/api/login` → чужой bearer-токен → полная impersonation любой роли;
- **I-2** `POST /api/actors/{id}/disable` (handlers_ui.go:266-279 → registry.go:384-398) —
  заблокировать ЛЮБОГО актора, включая Owner (DoS/захват узла);
- **I-3** `POST /api/invites` (handlers_ui.go:289-308 → invites.go:37-63) + публичный
  `/api/register` (handlers_ui.go:311) — выписать себе инвайт ЛЮБОЙ роли
  (GeneralDirector и т.п.) и тут же погасить → прямая эскалация роли.

Evidence: TECHDEBT.md:258-271 (SEC-1, P1). MCP-поверхности нет (tools.go: нет
password/invite/disable) — только REST.

## Существующая admin-конвенция (факт)

`engine.defApprover` (default **"Owner"**, engine.go:198, флаг `--approver`,
main.go:474) уже является узловой admin-границей: definition-подписи
(proposals.go:124), shadow-переключения (shadow.go:174,297), core reference data
(permissions.go:40,111). Identity-операции просто не используют её.

## Варианты

### A — self-or-admin + admin-only + no-admin-invite (рекомендуется)

1. `setPassword`: разрешить если `actor.ID == id` (self-service) ИЛИ
   `actor.Role == defApprover` (admin reset чужого).
2. `disableActor`: только `actor.Role == defApprover` (self-disable не существует).
3. `createInvite`: любой человек может выписать инвайт ЛЮБОЙ роли, КРОМЕ роли
   `defApprover`; инвайт на admin-роль — только сам admin. (Сохраняет портал-сценарий
   Manager→Client из portal_test.go:100-104; закрывает эскалацию в Owner.)
4. Механика: `engine.DefinitionApprover()` (новый экспортный геттер) → api-хендлеры.
5. Честная граница: staff→staff инвайты (Manager→GeneralDirector) остаются в v0
   admin-модели «доверенный персонал» — документируется как известное ограничение;
   полноценная role-grant-модель — отдельный gated-дизайн (owner).

**Цена**: ~30 строк + геттер + тесты (self/admin/deny для каждой из 3 операций +
регрессии portal/login). Blast radius: internal/api + internal/engine (геттер) +
internal/identity (без изменений — проверки в api-слое, где есть actor+eng).

### B — только admin (жёстко)

Все три операции — только defApprover. Плюс: минимальная поверхность. Минус:
ломает портал (Manager не выпишет Client-инвайт) и self-service паролей — требует
новый онбординг-поток (вне scope цикла, гейт владельца).

### C — DSL-ролевая модель инвайтов (полноценно)

Пак декларирует, какая роль кому какие роли может выдавать. Плюс: правильная
модель. Минус: DSL-semantics изменение → HUMAN_GATE; слишком большой цикл для
закрытия P1 сейчас.

## Ожидаемое изменение архитектуры

- `internal/engine`: экспортный геттер `DefinitionApprover()`.
- `internal/api`: admin-гейт в setPassword/disableActor/createInvite по варианту A.
- `internal/api/login_test.go`, `portal_test.go`: новые негативные кейсы
  (не-admin → чужой пароль 403; не-admin → disable 403; не-admin → invite Owner 403;
  self-setPassword OK; admin reset OK; Manager→Client invite OK).
- Доки: TECHDEBT SEC-1 → FIXED; RELEASE_BLOCKERS SEC-1 → закрыт; DECISIONS D-014.

## Acceptance criteria

1. Не-admin человек НЕ может: сменить чужой пароль, заблокировать другого,
   выписать инвайт на admin-роль (тесты 403 + журнал без событий).
2. Self-setPassword работает; admin reset чужого работает; Manager→Client инвайт
   работает (портал-регрессия зелёная).
3. Агент не может ни одну из операций (Type != Human — существующий гейт, тест).
4. go build/vet/test ./... зелёные; 16 паков + 3 примера.
5. Reviewers: skeptic_pro + review_security_pro ACCEPT (trust boundary затронута).

## Риски

- Сломанный портал-флоу при ошибке в правиле 3 → регрессия portal_test.
- Ветка `defApprover` переименована/не настроена: геттер всегда возвращает default
  "Owner" — поведение предсказуемо.
- Остаточное: staff→staff инвайты (v0) — фиксируется как KNOWN LIMITATION + кандидат
  в C (HUMAN_GATE владельца).

## Human gates

Нет (усиление инварианта, DSL не меняется, журнал не меняется).

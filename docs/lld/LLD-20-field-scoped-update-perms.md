# LLD-20 — field-scoped update allow-list (ядровой фикс прав)

Дата: 2026-07-02. Автор решений: Fable. Реализация: Sonnet 5 по документу.
Родитель: BUG-02 hardening (DEMO-0), TECHDEBT «Legal пак / DEMO-0 hardening».
Зона: internal/engine (permissions.go / records.go) — ядро прав, необратимо,
ревью Fable обязателен. Инвариант: default-deny, deny>allow, choke point I-2.

**Мотив.** Least-privilege write невыразим: роль может либо `update [Entity]`
(пиши ЛЮБОЕ поле кроме deny), либо `update [Entity.f]` (тогда update этой
entity не проходит вообще — пишет НИЧЕГО). «Разрешено писать ТОЛЬКО эти поля»
недостижимо. Legal-агент обошёл через нескоуп-grant + ИСЧЕРПЫВАЮЩИЙ deny на
все human-поля — хрупко: забытое новое поле переоткрывает SoD-дыру. Это барьер,
на котором стоит тезис «агент не подделает договор» — он должен быть robust,
не «не забудь поле».

**Корень (точно по коду).** `engine.Update` (records.go:341) гейтит двумя
проверками: (1) `can(actor,"update",entity,"",rec)` — на весь рекорд с
`field=""`; (2) `checkFieldWrites` — `can(...,f,...)` на каждое пишущееся поле.
`itemMatches` (permissions.go:92): при `field=""` матчит ТОЛЬКО правило с
`item.Field ""/"*"`; field-scoped правило (`item.Field="assessment"`) даёт
`field==item.Field` → `""=="assessment"` → false. Значит scoped-only роль
проваливает гейт (1) ВСЕГДА и до гейта (2) не доходит.
**Ключ:** гейт (2) `checkFieldWrites` — УЖЕ корректный per-field allow+deny
(pass-2 требует allow-правило на поле, deny перекрывает, default-deny). Сломан
ТОЛЬКО гейт (1).

**Границы для исполнителя.** Sonnet НЕ решает сам: гейт (2)/`checkFieldWrites`
НЕ трогать — он уже правильный (§2); семантику deny/`itemMatches`/where НЕ
менять; фикс — только гейт (1) → существование, с ДОКАЗАТЕЛЬСТВОМ немонотонного
не-ослабления (§3). Любое расширение прав, кроме «scoped-only теперь пишет ровно
разрешённые поля», — красный флаг, откатить.

---

## 1. Решение: гейт (1) → проверка существования

Гейт (1) в `Update` перестаёт быть проверкой-на-весь-рекорд с `field=""`.
Становится: **«есть ли у актора ХОТЬ ОДНО update-allow-правило на entity
(скоуп неважен), не перекрытое безусловным whole-entity/`*` deny?»** — только
для раннего чистого отказа роли без права update вообще. РЕАЛЬНОЕ решение —
гейт (2) `checkFieldWrites` (уже корректный):
- `update [Entity]` (нескоуп): гейт (1) — правило есть → пропуск; гейт (2) —
  каждое поле матчит `item.Field=""` → allow (минус deny). Как сегодня.
- `update [Entity.assessment, Entity.risk]` (scoped-only): гейт (1) — правила
  есть → пропуск; гейт (2) — assessment/risk → allow; terms → нет allow-правила
  на поле → default-deny → отказ. **ИСТИННЫЙ allow-list.**
- нет ни одного update-правила: гейт (1) — отказ, чистая ошибка «нет права
  update на entity».

## 2. Что НЕ трогаем (уже корректно)

`checkFieldWrites` и `can`/`itemMatches`/deny-pass — без изменений. Record-level
where-deny (`deny update Entity where status=Signed`) остаётся enforced: у него
`item.Field=""`, значит в гейте (2) он матчит КАЖДОЕ поле → блокирует любой
write в подписанный договор и без гейта (1). Ослабление гейта (1) НЕ теряет
запертость записи — её держит гейт (2).

## 3. Доказательство не-ослабления (обязательно в тестах)

Фикс МОНОТОННО безопасен:
- До фикса scoped-only роль писала НИЧЕГО (гейт 1 всегда падал).
- После фикса та же роль пишет РОВНО поля из своих grant’ов (гейт 2 allow),
  всё прочее — default-deny.
- Нескоуп-grant роли: гейт (1) существование проходит так же, гейт (2) идентичен
  → поведение НЕ меняется.
- Ни один ранее запрещённый write не становится разрешённым. Новая способность
  ровно одна: scoped-only пишет свои поля.
Тест обязан покрыть это утверждение прямо (§5.4).

## 4. Следствие: Legal → истинный allow-list

После фикса роль `Legal` (packs/contracts/security.dsl) переводится с
нескоуп-`update [Contract]` + исчерпывающий deny НА `update [Contract.assessment,
Contract.risk]` (истинный allow-list). Новое поле Contract тогда НЕ пишется
агентом ПО УМОЛЧАНИЮ (robust), а не «пока не забыли в deny». Убирает остаточный
риск BUG-02 полностью. Интерим-гейт-тест (Legal пишет ровно {assessment,risk})
остаётся как ремень-и-подтяжки.

## 5. Приёмочные тесты (фиксированы)

1. `TestScopedUpdateWritesOnlyGrantedFields` — роль с `update [X.a, X.b]`
   (без нескоуп): пишет a/b; write в c → denied; чтение не затронуто.
2. `TestUnscopedUpdateUnchanged` — роль `update [X]`: пишет любое поле кроме
   deny — байт-в-байт как до фикса (регресс-страховка).
3. `TestRecordDenyStillLocks` — `deny update X where status=Signed`: любой write
   в signed-запись отклонён и при scoped, и при нескоуп grant (гейт 2 держит).
4. `TestUpdateNotLoosened` — прямое утверждение §3: перебор (роль × поле),
   множество разрешённых write ПОСЛЕ ⊇ ДО только на scoped-поля, нигде иначе.
5. `TestNoUpdateRightCleanError` — роль без update-правил → чистый отказ на
   гейте (1), не паника/не пусто.
6. `TestLegalAllowListPoison` — Legal переведён на allow-list; honeypot-договор
   (TestDogfoodContractsPoisonedTerms…) по-прежнему: Finding создаётся, terms/
   status/подпись — denied. Новое (симулированное) поле Contract НЕ пишется
   агентом без явного grant.
7. Golden/I-1: все паки компилируются; старый журнал реплеится; go test ./...
   зелёный.

## 6. PR-нарезка

PR-1: гейт (1) → существование в `engine.Update` + тесты 1–5. ← **ревью Fable**
      (ядро прав, доказательство не-ослабления §3 — обязательно).
PR-2: Legal → allow-list в security.dsl + тест 6; снять исчерпывающий deny.
PR-3: (опц.) распространить allow-list на прочие least-privilege роли паков,
      где обход deny-list был вынужденным (crm Intake, site MirroredLead и т.п.).

**Порядок:** маленький ядровой фикс, зона internal/engine + packs/contracts —
не пересекается с Волной B (LLD-06: workers/packages/assistant). Делать отдельной
core-волной с моим ревью; можно после демо-петли (барьер уже держит на deny-list),
но ДО пилота (robust SoD — часть продаваемого тезиса).

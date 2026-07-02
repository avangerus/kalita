# LLD-05 — Product Kit: атомарная дизайн-система + обвязка поставки

Дата: 2026-07-02. Автор решений: Fable. Реализация: Sonnet 5 по документу.
Родитель: HLD-V1 §H7.1 (PK-1), §H7.4 (PK-4), ADR-003/004 (WordPress-модель,
buildless). Первый потребитель — KnowVault M2. Зона: web/, packages/, cmd/kalita
(scaffold), небольшой core-pack для password-credential.

**Границы для исполнителя.** Sonnet НЕ решает сам: слои дизайн-системы §1
(atoms→molecules→organisms→faces — порядок и контракт метаданных); модель
password-credential §3 (событие в журнал, не отдельная таблица — I-4/I-5);
запрет билд-шага (ADR-004 — buildless обязателен). Всё, что касается
consumer-масштаба (массовый self-signup, OAuth) — НЕ здесь, это внешний IdP
(остаётся за релизом, N7). Бренд/тема — данные пака, не форк кода.

---

## 1. PK-1 — дизайн-система по атомарной модели

Пакет `packages/kalita-ui` рядом с существующим `@kalita/sdk`. Слои снизу вверх,
каждый потребляет `/api/meta`-дескрипторы, доменных знаний ноль (ADR-004):

- **tokens** (`tokens.css`): CSS custom properties — цвет, типографика,
  плотность, радиусы. Темизация БЕЗ билда: перекрытие переменных на `:root` из
  бренд-манифеста пака (§4). Никаких препроцессоров.
- **atoms**: `Input, Select, Money, Badge, StatusChip, DateTime, Bool, RefLink`.
  Извлечь из существующих `FieldInput`/`FieldValue` (web/app.js) — те остаются
  ДИСПЕТЧЕРОМ (по типу поля из meta выбирают атом), логика типов не дублируется.
- **molecules**: `FieldRow, RecordCard, ApprovalCard, TimelineItem, DashboardTile`.
- **organisms**: `KList, KDetail, KBoard, KInbox` — уже есть в @kalita/sdk
  (ADR-003), переключить на atoms/molecules этого пакета, не переписывать API.
- **faces** (шаблоны продуктовых лиц): `SearchFace` (поиск+ответ+источники —
  для KnowVault), `RiskCardFace` (клаузный риск — для Legal), `ChatFace`
  (заглушка-контракт, наполняется в LLD-06). Face = композиция organisms+
  molecules под один экран продукта; собирается из библиотеки, НЕ пишется с нуля.

**Каталог-витрина** `web/catalog.html` (наш storybook, buildless): каждый
компонент со списком примеров и knobs, читает те же meta-дескрипторы. Служит
и визуальной регрессией, и гайдом автора продукта.

**Миграция без разрыва:** встроенный админ-клиент (web/app.js) постепенно
переводится на atoms; в LLD-05 достаточно, чтобы app.js ИСПОЛЬЗОВАЛ извлечённые
atoms (один источник рендера типа), два fetch-клиента (app.js + sdk/client.js)
сходятся на client.js отдельным шагом (не блокер LLD-05, зафиксировать долг).

## 2. Контракт компонента (что делает вертикали дешёвыми)

Компонент получает ТОЛЬКО meta-дескриптор поля/сущности/действия (тип, права
readable/writable, actions[].can_act) и значение. Он НЕ знает домена, НЕ
хардкодит имён паков. Продуктовое «лицо» = выбор faces + токены темы + бренд-
манифест — без единой строки доменного JS. Это и есть проверяемая цель PK.

## 3. PK-4 — обвязка поставки

- **Бренд-манифест в паке** (§4): секция `product { name, logo, colors{...},
  home: <FaceName>, auth: password|invite|token }`. Узловые флаги `--brand*`
  становятся дефолтами из значений пака. Тема применяется перекрытием tokens.
- **Password как credential актора** (НЕ consumer-масштаб): событие
  `actor.password_set {bcrypt_hash}` в журнал (I-4: сам пароль не хранится,
  только bcrypt; I-5: проверяемо офлайн); `POST /api/login {login, password}`
  сверяет bcrypt → выдаёт ОБЫЧНЫЙ bearer-токен существующего актора. Это
  UX-обёртка над токенами для нетехнаря, НЕ новая identity-модель. Массовый
  self-signup — внешний IdP, вне этого LLD.
  bcrypt — разрешённая новая зависимость (`golang.org/x/crypto/bcrypt`), либо
  scrypt из stdlib-совместимого — Sonnet выбирает, фиксирует в отчёте.
- **`kalita product scaffold <pack>`** (cmd/kalita): генерит deploy-compose
  (по образцу deploy/knowvault), status-страницу (переиспользовать /healthz,
  /readyz — уже есть), skeleton бренд-манифеста, runbook-шаблоны. Не магия —
  копирование шаблонов с подстановкой имени пака.
- **Status-страница**: один HTML на organisms, читает /healthz + /readyz +
  (если есть) счётчики воркеров — «здоровье продукта» для нетехнаря.

## 4. Приёмочные тесты (фиксированы)

1. `TestProductManifestParses` — секция product в паке компилируется, дефолты
   бренда доступны через /api/meta.
2. `TestPasswordLogin` — actor.password_set → /api/login выдаёт рабочий bearer;
   неверный пароль → 401; журнал не содержит пароля, только bcrypt.
3. `TestPasswordVerifyOffline` — `kalita verify` проходит на журнале с
   password_set (bcrypt-событие в цепочке валидно).
4. UI (ручной чек-лист в catalog.html + один автоматический smoke): каждый atom
   рендерит все свои meta-типы; SearchFace/RiskCardFace собираются из библиотеки
   без доменного JS.
5. `TestScaffoldGeneratesCompose` — `kalita product scaffold knowvault` создаёт
   валидный compose + status + манифест-скелет.
6. Golden/I-1: существующий web/app.js после перевода на atoms рендерит все
   текущие паки (визуальная регрессия через catalog.html); go test ./... зелёный.

## 5. PR-нарезка

PR-1: tokens + atoms (извлечь из FieldInput/FieldValue, app.js на них) +
      catalog.html. Тест 4, 6.
PR-2: molecules + faces (SearchFace, RiskCardFace) + organisms на новые слои.
PR-3: бренд-манифест product{} в грамматике пака + /api/meta expose (тест 1);
      тема через tokens.
PR-4: password-credential (actor.password_set, /api/login, verify) — тесты 2, 3.
      ← ревью Fable (identity/журнал/verify — необратимая зона).
PR-5: kalita product scaffold + status-страница (тест 5).

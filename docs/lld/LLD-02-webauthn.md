# LLD-02 — WebAuthn/passkey: неотрекаемая подпись человека

Дата: 2026-07-02. Автор решений: Fable. Реализация: Sonnet 5 строго по
документу. Родитель: HLD-V1 §H1. Зависимость: LLD-01 §1.1 (schema_v).

**Границы для исполнителя.** Sonnet НЕ решает сам: формат канонической строки;
политику challenge (§2 — решение с обоснованием, менять нельзя); набор
алгоритмов; выбор зависимостей (§6); ослабление приёмки. Крипто-код (§3
верификация) — обязательное ревью Fable до merge.

---

## 1. События (все schema_v: 1)

| kind | payload |
|---|---|
| `actor.credential_registered` | `{actor_id, credential_id (b64url), public_key_cose (b64, CBOR), alg (int, COSE), aaguid, sign_count, transports[], label}` |
| `actor.credential_revoked` | `{actor_id, credential_id, reason}` |
| `actor.credential_warning` | `{actor_id, credential_id, kind: "sign_count_regression", observed, expected}` |

`approval.decided` дополняется АДДИТИВНЫМ полем payload
`human_signature {credential_id, authenticator_data (b64), client_data_json
(b64), signature (b64)}`. Отсутствие поля = токен-решение (dev-режим) —
существующие события валидны как были (I-1).

Проекция identity расширяется fold'ом: `actor → []credential`
(id, cose, alg, sign_count, revoked).

## 2. Каноническая строка и challenge (решение, не обсуждается в LLD)

```
canonical := "kalita-approval-v1" NL
             task_id NL
             entity "/" record_id NL
             transition NL
             ("grant" | "deny") NL
             basis_type ":" basis_id NL
             head_seq NL
             node_id
challenge := SHA-256(canonical)          // 32 байта
```
- `head_seq` — seq головы журнала в момент выдачи approval-карточки клиенту:
  подпись фиксирует «что человек видел» (состояние ≤ head_seq).
- `node_id` — публичный ключ узла (отпечаток): подпись непереносима между
  узлами.
- **Почему детерминированный challenge допустим** (WebAuthn-спека советует
  random против replay): здесь use-case не аутентификация сессии, а
  **подпись содержимого** (digital signature over canonical). Replay-атака
  бессмысленна: task_id одноразов (approval решается один раз, повтор
  отклоняется движком), idempotency дедупит, канонال включает node_id и
  head_seq. Второй submit той же подписи не может произвести второе решение.
- Сервер при проверке ВОССТАНАВЛИВАЕТ canonical из собственного состояния
  задачи (не доверяет клиентскому пересказу) и сверяет challenge из
  clientDataJSON с SHA-256(canonical).

## 3. Верификация assertion (порядок обязателен)

1. `clientDataJSON`: `type == "webauthn.get"`; `challenge ==
   base64url(SHA-256(canonical))`; `origin` ∈ разрешённых origin узла
   (те же значения, что same-origin-политика api.Secure).
2. `authenticatorData`: rpIdHash == SHA-256(rpId узла); флаг UP (user
   presence) установлен; UV (user verification) — required при enforcement.
3. Подпись: `verify(public_key, signature, authenticator_data ||
   SHA-256(client_data_json))`. Алгоритмы: **ES256 обязателен, RS256
   допустим**, прочие → reject.
4. `sign_count`: если новый ≤ хранимого И оба ≠ 0 → Append
   `actor.credential_warning` (НЕ блокировать: синхронизируемые passkey
   Apple/Google легитимно не инкрементируют счётчик). Обновить проекцию.
5. Credential revoked → reject до всех прочих проверок.

## 4. Потоки

- **Регистрация:** аутентифицированный человек (bearer) →
  `POST /api/credentials/begin` (сервер выдаёт random challenge регистрации —
  здесь классический WebAuthn-flow) → `navigator.credentials.create` →
  `POST /api/credentials/finish` → верификация attestation ("none"
  достаточно, устройство не аттестуем) → Append credential_registered.
  Несколько credential на актора — норма (список). `label` — свободная
  строка («рабочий ноутбук»).
- **Подпись решения:** UI открывает approval-карточку → `GET
  /api/approvals/{id}/challenge` возвращает canonical-поля + challenge →
  `navigator.credentials.get({challenge})` → `decide {grant, basis,
  human_signature}` → верификация §3 → Append approval.decided.
- **Revoke:** сам актор или admin: `kalita user revoke-credential` /
  `DELETE /api/credentials/{id}` → Append credential_revoked.
- **Потеря устройства:** новая регистрация новым credential (инвайт-ре-онбординг
  при полной потере). Старые подписи валидны навсегда (ключи в журнале).
- **MCP:**人 через MCP при enforcement подписывать НЕ может — decide через
  MCP без human_signature отклоняется `SIGNATURE_REQUIRED` (подпись возможна
  только из браузера/SDK). Fix_hint указывает на UI.

## 5. Enforcement и verify

- Флаг узла `--require-human-signature`: все approve/reject людей — только с
  валидной human_signature; UV required. Демо/дев — без флага, поведение
  прежнее.
- `kalita verify --pub <node.pub>` расширяется: строит credential-реестр из
  журнала → проверяет каждую human_signature (полный §3, кроме origin —
  офлайн нет rpId? rpId ЕСТЬ: он в canonical node_id и в событии
  approval.decided воспроизводим из журнала конфигурации узла — **решение:
  rpId пишется в payload события approval.decided** рядом с human_signature,
  чтобы офлайн-проверка была самодостаточной).
  Новый флаг `--require-signed-approvals`: наличие неподписанных
  human-approve → FAIL (для аудиторской проверки прод-журнала).

## 6. Зависимости и реализация

- **Разрешённая новая зависимость: `github.com/fxamacker/cbor/v2`** (pure Go,
  security-focused) — парсинг COSE-ключей. Библиотеку go-webauthn НЕ брать
  целиком (она session-ориентирована, наш challenge-flow нестандартен):
  верификация пишется на crypto/ecdsa + crypto/rsa + cbor. Это ~150 строк,
  зато поверхность зависимостей минимальна (go.mod остаётся пуристским).
- Пакет: `internal/identity/webauthn.go` (+ webauthn_test.go с векторами).
- Тест-векторы: сгенерировать фиксированные assertion-байты хелпером на
  cose/ecdsa в тестах (без браузера); плюс один записанный реальный вектор
  из Chrome (файл testdata, получен вручную, помечен источником).

## 7. Приёмочные тесты (фиксированы)

1. Round-trip: register → sign → verify (юнит, синтетический вектор ES256).
2. Реальный Chrome-вектор из testdata проходит верификацию.
3. Подмена любого поля canonical (task_id, grant, head_seq, node_id) →
   challenge mismatch → reject.
4. Неверный origin / rpIdHash / отсутствие UP → reject (по одному тесту).
5. sign_count-регресс → warning-событие, решение проходит.
6. Revoked credential → reject.
7. Enforcement: токен-decide людей → SIGNATURE_REQUIRED (REST и MCP);
   без флага — прежнее поведение (golden-совместимость, I-1).
8. `kalita verify` на pg_dump-копии чужой машиной: подписи валидируются
   офлайн; `--require-signed-approvals` ловит вставленный токен-approve.
9. RS256-вектор проходит; ES384/EdDSA → reject "unsupported alg".

## 8. PR-нарезка

PR-1: события + fold + проекция credential (+тест-скелеты).
PR-2: webauthn.go верификация + векторы (тесты 1–6, 9). ← ревью Fable
PR-3: REST-потоки регистрации/challenge/decide + UI (Inbox.decide) + SDK.
PR-4: enforcement-флаг + MCP-отказ + verify-расширение (тесты 7, 8).

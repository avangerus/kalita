# Execution API contracts — LLD

**Статус:** предлагаемый внешний контракт; маршруты `/api/next/...` ещё не следует считать существующими.  
**Связанные документы:** `EXECUTION.md`, `AUTHORITY_SECURITY.md`, `WORK_MODEL.md`, `INTEGRATIONS.md`, `EXPERIENCE.md`.

## 1. Совместимость и представление данных

Контракт добавляется отдельным namespace `/api/next`. Существующие `/api/tasks`, workflow и MCP tools остаются legacy compatibility surface до миграции и не переименовываются неявно. Для каждого legacy метода документируется точное отображение: прямой вызов, read-адаптер с прежним response shape или write-адаптер с обязательным новым fencing protocol или unsupported с machine-readable error. На deployment только один writer управляет конкретной очередью; старый и новый dispatcher нельзя одновременно активировать.

JSON envelope содержит `api_version`, `request_id`, `actor`, `data`, `basis` и `extensions`; `actor` формируется сервером и игнорируется из body при попытке подмены. Даты — RFC 3339 UTC; деньги и точные десятичные величины — decimal string + currency/unit; enum — стабильные lowercase identifiers; неизвестное поле либо отклоняется в command, либо допускается только в явно версионированном `extensions`. Payload canonicalized до хеширования для idempotency. Сервер возвращает correlation IDs и не сообщает секреты/неавторизованные поля через ошибки.

Изменения уже существующего объекта используют `If-Match` с `version`/ETag. Повторяемые create/dispatch операции требуют `Idempotency-Key`; ключ ограничен actor/tenant и scope, хранится не меньше максимального периода retry и привязан к digest. Один ключ и один digest возвращают исходный результат; другой digest — `409 idempotency_conflict`. `If-Match` защищает от устаревшей записи, idempotency — от дубликата доставки; это разные гарантии.

Ошибки имеют стабильный объект `{code, message, retryable, correlation_id, details?}`. `details` ограничен полями, которые caller вправе видеть. Типовые коды: `unauthenticated`, `forbidden`, `not_found_or_forbidden`, `validation_failed`, `precondition_failed`, `idempotency_conflict`, `stale_lease`, `budget_exhausted`, `policy_changed`, `effect_unknown`, `contract_version_unsupported`, `rate_limited`. Клиент не должен трактовать любой 5xx как безопасное разрешение повторить внешний side effect.

## 2. Run commands

Предлагаемые маршруты:

```text
POST /api/next/runs
GET  /api/next/runs/{run_id}
GET  /api/next/runs?object_ref=...&state=...&cursor=...
POST /api/next/runs/{run_id}:cancel
POST /api/next/runs/{run_id}:rebase
GET  /api/next/runs/{run_id}/timeline
```

`POST /runs` принимает `workflow_ref`, `object_ref`, typed `input`, `definition_version`, `schema_version`, `purpose`, optional `parent_run_ref`, deadline и idempotency key в header. Клиент может предложить workflow/version, но server revalidates доступ и доступность именно этой версии. Некорректное или устаревшее условие возвращает conflict, не запускает «похожую» новую версию. Ответ: `run_id`, accepted contract refs, state, created event ref, `Location` и текущая `version`.

`GET` фильтрует каждую строку по текущим правам; знание run ID не раскрывает существование или PII. Timeline — составное представление run, attempts, approvals и effects, но каждый компонент проходит собственную authorization и field mask. Cursor подписан/неподменяем и включает sort key; пагинация ограничена. Cancel имеет `If-Match`, reason code и необязательный user text с лимитом. Он возвращает `cancel_requested`/`cancelled`, а не обещает undo внешнего эффекта.

Rebase — отдельная команда, не PUT версии существующего run. Она требует policy-specific eligibility, указывает старый и новый definition digest, сохраняет origin run, формирует migration basis и может потребовать approval. Если нет безопасного перехода, возвращается `409 rebase_not_supported`; оператор/владелец запускает новый run с явной связью.

## 3. Task execution protocol

Адаптерный API для доверенного worker/broker изолируется от пользовательского REST scope. Worker authentication даёт право только на назначенную очередь и операции, выданные capability. Сетевой ingress к этому endpoint — отдельная политика; вызовы не должны попадать в browser session API.

```text
POST /api/next/worker/claims
POST /api/next/attempts/{attempt_id}:heartbeat
POST /api/next/attempts/{attempt_id}:complete
POST /api/next/attempts/{attempt_id}:fail
```

Claim request задаёт `queue`, `worker_profile`, `max_items` (малый верхний предел), supported `contract_versions`, optional wait duration. Нельзя передать actor или scopes. Response включает непрозрачный `lease_token`, `fence`, `leased_until`, `input_ref`, policy/definition/contract refs, минимальный typed context, capability handle и `attempt_id`. Capability привязан к точным args/context digest и истекает не позже lease/короткого максимального TTL. Нельзя вернуть credentials из connector.

Heartbeat продлевает только существующий незавершённый lease, в пределах policy max lease duration. Прогресс — ограниченное число и размер безопасных структурированных полей. Complete содержит output по output schema, outcome enum, references на effect receipts, basis/observations и lease binding. Broker проверяет fence, schema, authority и state transition; worker не может сам объявить `effect.succeeded` без валидного receipt/reconciliation. Fail несёт ограниченный code и безопасную классификацию; stack trace/LLM transcript не принимается в audit payload.

Для команды, которая потеряла ответ на completion, worker может повторить ровно тот же request с idempotency key, привязанным к attempt и payload digest. Повтор с отличным body конфликтует. После lease expiry новая попытка получает новый ID и fence; прежний worker не сможет обновить run. Семантика idempotency worker API не распространяется на удалённый connector.

## 4. Grants, approval и revocation

```text
POST /api/next/runs/{run_id}/grants
GET  /api/next/grants/{grant_id}
POST /api/next/grants/{grant_id}:revoke
POST /api/next/effects/{effect_id}:approve
POST /api/next/effects/{effect_id}:reconcile
```

Grant command адресуется полномочным company actor, требует `If-Match` parent authority/policy version и задаёт recipient, operation/resource allowlists, purpose, expiry, budget ceilings, child depth/count и передачи полномочий. Расширение scope относительно issuer grant отклоняется даже если заявитель — владелец parent run; отдельная company policy edge может выдать новое полномочие как самостоятельное решение. Ответ возвращает ограничения и digest, не credential.

Revocation идемпотентна и пишет audit event/outbox в одной транзакции с обновлением grant state. Она запрещает будущие claims/reads/dispatch, но не заявляет отмену уже совершённых эффектов. Подписки/broker cache инвалидируются; контрольная точка перед dispatch всё равно обращается к актуальному решению/версии revocation. Если система временно не может подтвердить свежую authority для side effect, действует fail closed.

Approval endpoint принимает challenge/nonce, typed human decision, `effect_digest`, amount/recipient/resource limits, срок и доказательство подтверждения. Server выводит identity из сессии и проверяет human-only policy/роль/signature как требуется. Нельзя прислать `actor_type=human` в JSON и получить human authority. Повторный nonce, истёкший challenge или изменённый digest возвращает конфликт. Reconcile принимает один из `confirmed_succeeded`, `confirmed_not_applied`, `still_unknown`, ссылку на источник проверки, operator/reviewer basis и при необходимости второе подтверждение; executor не может самостоятельно подтверждать спорный внешний эффект.

## 5. Effects и receipts

```text
POST /api/next/effects/{effect_id}:dispatch
GET  /api/next/effects/{effect_id}
GET  /api/next/effects/{effect_id}/receipt
```

Публичный orchestration API не принимает произвольный URL и произвольные headers. `effect_id` создаётся вместе с durable intent; connector binding выбирается из разрешённого integration registry. Перед dispatch broker проверяет текущую authority, risk policy, approval digest, бюджет, cancellation state и retry eligibility. Только затем он получает secret reference и делает вызов. Запись intent-before-effect позволяет пережить падение между подготовкой и dispatch; после ответа receipt завершает эффект. При падении в промежутке состояние `unknown` требует endpoint lookup, idempotency retry или ручного reconcile.

Receipt содержит `delivery_id`, connector/version, request digest, безопасный endpoint reference, время отправки/ответа, удалённый receipt ID, статус, outcome digest и hash/redaction status. Он не хранит secret, полный URL с query token, полный request body или PII сверх необходимых ссылок. Политика retention может отделить закрытый encrypted payload от append-only метаданных, но hash без сохранённых доказательств не должен называться подтверждением факта.

Повторная доставка по `/dispatch` возвращает существующее состояние и не должна заново отправлять эффект сама по себе. Явная retry command допустима только для доказанно retry-safe connector и включает новый attempt, сохраняя тот же logical `delivery_id`/idempotency key по контракту endpoint. Когда сервер не знает, сохранил ли удалённый endpoint ключ, автоматическое повторение запрещено.

## 6. Read/query и видимость

Все read API (`GET`, search, dashboard агрегаты, event timeline, logs, MCP resources) применяют одну и ту же actor/tenant/object/field authorization. Проверка по коллекции должна происходить до подсчёта и выдачи агрегата, иначе число раскрывает скрытые записи. Unauthorized и not found можно свести к одной ошибке там, где это нужно для защиты существования объекта. `source_ref`/`view_ref` и `method_ref` сохраняют provenance и версию; они не являются bearer capability и не обходят проверку.

Контекстный API для broker принимает точный список refs/fields и purpose. В ответе для каждого элемента содержатся доступные typed fields, source/view/method reference, observed-at, schema version и решение доступа. Недоступное поле либо опускается с явным redaction marker, либо целиком отказывает согласно policy; частичный ответ не должен выглядеть как пустое значение. Search snippets, embeddings, cache и derived views подчиняются исходным правам и invalidation. Отзыв доступа очищает/пересчитывает кеши; один раз переданный executor-у контекст уже нельзя отозвать, поэтому минимизация действует до передачи.

REST и MCP — разные transports к одному typed service contract. Для MCP не создаются параллельные слабее защищённые методы. Схемы запросов/ответов, enums, authorization hooks и error codes должны генерироваться из общего contract definition или сверяться contract tests; функциональная parity включает скрытие полей, idempotency, preconditions, revocation и effect unknown. UI-агрегаты не обращаются к сырым execution tables напрямую.

## 7. Версии и миграция клиентов

`/api/next` версионируется независимо от DSL version и connector contract version. Breaking changes требуют новой API version либо периода dual response с явной deprecation даты. Unknown enum у старого клиента не превращается в `succeeded`; он отображается как unsupported. Расширение schema выполняется additive: новые optional fields, новые стабильные refs, миграция данных с dual-read; destructive removal — только после завершения активных run, retention window и compatibility проверки.

Legacy façade сохраняет прежние read responses. Claims/completions требуют нового протокола с Attempt ID/token/fence: façade не синтезирует token по actor ID. Старые workers дренируются и перестают писать до cutover; неизвестные active outcomes идут в blocked/reconcile. Прозрачная write compatibility без fencing запрещена. Фасад не должен создавать отдельную очередь или повторно проверять права по более слабому правилу. Сначала новую модель включают shadow-read/diagnostic, затем импортируют и сверяют active tasks, затем один feature flag переключает writer. На время rollback новые записи остаются доступными новому reader; выключение флага не означает удаление execution state.

### Примеры contract-level проверок

- Идемпотентный `POST /runs` возвращает тот же ID только при идентичном canonical body.
- Изменение `If-Match`-защищённого объекта после чтения приводит к `412`, не к перезаписи.
- Устаревший lease completion отвечает `stale_lease` и не меняет бизнес-событие.
- Изменённый recipient после approval приводит к `policy_changed`/digest mismatch до dispatch.
- Run ID без права чтения не раскрывает summary, owner, timeline, count или existence-sensitive error.
- Повтор effect dispatch не пересылает неизвестный внешний запрос автоматически.
- REST и MCP одинаково применяют field mask и grant revocation.

Этот LLD задаёт целевую форму API. До реализации маршруты, поля и гарантии не являются поддерживаемым продуктовым контрактом.

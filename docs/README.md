# docs/ — карта документации kalita

Обновлено: 2026-10-28. Одна тема — один документ; устаревшее удаляется, а не копится рядом.

## С чего начать

| Док | Зачем |
|---|---|
| [STATUS.md](STATUS.md) | **Текущее состояние**: версия, что работает, известные ограничения, открытые решения |
| [DEVELOPMENT.md](DEVELOPMENT.md) | Как собрать, запустить, протестировать; правила работы в репозитории |
| [next/README.md](next/README.md) | **Целевая архитектура N1 и план развития** (R0–R7, I00–I15, 80 задач) |

Правило авторитета. Для **новой разработки** действует N1: [продуктовый контракт](next/PRODUCT_CONTRACT.md) →
[HLD N1](next/HLD.md) → [N-ADR](next/DECISIONS.md) → LLD N1 → [delivery](next/delivery/ROADMAP.md).
Для **существующего** поведения (грамматика DSL, API, формат журнала, выпущенные релизы) действуют
справочные документы ниже. Где N-ADR явно заменяет прежнее правило, прежний документ помечен notice.

## Справочник: продукт как он есть

**Принципы и архитектура**

| Док | Суть |
|---|---|
| [HLD.md](HLD.md) | Конституция платформы: 7 принципов (spec→runtime, event-sourcing, подпись, agent=сотрудник, on-prem) |
| [HLD-V1.md](HLD-V1.md) | Расширение: инварианты I-1..I-6 и подсистемы релиза 1.0 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Движок на одной странице: команда→факт, двое ворот, admission gate |
| [SYSTEM_TRUTH.md](SYSTEM_TRUTH.md) | Модель существующей системы по коду: компоненты, потоки выполнения, матрица DSL |
| [DECISIONS.md](DECISIONS.md) | Журнал решений D-xxx по реализованному продукту + открытые вопросы |
| [adr/](adr/) | ADR-001 проекции в RAM · 002 tiered storage (не реализовано, фальсифицируемо) · 003 UI-SDK · 004 слои фронтенда · 005 индексы permitted-set |

**Язык**

| Док | Суть |
|---|---|
| [LANGUAGE.md](LANGUAGE.md) | Канон языка (DSL v0.9): словарь, граница язык/код, ревью-гейт |
| [DSL-SPEC-v0.md](DSL-SPEC-v0.md) | Грамматика текущего парсера |
| [DSL-CORE-LIB.md](DSL-CORE-LIB.md) | `dsl.core.*`: гейт допуска и состав (частично реализовано: Calendar, mdg, миксины) |
| [PLATFORM-STDLIB.md](PLATFORM-STDLIB.md) | Что платформа даёт из коробки (частично: core.reference, Department, core.notify — нет) |

**Журнал, интерфейсы, безопасность, эксплуатация**

| Док | Суть |
|---|---|
| [EVENT-STORE-v0.md](EVENT-STORE-v0.md) | Append-only журнал, hash-chain, детерминированный replay, чекпойнты |
| [MCP-CONTRACT-v1.md](MCP-CONTRACT-v1.md) | MCP-инструменты — **генерируется** `kalita mcp contract`, гейт `TestMcpContractMatchesCode` |
| [SECURITY.md](SECURITY.md) | Модель угроз T1–T6, что держим и что нет |
| [THREAT_MODEL.md](THREAT_MODEL.md) | Состязательный разбор 24 поверхностей атаки (Milestone G) |
| [OPERATIONS.md](OPERATIONS.md) | Runbook оператора: verify, backup, upgrade, секреты |
| [runbooks/](runbooks/README.md) | Day-2 процедуры по инцидентам |
| [ACCEPTANCE.md](ACCEPTANCE.md) | Приёмка релиза «с нуля» на PG (20 шагов + сценарии egress A–H) |
| [TECHDEBT.md](TECHDEBT.md) | Живой реестр технического долга P0–P3 |

**Self-building продукт (Ask Kalita / builder)**

| Док | Суть |
|---|---|
| [builder/ARCHITECTURE.md](builder/ARCHITECTURE.md) | Четыре слоя знания, сквозной product loop, AI safety boundary |
| [builder/CONVERSATIONAL_BUILDER.md](builder/CONVERSATIONAL_BUILDER.md) | DesignSession и протокол разговора → proposal → подпись |
| [builder/BUSINESS_FRAMEWORKS.md](builder/BUSINESS_FRAMEWORKS.md) | Curated бизнес-фреймворки (`frameworks/*.json`), правила доверия |
| [builder/UX.md](builder/UX.md) | UX веб-оболочки и Error UX |

Документация паков — рядом с паком (например, [packs/knowvault/README.md](../packs/knowvault/README.md)).

### LLD — механика (статус по коду на 2026-10-28)

| LLD | Суть | Статус |
|---|---|---|
| [01](lld/LLD-01-journal-mechanics.md) | Журнальная механика: schema_v, op-скобки, ошибка-событие | реализовано |
| [02](lld/LLD-02-webauthn.md) | WebAuthn/passkey-подпись | реализовано |
| [03](lld/LLD-03-upgrade-eras.md) | Upgrade, эры, golden replay, защита от downgrade | реализовано (cosign keyless, D-022) |
| [05](lld/LLD-05-product-kit-ui.md) | Product Kit: atoms→faces (`packages/kalita-ui`) | реализовано |
| [06](lld/LLD-06-product-mcp-chat.md) | Продуктовый MCP-façade + чат | реализовано |
| [07](lld/LLD-07-federation-bridge.md) | Федерация через bridge-воркер | реализовано на уровне воркера |
| [08](lld/LLD-08-journal-outbox-read.md) | `read_events`, verb `tail` | реализовано |
| [09](lld/LLD-09-deployment-product.md) | Shadow-режим, отчёт «неделя в цифрах», egress-отчёт | реализовано |
| [10](lld/LLD-10-identity-hardening.md) | Атомарная регистрация, lockdown, rate-limit | реализовано |
| [11](lld/LLD-11-transport-parity.md) | Паритет REST/MCP, автоген контракта | реализовано |
| [12](lld/LLD-12-node-passport-hotpath.md) | Паспорт узла, auth O(1), replay-бюджет | реализовано |
| [15](lld/LLD-15-component-catalog-v1.md) | Каталог UI-компонентов v1 | частично (нет KTable, Drawer, Tabs, Pagination, geo…) |
| [16](lld/LLD-16-crypto-shredding-erasure.md) | Право на забвение: crypto-shredding | реализовано |
| [17](lld/LLD-17-backup-dr-anchor.md) | Backup / DR / внешний якорь | частично (нет DEK/KEK-конверта, `verify --anchor`) |
| [18](lld/LLD-18-secrets-key-rotation.md) | Секреты, ротация ключей | частично (нет эпох ключа узла) |
| [20](lld/LLD-20-field-scoped-update-perms.md) | Field-scoped update: двое ворот прав | реализовано |
| [21](lld/LLD-21-durable-event-subscriptions.md) | Надёжные подписки: at-least-once, DLQ | частично (библиотека воркера; паки не объявляют subscription) |
| [22](lld/LLD-22-field-mixins-include.md) | `include`: миксины полей | реализовано |

## Целевая архитектура N1 — [next/](next/README.md)

Продуктовый контракт, HLD, решения N-ADR, LLD N1 (work model, agent protocol, execution, authority,
API, integrations, views, experience, operations), starter pack Client Delivery и delivery-план.
Основания — исследования [выбора starter pack](research/STARTER_PACK_SELECTION.md) и [сценариев 2036](research/future-2036/README.md) (гипотезы, не подтверждение рынка). Все задачи N1 в статусе `planned`;
структурная проверка — `node docs/next/delivery/validate.cjs`.

## История

Удалённые при уборке 2026-10-28 документы — старые планы (ROADMAP, RELEASE-1.0, V1-GATE,
PROD-READINESS, BACKLOG-MVP…), нереализованные LLD (04 WASM, 13 data-pack, 14 UI-as-data,
19 слои паков), MCP-CONTRACT-v0, журналы циклов и прогонов `docs/forge/` — остаются в истории git.
На них ссылаются отдельные комментарии в коде и справочные документы (как на происхождение решения); открыть: `git show 5447fc5:docs/<путь>`
(например, `git show 5447fc5:docs/RELEASE-1.0.md`). Доказательства релизов — в тегах
(`v1.0.0-rc.2`) и коммитах, указанных в [STATUS.md](STATUS.md).

Стратегические и коммерческие документы в этот публичный репозиторий не входят.

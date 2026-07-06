# docs/ — карта проекта kalita

Индекс всех документов. Обновлён: 2026-07-05.

## Как читать (порядок входа)

1. [HLD.md](HLD.md) — конституция платформы (7 принципов, on-prem, agent=сотрудник).
2. [ARCHITECTURE.md](ARCHITECTURE.md) — якорь движка одной страницей (команда→факт, choke point).
3. [LANGUAGE.md](LANGUAGE.md) — **канон языка** (DSL v0.9: словарь, граница язык/код, ревью-гейт).
4. [PLATFORM-STDLIB.md](PLATFORM-STDLIB.md) — что платформа даёт из коробки.

## Иерархия канона (правило конфликта)

**HLD/HLD-V1 → ARCHITECTURE → LANGUAGE → DSL-CORE-LIB/PLATFORM-STDLIB → LLD → ADR.**
Если нижний документ противоречит верхнему — прав верхний. Язык — авторитет
`LANGUAGE.md`; движок — `ARCHITECTURE.md`; принципы платформы — `HLD*`.

---

## Якоря и конституция

| Док | Суть |
|---|---|
| [HLD.md](HLD.md) | Конституция: 7 принципов (spec→runtime, Lego, event-sourcing, подпись, agent=сотрудник, think-big/commit-small, on-prem) |
| [HLD-V1.md](HLD-V1.md) | Расширение: инварианты I-1..I-6 + подсистемы (не заменяет HLD.md) |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Якорь движка: команда→факт, двое ворот, base/extended, admission gate |

## Язык и стандартная библиотека

| Док | Суть |
|---|---|
| [LANGUAGE.md](LANGUAGE.md) | **Канон языка v0.9**: инвариант читаемости/доказуемости/обратимости, лексикон, граница язык/код, агенты, тиры |
| [DSL-CORE-LIB.md](DSL-CORE-LIB.md) | Устав `dsl.core.*`: гейт допуска в core, состав по namespace |
| [PLATFORM-STDLIB.md](PLATFORM-STDLIB.md) | Батарейки: скелет из коробки, приоритет A/B/C/D, keystone=миксины |
| [TYPE-SYSTEM-V1.md](TYPE-SYSTEM-V1.md) | (planning) расширения типов — привязать к LANGUAGE §5 по триггеру |
| [DSL-SPEC-v0.md](DSL-SPEC-v0.md) | Грамматика текущего парсера (frozen); цель — LANGUAGE.md |

## Спеки-основания (day-1, необратимое)

| Док | Суть |
|---|---|
| [EVENT-STORE-v0.md](EVENT-STORE-v0.md) | Append-only, hash-chain, детерминированный replay, чекпойнты |
| [SECURITY.md](SECURITY.md) | Threat model T1–T6, что держим/не держим, P0–P2 |
| [MCP-CONTRACT-v0.md](MCP-CONTRACT-v0.md) | MCP: no anonymous agents, всё через права, всё в журнал |
| [MCP-CONTRACT-v1.md](MCP-CONTRACT-v1.md) | Сгенерирован `kalita mcp contract` из `internal/mcp/tools.go` (LLD-11 §3) — счётчик/имена тулов гейтятся `TestMcpContractMatchesCode`, не пишутся руками |

## LLD (детали механики — реализация Sonnet по спеке, ревью Fable на крипте/правах)

| LLD | Суть |
|---|---|
| [01](lld/LLD-01-journal-mechanics.md) | Журнальная механика: schema_v, новые kinds, ошибка-событие |
| [02](lld/LLD-02-webauthn.md) | WebAuthn/passkey |
| [03](lld/LLD-03-upgrade-eras.md) | Upgrade, эры, golden replay, защита от downgrade |
| [04](lld/LLD-04-wasm-abi.md) | WASM escape hatch (чистое вычисление) |
| [05](lld/LLD-05-product-kit-ui.md) | Product Kit: дизайн-система atoms→faces |
| [06](lld/LLD-06-product-mcp-chat.md) | Продуктовый MCP-façade + KChat, pass-through auth |
| [07](lld/LLD-07-federation-bridge.md) | Федерация: bridge, async, exactly-once |
| [08](lld/LLD-08-journal-outbox-read.md) | `read_events` outbox, verb `tail` |
| [09](lld/LLD-09-deployment-product.md) | Shadow-режим, «неделя в цифрах», egress-отчёт |
| [10](lld/LLD-10-identity-hardening.md) | Атомарная регистрация, lockdown, rate-limit |
| [11](lld/LLD-11-transport-parity.md) | Паритет REST/MCP, автоген контракта |
| [12](lld/LLD-12-node-passport-hotpath.md) | Паспорт узла, auth O(1), replay-бюджет |
| [13](lld/LLD-13-data-pack-format.md) | Data-pack: справочник = seed-записи |
| [14](lld/LLD-14-ui-as-data-embed.md) | UI-как-данные (I-6), zero HTML от агента |
| [15](lld/LLD-15-component-catalog-v1.md) | Каталог компонентов v1 |
| [16](lld/LLD-16-crypto-shredding-erasure.md) | Право на забвение: crypto-shredding |
| [17](lld/LLD-17-backup-dr-anchor.md) | Backup/DR/внешний якорь |
| [18](lld/LLD-18-secrets-key-rotation.md) | Секреты, ротация ключей по эпохам |
| [19](lld/LLD-19-pack-layering-presets.md) | Слоёвость паков L1/L2/L3, оверлей переживает апгрейд |
| [20](lld/LLD-20-field-scoped-update-perms.md) | Field-scoped update: двое ворот прав |
| [21](lld/LLD-21-durable-event-subscriptions.md) | Надёжные подписки: at-least-once, DLQ |

## ADR (зафиксированные решения)

| ADR | Суть |
|---|---|
| [001](adr/ADR-001-in-memory-projections.md) | Проекции в RAM, журнал в PG (не трогать без ADR-002) |
| [002](adr/ADR-002-tiered-storage.md) | Tiered storage при масштабе (фальсифицируемо) |
| [003](adr/ADR-003-ui-sdk.md) | UI-SDK: HTTP-контракт, @kalita/sdk, no build |
| [004](adr/ADR-004-frontend-layers.md) | Три слоя: Backend/Frontend/Product (WordPress-модель) |
| [005](adr/ADR-005-permitted-set-index.md) | Вторичные индексы для row-level ABAC |

## Планирование и готовность

| Док | Суть |
|---|---|
| [ROADMAP.md](ROADMAP.md) | Принцип L0→L1→L2, гэпы паков текут в ядро |
| [ROADMAP-TREE.md](ROADMAP-TREE.md) | Дерево фич v0.2→v1.0 |
| [RELEASE-1.0.md](RELEASE-1.0.md) | Инженерный проект: точки напряжения N1–N11 |
| [V1-GATE.md](V1-GATE.md) | Определение 1.0 + блокеры |
| [PROD-READINESS.md](PROD-READINESS.md) | Day-2: O1–O10 (забвение, DR, наблюдаемость…) |
| [APPS-GAP-PLAN.md](APPS-GAP-PLAN.md) | Гэпы ядра под HR/CRM/ServiceDesk |
| [DESIGN-REVIEW-01-simplify-extend.md](DESIGN-REVIEW-01-simplify-extend.md) | Самокритика: упрощения + стресс-тест системами |
| [DEMO-0-legal-loop.md](DEMO-0-legal-loop.md) | Минимальный governed-цикл Legal |
| [TECHDEBT.md](TECHDEBT.md) | Живой реестр долга P0–P3 |
| [FABLE-PLAN.md](FABLE-PLAN.md) | Реконструкция Fable-работы (контекст) |

## Стратегия (продукт/vision)

| Док | Суть |
|---|---|
| [PORTAL-VISION.md](PORTAL-VISION.md) | Портал=DSL (гарантии) vs витрина=кастом (свобода) |
| [KNOWVAULT-INTEGRATION.md](KNOWVAULT-INTEGRATION.md) | KnowVault как пак на ядре — доказательство архитектуры |

## Историческое (контекст, не актуальный план)

| Док | Причина |
|---|---|
| [BACKLOG-MVP.md](BACKLOG-MVP.md) | 8-недельный бэклог стройки, выполнен |
| [DSL-SPEC-v0.md](DSL-SPEC-v0.md) | Грамматика текущего парсера; цель — LANGUAGE.md |

---

*Стратегические/коммерческие доки (MASTERPLAN, products/, GTM) — в `agent/strategy/`
(gitignored), НЕ в публичном репо. Здесь — только технический design-of-record.*

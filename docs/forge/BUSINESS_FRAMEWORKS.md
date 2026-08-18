# BUSINESS FRAMEWORKS — curated business expertise layer

> Слой 2 из PRODUCT_ARCHITECTURE.md: curated reusable business expertise.
> Не Go-код индустрии в kernel, не LLM-знания, не company policy.
> Фреймворк — статический, версионированный, целостно-верифицируемый источник
> знаний, который Expert использует в разговоре. Обновляется релизом, никогда —
> runtime'ом и никогда — самой LLM.

## 1. Архитектура слоя

```
frameworks/
  manifest.json      # identity/версии/origin/sha256/compatibility всех фреймворков
  ap.json            # Accounts Payable / Invoice Approval
  itsm.json          # IT Service Management
  crm.json           # Sales CRM
```

Загрузка: worker builder при старте читает manifest, для каждого файла сверяет
sha256, загружает, держит read-only. Рассинхрон manifest↔файл = отказ запуска
builder'а с понятной ошибкой (не тихий пропуск).

## 2. Формат фреймворка (schema v1)

```json
{
  "meta": {
    "id": "ap",
    "name": "Accounts Payable / Invoice Approval",
    "version": "1.0.0",
    "origin": "kalita/frameworks (curated by Forge, reviewed Architect+Security Pro)",
    "compatibility": { "builder_protocol": ">=1.0", "dsl_features": ["approval_ladder","agent_role","events","dashboards"] },
    "supersedes": []
  },
  "vocabulary":    [ {"term":"Invoice","meaning":"счёт поставщика к оплате"} ],
  "entities":      [ {"name":"Invoice","kind":"core","fields":[...],"notes":"..."} ],
  "roles":         [ {"name":"APClerk","human_or_agent":"human"}, {"name":"APAgent","human_or_agent":"agent"} ],
  "process_patterns": [ {"id":"intake_validate_approve_pay","stages":[...]} ],
  "segregation_of_duties": [ {"rule":"роль, вводящая счёт, не утверждает его"} ],
  "control_points": [ {"point":"duplicate check","what":"vendor+number+date unique"} ],
  "kpis":          [ {"id":"cycle_time","formula":"days from intake to payment handoff"} ],
  "risks":         [ {"id":"duplicate_payment","likelihood":"medium","impact":"high"} ],
  "discovery": {
    "required":  [ {"id":"approval_authority","question":"Кто утверждает счета?","why":"без этого нельзя построить ladder"} ],
    "important": [ {"id":"amount_thresholds","question":"Есть ли пороги по сумме?","default":"единое утверждение"} ],
    "optional":  [ {"id":"po_matching","question":"Сверяем с заказами (PO)?","deferrable":true} ]
  },
  "alternatives": [ {"id":"single_approver","when":"малый бизнес","tradeoffs":"..."} ],
  "anti_patterns": [ {"id":"same_person_enters_and_approves","why":"..."} ],
  "regulatory":   [ {"note":"tax/audit considerations","region":"RU","optional":true} ],
  "automations":  [ {"id":"agent_triage","what":"агент проверяет vendor/amount/duplicates"} ],
  "agent_roles":  [ {"name":"APAgent","permitted_actions":["read Invoice","create finding"],"denied":["approve","pay"]} ],
  "approvals":    [ {"ladder":"amount-based","example":"≤100k Manager, ≤1M CFO, >1M CEO"} ],
  "dashboards":   [ {"title":"Open invoices by age","tiles":[...]} ],
  "integrations": [ {"id":"source_system","direction":"inbound"}, {"id":"payment_system","direction":"outbound"} ]
}
```

Правила схемы:
- каждый discovery-вопрос имеет `why` (Expert обязан объяснить, зачем спрашивает);
- `alternatives`/`anti_patterns` несут tradeoffs/причины — Expert не должен
  выдумывать их на лету;
- поле `origin` — честное происхождение (кто курировал, какая ревизия);
- всё это — JSON-данные, читаемые и тестируемые; никакого исполняемого кода.

## 3. Trust model (FRAMEWORK TRUST)

| Свойство | Механизм |
|---|---|
| identity | `meta.id` + `meta.name`; manifest — реестр |
| version | `meta.version` (semver); `supersedes` для цепочек миграций |
| origin | `meta.origin`; изменения — только через git/релиз репо |
| integrity | sha256 в manifest, сверка при каждом старте builder'а |
| compatibility | `meta.compatibility` — builder_protocol и dsl_features; несовместимый фреймворк не используется |

**Правила доверия**:
1. Фреймворк НИКОГДА не меняет действующую компанию. Он — только знание для
   будущих сессий. Обновление фреймворка может ПОРЕКОМЕНДОВАТЬ миграцию
   (Expert сравнивает версию фреймворка, с которой строилась модель, с текущей)
   — сама миграция идёт обычным proposal → подпись.
2. Каждая рекомендация Expert несёт метку provenance: «по фреймворку ap@1.0.0».
   Компания всегда знает, какой фреймворк рекомендовал.
3. Фреймворки не загружаются из сети runtime'ом — только из релизного каталога
   с verified sha256. Supply-chain поверхность = репо + релиз-процесс.
4. Контент фреймворка в промпте — SYSTEM-раздел, маркированный; пользовательский
   текст — DATA-раздел через promptbox. Фреймворк не может «приказывать» LLM
   обойти валидацию (валидация — вне LLM).

## 4. Три стартовых фреймворка (доказательные домены)

### 4.1 `ap` — Accounts Payable / Invoice Approval (вертикальный слайс №1)
- Сущности: Invoice (vendor, number, date, amount, currency, status,
  contract_ref, invoice_file), Vendor, Contract (для сверки), PaymentHandoff.
- Паттерн: intake → agent triage (duplicate/amount/vendor check) → approval
  ladder (amount-based) → payment handoff (событие egress).
- SoD: ввод счёта ≠ утверждение; агент проверяет, не платит.
- Control points: duplicate risk (vendor+number+date), amount thresholds,
  contract existence.
- Риски: дубль-платёж, платёж без договора, изменение суммы после парковки
  (kernel уже перепроверяет guard в Decide — Cycle-003).
- KPI: время от получения до handoff; доля отклонённых агентом.
- Интеграции: source system X (inbound, паттерн inbound.py), payment system Y
  (outbound-событие; доставка — существующий kernel-gap D-011, честно
  помечается при построении).
- Discovery required: кто утверждает; что является «счётом к оплате».
  Important: пороги сумм, сверка с договорами, сверка с PO. Optional: валюты,
  НДС-проверки, переплаты.

### 4.2 `itsm` — IT Service Management (вертикальный слайс №2, generality-proof)
- Сущности: Incident, ServiceRequest, Change, SLAPolicy, Knowledge.
- Паттерны: incident lifecycle (New→Investigating→Identified→Resolved→Closed),
  SLA-эскалация, CAB-гейт для Change, knowledge-фиксация после инцидента.
- SoD: исполнитель change ≠ член CAB; агент назначает/диагностирует, не
  утверждает change.
- Control points: SLA breach (бизнес-календарь), эскалация при stuck, auto-close
  для мониторинговых событий.
- Риски: незаметный SLA-бріч, несанкционированный change, потеря знания.
- KPI: открытые инциденты, SLA breach rate, MTTR.
- Соответствие существующему golden-паку packs/servicedesk (полный цикл уже
  проверен TestDogfoodServiceDeskPack) — фреймворк должен уметь ПРИВЕСТИ к
  модели того же класса.

### 4.3 `crm` — Sales CRM
- Сущности: Lead, Account, Opportunity, PipelineStage, Forecast.
- Паттерны: lead qualification → opportunity creation → pipeline movement →
  ownership (account manager) → forecast rollup; discount approvals.
- SoD: продавец ≠ утверждающий скидку; forecast утверждает руководитель.
- Control points: ownership assignment, discount thresholds, stale-opportunity
  automation (напоминание/эскалация).
- Риски: потерянные лиды, дубли аккаунтов, оптимистичный forecast.
- KPI: pipeline value, conversion rate, forecast accuracy.
- Соответствие: packs/crm — живой референс (Lead-интейк через inbound уже есть).

## 5. Жизненный цикл и миграции

1. **Курирование**: изменение фреймворка = PR в репо → review (Architect +
   Security для discovery-вопросов и SoD-правил) → релиз. Внутри релиза —
   обновление manifest sha256.
2. **Использование**: builder стартует → verify → read-only в памяти.
3. **Миграция компании**: при старте сессии Expert смотрит, с какой версией
   фреймворка связана существующая модель (запись в DesignSession/Company
   Knowledge); если появилась новая версия — может предложить migration
   proposal, но НЕ применяет автоматически.
4. **Отзыв/замена**: `supersedes` + removal из manifest; builder отказывается
   грузить отсутствующий в manifest файл (fail-closed).

## 6. Анти-паттерны (не делать)

- ❌ Фреймворк как Go-пакет внутри kernel (нарушает границу слой 2/4).
- ❌ Runtime-мутация фреймворка из сессии (знания ≠ данные компании).
- ❌ LLM дописывает фреймворк в памяти и использует это как curated (нет
  provenance) — рекомендации не из фреймворка обязаны быть помечены как
  «general knowledge, не curated».
- ❌ Фреймворк подменяет валидацию: всё, что идёт в модель, проходит
  validate_dsl + additive-гейты независимо от источника.
- ❌ Сотни фреймворков до доказательства loop'а на двух доменах (scope).


# Kalita

**Kalita** is a next-generation low-code platform for rapid business application development.
Describe your business system in simple DSL files — entities, workflows, permissions, UI, integrations, reference data, analytics — and Kalita turns them into a working application: REST API, validation, workflows, dashboards, and UI.

## 💡 Why Kalita

* **Everything is text** — entities, processes, UI, rules, and integrations in human-readable DSL.
* **Single source of truth** — API, validation, workflows, and UI metadata stay in sync.
* **Zero boilerplate** — no controllers, validators, or SQL by hand.
* **Hot-reload modules** — plug business domains without downtime.
* **Extensible by design** — add modules, plugins, and integrations on the fly.
* **Analytics-first** — designed for OLAP, BI, and reporting from day one.

---

## ✨ Key Features (final vision)

* **Declarative DSL for everything**

  * **Data model:** entities, fields, types, enums, references, constraints, defaults, computed fields.
  * **UI:** pages, list views, forms, sections, field widgets, actions/buttons, filters, validation messages, navigation, themes.
  * **Workflows:** states, transitions, guards, effects, approvals.
  * **Permissions:** roles, policies, field-level security, sharing rules, record types.
  * **Automation:** events, triggers, scheduled jobs, SLAs/escalations, webhooks, notifications.
  * **Integrations:** REST mappings, message bus, external objects/virtual tables, sync jobs.
  * **Reference data:** directories, trees, versioned data, valid-from/to.
  * **Analytics:** dashboards, metrics, charts, OLAP cubes, reports.
  * **I18n:** labels, picklists, UI text, localization.
  * **DevOps:** packaging, migrations, seed data, environment config.

* **Live REST API** for all entities with instant validation & business rules.

* **UI generation** from DSL and Meta API: list/table pages, filters, forms, actions, and navigation built automatically.

* **Workflow & approvals** defined in plain text.

* **Flexible permissions** (RBAC/ABAC) including field-level and sharing rules.

* **Integration-ready** (webhooks, REST, events, bus, external objects).

* **Analytics** (dashboards, charts, cubes, reports).

---

## 📝 DSL Examples (updated)

### Entities & Constraints

```dsl
entity Project:
    name: string required
    status: enum[Draft, InWork, Closed] default=Draft
    manager_id: ref[core.User] on_delete=set_null
    member_ids: array[ref[core.User]] on_delete=set_null
    company_id: ref[Company] on_delete=restrict

entity ExchangeRate:
    base: ref[Currency] required
    quote: ref[Currency] required
    rate: float required
    date: date required

constraints:
    unique(base, quote, date)
```

### UI: Pages, Lists, Forms, Actions

```dsl
ui Project:
  navigation:
    group: "Projects"
    icon: "FolderKanban"

  list:
    title: "Projects"
    columns: [name, status, manager_id, updated_at]
    default_sort: -updated_at
    filters:
      - status: enum[Draft, InWork, Closed]
      - manager_id: ref[core.User]
    views:
      - name: "My Projects"
        filter: manager_id = $me
        sort: -updated_at

    actions:
      - name: "New"
        type: create
      - name: "Close Selected"
        type: bulk_update
        set: { status: "Closed" }

  form:
    title: "Project"
    sections:
      - "General":
          fields: [name, status, company_id]
      - "Team":
          fields: [manager_id, member_ids]

    actions:
      - name: "Save"
        type: save
      - name: "Archive"
        type: update
        when: status != "Closed"
        set: { status: "Closed" }
```

### Workflow & Approvals

```dsl
workflow Invoice:
  states: [Draft, InApproval, Approved, Paid, Cancelled]
  transitions:
    - [Draft] -> InApproval: submit
    - InApproval -> Approved: approve when has_role("Approver")
    - InApproval -> Draft: reject
    - Approved -> Paid: pay
    - * -> Cancelled: cancel
```

### Permissions (Roles, Policies, Field-level)

```dsl
role Admin:
  allow: [*]  # full access

role Manager:
  allow:
    - Project: [create, update, view]
    - Invoice: [view, approve]
  deny:
    - Invoice: [delete]

policy FieldSecurity:
  entity: Project
  fields:
    - member_ids: read when has_role("Manager")
    - manager_id: write when has_role("Admin")
```

### Automation: Events, Triggers, Schedules, Webhooks

```dsl
automation RemindOverdueInvoices:
  when: Invoice.due_date < today() and Invoice.status != "Paid"
  schedule: daily 09:00
  do:
    - notify: role:Accounting message: "Overdue invoices need attention"
    - webhook:
        url: https://hooks.example.com/slack
        body: { text: "Overdue invoices detected" }
```

### Integrations (REST Mapping / External Objects)

```dsl
integration InvoiceToERP:
  on: Invoice.Approved
  action: POST
  url: https://erp.example.com/api/invoice
  mapping:
    - number: invoiceNumber
    - amount: total

external_object CRMContact:
  source: "crm.rest"
  path: "/api/contacts/{id}"
  fields:
    id: string
    email: string
    account_id: ref[Account]
```

### Reference Data & Trees

```yaml
# reference/enums/currency.yaml
name: Currency
items:
  - code: USD
    name: US Dollar
  - code: EUR
    name: Euro
```

```dsl
entity Account:
  code: string required unique
  name: string required
  parent_id: ref[Account] on_delete=restrict
```

### Reports & Dashboards (future)

```dsl
dashboard PMOverview:
  cards:
    - metric: count(Project where status="InWork")
    - chart:
        type: bar
        query: by month(created_at) measure count(Project)

report InvoicesPerMonth:
  entity: Invoice
  dimensions: [month(issued_at)]
  measures: [sum(amount)]
  filter: status = "Paid"
```

### I18n (labels, picklists)

```dsl
i18n:
  en:
    Project.name: "Name"
    Project.status: "Status"
  de:
    Project.name: "Name"
    Project.status: "Status"
```

---

## 🚀 Getting Started

1. Install dependencies:

   ```bash
   go mod tidy
   ```
2. Run the server:

   ```bash
   go run main.go
   ```
3. Try it out:

   * Meta: `GET /api/meta/entities`
   * List: `GET /api/<module>/<entity>?limit=10&sort=-created_at`
   * Filters: `GET /api/<module>/<entity>?q=...&status__in=Draft,Closed`

---

## 🛣 Roadmap to MVP

### ✅ Already implemented

* Entity DSL with field types: `string`, `int`, `float`, `money`, `bool`, `date`, `datetime`, `enum[...]`, `ref[...]`, `array[...]`.
* Field attributes: `required`, `unique`, `default=...`, `readonly`.
* Composite uniqueness with `constraints.unique(...)`.
* Fully qualified references (`ref[module.Entity]`), arrays of references.
* Delete policies for references: `on_delete=restrict` or `set_null`.
* Validation: required fields, strict types, enum values, uniqueness, reference integrity, readonly/system field protection.
* Optimistic locking with `version` + `ETag` and `If-Match` support.
* Bulk operations: create, update, delete, restore.
* Filtering & search: comparison operators (`__gt`, `__gte`, `__lt`, `__lte`), `in:`, full-text search `q=...`.
* Pagination & multi-field sorting with `X-Total-Count`.
* Meta API for UI auto-generation.

### 📍 Planned before MVP release

* UI generation baseline (lists, forms, actions) from DSL + Meta.
* Finalize parser for fully qualified references (`ref[module.Entity]`).
* Enforce `on_delete` policies in delete operations.
* Admin endpoint for hot-reloading DSL without restart.
* Self-references & tree validation (e.g., `Account.parent_id`).
* Base modules for OLGA (core, finance, project, accounting).
* Test suite covering validation, constraints, references, filters, and UI metadata.

---

> Kalita — **Business logic as text.**
> Models in text — API in life.

---



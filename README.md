# Kalita

**Kalita** is a next-generation low-code platform for rapid business application development.  
You describe your business system using simple DSL files — entities, workflows, permissions, integrations, reference data, analytics — and Kalita instantly turns them into a working application: REST API, validation, workflows, analytics, and UI.

---

## 💡 Why Kalita

- **Everything is text** — entities, processes, rules, integrations are defined in human-readable DSL files.
- **Single source of truth** — backend API, validation, workflows, and UI metadata are always in sync.
- **Zero boilerplate** — no need to write controllers, validators, or SQL manually.
- **Hot-reload modules** — plug in new business domains without downtime.
- **Extensible by design** — add modules, plugins, and integrations on the fly.
- **Analytics-first** — designed for OLAP, BI, and reporting from the start.

---

## ✨ Key Features (final vision)

- **Declarative DSL for everything**: entities, workflows, permissions, reference data, analytics.
- **Live REST API** for all entities, with instant validation and business rules.
- **Workflow & approval engine**: describe processes and state flows in plain text.
- **Flexible permissions**: roles, policies, rules, and conditions in DSL.
- **Reference data**: enums, directories, trees, versioned data, valid-from-to support.
- **Integration-ready**: REST, events, message bus, cross-instance sync.
- **Analytics**: BI, OLAP cubes, and custom reports.
- **Extensible**: add modules, plugins, integrations with no downtime.

---

## 📝 DSL Examples

### Entities
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
````

---

### Workflow

```dsl
workflow Invoice:
    states: [Draft, InApproval, Approved, Paid, Cancelled]
    transitions:
        - [Draft] -> InApproval: submit
        - InApproval -> Approved: approve
        - InApproval -> Draft: reject
        - Approved -> Paid: pay
        - * -> Cancelled: cancel
```

---

### Permissions

```dsl
role Admin:
    allow: [*]  # full access

role Manager:
    allow:
        - Project: [create, update, view]
        - Invoice: [view, approve]
    deny:
        - Invoice: [delete]
```

---

### Reference Data

```yaml
# reference/enums/currency.yaml
name: Currency
items:
  - code: USD
    name: US Dollar
  - code: EUR
    name: Euro
```

**Inline in DSL:**

```dsl
status: enum[Draft, Approved, Paid, Cancelled] default=Draft
currency: ref[Currency]
```

---

### Integrations

```dsl
integration InvoiceToERP:
    on: Invoice.Approved
    action: POST
    url: https://erp.example.com/api/invoice
    mapping:
        - number: invoiceNumber
        - amount: total
```

---

### Events

```dsl
event InvoicePaid:
    when: Invoice.status == "Paid"
    do:
        - notify: "Accounting"
        - trigger: UpdateBalance
```

---

### Reports (future syntax)

```dsl
report InvoicesPerMonth:
    entity: Invoice
    dimensions: [month(issued_at)]
    measures: [sum(amount)]
    filter: status = "Paid"
```

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

* Finalize parser for fully qualified references (`ref[module.Entity]`).
* Implement `on_delete` policy enforcement in delete operations.
* Add admin endpoint for hot-reloading DSL without restart.
* Extend DSL and validation for self-references in tree structures.
* Prepare base modules for OLGA (core, finance, project, accounting).
* Full test suite covering validation, constraints, references, and filters.

---

> Kalita — **Business logic as text.**
> Models in text — API in life.



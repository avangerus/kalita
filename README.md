


# Kalita

**Kalita** is a next-generation low-code platform for rapid business app development.  
Define your entire business system using simple DSL files — entities, workflows, permissions, integrations, reference data, and more.  
Kalita instantly brings your models to life: REST API, validation, workflows, analytics and UI — with zero code.

---

## ✨ Key Features

- **Declarative DSL for everything:** entities, workflows, permissions, reference, analytics
- **Live REST API** for all entities, with instant validation
- **Hot-reload modules:** plug and play business domains
- **Workflow & approval engine:** describe status flows and processes in plain text
- **Flexible permissions:** roles, policies, rules in DSL
- **Reference data:** enums, directories, trees, valid-from-to support (yaml)
- **Integration-ready:** REST, events, message bus, cross-instance sync
- **Analytics-first:** BI, OLAP cubes and custom reports (roadmap)
- **Extensible:** add modules, plugins, integrations with no downtime

---

## 📝 DSL Examples

### **Entities**

```dsl
entity User:
    name: string required
    email: string unique required
    role: enum[Admin, Manager, Employee] default=Employee

entity Project:
    name: string required
    status: enum[Draft, InWork, Closed] default=Draft

entity Invoice:
    number: string unique required
    amount: float required
    status: enum[Draft, Approved, Paid, Cancelled] default=Draft
    issued_at: date
````

---

### **Workflow**

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

### **Permissions (RBAC / Policies)**

```dsl
role Admin:
    allow: [*]  # full access

role Manager:
    allow:
        - Project: [create, update, view]
        - Invoice: [view, approve]
    deny:
        - Invoice: [delete]

role Employee:
    allow:
        - Project: [view]
        - Invoice: [view]
```

---

### **Reference Data (enums, directories, trees)**

**YAML:**

```yaml
# reference/enums/currency.yaml
name: Currency
items:
  - code: RUB
    name: Russian Ruble
  - code: USD
    name: US Dollar
  - code: EUR
    name: Euro
```

**DSL (enum inline):**

```dsl
status: enum[Draft, Approved, Paid, Cancelled] default=Draft
currency: ref[Currency]
```

---

### **Integration (Events, REST, Sync)**

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

### **Events**

```dsl
event InvoicePaid:
    when: Invoice.status == "Paid"
    do:
        - notify: "Accounting"
        - trigger: UpdateBalance
```

---

### **Analytics / Reports (future syntax)**

```dsl
report InvoicesPerMonth:
    entity: Invoice
    dimensions: [month(issued_at)]
    measures: [sum(amount)]
    filter: status = "Paid"
```

---

## 🚦 Roadmap

**Current:**

* ✅ Declarative entities, enums, and validation via DSL
* ✅ Live REST API for all models
* ✅ Modular DSL structure (core, modules)
* ✅ Enum & reference YAML integration
* ✅ Workflow (state machine) DSL
* ✅ Role-based permissions in DSL

**Next:**

* [ ] BI & OLAP cubes, analytics DSL
* [ ] Advanced workflow engine (approvals, chains)
* [ ] Pluggable integrations, event bus
* [ ] UI auto-generation from DSL
* [ ] Hot reload, module marketplace

---

## 🚀 Get Started

1. **Clone the repo:**
   `git clone https://github.com/yourorg/kalita.git`
2. **Describe your business in DSL:**
   Edit `dsl/core/entities.dsl` and add modules in `dsl/modules/`
3. **Run the server:**
   `go run main.go` or `go run ./cmd/server/main.go`
4. **Enjoy:**
   Your REST API, validation, and workflows are live!

---

> Kalita — **Business logic as text.**
> Build powerful, secure enterprise apps at the speed of thought.

---



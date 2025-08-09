# =========================================
# Module OLGA — canonical data model (v1)
# Фокус: сущности и хранение (без UI/WF/OLAP)
# Поддерживаемые типы: string,int,float,bool,date,datetime,
#   enum[...], ref[...], array[T], array[ref[...]]
# Опции: required, unique, default=...
# =========================================

module olga

# ---------- Справочники ----------
entity Company:
    name: string required unique
    code: string unique
    is_active: bool default=true

entity Client:
    name: string required unique
    inn: string
    kpp: string

entity Brand:
    name: string required
    client_id: ref[Client] required
    unique_key: string unique

entity Currency:
    code: enum[RUB, USD, EUR, GBP, CNY] required
    name: string
    symbol: string

entity ExchangeRate:
    base: ref[Currency] required
    quote: ref[Currency] required
    rate: float required
    date: date required

constraints:
    unique(base, quote, date)

entity Unit:
    code: string required unique
    name: string required

# План счетов/категории затрат (дерево)
entity Account:
    code: string required unique
    name: string required
    parent_id: ref[Account]

# Пользователь (используем core.User, но локальные роли можно расширять)
entity Employee:
    user_id: ref[core.User] required
    company_id: ref[Company]
    title: string
    is_active: bool default=true

# ---------- Проекты/брифы ----------
entity Brief:
    project_name: string required
    client_text: string               # текстовое имя (по ТЗ — без пополнения справочника)
    brand_text: string
    description: string
    due_date: date
    attachments: array[string]
    status: enum[Draft, Submitted, Approved, Rejected] required

entity Project:
    name: string required
    code: string unique
    company_id: ref[Company] required on_delete=restrict
    client_id: ref[Client]
    brand_id: ref[Brand]
    manager_id: ref[core.User] on_delete=set_null
    member_ids: array[ref[core.User]] on_delete=set_null
    start_date: date
    end_date: date
    status: enum[Draft, InWork, Closed] default=Draft
    tags: array[string]
    budget: float

# ---------- Сметы ----------
# Иерархия: Estimate -> Blocks -> Sections -> Lines -> Sublinеs
entity Estimate:
    project_id: ref[Project] required
    name: string required
    currency_id: ref[Currency] required
    valid_from: date
    valid_to: date
    status: enum[Draft, ForApproval, Approved, InWork, Editing, Closed, Stopped] required
    version_label: string            # человекочитаемая метка версии
    is_primary: bool default=false

entity EstimateBlock:
    estimate_id: ref[Estimate] required
    name: string required
    order_no: int

entity EstimateSection:
    estimate_id: ref[Estimate] required
    block_id: ref[EstimateBlock]
    parent_id: ref[EstimateSection]  # для вложенных разделов (опционально)
    name: string required
    order_no: int

# Кол-во можно хранить как массив позиций (ед., значение), чтоб не городить отдельную таблицу
entity EstimateLine:
    estimate_id: ref[Estimate] required
    section_id: ref[EstimateSection]
    account_id: ref[Account]         # код из плана счетов
    item: string required
    qty: array[string]               # например: ["1 шт", "8 ч", "2 дн"] — или "3 шт" в одной строке
    unit_cost: float                 # стоимость за условную единицу
    currency_id: ref[Currency] required
    status: enum[Active, Removed] required
    order_no: int

# Подстроки трёх типов (расход/доход/аналитика)
entity Subline:
    line_id: ref[EstimateLine] required
    type: enum[Out, In, An] required
    description: string
    amount: float required
    currency_id: ref[Currency] required

# ---------- Фактические расходы и поступления ----------
entity Expense:
    project_id: ref[Project] required
    estimate_id: ref[Estimate]
    line_id: ref[EstimateLine]
    subline_id: ref[Subline]         # для привязки к Out/An
    supplier: string required
    doc_date: date required
    amount: float required
    currency_id: ref[Currency] required
    status: enum[Draft, ForApproval, Approved, Booked, Rejected] required
    comment: string
    attachment_ids: array[string]

# Внутригрупповые документы: зачисление и расход с IN-подстрок
entity IcoInvoice:
    from_company_id: ref[Company] required
    to_company_id: ref[Company] required
    estimate_id: ref[Estimate] required
    subline_id: ref[Subline]         # как правило, In
    amount: float required
    currency_id: ref[Currency] required
    doc_date: date required
    status: enum[Draft, ForApproval, Posted, Rejected] required

entity IcoExpense:
    from_company_id: ref[Company] required
    to_company_id: ref[Company] required
    estimate_id: ref[Estimate] required
    subline_id: ref[Subline]         # как правило, Out
    amount: float required
    currency_id: ref[Currency] required
    doc_date: date required
    status: enum[Draft, ForApproval, Posted, Rejected] required

# Договорной контур и поступления
entity Contract:
    project_id: ref[Project] required
    client_id: ref[Client] required
    number: string required unique
    date: date required
    currency_id: ref[Currency] required
    amount: float
    status: enum[Draft, Active, Closed, Cancelled] required

entity Invoice:
    project_id: ref[Project] required
    contract_id: ref[Contract]
    number: string required unique
    date: date required
    amount: float required
    currency_id: ref[Currency] required
    status: enum[Draft, Issued, PaidPartial, Paid, Cancelled] required

entity Payment:
    invoice_id: ref[Invoice] required
    date: date required
    amount: float required
    currency_id: ref[Currency] required
    method: enum[Bank, Cash, Other] required

# ---------- Служебные общие сущности ----------
entity Attachment:
    owner_entity: string required     # FQN, например "olga.Estimate"
    owner_id: string required
    file_name: string required
    file_size: int
    mime: string
    uploaded_by: ref[core.User]
    uploaded_at: datetime required

# История статусов (согласования/изменения) — универсально для любых документов
entity StatusHistory:
    owner_entity: string required
    owner_id: string required
    from_status: string
    to_status: string required
    changed_by: ref[core.User] required
    changed_at: datetime required
    comment: string

# Подписи (Approval)
entity Signature:
    owner_entity: string required
    owner_id: string required
    role: enum[Manager, CFO, CEO, Controller, Custom] required
    user_id: ref[core.User]
    decision: enum[Pending, Approved, Rejected] required
    decided_at: datetime
    comment: string

# Версионирование смет (минимальная модель — хранить метку/комментарий)
entity Version:
    owner_entity: string required     # например "olga.Estimate"
    owner_id: string required
    label: string required
    created_by: ref[core.User] required
    created_at: datetime required
    comment: string

# Нумераторы и фин. годы (пригодится для контрактов/счетов)
entity Sequence:
    scope: string required            # напр. "olga.Invoice:2025"
    next: int required
    pattern: string                   # напр. "INV-{yyyy}-{seq}"

entity FiscalYear:
    code: string required unique      # напр. "FY2025"
    start_date: date required
    end_date: date required

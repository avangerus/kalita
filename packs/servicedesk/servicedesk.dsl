# Service Desk (СТП ПУИТ) — функциональное ядро ITSM на kalita.
# Источник: D:/work/tecius/it_puit, HLD 03-domain-model.yaml. Здесь — данные и
# поведение: сущности, state-машины, RBAC по 10 ролям ТЗ (Приложение 3),
# SLA-данные, дашборды. Инфраструктура ТЗ ложится на нативные примитивы kalita:
#   BPMN/Flowable        -> workflow + automation
#   Outbox/Kafka         -> журнал событий (event store)
#   Keycloak/AD RBAC     -> roles + ABAC permissions + identity
#   S3 вложения          -> file-поля
#   Tivoli-приёмник, 1С  -> воркеры-агенты (вне этого пака)

# --- справочники / CMDB ------------------------------------------------------

entity Service "Услуга":
    code: string required unique
    name: string required
    description: text
    category: string
    request_form_schema: json          # схема формы запроса (как в ТЗ JSONB)
    published: bool default=false

entity ConfigItem "Конфигурационная единица":
    external_id: string required unique # ID из 1С
    name: string required
    ci_type: enum[Software, Hardware, ServiceCI, License, Document, Other] default=Other
    status: enum[Active, Retired, Planned] default=Active
    attributes: json
    owner: ref[core.User]
    source: enum[Sync1C, Manual] default=Manual

entity SLAPolicy "Политика SLA":
    name: string required
    applies_to: enum[Incident, ServiceRequest, Change, Ticket] default=Incident
    priority: enum[P1, P2, P3, P4] default=P3
    response_minutes: int
    resolution_minutes: int
    business_calendar: string default="24x7"

# --- проблемы и известные ошибки ---------------------------------------------

entity Problem "Проблема":
    number: serial format="PRB-{year}-{seq:6}"
    title: string required
    description: text
    root_cause: text
    priority: enum[P1, P2, P3, P4] default=P3
    assignee: ref[core.User]
    opened: datetime default=$now
    age_days: int computed = days_since(opened)
    status: enum[New, Investigating, RootCauseIdentified, KnownErrorState, Resolved, Closed] default=New

workflow Problem on status:
    New                 -> Investigating:       investigate_problem label="Расследовать"
    Investigating       -> RootCauseIdentified: identify_rca label="Первопричина найдена"
    RootCauseIdentified -> KnownErrorState:     register_known_error label="В известные ошибки"
    KnownErrorState     -> Resolved:            resolve_problem label="Решить"
    Resolved            -> Closed:              close_problem label="Закрыть"

entity KnownError "Известная ошибка":
    problem: ref[Problem] on_delete=cascade
    symptoms: text
    workaround: text
    permanent_fix: text
    kb_article: ref[KBArticle]
    status: enum[Active, Resolved] default=Active

# --- база знаний (FTS у kalita нативный — search по text/string) --------------

entity KBArticle "Статья базы знаний":
    title: string required
    body: text
    category: string
    tags: array[string]
    author: ref[core.User] default=$me
    updated: datetime default=$now
    status: enum[Draft, Published, Archived] default=Draft

workflow KBArticle on status:
    Draft     -> Published: publish_article requires approval(KbEditor) label="Опубликовать"
    Published -> Archived:  archive_article label="В архив"
    Archived  -> Draft:     revise_article label="Вернуть в черновик"

# --- инциденты ----------------------------------------------------------------

entity Incident "Инцидент":
    number: serial format="INC-{year}-{seq:6}" label="Номер"
    title: string required label="Тема"
    description: text label="Описание"
    priority: enum[P1, P2, P3, P4] default=P3 label="Приоритет"
    impact: enum[High, Medium, Low] default=Medium label="Влияние"
    urgency: enum[High, Medium, Low] default=Medium label="Срочность"
    source: enum[Manual, Tivoli, Email, Portal] default=Manual label="Источник"
    ci: ref[ConfigItem] label="Конфиг-единица"
    problem: ref[Problem] label="Проблема"
    attachments: array[file] label="Вложения"          # скриншоты + логи
    reporter: ref[core.User] default=$me label="Заявитель"
    assignee: ref[core.User] label="Исполнитель"
    sla_policy: ref[SLAPolicy] label="Политика SLA"
    opened: datetime default=$now label="Открыт"
    resolved_at: datetime label="Решён"
    age_days: int computed = days_since(opened) label="Возраст, дн."
    # живой SLA: прошло минут с открытия и сколько минут до нарушения порога
    # из связанной политики (ref-путь sla_policy.resolution_minutes). Отрицательный
    # sla_left = SLA просрочен.
    minutes_open: int computed = minutes_since(opened) label="В работе, мин."
    sla_left:     int computed = sla_policy.resolution_minutes - minutes_since(opened) label="До SLA, мин."
    status: enum[New, Investigating, Identified, Resolved, Closed] default=New label="Статус"

workflow Incident on status:
    New           -> Investigating: investigate assignee=OperatorL2 label="Взять в работу"
    Investigating -> Identified:    identify label="Причина найдена"
    Identified    -> Resolved:      resolve_incident label="Решить"
    Resolved      -> Closed:        close_incident label="Закрыть"
    Resolved      -> Investigating: reopen_incident label="Переоткрыть"
    New           -> Closed:        auto_close when source = Tivoli label="Автозакрытие"

# дубли инцидентов — двунаправленная связь, синхронизируется рантаймом
link Incident -> Incident as duplicates / duplicated_by

# --- запросы на обслуживание из каталога -------------------------------------

entity ServiceRequest "Запрос на обслуживание":
    number: serial format="SR-{year}-{seq:6}"
    service: ref[Service] on_delete=restrict
    requester: ref[core.User] default=$me
    form_data: json
    approval_required: bool default=false
    opened: datetime default=$now
    status: enum[Submitted, ApprovalPending, Approved, Rejected, Fulfilling, Fulfilled, Closed] default=Submitted

workflow ServiceRequest on status:
    Submitted       -> ApprovalPending: require_approval when approval_required = true label="На согласование"
    Submitted       -> Fulfilling:      auto_approve when approval_required = false label="В работу"
    ApprovalPending -> Approved:        approve_request requires approval(Supervisor) label="Согласовать"
    ApprovalPending -> Rejected:        reject_request requires approval(Supervisor) label="Отклонить"
    Approved        -> Fulfilling:      start_fulfillment label="Начать выполнение"
    Fulfilling      -> Fulfilled:       fulfill label="Выполнено"
    Fulfilled       -> Closed:          close_request label="Закрыть"

# --- запросы на изменение (RFC) с согласованием CAB --------------------------

entity Change "Запрос на изменение":
    number: serial format="CHG-{year}-{seq:6}"
    title: string required
    description: text
    change_type: enum[Standard, Normal, Emergency] default=Normal
    risk: enum[Low, Medium, High] default=Medium
    requester: ref[core.User] default=$me
    cab_approved_by: ref[core.User]
    planned_start: datetime
    planned_end: datetime
    affected_ci: array[ref[ConfigItem]]
    status: enum[Draft, Assessment, CabApproval, Approved, Scheduled, Implementing, Review, Closed, Rejected] default=Draft

workflow Change on status:
    Draft        -> Assessment:   submit_change label="На оценку"
    Assessment   -> CabApproval:  request_cab label="На CAB"
    CabApproval  -> Approved:     approve_change requires approval(ChangeManager) label="Согласовать (CAB)"
    CabApproval  -> Rejected:     reject_change requires approval(ChangeManager) label="Отклонить"
    Approved     -> Scheduled:    schedule_change label="Запланировать"
    Scheduled    -> Implementing: implement_change label="Внедрять"
    Implementing -> Review:       complete_change label="Завершить"
    Review       -> Closed:       close_change label="Закрыть"

# --- рабочие задания (наряды) -------------------------------------------------
# ТЗ: полиморфный родитель (parent_type + parent_id). У kalita нет полиморфных
# ссылок (пробел Ж в APPS-GAP-PLAN), поэтому моделируем явными ссылками +
# дискриминатором parent_type. Заполняется одна из ссылок по типу.

entity WorkOrder "Рабочее задание":
    number: serial format="WO-{year}-{seq:6}" label="Номер"
    parent_type: enum[Incident, ServiceRequest, Change, Problem] default=Incident label="Тип источника"
    incident: ref[Incident] label="Инцидент-источник"
    request: ref[ServiceRequest] label="Запрос-источник"
    change: ref[Change] label="Изменение-источник"
    problem: ref[Problem] label="Проблема-источник"
    assignee: ref[core.User] label="Исполнитель"
    due: datetime label="Срок"
    result: text label="Результат"
    status: enum[Created, Assigned, InProgress, Completed, Cancelled] default=Created label="Статус"

workflow WorkOrder on status:
    Created    -> Assigned:   assign_wo label="Назначить"
    Assigned   -> InProgress: start_wo label="Начать"
    InProgress -> Completed:  complete_wo label="Завершить"
    Created    -> Cancelled:  cancel_wo label="Отменить"

# --- роли RBAC (ТЗ Приложение 3) ---------------------------------------------

roles:
    LkpUser
    OperatorL1
    OperatorL2
    Supervisor
    ProblemManager
    ChangeManager
    CatalogManager
    KbEditor
    ReportViewer
    Admin

permissions:
    # Пользователь ЛКП: создаёт запросы, видит только свои обращения
    LkpUser:
        create [ServiceRequest]
        read ServiceRequest where requester = $me
        read Incident where reporter = $me
        read [Service, KBArticle]
    # 1 линия: приём и первичная обработка инцидентов и нарядов
    OperatorL1:
        read   [Incident, ServiceRequest, ConfigItem, Service, KBArticle, WorkOrder]
        create [Incident, WorkOrder]
        update [Incident, WorkOrder]
        act    [investigate, identify, start_fulfillment, fulfill, assign_wo, start_wo, complete_wo, cancel_wo]
    # 2 линия: разбор инцидентов и проблем
    OperatorL2:
        read   [Incident, Problem, ServiceRequest, ConfigItem, WorkOrder, KBArticle]
        create [Incident, WorkOrder, Problem]
        update [Incident, WorkOrder, Problem]
        act    [investigate, identify, resolve_incident, close_incident, reopen_incident, investigate_problem, identify_rca, assign_wo, start_wo, complete_wo]
    # Руководитель/диспетчер: полный контроль очереди, согласование запросов
    Supervisor:
        full    [Incident, ServiceRequest, WorkOrder]
        read    [Problem, Change, ConfigItem, Service, KBArticle, SLAPolicy]
        approve [approve_request, reject_request]
        act     [require_approval, auto_approve, approve_request, reject_request, start_fulfillment, fulfill, close_request, investigate, identify, resolve_incident, close_incident, reopen_incident]
    # Менеджер проблем
    ProblemManager:
        full [Problem, KnownError]
        read [Incident, ConfigItem]
        act  [investigate_problem, identify_rca, register_known_error, resolve_problem, close_problem]
    # Менеджер изменений: ведёт RFC, подписывает CAB
    ChangeManager:
        full    [Change]
        read    [ConfigItem, Incident]
        approve [approve_change, reject_change]
        act     [submit_change, request_cab, approve_change, reject_change, schedule_change, implement_change, complete_change, close_change]
    # Менеджер каталога услуг
    CatalogManager:
        full [Service]
        read [ServiceRequest]
    # Редактор базы знаний: пишет и публикует статьи (публикация — HITL)
    KbEditor:
        full    [KBArticle]
        approve [publish_article]
        act     [publish_article, archive_article, revise_article]
    # Аналитик: только чтение и дашборды
    ReportViewer:
        read [Incident, ServiceRequest, Change, Problem, WorkOrder, ConfigItem, Service, KBArticle, SLAPolicy]
    # Администратор
    Admin:
        full [Service, ConfigItem, SLAPolicy, Incident, ServiceRequest, Change, WorkOrder, Problem, KnownError, KBArticle]
        act  [investigate, identify, resolve_incident, close_incident, reopen_incident, auto_close, require_approval, auto_approve, start_fulfillment, fulfill, close_request, submit_change, request_cab, schedule_change, implement_change, complete_change, close_change, assign_wo, start_wo, complete_wo, cancel_wo, investigate_problem, identify_rca, register_known_error, resolve_problem, close_problem, archive_article, revise_article]

# --- автоматизация: эскалации и уведомления ----------------------------------

automation:
    on create Incident:
        notify email(reporter)
    on stuck Incident in New for 1d:
        escalate_to Supervisor
    on stuck Change in CabApproval for 3d:
        escalate_to ChangeManager

# --- дашборды (сводки по всей таблице, с учётом прав строки) ------------------

dashboard OperatorBoard "Очередь оператора":
    tile "Открытые инциденты":       count Incident where status != Closed and status != Resolved
    tile "Не назначены":             count Incident where assignee = null
    tile "Просрочка SLA":            count Incident where sla_left < 0
    tile "Инциденты по статусу":     count Incident group by status
    tile "Инциденты по приоритету":  count Incident group by priority

dashboard ServiceRequestsBoard "Запросы на обслуживание":
    tile "В работе":         count ServiceRequest where status != Closed
    tile "На согласовании":  count ServiceRequest where status = ApprovalPending
    tile "По статусу":       count ServiceRequest group by status

dashboard ChangesBoard "Изменения":
    tile "Активные RFC":  count Change where status != Closed and status != Rejected
    tile "Ждут CAB":      count Change where status = CabApproval
    tile "По типу":       count Change group by change_type
    tile "По риску":      count Change group by risk

# Service Desk (ITSM Core) — functional kernel for ITSM on kalita.
# Source: D:/work/tecius/it_puit, HLD 03-domain-model.yaml. Here — data and
# behavior: entities, state machines, RBAC per 10 roles (Appendix 3),
# SLA data, dashboards. Infrastructure maps to native kalita primitives:
#   BPMN/Flowable        -> workflow + automation
#   Outbox/Kafka         -> event journal (event store)
#   Keycloak/AD RBAC     -> roles + ABAC permissions + identity
#   S3 attachments       -> file fields
#   Tivoli receiver, 1C  -> worker agents (outside this pack)

# --- reference data / CMDB --------------------------------------------------

entity Service "Service":
    code: string required unique
    name: string required
    description: text
    category: string
    request_form_schema: json          # request form schema (as in spec JSONB)
    published: bool default=false

entity ConfigItem "Configuration Item":
    external_id: string required unique # ID from 1C
    name: string required
    ci_type: enum[Software, Hardware, ServiceCI, License, Document, Other] default=Other
    status: enum[Active, Retired, Planned] default=Active
    attributes: json
    owner: ref[core.User]
    source: enum[Sync1C, Manual] default=Manual

entity SLAPolicy "SLA Policy":
    name: string required
    applies_to: enum[Incident, ServiceRequest, Change, Ticket] default=Incident
    priority: enum[P1, P2, P3, P4] default=P3
    response_minutes: int
    resolution_minutes: int
    business_calendar: string default="24x7"

# --- problems and known errors -----------------------------------------------

entity Problem "Problem":
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
    New                 -> Investigating:       investigate_problem label="Investigate"
    Investigating       -> RootCauseIdentified: identify_rca label="Root cause found"
    RootCauseIdentified -> KnownErrorState:     register_known_error label="Register as known error"
    KnownErrorState     -> Resolved:            resolve_problem label="Resolve"
    Resolved            -> Closed:              close_problem label="Close"

entity KnownError "Known Error":
    problem: ref[Problem] on_delete=cascade
    symptoms: text
    workaround: text
    permanent_fix: text
    kb_article: ref[KBArticle]
    status: enum[Active, Resolved] default=Active

# --- knowledge base (FTS native in kalita — search on text/string) --------

entity KBArticle "KB Article":
    title: string required
    body: text
    category: string
    tags: array[string]
    author: ref[core.User] default=$me
    updated: datetime default=$now
    status: enum[Draft, Published, Archived] default=Draft

workflow KBArticle on status:
    Draft     -> Published: publish_article requires approval(KbEditor) label="Publish"
    Published -> Archived:  archive_article label="Archive"
    Archived  -> Draft:     revise_article label="Return to draft"

# --- incidents ---------------------------------------------------------------

entity Incident "Incident":
    number: serial format="INC-{year}-{seq:6}" label="Number"
    title: string required label="Subject"
    description: text label="Description"
    priority: enum[P1, P2, P3, P4] default=P3 label="Priority"
    impact: enum[High, Medium, Low] default=Medium label="Impact"
    urgency: enum[High, Medium, Low] default=Medium label="Urgency"
    source: enum[Manual, Tivoli, Email, Portal] default=Manual label="Source"
    ci: ref[ConfigItem] label="Configuration item"
    problem: ref[Problem] label="Problem"
    attachments: array[file] label="Attachments"          # screenshots + logs
    reporter: ref[core.User] default=$me label="Reporter"
    assignee: ref[core.User] label="Assignee"
    sla_policy: ref[SLAPolicy] label="SLA policy"
    opened: datetime default=$now label="Opened"
    resolved_at: datetime label="Resolved"
    age_days: int computed = days_since(opened) label="Age, days"
    # live SLA: minutes elapsed since opening and minutes remaining before threshold
    # from related policy (ref path sla_policy.resolution_minutes). Negative
    # sla_left = SLA breached.
    # SLA counts WORKING minutes (8x5 via the node's business calendar), not wall
    # clock — a ticket opened Friday evening is not "breached" by Monday morning
    minutes_open: int computed = business_minutes_since(opened) label="In progress, min"
    sla_left:     int computed = sla_policy.resolution_minutes - business_minutes_since(opened) label="Time to SLA, min"
    status: enum[New, Investigating, Identified, Resolved, Closed] default=New label="Status"

workflow Incident on status:
    New           -> Investigating: investigate assignee=OperatorL2 label="Investigate"
    Investigating -> Identified:    identify label="Cause identified"
    Identified    -> Resolved:      resolve_incident label="Resolve"
    Resolved      -> Closed:        close_incident label="Close"
    Resolved      -> Investigating: reopen_incident label="Reopen"
    New           -> Closed:        auto_close when source = Tivoli label="Auto-close"

# incident duplicates — bidirectional link, synchronized at runtime
link Incident -> Incident as duplicates / duplicated_by

# --- service requests from catalog -------------------------------------------

entity ServiceRequest "Service Request":
    number: serial format="SR-{year}-{seq:6}"
    service: ref[Service] on_delete=restrict
    requester: ref[core.User] default=$me
    form_data: json
    approval_required: bool default=false
    opened: datetime default=$now
    status: enum[Submitted, ApprovalPending, Approved, Rejected, Fulfilling, Fulfilled, Closed] default=Submitted

workflow ServiceRequest on status:
    Submitted       -> ApprovalPending: require_approval when approval_required = true label="Require approval"
    Submitted       -> Fulfilling:      auto_approve when approval_required = false label="Start fulfillment"
    ApprovalPending -> Approved:        approve_request requires approval(Supervisor) label="Approve"
    ApprovalPending -> Rejected:        reject_request requires approval(Supervisor) label="Reject"
    Approved        -> Fulfilling:      start_fulfillment label="Start fulfillment"
    Fulfilling      -> Fulfilled:       fulfill label="Fulfilled"
    Fulfilled       -> Closed:          close_request label="Close"

# --- change requests (RFC) with CAB approval --------------------------------

entity Change "Change Request":
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
    Draft        -> Assessment:   submit_change label="Submit for assessment"
    Assessment   -> CabApproval:  request_cab label="Request CAB approval"
    CabApproval  -> Approved:     approve_change requires approval(ChangeManager) label="Approve (CAB)"
    CabApproval  -> Rejected:     reject_change requires approval(ChangeManager) label="Reject"
    Approved     -> Scheduled:    schedule_change label="Schedule"
    Scheduled    -> Implementing: implement_change label="Implement"
    Implementing -> Review:       complete_change label="Complete"
    Review       -> Closed:       close_change label="Close"

# --- work orders (assignments) -----------------------------------------------
# Spec: polymorphic parent (parent_type + parent_id). kalita has no polymorphic
# refs (gap in APPS-GAP-PLAN), so we model with explicit refs +
# parent_type discriminator. One ref is populated by type.

entity WorkOrder "Work Order":
    number: serial format="WO-{year}-{seq:6}" label="Number"
    parent_type: enum[Incident, ServiceRequest, Change, Problem] default=Incident label="Source type"
    incident: ref[Incident] label="Source incident"
    request: ref[ServiceRequest] label="Source request"
    change: ref[Change] label="Source change"
    problem: ref[Problem] label="Source problem"
    assignee: ref[core.User] label="Assignee"
    due: datetime label="Due"
    result: text label="Result"
    status: enum[Created, Assigned, InProgress, Completed, Cancelled] default=Created label="Status"

workflow WorkOrder on status:
    Created    -> Assigned:   assign_wo label="Assign"
    Assigned   -> InProgress: start_wo label="Start"
    InProgress -> Completed:  complete_wo label="Complete"
    Created    -> Cancelled:  cancel_wo label="Cancel"

# --- RBAC roles (Spec Appendix 3) -------------------------------------------

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
    # Portal user: creates requests, sees only own tickets
    LkpUser:
        create [ServiceRequest]
        read ServiceRequest where requester = $me
        read Incident where reporter = $me
        read [Service, KBArticle]
    # Level 1: incident reception and initial triage
    OperatorL1:
        read   [Incident, ServiceRequest, ConfigItem, Service, KBArticle, WorkOrder]
        create [Incident, WorkOrder]
        update [Incident, WorkOrder]
        act    [investigate, identify, start_fulfillment, fulfill, assign_wo, start_wo, complete_wo, cancel_wo]
    # Level 2: incident and problem analysis
    OperatorL2:
        read   [Incident, Problem, ServiceRequest, ConfigItem, WorkOrder, KBArticle]
        create [Incident, WorkOrder, Problem]
        update [Incident, WorkOrder, Problem]
        act    [investigate, identify, resolve_incident, close_incident, reopen_incident, investigate_problem, identify_rca, assign_wo, start_wo, complete_wo]
    # Supervisor/dispatcher: full queue control, request approval
    Supervisor:
        full    [Incident, ServiceRequest, WorkOrder]
        read    [Problem, Change, ConfigItem, Service, KBArticle, SLAPolicy]
        approve [approve_request, reject_request]
        act     [require_approval, auto_approve, approve_request, reject_request, start_fulfillment, fulfill, close_request, investigate, identify, resolve_incident, close_incident, reopen_incident]
    # Problem manager
    ProblemManager:
        full [Problem, KnownError]
        read [Incident, ConfigItem]
        act  [investigate_problem, identify_rca, register_known_error, resolve_problem, close_problem]
    # Change manager: manages RFCs, signs off on CAB
    ChangeManager:
        full    [Change]
        read    [ConfigItem, Incident]
        approve [approve_change, reject_change]
        act     [submit_change, request_cab, approve_change, reject_change, schedule_change, implement_change, complete_change, close_change]
    # Service catalog manager
    CatalogManager:
        full [Service]
        read [ServiceRequest]
    # KB editor: writes and publishes articles (publication is HITL)
    KbEditor:
        full    [KBArticle]
        approve [publish_article]
        act     [publish_article, archive_article, revise_article]
    # Analyst: read-only and dashboards
    ReportViewer:
        read [Incident, ServiceRequest, Change, Problem, WorkOrder, ConfigItem, Service, KBArticle, SLAPolicy]
    # Administrator
    Admin:
        full [Service, ConfigItem, SLAPolicy, Incident, ServiceRequest, Change, WorkOrder, Problem, KnownError, KBArticle]
        act  [investigate, identify, resolve_incident, close_incident, reopen_incident, auto_close, require_approval, auto_approve, start_fulfillment, fulfill, close_request, submit_change, request_cab, schedule_change, implement_change, complete_change, close_change, assign_wo, start_wo, complete_wo, cancel_wo, investigate_problem, identify_rca, register_known_error, resolve_problem, close_problem, archive_article, revise_article]

# --- automation: escalations and notifications -------------------------------

automation:
    on create Incident:
        notify email(reporter)
    on stuck Incident in New for 1d:
        escalate_to Supervisor
    on stuck Change in CabApproval for 3d:
        escalate_to ChangeManager

# --- dashboards (summaries across all records, respecting row permissions) ---

dashboard OperatorBoard "Operator queue":
    tile "Open incidents":       count Incident where status != Closed and status != Resolved
    tile "Unassigned":           count Incident where assignee = null
    tile "SLA breaches":         count Incident where sla_left < 0
    tile "Incidents by status":  count Incident group by status
    tile "Incidents by priority": count Incident group by priority

dashboard ServiceRequestsBoard "Service requests":
    tile "In progress":      count ServiceRequest where status != Closed
    tile "Awaiting approval": count ServiceRequest where status = ApprovalPending
    tile "By status":        count ServiceRequest group by status

dashboard ChangesBoard "Changes":
    tile "Active RFCs": count Change where status != Closed and status != Rejected
    tile "Awaiting CAB": count Change where status = CabApproval
    tile "By type":     count Change group by change_type
    tile "By risk":     count Change group by risk

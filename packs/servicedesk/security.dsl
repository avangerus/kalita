# Roles and permissions for the servicedesk pack.

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

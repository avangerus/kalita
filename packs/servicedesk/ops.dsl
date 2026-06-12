# Automation rules and dashboards for the servicedesk pack.

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

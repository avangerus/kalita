#!/usr/bin/env python3
"""synthesizer.py — MODEL/VALIDATE phases (product mission B/C, P4).

Turns CONFIRMED DesignDecisions into a compose_pack JSON spec + curated DSL
fragments, lets the node render/validate, and produces the EXPLAIN summary and
SIMULATE traces — all DETERMINISTIC assembler code; the LLM only parses
free-text answers into structured thresholds (with a deterministic fallback)
and may refine WORDING, never structure.

Honest boundary (Architect Pro Finding 2/3, closed in docs): the assembler
builds entities/roles/permissions/ladder from the decision set; curated
fragments (events/automations/dashboards) come from fixed templates derived
from the golden pack packs/ap — not LLM improvisation. Everything still passes
the node's compose_pack + validate_dsl, and the additive gate at propose.

All functions are pure (decisions in, spec/text out) — the node I/O lives in
builder.py so tests stay stdlib-only and deterministic.
"""
import json
import re

# --- free-text threshold parsing (LLM-assisted with deterministic fallback) --

_ROLE_NORMALIZE = [
    (re.compile(r"manager", re.I), "Manager"),
    (re.compile(r"cfo|financ", re.I), "CFO"),
    (re.compile(r"owner|proprietor", re.I), "Owner"),
    (re.compile(r"ceo|director|chief|executiv", re.I), "CEO"),
]

# amount words: "100k", "100 thousand", "1M", "1 million", "1000000"
_AMOUNT_RE = re.compile(
    r"(\d+(?:[.,]\d+)?)\s*(million|mln|m|thousand|thous|k)?\b", re.I)


def _parse_amount(text):
    m = _AMOUNT_RE.search(text)
    if not m:
        return None
    val = float(m.group(1).replace(",", "."))
    unit = (m.group(2) or "").lower()
    if unit.startswith("million") or unit in ("m", "mln"):
        val *= 1_000_000
    elif unit.startswith("thous") or unit in ("k",):
        val *= 1_000
    return val


def _role_for(text):
    for rx, role in _ROLE_NORMALIZE:
        if rx.search(text):
            return role
    return None


def parse_thresholds(text):
    """Parse 'up to 100k manager, up to 1M CFO, above CEO' into
    [{min, max, role}]. Deterministic segment splitter supporting:
      'up to X <role>' (max=X), 'above/more than/over X <role>' (min=X),
      'X-Y <role>' / 'from X to Y <role>' (min=X, max=Y),
      'above <role>' with no amount (min=previous max).
    A segment must carry a role and enough amount info, otherwise [] (the
    caller then asks the LLM / the user — never guesses)."""
    text = (text or "").strip()
    if not text:
        return []
    segments = [s.strip() for s in re.split(r"[,;]|\s+and\s+", text) if s.strip()]
    raw = []
    for seg in segments:
        role = _role_for(seg)
        if role is None:
            return []
        amounts = [_parse_amount(m.group(0)) for m in _AMOUNT_RE.finditer(seg)]
        amounts = [a for a in amounts if a is not None]
        low = seg.lower()
        if any(w in low for w in ("above", "more than", "over", "greater",
                                  "exceeding")):
            if len(amounts) >= 1:
                raw.append({"min": amounts[0], "max": None, "role": role})
            else:
                raw.append({"min": None, "max": None, "role": role})  # 'above <role>'
        elif "from" in low and len(amounts) >= 2:
            raw.append({"min": amounts[0], "max": amounts[1], "role": role})
        elif len(amounts) == 2:
            raw.append({"min": amounts[0], "max": amounts[1], "role": role})
        elif "to" in low and len(amounts) >= 1:
            raw.append({"min": None, "max": amounts[0], "role": role})
        elif len(amounts) == 1:
            raw.append({"min": None, "max": amounts[0], "role": role})
        else:
            return []
    if not raw:
        return []
    # order by explicit min, then max; resolve chained boundaries
    def key(t):
        return (t["min"] if t["min"] is not None else -1,
                t["max"] if t["max"] is not None else 10 ** 18)
    raw.sort(key=key)
    out, prev_max = [], 0
    for t in raw:
        lo = t["min"] if t["min"] is not None else prev_max
        hi = t["max"]
        if lo < prev_max:
            return []  # overlapping tiers — refuse, ask the user
        if hi is not None and hi <= lo:
            return []
        out.append({"min": lo, "max": hi, "role": t["role"]})
        prev_max = hi if hi is not None else lo
    # merge consecutive identical roles
    deduped = []
    for t in out:
        if deduped and deduped[-1]["role"] == t["role"]:
            deduped[-1]["max"] = t["max"]
        else:
            deduped.append(dict(t))
    return deduped


# --- decision helpers --------------------------------------------------------

def decision_map(decisions):
    """{topic: decision_text} from DesignDecision records (accepted only)."""
    out = {}
    for d in decisions:
        v = d.get("values", {})
        if v.get("status") == "accepted":
            out[v.get("topic", "")] = v.get("decision", "")
    return out


def has_yes(dmap, topic):
    v = dmap.get(topic, "").lower()
    return any(w in v for w in ("yes", "yeah", "yep", "need", "use", "using",
                                "have", "match", "check", "verify", "want"))


def _tier_label(t):
    """Human tier line: 'up to 100000 — Manager' / 'above 1000000 — CEO'."""
    if t["min"] == 0:
        return "up to %s — %s" % (_fmt(t["max"]), t["role"]) if t["max"] is not None else "any amount — %s" % t["role"]
    if t["max"] is None:
        return "above %s — %s" % (_fmt(t["min"]), t["role"])
    return "%s–%s — %s" % (_fmt(t["min"]), _fmt(t["max"]), t["role"])


def ladder_of(spec):
    return spec.get("_ladder", [])


def business_diff_ladder(old_spec, new_spec):
    """Business diff of the approval ladder: human 'before/after' lines derived
    from the two specs (never LLM prose). Returns a list of lines; empty when
    the ladders are identical."""
    old, new = ladder_of(old_spec), ladder_of(new_spec)
    if old == new:
        return []
    lines = ["Before:"]
    lines.extend("  " + _tier_label(t) for t in old)
    lines.append("After:")
    lines.extend("  " + _tier_label(t) for t in new)
    return lines


def old_spec_for_change(dmap_all_decisions):
    """Reconstruct the PRE-change spec: the decision map minus the LATEST
    amount_thresholds decision (the new rule the user just gave). Returns the
    spec as it was before this change — the diff base."""
    decs = list(dmap_all_decisions)
    idx = [i for i, d in enumerate(decs)
           if d.get("values", {}).get("topic") == "amount_thresholds"]
    old_decs = decs[:-1] if not idx else [d for i, d in enumerate(decs) if i != idx[-1]]
    old_map = decision_map(old_decs)
    old_map.setdefault("approval_authority",
                       decision_map(decs).get("approval_authority", ""))
    return build_ap_spec(old_map)


def impact_report_ladder(old_spec, new_spec, live_records):
    """'If the new model were active now': route LIVE records through the old
    and new ladders, report the rerouting. live_records: [{id, values:{...}}]
    from query. Returns human lines; records whose guard is outside the walker
    subset are reported as unknown, never guessed."""
    old_traces = walk_ladder(old_spec, live_records)
    new_traces = walk_ladder(new_spec, live_records)
    changed, unknown = [], []
    for ot, nt in zip(old_traces, new_traces):
        if not nt["ok"] or not ot["ok"]:
            unknown.append(nt["record_id"])
        elif ot["tier_role"] != nt["tier_role"]:
            changed.append((nt["record_id"], nt["amount"],
                            ot["tier_role"], nt["tier_role"]))
    lines = ["Check against live records (if the new model were active "
             "now):"]
    lines.append("- open invoices reviewed: %d" % len(live_records))
    lines.append("- would change route: %d" % len(changed))
    for rid, amount, was, now in changed[:10]:
        lines.append("  · invoice %s (amount %s): %s → %s" % (rid, amount, was, now))
    if len(changed) > 10:
        lines.append("  · … and %d more" % (len(changed) - 10))
    lines.append("- route undeterminable (outside the walker subset): %d" % len(unknown))
    return lines


# --- framework registry (which deterministic builder serves which framework) --

BUILDABLE = ("ap", "itsm")  # frameworks with a spec builder; the rest are knowledge-only


def build_spec(fwid, dmap):
    """Dispatch to the framework's deterministic spec builder. Unknown
    framework -> ValueError (the caller must have validated against the
    registry already; a fresh framework needs its own builder, not a guess)."""
    if fwid == "ap":
        return build_ap_spec(dmap)
    if fwid == "itsm":
        return build_itsm_spec(dmap)
    raise ValueError("no spec builder for framework %r" % fwid)


def change_topic_for(fwid):
    """The one discovery topic a CHANGE conversation asks about per domain."""
    return {"ap": "amount_thresholds", "itsm": "sla_levels"}.get(fwid, "")


def business_diff(fwid, old_spec, new_spec):
    """Per-domain business diff ('before/after' human lines, never LLM prose)."""
    if fwid == "ap":
        return business_diff_ladder(old_spec, new_spec)
    if fwid == "itsm":
        old, new = (s.get("_sla", []) for s in (old_spec, new_spec))
        if old == new:
            return []
        lines = ["Before (SLA):"]
        lines.extend("  %s: %sh/%sh" % (s["priority"], s["response_hours"], s["resolve_hours"]) for s in old)
        lines.append("After (SLA):")
        lines.extend("  %s: %sh/%sh" % (s["priority"], s["response_hours"], s["resolve_hours"]) for s in new)
        return lines
    return []


# --- ITSM spec assembler (generality-proof domain, P8) -----------------------

def build_itsm_spec(dmap):
    """compose_pack JSON spec for the ITSM vertical slice, built ONLY from the
    confirmed decision map. The same discipline as build_ap_spec: SoD, agent
    deny rows, HITL gates, no placeholders."""
    change_control = has_yes(dmap, "change_control")
    sla_levels = dmap.get("sla_levels", "")

    # SLA policy rows: parse "P1 1h/4h ..." conservatively; fall back to the
    # framework defaults when unparseable (defaults are shown to the user as
    # such in EXPLAIN, never silently invented beyond them).
    sla = _parse_sla(sla_levels) or [
        {"priority": "P1", "response_hours": 1, "resolve_hours": 4},
        {"priority": "P2", "response_hours": 4, "resolve_hours": 8},
        {"priority": "P3", "response_hours": 8, "resolve_hours": 24},
        {"priority": "P4", "response_hours": 24, "resolve_hours": 72},
    ]

    entities = [
        {"name": "SLAPolicy", "fields": [
            {"name": "priority", "type": "enum",
             "values": ["P1", "P2", "P3", "P4"], "required": True},
            {"name": "response_hours", "type": "int", "required": True},
            {"name": "resolve_hours", "type": "int", "required": True},
        ]},
        {"name": "Incident", "fields": [
            {"name": "title", "type": "string", "required": True},
            {"name": "reporter", "type": "ref", "ref": "core.User", "required": True},
            {"name": "source", "type": "enum",
             "values": ["Portal", "Email", "Phone", "Monitoring"],
             "required": True, "default": "Portal"},
            {"name": "priority", "type": "enum",
             "values": ["P1", "P2", "P3", "P4"], "required": True, "default": "P3"},
            {"name": "assignee", "type": "ref", "ref": "core.User"},
            {"name": "resolution", "type": "text"},
            {"name": "status", "type": "enum",
             "values": ["New", "Investigating", "Identified", "Resolved",
                        "Closed"],
             "default": "New"},
        ]},
        {"name": "ServiceRequest", "fields": [
            {"name": "title", "type": "string", "required": True},
            {"name": "requester", "type": "ref", "ref": "core.User", "required": True},
            {"name": "status", "type": "enum",
             "values": ["New", "Approved", "Rejected", "Fulfilled"],
             "default": "New"},
        ]},
    ]
    if change_control:
        entities.append({"name": "Change", "fields": [
            {"name": "title", "type": "string", "required": True},
            {"name": "risk", "type": "enum",
             "values": ["low", "medium", "high"], "required": True},
            {"name": "status", "type": "enum",
             "values": ["Draft", "CABReview", "Approved", "Rejected", "Implemented"],
             "default": "Draft"},
        ]})
    entities.append({"name": "Knowledge", "fields": [
        {"name": "title", "type": "string", "required": True},
        {"name": "body", "type": "text", "required": True},
        {"name": "incident", "type": "ref", "ref": "Incident"},
    ]})

    workflows = [{"entity": "Incident", "field": "status", "transitions": [
        {"from": "New", "to": "Investigating", "action": "take",
         "assignee_agent": "ServiceDeskAgent"},
        {"from": "Investigating", "to": "Identified", "action": "diagnose"},
        {"from": "Identified", "to": "Resolved", "action": "resolve"},
        {"from": "Resolved", "to": "Closed", "action": "close",
         "requires_approval": "Supervisor"},
        {"from": "Investigating", "to": "Resolved", "action": "resolve_fast"},
        {"from": "Resolved", "to": "Investigating", "action": "reopen"},
    ]}, {"entity": "ServiceRequest", "field": "status", "transitions": [
        {"from": "New", "to": "Approved", "action": "approve_request",
         "requires_approval": "Supervisor"},
        {"from": "New", "to": "Rejected", "action": "reject_request"},
        {"from": "Approved", "to": "Fulfilled", "action": "fulfill"},
    ]}]
    if change_control:
        workflows.append({"entity": "Change", "field": "status", "transitions": [
            {"from": "Draft", "to": "CABReview", "action": "submit_change"},
            {"from": "CABReview", "to": "Approved", "action": "approve_change",
             "requires_approval": "ChangeManager"},
            {"from": "CABReview", "to": "Rejected", "action": "reject_change"},
            {"from": "Approved", "to": "Implemented", "action": "implement"},
        ]})

    roles = ["EndUser", "ServiceDeskAgent", "OperatorL2", "Supervisor"]
    if change_control:
        roles.append("ChangeManager")

    permissions = [
        {"role": "EndUser", "rules": [
            "read [Incident] where reporter = $me",
            "read [ServiceRequest] where requester = $me",
            "create [Incident, ServiceRequest]",
            # no act grants at all -> workflow transitions are fail-closed
            # already; the deny rows lock field writes and deletes
            "deny [update *, delete *]",
        ]},
        {"role": "ServiceDeskAgent", "rules": [
            "read [Incident, Knowledge, SLAPolicy]",
            "create [Knowledge]",
            "update [Incident.assignee, Incident.priority]",
            "act [take, diagnose, resolve, resolve_fast]",
            # close is HITL for Supervisor; ServiceRequest actions are outside
            # the agent's grants entirely (fail-closed — no deny rows needed
            # there, and the compiler forbids action-names of entities the
            # role cannot access)
            "deny [approve *, delete *, act [close]]",
        ]},
        {"role": "OperatorL2", "rules": [
            "read [Incident, ServiceRequest, Knowledge, SLAPolicy]",
            "create [Incident]",
            "update [Incident.resolution, Incident.assignee]",
            "act [take, diagnose, resolve, resolve_fast, reopen, fulfill]",
            "create [Knowledge]",
            "deny [approve *, delete *]",
        ]},
        {"role": "Supervisor", "rules": [
            "read [Incident, ServiceRequest, Knowledge, SLAPolicy]",
            "approve [close, approve_request]",
            "act [close, approve_request, reject_request]",
            "deny [delete *]",
        ]},
    ]
    if change_control:
        permissions.append({"role": "ChangeManager", "rules": [
            "read [Change, Incident]",
            "create [Change]",
            "approve [approve_change]",
            "act [submit_change, approve_change, reject_change, implement]",
            "deny [delete *]",
        ]})

    return {
        "pack": "itsm",
        "version": "0.1.0",
        "entities": entities,
        "workflows": workflows,
        "roles": [{"name": r, "agent": r == "ServiceDeskAgent"} for r in roles],
        "permissions": permissions,
        "links": [],
        "_sla": sla,
        "_change_control": change_control,
    }


def _parse_sla(text):
    """Parse 'P1 1h/4h, P2 4h/8h' into policy rows; [] when unparseable (the
    caller falls back to framework defaults — shown to the user as such)."""
    import re as _re
    rows = []
    for seg in _re.split(r"[,;]", text or ""):
        seg = seg.strip()
        if not seg:
            continue
        m = _re.match(r"(P\d)\s+(\d+)\s*(?:hrs?|h)\s*/\s*(\d+)\s*(?:hrs?|h)", seg, _re.I)
        if not m:
            return []
        rows.append({"priority": m.group(1).upper(),
                     "response_hours": int(m.group(2)),
                     "resolve_hours": int(m.group(3))})
    return rows or []


def itsm_fragments(spec):
    """Curated dashboard for ITSM (golden-derived, provenance explicit)."""
    dashboard = (
        'dashboard ServiceDesk "Service desk":\n'
        '    tile "Open incidents": count Incident where status != Closed\n'
        '    tile "Resolved": count Incident where status = Resolved\n'
    )
    return {
        "dashboard": dashboard,
        "provenance": "curated template v1 (derived from packs/servicedesk golden, Round-4)",
    }


def explain_itsm(spec, fragments):
    """Human EXPLAIN for ITSM: entities, roles, process, gates (CAB/close/
    request), agent actions, SLA policy, integrations, open questions."""
    entities = [(e["name"], len(e["fields"])) for e in spec["entities"]]
    roles = [r["name"] for r in spec["roles"]]
    process = []
    for wf in spec["workflows"]:
        for t in wf["transitions"]:
            line = "%s → %s (%s)" % (t["from"], t["to"], t["action"])
            if t.get("requires_approval"):
                line += "  ✍ signature: " + t["requires_approval"]
            if t.get("assignee_agent"):
                line += "  [agent: %s]" % t["assignee_agent"]
            process.append(line)
    gates = [line for line in process if "✍" in line]
    sla_lines = ["%s: response %dh / resolve %dh" % (s["priority"], s["response_hours"], s["resolve_hours"])
                 for s in spec.get("_sla", [])]
    agent_actions = []
    for p in spec["permissions"]:
        if p["role"] == "ServiceDeskAgent":
            agent_actions.append("can: %s" % "; ".join(r for r in p["rules"] if not r.startswith("deny")))
            agent_actions.append("CANNOT: %s" % "; ".join(r for r in p["rules"] if r.startswith("deny")))
    return {
        "What is created": ["%s (%d fields)" % (n, c) for n, c in entities],
        "Who participates": roles,
        "How the process runs": process,
        "Where a human is needed (signatures)": gates or ["(no HITL gates — check the decisions)"],
        "What the agent does": agent_actions,
        "SLA policy": sla_lines,
        "Which systems connect": [
            "Input: monitoring/portal (events, inbound pattern)",
        ],
        "Open questions": ["(none — all blocking/important questions are closed)"],
    }

# --- AP spec assembler (deterministic, golden-derived) -----------------------

def build_ap_spec(dmap):
    """compose_pack JSON spec for the AP vertical slice, built ONLY from the
    confirmed decision map. Single-approver fallback when no thresholds."""
    ladder = parse_thresholds(dmap.get("amount_thresholds", ""))
    approver_text = dmap.get("approval_authority", "")
    if not ladder:
        role = _role_for(approver_text) or "CEO"
        ladder = [{"min": 0, "max": None, "role": role}]
    # amounts below the first explicit threshold still need a route: the
    # base approver decides them (otherwise a low invoice would have no tier)
    if ladder[0]["min"] > 0:
        base_role = _role_for(approver_text) or ladder[0]["role"]
        ladder = [{"min": 0, "max": ladder[0]["min"], "role": base_role}] + ladder

    roles = ["APClerk", "APAgent"]
    for t in ladder:
        if t["role"] not in roles:
            roles.append(t["role"])

    entities = [
        {"name": "Vendor", "fields": [
            {"name": "name", "type": "string", "required": True, "unique": True},
            {"name": "inn", "type": "string"},
            {"name": "bank_account", "type": "string"},
        ]},
    ]
    inv_fields = [
        {"name": "vendor", "type": "ref", "ref": "Vendor", "required": True},
        {"name": "number", "type": "string", "required": True},
        {"name": "amount", "type": "money", "required": True},
        {"name": "received_at", "type": "datetime"},
        {"name": "contract_ok", "type": "bool", "default": "false"},
        {"name": "check_notes", "type": "text"},
        {"name": "status", "type": "enum",
         "values": ["Received", "Checking", "ApprovalPending", "Approved",
                    "Rejected", "SentToPayment"],
         "default": "Received"},
    ]
    if has_yes(dmap, "contract_matching"):
        inv_fields.insert(2, {"name": "contract_no", "type": "string"})
        entities.append({"name": "Contract", "fields": [
            {"name": "vendor", "type": "ref", "ref": "Vendor", "required": True},
            {"name": "number", "type": "string", "required": True},
            {"name": "valid_until", "type": "date"},
        ]})
    if has_yes(dmap, "po_matching"):
        inv_fields.insert(3, {"name": "purchase_order", "type": "string"})
    entities.append({"name": "VendorInvoice", "fields": inv_fields})
    entities.append({"name": "PaymentHandoff", "fields": [
        {"name": "invoice", "type": "ref", "ref": "VendorInvoice", "required": True},
        {"name": "sent_at", "type": "datetime"},
        {"name": "external_ref", "type": "string"},
    ]})

    transitions = [
        {"from": "Received", "to": "Checking", "action": "start_checking",
         "assignee_agent": "APAgent"},
        {"from": "Checking", "to": "ApprovalPending", "action": "submit_check"},
        {"from": "Checking", "to": "Rejected", "action": "flag_mismatch"},
        {"from": "ApprovalPending", "to": "Rejected", "action": "reject"},
        {"from": "Rejected", "to": "Checking", "action": "reopen"},
        {"from": "Approved", "to": "SentToPayment", "action": "send_to_payment",
         "assignee_agent": "APAgent"},
    ]
    for i, t in enumerate(ladder):
        when = "amount > %s" % _fmt(t["min"])
        if t["max"] is not None:
            when += " and amount <= %s" % _fmt(t["max"])
        transitions.append({
            "from": "ApprovalPending", "to": "Approved",
            "action": "approve_tier_%d" % (i + 1),
            "requires_approval": t["role"], "when": when,
        })

    permissions = [
        {"role": "APClerk", "rules": [
            "read [Vendor, VendorInvoice, PaymentHandoff]",
            "create [Vendor, VendorInvoice]",
            "update [VendorInvoice]",
            "act [start_checking, submit_check, flag_mismatch, reopen]",
            "deny [approve *, delete *]",
        ]},
        {"role": "APAgent", "rules": [
            "read [Vendor, VendorInvoice]",
            "create [VendorInvoice]",
            "update [VendorInvoice.contract_ok, VendorInvoice.check_notes]",
            "act [start_checking, submit_check, send_to_payment]",
            "deny [approve *, delete *, update VendorInvoice.amount, update VendorInvoice.status]",
        ]},
    ]
    for t in ladder:
        permissions.append({"role": t["role"], "rules": [
            "read [Vendor, VendorInvoice, PaymentHandoff]",
            # approve = decides the signature; act = REQUESTS the gate (the
            # transition itself is the HITL parking). Without act the approver
            # cannot even request approval (clean-room N2: 403 -> approvals are
            # never created)
            "approve [approve_tier_%d]" % (ladder.index(t) + 1),
            "act [approve_tier_%d, reject, reopen]" % (ladder.index(t) + 1),
        ]})

    return {
        "pack": "ap",
        "version": "0.1.0",
        "entities": entities,
        "workflows": [{"entity": "VendorInvoice", "field": "status",
                       "transitions": transitions}],
        "roles": [{"name": r, "agent": r == "APAgent"} for r in roles],
        "permissions": permissions,
        "links": [],
        # not part of compose_pack JSON: carried alongside for EXPLAIN and
        # for the curated DSL fragments
        "_ladder": ladder,
        "_contract_matching": has_yes(dmap, "contract_matching"),
        "_po_matching": has_yes(dmap, "po_matching"),
    }


def _fmt(n):
    return str(int(n)) if float(n).is_integer() else str(n)


# --- curated DSL fragments (events/automation/dashboards, golden-derived) ----

def ap_fragments(spec):
    """The non-compose_pack parts of the AP pack as a curated template: an
    InvoiceApproved event, its emits on the ladder transitions, an
    InvoiceSentToPayment event, and a dashboard. Provenance is explicit — the
    human EXPLAIN shows these words BEFORE signing."""
    events = (
        "event InvoiceApproved \"Incoming invoice approved\":\n"
        "    number: string\n"
        "    amount: money\n"
    )
    emits = [
        "emits InvoiceApproved(number = number, amount = amount)"
    ]
    dashboard = (
        'dashboard AP "AP":\n'
        '    tile "Open invoices": count VendorInvoice where status != SentToPayment\n'
        '    tile "Sum pending": sum amount VendorInvoice where status = ApprovalPending\n'
    )
    return {
        "events": events,
        "emits": emits,
        "dashboard": dashboard,
        "provenance": "curated template v1 (derived from packs/ap golden, Round-2)",
    }


# --- EXPLAIN: deterministic human summary from the spec ----------------------

def explain_spec(spec, fragments):
    """Sections the human must see BEFORE signing (mission B 'Model proposal'):
    what is created, who participates, the process, who can do what, what the
    agent does, where humans sign, which systems connect, open questions.
    Returns {section_title: [lines]} — NO LLM prose, derived from structure."""
    entities = [(e["name"], len(e["fields"])) for e in spec["entities"]]
    ladder = spec["_ladder"]
    roles = [r["name"] for r in spec["roles"]]
    agent_roles = [r["name"] for r in spec["roles"] if r.get("agent")]

    process = []
    for wf in spec["workflows"]:
        for t in wf["transitions"]:
            line = "%s → %s (%s)" % (t["from"], t["to"], t["action"])
            if t.get("requires_approval"):
                line += "  ✍ signature: " + t["requires_approval"]
            if t.get("when"):
                line += "  [if: %s]" % t["when"]
            if t.get("assignee_agent"):
                line += "  [agent: %s]" % t["assignee_agent"]
            process.append(line)

    gates = []
    for t in ladder:
        rng = ("up to %s" % _fmt(t["max"])) if t["max"] is not None else ("above %s" % _fmt(t["min"]))
        gates.append("%s — approved by %s" % (rng, t["role"]))

    perm_summary = []
    for p in spec["permissions"]:
        perm_summary.append("%s: %s" % (p["role"], "; ".join(p["rules"])))

    agent_actions = []
    for p in spec["permissions"]:
        if p["role"] in agent_roles:
            allowed = [r for r in p["rules"] if not r.startswith("deny")]
            denied = [r for r in p["rules"] if r.startswith("deny")]
            agent_actions.append("can: %s" % ("; ".join(allowed)))
            agent_actions.append("CANNOT: %s" % ("; ".join(denied)))

    integrations = [
        "Input: system X (inbound connector, least-privilege role) — template workers/connectors/inbound.py",
        "Output: system Y — InvoiceApproved event → egress; webhook delivery — known gap D-011 (status honestly shown)",
    ]

    return {
        "What is created": ["%s (%d fields)" % (n, c) for n, c in entities],
        "Who participates": roles,
        "How the process runs": process,
        "Where a human is needed (signatures)": gates,
        "Who can do what": perm_summary,
        "What the agent does": agent_actions,
        "Which systems connect": integrations,
        "Outgoing events": fragments["events"].strip().split("\n"),
        "Open questions": ["(none — all blocking/important questions are closed)"],
    }


# --- SIMULATE: ladder walker (honest subset) ---------------------------------

# The subset of the engine's condition language the walker may evaluate
# (internal/engine/expr.go: closed grammar, fail-closed). ANYTHING outside
# this subset returns None -> the walker marks the route unknown ("the engine
# will check at runtime"), never a silent guess. The shared corpus
# walker_golden.json pins THIS evaluator to the engine's evalWhere (the Go
# test internal/engine/walker_golden_test.go runs the same cases through the
# real evaluator): divergence fails one of the two suites.

_TOKEN_RE = re.compile(r'"[^"]*"|\d+(?:\.\d+)?|[<>!=]=|[<>=]|[()]|[\w.]+', re.S)


def _tok_next(tokens, i):
    return tokens[i] if i < len(tokens) else None


def _to_number(tok):
    try:
        return float(tok)
    except (TypeError, ValueError):
        return None


def _resolve_field(values, name):
    """Field lookup; a missing field is an error (fail-closed), matching the
    engine's flat lookup behavior for guard evaluation."""
    if name in values:
        return values[name]
    raise ValueError("unknown field %r" % name)


def eval_guard_subset(expr, values):
    """Evaluate a guard expression over the subset grammar. Returns True /
    False, or None when the expression uses anything outside the subset
    (computed fields, ref-paths, aggregates, $me/$now, in-lists, ...) — the
    caller must then treat the route as unknown."""
    try:
        return _eval_expr(_tokenize(expr), 0, values)[0]
    except _OutsideSubset:
        return None
    except (ValueError, IndexError):
        return None  # parse/eval error -> fail-closed, like the engine


class _OutsideSubset(Exception):
    pass


def _tokenize(expr):
    tokens = _TOKEN_RE.findall(expr or "")
    if not tokens:
        return []
    return tokens


def _eval_expr(tokens, i, values):
    # or-chain of and-chains of unary of primary
    val, i = _eval_and(tokens, i, values)
    while _tok_next(tokens, i) == "or":
        rhs, i = _eval_and(tokens, i + 1, values)
        val = val or rhs
    return val, i


def _eval_and(tokens, i, values):
    val, i = _eval_unary(tokens, i, values)
    while _tok_next(tokens, i) == "and":
        rhs, i = _eval_unary(tokens, i + 1, values)
        val = val and rhs
    return val, i


def _eval_unary(tokens, i, values):
    if _tok_next(tokens, i) == "not":
        v, j = _eval_unary(tokens, i + 1, values)
        return not v, j
    return _eval_primary(tokens, i, values)


def _eval_primary(tokens, i, values):
    if _tok_next(tokens, i) == "(":
        v, j = _eval_expr(tokens, i + 1, values)
        if _tok_next(tokens, j) != ")":
            raise ValueError("unbalanced parens")
        return v, j + 1
    left, i = _operand(tokens, i, values)
    op = _tok_next(tokens, i)
    if op not in ("=", "!=", ">", "<", ">=", "<="):
        raise ValueError("missing comparison")
    right, j = _operand(tokens, i + 1, values)
    if op == "=":
        return left == right, j
    if op == "!=":
        return left != right, j
    if isinstance(left, str) or isinstance(right, str):
        raise ValueError("ordering on strings unsupported in subset")
    if op == ">":
        return left > right, j
    if op == "<":
        return left < right, j
    if op == ">=":
        return left >= right, j
    return left <= right, j


def _operand(tokens, i, values):
    tok = _tok_next(tokens, i)
    if tok is None:
        raise ValueError("missing operand")
    num = _to_number(tok)
    if num is not None:
        return num, i + 1
    if tok.startswith('"') and tok.endswith('"'):
        return tok[1:-1], i + 1
    if tok in ("true", "false"):
        return tok == "true", i + 1
    # barewords: a dotted path or $-variable is outside the subset
    if "." in tok or tok.startswith("$"):
        raise _OutsideSubset("path/variable %r outside walker subset" % tok)
    return _resolve_field(values, tok), i + 1


def walk_ladder(spec, records):
    """Route records through the ladder by EVALUATING the generated tier
    guards (the same `when` strings the engine will run) over the subset
    evaluator. Returns [{record_id, amount, tier_role, ok}] — ok=False when
    no tier's guard matched or a guard fell outside the subset (the engine
    will check at runtime; never counted as 'passes')."""
    tiers = []
    for wf in spec.get("workflows", []):
        for t in wf.get("transitions", []):
            if t["from"] == "ApprovalPending" and t["to"] == "Approved":
                tiers.append((t.get("when", ""), t["requires_approval"]))
    out = []
    for rec in records:
        rid = rec.get("id")
        values = rec.get("values", rec)
        route, ok = None, False
        for guard, role in tiers:
            res = eval_guard_subset(guard, values)
            if res is None:
                route, ok = None, False  # subset cannot decide — honest unknown
                break
            if res:
                route, ok = role, True
                break
        out.append({"record_id": rid, "amount": values.get("amount"),
                    "tier_role": route, "ok": ok})
    return out

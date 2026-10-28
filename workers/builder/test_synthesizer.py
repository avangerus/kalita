#!/usr/bin/env python3
"""test_synthesizer.py — deterministic MODEL/EXPLAIN/SIMULATE tests (P4).

The assembler is pure code: every test runs without an LLM and without a node.
The guard/ladder shapes here mirror the golden pack packs/ap (H1 pattern:
guarded transitions ApprovalPending -> Approved with requires approval(Role)).
"""
import os
import sys

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
import synthesizer  # noqa: E402


def test_parse_thresholds_three_tier_ladder():
    tiers = synthesizer.parse_thresholds(
        "up to 100k approved by the manager, up to 1 million — CFO, above — CEO")
    assert tiers == [
        {"min": 0, "max": 100000, "role": "Manager"},
        {"min": 100000, "max": 1000000, "role": "CFO"},
        {"min": 1000000, "max": None, "role": "CEO"},
    ], tiers


def test_parse_thresholds_ranges_and_above():
    tiers = synthesizer.parse_thresholds(
        "from 100 thousand to 1M CFO; above 2M owner")
    assert tiers == [
        {"min": 100000, "max": 1000000, "role": "CFO"},
        {"min": 2000000, "max": None, "role": "Owner"},
    ], tiers


def test_parse_thresholds_unparseable_returns_empty():
    assert synthesizer.parse_thresholds("let someone else decide") == []
    assert synthesizer.parse_thresholds("up to 100k") == []  # amount, no role
    assert synthesizer.parse_thresholds("") == []


def test_parse_thresholds_hedged_english_catch_all():
    """Product Skeptic F1: ordinary hedged phrasings must parse, not collapse
    to a single approver."""
    tiers = synthesizer.parse_thresholds("over 2M the owner, otherwise the CFO")
    assert tiers == [
        {"min": 0, "max": 2000000, "role": "CFO"},
        {"min": 2000000, "max": None, "role": "Owner"},
    ], tiers
    tiers = synthesizer.parse_thresholds("up to 100k manager, otherwise CFO")
    assert tiers == [
        {"min": 0, "max": 100000, "role": "Manager"},
        {"min": 100000, "max": None, "role": "CFO"},
    ], tiers
    # a change-statement shape is NOT silently defaulted: no role+amount
    # structure the parser trusts -> [] -> the builder re-asks
    assert synthesizer.parse_thresholds("actually, make it 200k instead") == []


def test_has_amount_mention():
    assert synthesizer.has_amount_mention("actually, make it 200k instead")
    assert synthesizer.has_amount_mention("up to one million")
    assert not synthesizer.has_amount_mention("let the director decide")
    assert not synthesizer.has_amount_mention("")


def test_build_ap_spec_ladder_with_base_tier():
    dmap = {
        "what_is_invoice": "supplier invoices",
        "approval_authority": "director",
        "amount_thresholds": "up to 100k manager, up to 1M CFO, above CEO",
        "contract_matching": "yes, we match",
    }
    spec = synthesizer.build_ap_spec(dmap)
    names = [e["name"] for e in spec["entities"]]
    assert names == ["Vendor", "Contract", "VendorInvoice", "PaymentHandoff"]
    wf = spec["workflows"][0]
    ladder_ts = [t for t in wf["transitions"]
                 if t["from"] == "ApprovalPending" and t["to"] == "Approved"]
    assert len(ladder_ts) == 3
    guards = [(t["requires_approval"], t["when"]) for t in ladder_ts]
    assert guards == [
        ("Manager", "amount > 0 and amount <= 100000"),
        ("CFO", "amount > 100000 and amount <= 1000000"),
        ("CEO", "amount > 1000000"),
    ], guards
    roles = {r["name"]: r.get("agent", False) for r in spec["roles"]}
    assert roles == {"APClerk": False, "APAgent": True,
                     "Manager": False, "CFO": False, "CEO": False}
    agent_rules = [p["rules"] for p in spec["permissions"]
                   if p["role"] == "APAgent"][0]
    assert any(r.startswith("deny") for r in agent_rules), "agent must have deny rows"
    assert any("approve *" in r for r in agent_rules), "agent must never approve"
    # N2 regression: every approver tier must be able to REQUEST its own gate
    # (act) AND decide it (approve) — without act the approval is never created
    for t in spec["_ladder"]:
        rules = [p["rules"] for p in spec["permissions"] if p["role"] == t["role"]][0]
        joined = " ".join(rules)
        assert "approve [approve_tier_%d]" % (spec["_ladder"].index(t) + 1) in joined
        assert "act [approve_tier_%d" % (spec["_ladder"].index(t) + 1) in joined, \
            "approver must have act on its tier: %r" % rules


def test_build_ap_spec_single_approver_fallback():
    spec = synthesizer.build_ap_spec({"approval_authority": "all approved by the director"})
    wf = spec["workflows"][0]
    ladder_ts = [t for t in wf["transitions"]
                 if t["from"] == "ApprovalPending" and t["to"] == "Approved"]
    assert len(ladder_ts) == 1
    assert ladder_ts[0]["requires_approval"] == "CEO"
    assert ladder_ts[0]["when"] == "amount > 0"


def test_build_ap_spec_base_tier_below_first_threshold():
    spec = synthesizer.build_ap_spec({
        "approval_authority": "director",
        "amount_thresholds": "above 2M owner",
    })
    ladder = spec["_ladder"]
    assert ladder == [
        {"min": 0, "max": 2000000, "role": "CEO"},
        {"min": 2000000, "max": None, "role": "Owner"},
    ], ladder


def test_no_po_no_contract_entities():
    spec = synthesizer.build_ap_spec({"approval_authority": "director"})
    names = [e["name"] for e in spec["entities"]]
    assert names == ["Vendor", "VendorInvoice", "PaymentHandoff"]


def test_decision_map_accepted_only():
    decs = [
        {"values": {"topic": "a", "decision": "one", "status": "accepted"}},
        {"values": {"topic": "b", "decision": "two", "status": "rejected"}},
        {"values": {"topic": "c", "decision": "three", "status": "accepted"}},
    ]
    assert synthesizer.decision_map(decs) == {"a": "one", "c": "three"}


def test_explain_spec_sections_cover_mission_b():
    spec = synthesizer.build_ap_spec({
        "approval_authority": "director",
        "amount_thresholds": "up to 100k manager, above CFO",
    })
    frag = synthesizer.ap_fragments(spec)
    sections = synthesizer.explain_spec(spec, frag)
    for required in ("What is created", "Who participates", "How the process runs",
                     "Where a human is needed (signatures)", "Who can do what",
                     "What the agent does", "Which systems connect",
                     "Open questions"):
        assert required in sections, "missing EXPLAIN section: %s" % required
    gates = sections["Where a human is needed (signatures)"]
    assert "up to 100000 — approved by Manager" in gates
    assert "above 100000 — approved by CFO" in gates


def test_walk_ladder_routes_and_honest_unknown():
    spec = synthesizer.build_ap_spec({
        "approval_authority": "director",
        "amount_thresholds": "up to 100k manager, up to 1M CFO, above CEO",
    })
    traces = synthesizer.walk_ladder(spec, [
        {"id": "a", "values": {"amount": 80_000}},
        {"id": "b", "values": {"amount": 500_000}},
        {"id": "c", "values": {"amount": 1_500_000}},
    ])
    roles = [t["tier_role"] for t in traces]
    assert roles == ["Manager", "CFO", "CEO"], traces
    assert all(t["ok"] for t in traces)
    # a tierless model (single approver) still routes everything to that role
    single = synthesizer.build_ap_spec({"approval_authority": "director"})
    traces = synthesizer.walk_ladder(single, [{"id": "d", "values": {"amount": 80_000}}])
    assert traces[0]["tier_role"] == "CEO"


def test_ap_fragments_carry_provenance():
    spec = synthesizer.build_ap_spec({"approval_authority": "director"})
    frag = synthesizer.ap_fragments(spec)
    assert "provenance" in frag and "packs/ap" in frag["provenance"]
    assert "event InvoiceApproved" in frag["events"]
    assert "dashboard" in frag["dashboard"]


# --- ITSM (generality-proof domain) ------------------------------------------

def test_build_itsm_spec_defaults_and_gates():
    dmap = {
        "what_is_incident": "any service outages",
        "who_resolves": "our own team",
    }
    spec = synthesizer.build_itsm_spec(dmap)
    names = [e["name"] for e in spec["entities"]]
    assert "Incident" in names and "ServiceRequest" in names and "SLAPolicy" in names
    assert "Change" not in names, "no change control confirmed -> no Change entity"
    roles = {r["name"]: r.get("agent", False) for r in spec["roles"]}
    assert roles["ServiceDeskAgent"] is True
    agent_rules = [p["rules"] for p in spec["permissions"]
                   if p["role"] == "ServiceDeskAgent"][0]
    assert any("deny" in r for r in agent_rules)
    # SoD: the agent cannot close (close is HITL for Supervisor)
    assert any("act [close" in r and "deny" in r or "deny" in r and "close" in r
               for r in agent_rules), agent_rules
    # HITL gates exist
    wf = spec["workflows"][0]
    gates = [t for t in wf["transitions"] if t.get("requires_approval")]
    assert gates and gates[0]["requires_approval"] == "Supervisor"
    # SLA defaults present and shown
    assert len(spec["_sla"]) == 4


def test_build_itsm_spec_with_change_control():
    spec = synthesizer.build_itsm_spec({
        "who_resolves": "our own team",
        "change_control": "yes, need CAB",
    })
    names = [e["name"] for e in spec["entities"]]
    assert "Change" in names
    change_wf = [w for w in spec["workflows"] if w["entity"] == "Change"][0]
    gates = [t for t in change_wf["transitions"] if t.get("requires_approval")]
    assert gates and gates[0]["requires_approval"] == "ChangeManager"


def test_parse_sla():
    rows = synthesizer._parse_sla("P1 1h/4h, P2 4h/8h, P3 8h/24h, P4 24h/72h")
    assert rows == [
        {"priority": "P1", "response_hours": 1, "resolve_hours": 4},
        {"priority": "P2", "response_hours": 4, "resolve_hours": 8},
        {"priority": "P3", "response_hours": 8, "resolve_hours": 24},
        {"priority": "P4", "response_hours": 24, "resolve_hours": 72},
    ], rows
    assert synthesizer._parse_sla("somehow") == []


def test_explain_itsm_sections_and_sla():
    spec = synthesizer.build_itsm_spec({"who_resolves": "our own team"})
    frag = synthesizer.itsm_fragments(spec)
    sections = synthesizer.explain_itsm(spec, frag)
    for required in ("What is created", "Who participates", "How the process runs",
                     "Where a human is needed (signatures)", "What the agent does",
                     "SLA policy", "Open questions"):
        assert required in sections, "missing ITSM EXPLAIN section: %s" % required
    assert "P1: response 1h / resolve 4h" in sections["SLA policy"]


def test_itsm_business_diff_sla():
    old = synthesizer.build_itsm_spec({"who_resolves": "team", "sla_levels": "P1 1h/4h, P2 4h/8h, P3 8h/24h, P4 24h/72h"})
    new = synthesizer.build_itsm_spec({"who_resolves": "team", "sla_levels": "P1 1h/2h, P2 4h/8h, P3 8h/24h, P4 24h/72h"})
    lines = synthesizer.business_diff("itsm", old, new)
    assert "Before (SLA):" in "\n".join(lines)
    assert "P1: 1h/4h" in "\n".join(lines)
    assert "P1: 1h/2h" in "\n".join(lines)
    assert synthesizer.business_diff("itsm", old, old) == []


def test_build_spec_dispatch():
    ap = synthesizer.build_spec("ap", {"approval_authority": "director"})
    assert ap["pack"] == "ap"
    itsm = synthesizer.build_spec("itsm", {"who_resolves": "team"})
    assert itsm["pack"] == "itsm"
    try:
        synthesizer.build_spec("crm", {})
        raise AssertionError("crm has no builder yet — must refuse, not guess")
    except ValueError:
        pass
    assert synthesizer.change_topic_for("ap") == "amount_thresholds"
    assert synthesizer.change_topic_for("itsm") == "sla_levels"

#!/usr/bin/env python3
"""test_builder.py — protocol tests for workers/builder/builder.py (product
mission B). The node MCP is faked (capturing every call and its bearer), the
LLM is left unconfigured so the deterministic expert ladder runs — the exact
isolation pattern of workers/chat_runner/test_chat_runner.py.

What these tests prove (DEVELOPMENT_GRAPH.md P3 acceptance):
  1. pass-through: EVERY node call carries the caller's bearer, no other
     identity exists;
  2. UNDERSTAND: a session opens with the matched framework (ap for invoice
     goals), provenance labeled framework@version;
  3. DISCOVER: the FIRST question is a blocking question (approval authority
     class), never an optional optimization;
  4. question discipline: one question per expert turn;
  5. CONFIRM: an answer to a question becomes a DesignDecision(accepted) —
     and a bare fact statement does NOT become a decision (facts never feed
     MODEL on their own);
  6. no-framework case asks honestly instead of guessing;
  7. recommendations carry provenance and are labeled, not silently applied.
"""
import json
import os
import sys
import threading

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
import builder  # noqa: E402
import frameworks  # noqa: E402


class FakeNode:
    """In-memory fake of the node MCP surface: DesignSession/SessionMessage/
    DesignDecision storage + the tools the builder uses. Every call records
    the bearer it arrived under."""

    def __init__(self):
        self.records = {}          # entity -> {id: values}
        self.seq = {"DesignSession": 0, "SessionMessage": 0, "DesignDecision": 0}
        self.calls = []            # [(tool, bearer, arguments)]
        self.proposal_status = {"status": "pending"}  # what get_proposal reports
        self.live_invoices = []    # what query(VendorInvoice) returns
        self.deny_invoices = False  # simulate the caller lacking read rights
        self.has_ap_model = False  # what describe_system reports

    def mcp(self, bearer, tool, arguments):
        self.calls.append((tool, bearer, arguments))
        if tool == "create_record":
            ent = arguments["entity"]
            self.seq[ent] += 1
            rid = "%s-%d" % (ent, self.seq[ent])
            values = dict(arguments.get("values") or {})
            values["id"] = rid
            values["created_at"] = self.seq[ent]
            self.records.setdefault(ent, {})[rid] = values
            return {"id": rid, "values": dict(values)}, False
        if tool == "update_record":
            ent, rid = arguments["entity"], arguments["id"]
            if rid not in self.records.get(ent, {}):
                return {"code": "NOT_FOUND", "message": "missing"}, True
            self.records[ent][rid].update(arguments.get("values") or {})
            return {"id": rid}, False
        if tool == "get_record":
            ent, rid = arguments["entity"], arguments["id"]
            rec = self.records.get(ent, {}).get(rid)
            if rec is None:
                return {"code": "NOT_FOUND", "message": "missing"}, True
            return {"values": rec}, False
        if tool == "query":
            ent = arguments["entity"]
            if ent == "VendorInvoice":
                if self.deny_invoices:
                    return {"code": "PERMISSION_DENIED", "message": "no read"}, True
                return {"records": list(self.live_invoices)}, False
            rows = list(self.records.get(ent, {}).values())
            where = arguments.get("where", "")
            # generic-ish where filter for the faked fields the builder uses
            import re as _re
            for field, val in _re.findall(r'(\w+)\s*=\s*"([^"]*)"', where):
                rows = [r for r in rows if str(r.get(field, "")) == val]
            if "session" in where and "SessionMessage" in ent:
                sid = _re.search(r'session\s*=\s*"([^"]*)"', where).group(1)
                rows = [r for r in rows if r.get("session") == sid]
            sort = arguments.get("sort") or []
            desc = "-created_at" in sort
            if sort:
                rows = sorted(rows, key=lambda r: (r.get("turn", 0), r.get("id", "")),
                              reverse=desc)
            # emulate the worker's sort: turn first (conversation order),
            # then id for stability within a turn
            if ent == "SessionMessage":
                rows = sorted(rows, key=lambda r: (r.get("turn", 0), r.get("id", "")),
                              reverse=desc)
            return {"records": [{"id": r["id"], "values": r} for r in rows]}, False
        if tool == "compose_pack":
            # fake renderer: a marker DSL the validate step accepts; the REAL
            # renderer is the node (verified by the live opt-in test)
            self.composed = arguments
            return {"ok": True, "dsl": "pack ap\nversion 0.1.0\n"}, False
        if tool == "validate_dsl":
            self.validated = arguments
            return {"errors": []}, False
        if tool == "describe_system":
            entities = [{"name": "VendorInvoice"}] if self.has_ap_model else []
            return {"def_version": 3, "entities": entities}, False
        if tool == "get_proposal":
            return dict(self.proposal_status), False
        if tool == "get_definition":
            return {"files": {"builder.dsl": "pack builder\nversion 0.1.0\n"},
                    "def_version": 3}, False
        if tool == "propose_change":
            self.proposed = arguments
            return {"ok": True, "proposal_id": "prop-1"}, False
        return {"code": "NO_TOOL", "message": tool}, True


def _fresh(monkeypatch, node):
    monkeypatch.setattr(builder, "FRAMEWORKS", frameworks.load_frameworks())
    monkeypatch.setattr(builder, "LLM_URL", "")  # deterministic expert ladder
    monkeypatch.setattr(builder, "mcp_call",
                        lambda bearer, tool, args: node.mcp(bearer, tool, args))
    # per-test isolation: the worker's call-budget caches are module state
    monkeypatch.setattr(builder, "_SYNC_CACHE", {})
    monkeypatch.setattr(builder, "_BASE_CACHE", {})
    return node


def test_pass_through_bearer_on_every_call(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    builder.handle_turn("token-human-1", None, "Create an invoice approval system")
    assert node.calls, "the builder must talk to the node"
    for tool, bearer, _args in node.calls:
        assert bearer == "token-human-1", "every call must carry the caller's bearer, got %r" % (bearer,)


def test_understand_creates_session_with_framework(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    reply, session_id, state = builder.handle_turn(
        "tok", None, "We get invoices from system X, need approval")
    assert state["framework"] == "ap"
    sess = node.records["DesignSession"][session_id]
    assert sess["framework"].startswith("ap@")
    assert sess["goal"]


def test_first_question_is_blocking(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    _, session_id, state1 = builder.handle_turn(
        "tok", None, "Create an invoice approval system")
    assert state1["phase"] == "discover"
    assert state1["class"] == "required", "first question must be blocking, got %r" % (state1,)
    # the recorded expert question carries provenance + topic
    q_msgs = [m for m in node.records["SessionMessage"].values()
              if m["kind"] == "question"]
    assert q_msgs and q_msgs[0]["provenance"].startswith("ap@")
    assert q_msgs[0]["topic"] == state1["topic"]


def test_one_question_per_turn(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    _, session_id, _ = builder.handle_turn("tok", None, "Create an invoice approval system")
    for _ in range(3):
        _, _, state = builder.handle_turn("tok", session_id, "don't know")
        if state["phase"] != "discover":
            break
    q_msgs = [m for m in node.records["SessionMessage"].values()
              if m["kind"] == "question"]
    turns = {m["turn"] for m in q_msgs}
    assert len(turns) == len(q_msgs), "at most one question per turn"


def test_answer_becomes_accepted_decision_fact_does_not(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    _, session_id, state1 = builder.handle_turn(
        "tok", None, "Create an invoice approval system")
    topic = state1["topic"]

    # a plain fact (no open question) must NOT become a decision
    builder.classify_statement = lambda q, text: ("fact", "")  # type: ignore[assignment]
    _, _, _ = builder.handle_turn("tok", session_id, "All invoices are approved by the director")
    assert not node.records.get("DesignDecision", {}), "a bare fact must not become a decision"

    # an answer to the open question DOES become an accepted decision
    builder.classify_statement = lambda q, text: ("answer", "")  # type: ignore[assignment]
    _, _, state2 = builder.handle_turn("tok", session_id, "Approved by the director")
    decs = node.records.get("DesignDecision", {}).values()
    assert any(d["topic"] == topic and d["status"] == "accepted" for d in decs)
    if state2["phase"] == "discover":
        assert state2["topic"] != topic, "the answered question must not be asked again"


def test_no_framework_asks_honestly(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    reply, session_id, state = builder.handle_turn("tok", None, "I want to automate something")
    assert state["framework"] == ""
    assert "domain" in reply or "detail" in reply


def test_discovery_completion_is_labeled_not_applied(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    _, session_id, state1 = builder.handle_turn("tok", None, "Create an invoice approval system")
    _run_discovery(node, session_id)
    last_q = [m for m in node.records["SessionMessage"].values()
              if m["kind"] == "question"][-1]
    assert last_q["topic"] == "build_confirm"
    assert last_q["provenance"].startswith("ap@")
    assert "recommend" in last_q["body"]
    assert "not a rule" in last_q["body"]
    # nothing was applied: no propose_change call ever happened in discovery
    assert not any(tool == "propose_change" for tool, _, _ in node.calls)


def test_session_is_scoped_by_mcp_not_by_worker(monkeypatch):
    """The worker stores NO local state: session contents come back from the
    node each turn (governed store), the worker is stateless between turns."""
    node = _fresh(monkeypatch, FakeNode())
    _, session_id, _ = builder.handle_turn("tok", None, "Create an invoice approval system")
    before = len(node.records["SessionMessage"])
    builder.handle_turn("tok", session_id, "answer")
    after = len(node.records["SessionMessage"])
    assert after == before + 2, "each turn journals user + expert messages"


def _run_discovery(node, session_id):
    """Answer every required/important question deterministically; STOP when
    the build_confirm question appears (its answer belongs to the test)."""
    answers = {
        "what_is_invoice": "supplier invoices to pay",
        "approval_authority": "director",
        "amount_thresholds": "up to 100k manager, up to 1M CFO, above CEO",
        "contract_matching": "yes, we match",
        "po_matching": "no",
        "org_roles": "there are managers and a CFO",
    }
    state = None
    for _ in range(8):
        last_q = [m for m in node.records["SessionMessage"].values()
                  if m["kind"] == "question"]
        topic = last_q[-1]["topic"] if last_q else ""
        if topic == "build_confirm":
            break  # do not consume the build question here
        builder.classify_statement = lambda q, text: ("answer", "")  # type: ignore[assignment]
        _, _, state = builder.handle_turn(
            "tok", session_id, answers.get(topic, "yes, let's do that"))
    return state


def test_full_vertical_slice_model_then_propose(monkeypatch):
    """The AP vertical slice end-to-end over the faked node: goal phrase ->
    discovery -> MODEL (explain + simulate journaled) -> APPROVE (propose_change
    parked with the session id in the description)."""
    node = _fresh(monkeypatch, FakeNode())
    _, session_id, state1 = builder.handle_turn(
        "tok", None, "Create an invoice approval system")
    assert state1["framework"] == "ap"

    # answer discovery until the build question
    _run_discovery(node, session_id)
    last_q = [m for m in node.records["SessionMessage"].values()
              if m["kind"] == "question"][-1]
    assert last_q["topic"] == "build_confirm"

    # CONFIRM -> MODEL phase
    builder.classify_statement = lambda q, text: ("decision", "")  # type: ignore[assignment]
    reply, session_id, state = builder.handle_turn("tok", session_id, "yes, build")
    assert state["phase"] == "modeling"
    kinds = [m["kind"] for m in node.records["SessionMessage"].values()]
    assert "proposal_summary" in kinds, "EXPLAIN must be journaled before propose"
    assert "simulation" in kinds, "SIMULATE must be journaled before propose"
    assert not any(tool == "propose_change" for tool, _, _ in node.calls), \
        "MODEL must not propose without a second confirm"
    sess = node.records["DesignSession"][session_id]
    assert sess["status"] == "modeling"

    # second CONFIRM -> APPROVE phase (parked, never applied)
    builder.classify_statement = lambda q, text: ("decision", "")  # type: ignore[assignment]
    reply, session_id, state = builder.handle_turn("tok", session_id, "yes, propose")
    assert state["phase"] == "proposed"
    assert state["proposal_id"] == "prop-1"
    assert node.proposed["files"].get("ap.dsl"), "proposal must carry rendered DSL"
    assert node.proposed["files"].get("builder.dsl"), \
        "proposal must MERGE the current definition (additive gate)"
    assert node.proposed["base_def_version"] == 3
    assert session_id in node.proposed["description"], \
        "proposal description must correlate to the session"
    sess = node.records["DesignSession"][session_id]
    assert sess["status"] == "proposed"
    assert sess["proposal_id"] == "prop-1"
    assert "AWAITING your signature" in reply


def test_model_phase_validation_failure_never_proposes(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    _, session_id, _ = builder.handle_turn("tok", None, "Create an invoice approval system")
    _run_discovery(node, session_id)
    # force the fake validator to fail
    orig = node.mcp

    def failing(bearer, tool, args):
        if tool == "validate_dsl":
            return {"errors": [{"code": "E001", "message": "broken"}]}, False
        return orig(bearer, tool, args)

    node.mcp = failing
    builder.classify_statement = lambda q, text: ("decision", "")  # type: ignore[assignment]
    reply, session_id, state = builder.handle_turn("tok", session_id, "yes, build")
    assert state["phase"] == "validate_failed"
    assert "fails" in reply or "validation" in reply
    assert not any(tool == "propose_change" for tool, _, _ in node.calls), \
        "an invalid model must never be proposed"


def test_rejecting_build_confirm_changes_nothing(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    _, session_id, _ = builder.handle_turn("tok", None, "Create an invoice approval system")
    _run_discovery(node, session_id)
    builder.classify_statement = lambda q, text: ("decision", "")  # type: ignore[assignment]
    reply, session_id, state = builder.handle_turn("tok", session_id, "no, not yet")
    assert "won't change anything" in reply
    assert not any(tool in ("compose_pack", "propose_change") for tool, _, _ in node.calls)


def _build_live_model(node):
    """Drive a full create flow to a proposed session, then simulate the human
    decision (engine applied) — the precondition for a conversational CHANGE."""
    _, session_id, state1 = builder.handle_turn("tok", None, "Create an invoice approval system")
    assert state1["framework"] == "ap"
    _run_discovery(node, session_id)
    builder.classify_statement = lambda q, text: ("decision", "")  # type: ignore[assignment]
    _, session_id, state = builder.handle_turn("tok", session_id, "yes, build")
    assert state["phase"] == "modeling", state
    _, session_id, state = builder.handle_turn("tok", session_id, "yes, propose")
    assert state["phase"] == "proposed", state
    node.proposal_status = {"status": "applied", "def_version": 2}
    node.has_ap_model = True
    return session_id


def test_change_session_asks_only_for_the_new_rule(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    _build_live_model(node)
    builder.classify_statement = lambda q, text: ("goal", "")  # type: ignore[assignment]
    reply, sid, state = builder.handle_turn(
        "tok", None, "Now invoices above 2 million must additionally be approved by the owner")
    assert state["framework"] == "ap"
    sess = node.records["DesignSession"][sid]
    assert sess["mode"] == "change", sess
    qs = [m for m in node.records["SessionMessage"].values() if m["kind"] == "question"]
    assert qs and qs[-1]["topic"] == "amount_thresholds", qs


def test_change_model_phase_business_diff_and_impact(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    _build_live_model(node)
    node.live_invoices = [{"id": "inv-1", "values": {"amount": 2500000}}]
    builder.classify_statement = lambda q, text: ("goal", "")  # type: ignore[assignment]
    _, sid, state = builder.handle_turn(
        "tok", None, "Now invoices above 2M must be approved by the owner")
    assert state["framework"] == "ap"
    builder.classify_statement = lambda q, text: ("answer", "")  # type: ignore[assignment]
    _, sid, state = builder.handle_turn(
        "tok", sid, "up to 100k manager, up to 1M CFO, up to 2M CEO, above owner")
    assert state["phase"] == "discover"
    builder.classify_statement = lambda q, text: ("decision", "")  # type: ignore[assignment]
    reply, sid, state = builder.handle_turn("tok", sid, "yes, build")
    assert state["phase"] == "modeling", state
    summaries = [m["body"] for m in node.records["SessionMessage"].values()
                 if m["kind"] == "proposal_summary"]
    assert summaries, "the change must produce an EXPLAIN summary"
    summary = summaries[-1]
    assert "Before:" in summary and "After:" in summary, summary
    assert "1000000–2000000 — CEO" in summary and "above 2000000 — Owner" in summary, summary
    assert "would change route: 1" in summary, summary
    assert "Entities do not change." in summary
    assert "Integrations do not change" in summary


def test_change_approve_proposes_merged_sources(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    _build_live_model(node)
    builder.classify_statement = lambda q, text: ("goal", "")  # type: ignore[assignment]
    _, sid, _ = builder.handle_turn("tok", None, "Now invoices above 2M are approved by the owner")
    builder.classify_statement = lambda q, text: ("answer", "")  # type: ignore[assignment]
    _, sid, _ = builder.handle_turn("tok", sid, "up to 1M CFO, above owner")
    builder.classify_statement = lambda q, text: ("decision", "")  # type: ignore[assignment]
    _, sid, _ = builder.handle_turn("tok", sid, "yes, build")
    reply, sid, state = builder.handle_turn("tok", sid, "yes, propose")
    assert state["phase"] == "proposed", (state, reply)
    assert node.proposed["files"].get("ap.dsl")
    assert node.proposed["files"].get("builder.dsl"), "change proposals merge the definition too"
    sess = node.records["DesignSession"][sid]
    assert sess["status"] == "proposed" and sess["mode"] == "change"


def test_change_without_live_model_answers_honestly(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    node.has_ap_model = False
    builder.classify_statement = lambda q, text: ("goal", "")  # type: ignore[assignment]
    reply, sid, state = builder.handle_turn("tok", None, "Now invoices are approved by the CFO")
    assert sid == ""
    assert "CHANGE" in reply or "create" in reply


def test_change_impact_honest_when_caller_cannot_read(monkeypatch):
    node = _fresh(monkeypatch, FakeNode())
    _build_live_model(node)
    node.deny_invoices = True  # the operator cannot read invoices
    builder.classify_statement = lambda q, text: ("goal", "")  # type: ignore[assignment]
    _, sid, _ = builder.handle_turn("tok", None, "Now invoices above 2M are approved by the owner")
    builder.classify_statement = lambda q, text: ("answer", "")  # type: ignore[assignment]
    _, sid, _ = builder.handle_turn("tok", sid, "up to 1M CFO, above owner")
    builder.classify_statement = lambda q, text: ("decision", "")  # type: ignore[assignment]
    reply, sid, state = builder.handle_turn("tok", sid, "yes, build")
    assert state["phase"] == "modeling"
    assert "not calculated" in reply and "insufficient rights" in reply, reply


def test_second_change_preserves_original_options(monkeypatch):
    """Clean-room N3 regression: a change session's decisions are only its
    DELTA. The diff base must UNION all applied sessions' decisions, so a
    second change keeps the original build's optional elements (Contract)."""
    node = _fresh(monkeypatch, FakeNode())
    _build_live_model(node)  # build session: contract_matching = yes, applied

    # first change: new thresholds, apply it
    builder.classify_statement = lambda q, text: ("goal", "")  # type: ignore[assignment]
    _, sid, _ = builder.handle_turn("tok", None, "Now invoices above 2M are approved by the owner")
    builder.classify_statement = lambda q, text: ("answer", "")  # type: ignore[assignment]
    _, sid, _ = builder.handle_turn("tok", sid, "up to 100k manager, up to 1M CFO, up to 2M CEO, above owner")
    builder.classify_statement = lambda q, text: ("decision", "")  # type: ignore[assignment]
    _, sid, _ = builder.handle_turn("tok", sid, "yes, build")
    _, sid, state = builder.handle_turn("tok", sid, "yes, propose")
    assert state["phase"] == "proposed"
    node.proposal_status = {"status": "applied", "def_version": 3}

    # second change: the UNION base must keep Contract
    builder.classify_statement = lambda q, text: ("goal", "")  # type: ignore[assignment]
    _, sid2, _ = builder.handle_turn("tok", None, "Now invoices up to 50k are approved by the manager")
    builder.classify_statement = lambda q, text: ("answer", "")  # type: ignore[assignment]
    _, sid2, _ = builder.handle_turn("tok", sid2, "up to 50k manager, up to 1M CFO, up to 2M CEO, above owner")
    builder.classify_statement = lambda q, text: ("decision", "")  # type: ignore[assignment]
    reply, sid2, state = builder.handle_turn("tok", sid2, "yes, build")
    assert state["phase"] == "modeling", (state, reply)
    names = [e["name"] for e in node.composed.get("entities", [])]
    assert "Contract" in names, \
        "the second change must keep the original Contract option: %r" % names
    assert "Entities do not change." in reply, reply
    _, sid2, state = builder.handle_turn("tok", sid2, "yes, propose")
    assert state["phase"] == "proposed", (state,)


def test_itsm_vertical_slice_e2e(monkeypatch):
    """Generality-proof (P8): the SAME conversation loop builds an ITSM
    model — different framework, different questions, different spec builder;
    nothing in the protocol is AP-hardcoded."""
    node = _fresh(monkeypatch, FakeNode())
    _, sid, state = builder.handle_turn("tok", None, "We need a service desk with incidents and SLA")
    assert state["framework"] == "itsm", state

    answers = {
        "what_is_incident": "service outages and requests",
        "who_resolves": "our own team",
        "sla_levels": "P1 1h/4h, P2 4h/8h, P3 8h/24h, P4 24h/72h",
        "change_control": "yes, need CAB",
        "portal_self_service": "yes",
    }
    for _ in range(8):
        last_q = [m for m in node.records["SessionMessage"].values()
                  if m["kind"] == "question"]
        topic = last_q[-1]["topic"] if last_q else ""
        if topic == "build_confirm":
            break
        builder.classify_statement = lambda q, text: ("answer", "")  # type: ignore[assignment]
        _, sid, state = builder.handle_turn("tok", sid, answers.get(topic, "yes"))

    builder.classify_statement = lambda q, text: ("decision", "")  # type: ignore[assignment]
    reply, sid, state = builder.handle_turn("tok", sid, "yes, build")
    assert state["phase"] == "modeling", (state, reply)
    summaries = [m["body"] for m in node.records["SessionMessage"].values()
                 if m["kind"] == "proposal_summary"]
    assert summaries, "ITSM model must produce an EXPLAIN summary"
    assert "What is created" in summaries[-1]
    assert "Incident" in summaries[-1] and "Change" in summaries[-1]
    assert "P1: response 1h / resolve 4h" in summaries[-1]

    reply, sid, state = builder.handle_turn("tok", sid, "yes, propose")
    assert state["phase"] == "proposed", (state, reply)
    assert node.proposed["files"].get("itsm.dsl"), "proposal must carry the itsm pack"
    assert node.proposed["files"].get("builder.dsl"), "merged definition"


def test_builder_http_surface_and_cors(monkeypatch):
    """The HTTP surface: OPTIONS preflight is answered for LOOPBACK origins
    only (dev UI on another port), /healthz round-trips, and a turn without a
    bearer is refused AUTH_REQUIRED (the worker has no identity of its own)."""
    import threading
    import urllib.error
    import urllib.request
    from http.server import HTTPServer

    monkeypatch.setattr(builder, "FRAMEWORKS", frameworks.load_frameworks())
    srv = HTTPServer(("127.0.0.1", 0), builder.Handler)
    port = srv.server_address[1]
    th = threading.Thread(target=srv.serve_forever, daemon=True)
    th.start()
    base = "http://127.0.0.1:%d" % port
    try:
        # loopback preflight: CORS allowed
        req = urllib.request.Request(base + "/build/turn", data=b"", method="OPTIONS",
                                     headers={"Origin": "http://127.0.0.1:8080",
                                              "Access-Control-Request-Method": "POST"})
        with urllib.request.urlopen(req, timeout=5) as r:
            assert r.status == 204
            assert r.headers.get("Access-Control-Allow-Origin") == "http://127.0.0.1:8080"
        # non-loopback origin: no CORS grant (fail-closed)
        req = urllib.request.Request(base + "/build/turn", data=b"", method="OPTIONS",
                                     headers={"Origin": "https://evil.example",
                                              "Access-Control-Request-Method": "POST"})
        with urllib.request.urlopen(req, timeout=5) as r:
            assert r.headers.get("Access-Control-Allow-Origin") is None
        # healthz
        with urllib.request.urlopen(base + "/healthz", timeout=5) as r:
            body = json.loads(r.read())
            assert body["ok"] is True and set(body["frameworks"]) == {"ap", "itsm", "crm"}
        # a turn without a bearer: 401 AUTH_REQUIRED
        body = json.dumps({"message": "hello"}).encode()
        req = urllib.request.Request(base + "/build/turn", data=body,
                                     headers={"Content-Type": "application/json"})
        try:
            urllib.request.urlopen(req, timeout=5)
            raise AssertionError("a bearerless turn must be refused")
        except urllib.error.HTTPError as e:
            err = json.loads(e.read())
            assert e.code == 401 and err["code"] == "AUTH_REQUIRED", (e.code, err)
    finally:
        srv.shutdown()

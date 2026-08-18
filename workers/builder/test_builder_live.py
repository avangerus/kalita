#!/usr/bin/env python3
"""test_builder_live.py — the AP vertical slice against a REAL kalita node.

Opt-in (repo convention, cf. internal/mcp/author_live_model_test.go): runs only
with KALITA_LIVE_TEST=1. Spawns `kalita serve --pack packs/builder --demo
--approver ProcessOwner` (in-memory journal), then drives the FULL builder
conversation over real MCP under the demo human's bearer:

  фраза «Создай систему согласования счетов»
    → discovery (blocking/important вопросы, ответы)
    → MODEL: compose_pack (real renderer) + validate_dsl (real compiler)
    → EXPLAIN + SIMULATE journaled
    → APPROVE: propose_change parked (real engine)
    → человеческое решение через REST decide → applied pack, def_version+1
    → describe_system показывает живые сущности ap.*

This is the proof of DEVELOPMENT_GRAPH P4 acceptance: «фраза владельца →
валидный AP-пак → parked proposal → подпись → живая модель» — no LLM, no
developer workaround, no DSL handwritten anywhere.
"""
import json
import os
import socket
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
import builder  # noqa: E402
import frameworks  # noqa: E402

ROOT = os.path.abspath(os.path.join(_HERE, "..", ".."))
EXE = os.environ.get("KALITA_BIN", os.path.join(ROOT, "kalita.exe"))


def _free_port():
    """An ephemeral free port: dev nodes squat on fixed ports (8095 etc.), so
    a fixed test port collides and the spawned node dies AFTER printing its
    banner — a token from a node that never served. Random free port only."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _wait_http(url, timeout=30):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=2) as r:
                return r.status
        except (urllib.error.URLError, OSError):
            time.sleep(0.5)
    raise RuntimeError("node did not come up at %s" % url)


def _rest(path, token, body=None, base="http://127.0.0.1:0"):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(base + path, data=data, headers={
        "Content-Type": "application/json",
        "Authorization": "Bearer " + token,
    })
    with urllib.request.urlopen(req, timeout=30) as r:
        raw = r.read()
        return r.status, json.loads(raw) if raw else {}


def _get_text(path, token="", base="http://127.0.0.1:0"):
    """Fetch a NON-JSON resource (the static product shell)."""
    headers = {"Authorization": "Bearer " + token} if token else {}
    req = urllib.request.Request(base + path, headers=headers)
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.status, r.read().decode("utf-8", errors="replace")


def _live_test():
    port = str(_free_port())
    base = "http://127.0.0.1:" + port
    proc = subprocess.Popen(
        [EXE, "serve", "--pack", os.path.join(ROOT, "packs", "builder"),
         "--listen", "127.0.0.1:" + port, "--demo",
         "--approver", "ProcessOwner",
         "--ui-dir", os.path.join(ROOT, "web")],
        cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        text=True, encoding="utf-8", errors="replace")
    try:
        _wait_http(base + "/healthz")
        if proc.poll() is not None:
            raise RuntimeError("node exited at startup (port in use?): rc=%s" % proc.returncode)
        # the product shell is served: index.html carries the Ask Kalita surface
        status, body = _get_text("/", base=base)
        assert status == 200 and "Ask Kalita" in body, "product shell must serve #/ask"
        token = None
        # the demo banner prints "demo-ProcessOwner (ProcessOwner): <token>"
        import threading
        banner = []

        def drain():
            for line in proc.stdout:
                banner.append(line)
                if len(banner) > 400:
                    break

        t = threading.Thread(target=drain, daemon=True)
        t.start()
        deadline = time.time() + 20
        while token is None and time.time() < deadline:
            for line in banner:
                if "demo-ProcessOwner" in line:
                    token = line.split(":", 1)[1].strip()
            time.sleep(0.2)
        if not token:
            raise RuntimeError("demo token not found in banner: %s" % banner[-30:])

        builder.NODE = base
        builder.FRAMEWORKS = frameworks.load_frameworks()
        builder.LLM_URL = ""

        # 1) goal phrase → session with framework ap
        reply, sid, state = builder.handle_turn(token, None, "Создай систему согласования счетов")
        assert state["framework"] == "ap", state

        answers = {
            "what_is_invoice": "счета поставщиков к оплате",
            "approval_authority": "директор",
            "amount_thresholds": "до 100к руководитель, до 1М финдир, выше гендир",
            "contract_matching": "да, сверяем",
            "po_matching": "нет",
            "org_roles": "есть руководители и финдир",
        }
        # 2) discovery until the build question
        for _ in range(8):
            payload, is_err = builder.mcp_call(token, "query", {
                "entity": "SessionMessage",
                "where": 'session = "%s"' % sid, "sort": ["turn", "created_at"]})
            assert not is_err, payload
            questions = [r["values"] for r in payload.get("records", [])
                         if r["values"].get("kind") == "question"]
            topic = questions[-1]["topic"]
            if topic == "build_confirm":
                break
            builder.classify_statement = lambda q, text: ("answer", "")
            _, _, state = builder.handle_turn(token, sid, answers.get(topic, "да"))

        # 3) MODEL: real compose_pack + validate_dsl + EXPLAIN + SIMULATE
        builder.classify_statement = lambda q, text: ("decision", "")
        reply, sid, state = builder.handle_turn(token, sid, "да, построй")
        assert state["phase"] == "modeling", (state, reply)

        # 4) APPROVE: real propose_change parks a real proposal
        reply, sid, state = builder.handle_turn(token, sid, "да, предложи")
        assert state["phase"] == "proposed", (state, reply)
        proposal_id = state["proposal_id"]
        assert proposal_id

        # 5) parked until a human (ProcessOwner == defApprover here) decides
        status, body = _rest("/api/proposals", token, base=base)
        assert status == 200
        pending = [p for p in body["proposals"] if p["id"] == proposal_id]
        assert pending and pending[0]["status"] == "pending", body

        # 6) human decision via REST → applied, def_version+1
        status, body = _rest("/api/proposals/%s/decide" % proposal_id, token,
                             {"grant": True, "basis": {"type": "human", "id": "demo-ProcessOwner"}}, base=base)
        assert status == 200, (status, body)

        # 7) the described system is LIVE: ap entities exist, the agent role
        # has deny bounds, and the journal has the whole correlated chain
        sysinfo, is_err = builder.mcp_call(token, "describe_system", {})
        assert not is_err, sysinfo
        entities = {e["name"] for e in sysinfo.get("entities", [])}
        assert {"Vendor", "VendorInvoice", "Contract", "PaymentHandoff"} <= entities, entities
        sysinfo2, _ = builder.mcp_call(token, "describe_system", {})
        assert sysinfo2["def_version"] == 2, sysinfo2

        # 7b) the Ask UI's own data path: the session panel reads governed
        # records through POST /api/query (ABAC `where owner = $me`)
        status, body = _rest("/api/query/DesignSession", token,
                             {"sort": ["-created_at"], "limit": 20}, base=base)
        assert status == 200 and body.get("records"), body
        status, body = _rest("/api/query/SessionMessage", token,
                             {"where": 'session = "%s"' % sid,
                              "sort": ["turn", "created_at"], "limit": 500},
                             base=base)
        assert status == 200
        kinds = [r["values"]["kind"] for r in body.get("records", [])]
        assert "proposal_summary" in kinds and "simulation" in kinds, kinds

        # 8) CHANGE the live model conversationally. First, real work happens
        # in the system: an APClerk onboards via invite, creates a vendor and
        # a 2.5M invoice (which the OLD ladder would route to CEO).
        status, body = _rest("/api/invites", token,
                             {"role": "APClerk"}, base=base)
        assert status == 201, (status, body)
        invite_code = body["invite_code"]
        status, body = _rest("/api/register", "",
                             {"invite": invite_code, "id": "ap-clerk-1"}, base=base)
        assert status in (200, 201), (status, body)
        clerk_token = body["token"]
        vendor, is_err = builder.mcp_call(clerk_token, "create_record", {
            "entity": "Vendor", "values": {"name": "Vector LLC"},
            "basis": {"type": "human", "id": "ap-clerk-1"},
            "idempotency_key": "live-vendor-1"})
        assert not is_err, vendor
        invoice, is_err = builder.mcp_call(clerk_token, "create_record", {
            "entity": "VendorInvoice",
            "values": {"vendor": vendor["id"], "number": "INV-1", "amount": 2500000},
            "basis": {"type": "human", "id": "ap-clerk-1"},
            "idempotency_key": "live-inv-1"})
        assert not is_err, invoice

        # the change conversation (new session, change mode)
        builder.classify_statement = lambda q, text: ("goal", "")
        reply, sid2, state = builder.handle_turn(
            token, None, "Теперь счета выше 2М должен утверждать собственник")
        assert state["framework"] == "ap", state
        builder.classify_statement = lambda q, text: ("answer", "")
        reply, sid2, state = builder.handle_turn(
            token, sid2, "до 100к руководитель, до 1М финдир, до 2М гендир, выше собственник")
        builder.classify_statement = lambda q, text: ("decision", "")
        reply, sid2, state = builder.handle_turn(token, sid2, "да, построй")
        assert state["phase"] == "modeling", (state, reply)
        # the operator (ProcessOwner) has no read on invoices: the report must
        # say so honestly instead of guessing — and the diff must be human
        assert "Было:" in reply and "Станет:" in reply, reply
        assert "не посчитано" in reply, reply

        reply, sid2, state = builder.handle_turn(token, sid2, "да, предложи")
        assert state["phase"] == "proposed", (state, reply)
        proposal2 = state["proposal_id"]
        assert proposal2

        # 9) human decision on the change → applied, def_version 3
        status, body = _rest("/api/proposals/%s/decide" % proposal2, token,
                             {"grant": True, "basis": {"type": "human", "id": "demo-ProcessOwner"}}, base=base)
        assert status == 200, (status, body)
        sysinfo3, _ = builder.mcp_call(token, "describe_system", {})
        assert sysinfo3["def_version"] == 3, sysinfo3

        # 9b) SECOND change (clean-room N3 regression): the union base must
        # keep the original options (Contract) so the additive gate accepts
        builder.classify_statement = lambda q, text: ("goal", "")
        _, sid2b, state = builder.handle_turn(
            token, None, "Теперь счета до 50к утверждает руководитель")
        assert state["framework"] == "ap", state
        builder.classify_statement = lambda q, text: ("answer", "")
        _, sid2b, state = builder.handle_turn(
            token, sid2b, "до 50к руководитель, до 1М финдир, до 2М гендир, выше собственник")
        builder.classify_statement = lambda q, text: ("decision", "")
        reply, sid2b, state = builder.handle_turn(token, sid2b, "да, построй")
        assert state["phase"] == "modeling", (state, reply)
        assert "Сущности не меняются." in reply, reply
        reply, sid2b, state = builder.handle_turn(token, sid2b, "да, предложи")
        assert state["phase"] == "proposed", (state, reply)
        proposal2b = state["proposal_id"]
        status, body = _rest("/api/proposals/%s/decide" % proposal2b, token,
                             {"grant": True, "basis": {"type": "human", "id": "demo-ProcessOwner"}}, base=base)
        assert status == 200, (status, body)
        sysinfo3b, _ = builder.mcp_call(token, "describe_system", {})
        assert sysinfo3b["def_version"] == 4, sysinfo3b
        names3b = {e["name"] for e in sysinfo3b.get("entities", [])}
        assert "Contract" in names3b, \
            "the second change must keep Contract (union base): %r" % names3b

        # 10) SECOND DOMAIN on the same node (generality, mission B/C): the
        # SAME conversation loop builds an ITSM model — different framework,
        # different questions, different spec builder.
        builder.classify_statement = lambda q, text: ("goal", "")
        reply, sid3, state = builder.handle_turn(
            token, None, "Нам нужен service desk с инцидентами и SLA")
        assert state["framework"] == "itsm", state
        answers3 = {
            "what_is_incident": "сбои сервисов",
            "who_resolves": "своя команда",
            "sla_levels": "P1 1ч/4ч, P2 4ч/8ч, P3 8ч/24ч, P4 24ч/72ч",
            "change_control": "да, нужен CAB",
            "portal_self_service": "да",
        }
        for _ in range(8):
            payload, is_err = builder.mcp_call(token, "query", {
                "entity": "SessionMessage",
                "where": 'session = "%s"' % sid3, "sort": ["turn", "created_at"]})
            questions = [r["values"] for r in (payload or {}).get("records", [])
                         if r["values"].get("kind") == "question"]
            topic = questions[-1]["topic"] if questions else ""
            if topic == "build_confirm":
                break
            builder.classify_statement = lambda q, text: ("answer", "")
            _, sid3, state = builder.handle_turn(token, sid3, answers3.get(topic, "да"))
        builder.classify_statement = lambda q, text: ("decision", "")
        reply, sid3, state = builder.handle_turn(token, sid3, "да, построй")
        assert state["phase"] == "modeling", (state, reply)
        assert "Incident" in reply and "SLA" in reply, reply
        reply, sid3, state = builder.handle_turn(token, sid3, "да, предложи")
        assert state["phase"] == "proposed", (state, reply)
        proposal3 = state["proposal_id"]
        status, body = _rest("/api/proposals/%s/decide" % proposal3, token,
                             {"grant": True, "basis": {"type": "human", "id": "demo-ProcessOwner"}}, base=base)
        assert status == 200, (status, body)
        sysinfo4, _ = builder.mcp_call(token, "describe_system", {})
        assert sysinfo4["def_version"] == 5, sysinfo4
        entities4 = {e["name"] for e in sysinfo4.get("entities", [])}
        assert {"Incident", "Change", "SLAPolicy"} <= entities4, entities4
        # the ITSM model ships its SLA dashboard (Product Skeptic R10)
        dash, is_err = builder.mcp_call(token, "list_dashboards", {})
        assert not is_err, dash
        dash_names = [str(d.get("name", "")).lower() + str(d.get("title", "")).lower()
                      for d in (dash or {}).get("dashboards", [])]
        assert any("servicedesk" in n or "service desk" in n for n in dash_names), dash

        # 11) Changes/Audit data path: the model timeline lists ALL decisions
        # (applied + rejected), newest first — who changed the company, why
        status, body = _rest("/api/proposals?all=1", token, base=base)
        assert status == 200
        timeline = body.get("proposals", [])
        assert len(timeline) == 4, timeline
        assert all(p["status"] == "applied" for p in timeline), timeline
        assert timeline[0]["id"] == proposal3, "newest first (uuid v7 timeline)"
        assert any("builder session" in p["description"] for p in timeline)

        # 12) FULL HITL LOOP on the live generated pack (clean-room N2
        # regression: requires approval(X) must create a REAL approval):
        # clerk submits -> approver requests the gate -> approval exists ->
        # human decides -> approved -> agent hands off to Y.
        # Approver for >2M is Owner (after the change): onboard one.
        status, body = _rest("/api/invites", token, {"role": "Owner"}, base=base)
        assert status == 201, (status, body)
        status, body = _rest("/api/register", "",
                             {"invite": body["invite_code"], "id": "owner-live-1"}, base=base)
        assert status in (200, 201), (status, body)
        owner_token = body["token"]

        # clerk: Received -> Checking (the triage step), then submit
        status, body = _rest("/api/records/VendorInvoice/%s/act" % invoice["id"],
                             clerk_token, {"action": "start_checking",
                                           "basis": {"type": "human", "id": "ap-clerk-1"}}, base=base)
        assert status == 200, (status, body)
        # clerk: Checking -> ApprovalPending
        status, body = _rest("/api/records/VendorInvoice/%s/act" % invoice["id"],
                             clerk_token, {"action": "submit_check",
                                           "basis": {"type": "human", "id": "ap-clerk-1"}}, base=base)
        assert status == 200, (status, body)

        # owner: REQUEST the gate (act approve_tier_4) -> parked approval
        status, body = _rest("/api/records/VendorInvoice/%s/act" % invoice["id"],
                             owner_token, {"action": "approve_tier_4",
                                           "basis": {"type": "human", "id": "owner-live-1"}}, base=base)
        assert status == 200, (status, body)
        approval_id = body.get("approval_id") or (body.get("record") or {}).get("id")
        assert body.get("status") == "pending_approval" or body.get("approval_id"), body

        # the approval OBJECT must exist for the approver
        status, body = _rest("/api/approvals", owner_token, base=base)
        assert status == 200
        pending = [a for a in (body.get("approvals") or [])
                   if a.get("id") == approval_id or a.get("record_id") == invoice["id"]]
        assert pending, "requires approval must materialize a pending approval: %r" % body

        # human decides -> Approved
        status, body = _rest("/api/approvals/%s/decide" % pending[0]["id"], owner_token,
                             {"grant": True, "basis": {"type": "human", "id": "owner-live-1"}}, base=base)
        assert status == 200, (status, body)
        inv, _ = builder.mcp_call(clerk_token, "get_record",
                                  {"entity": "VendorInvoice", "id": invoice["id"]})
        assert (inv.get("values") or {}).get("status") == "Approved", inv

        # egress intent: the agent hands the approved invoice to Y
        status, body = _rest("/api/invites", token, {"role": "APAgent"}, base=base)
        assert status == 201, (status, body)
        status, body = _rest("/api/register", "",
                             {"invite": body["invite_code"], "id": "ap-agent-live"}, base=base)
        assert status in (200, 201), (status, body)
        agent_token = body["token"]
        env = dict(os.environ)
        env.update({"KALITA_URL": base, "KALITA_TOKEN": agent_token,
                    "KALITA_WORKER_ROLE": "APAgent", "KALITA_LLM_URL": "",
                    "KALITA_ONCE": "1"})
        subprocess.run([sys.executable, os.path.join(ROOT, "workers", "agent_runner", "runner.py")],
                       cwd=ROOT, env=env, capture_output=True, timeout=60)
        inv, _ = builder.mcp_call(clerk_token, "get_record",
                                  {"entity": "VendorInvoice", "id": invoice["id"]})
        assert (inv.get("values") or {}).get("status") == "SentToPayment", inv
        # the hand-off event is journaled (egress INTENT; delivery = gap D-011,
        # honestly disclosed by the product)
        status, body = _rest("/api/records/VendorInvoice/%s/journal" % invoice["id"],
                             clerk_token, base=base)
        assert status == 200
        kinds = [e.get("kind") for e in body.get("events", [])]
        assert "event.emitted" in kinds or "record.action" in kinds, kinds

        print("LIVE VERTICAL SLICE OK: goal phrase -> discovery -> model -> "
              "proposal %s -> human signature -> live ap.* entities; "
              "CHANGE conversationally -> diff -> proposal %s -> signature -> def_version 3; "
              "SECOND DOMAIN itsm -> proposal %s -> signature -> def_version 5; "
              "FULL HITL: submit -> approval request -> human decide -> agent hand-off"
              % (proposal_id, proposal2, proposal3))
        return True
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()


def test_live_vertical_slice():
    if os.environ.get("KALITA_LIVE_TEST") != "1":
        import pytest
        pytest.skip("set KALITA_LIVE_TEST=1 to run the live vertical slice")
    assert _live_test()

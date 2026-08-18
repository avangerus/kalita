#!/usr/bin/env python3
"""builder.py — the Business Expert + Conversational Builder worker (product
mission A/B, PRODUCT_ARCHITECTURE.md §3.3).

The worker is an ORDINARY ACTOR on the node: it has NO service credential.
Every node call carries the CALLER'S bearer (pass-through, workers/chat_runner
precedent, LLD-06 §1/§2) — the human chatting through the UI is the actor of
every session write, and the kernel's own Permission Engine gates everything
the model decides to do. The definition change path is the EXISTING one:
propose_change parks, a human with the defApprover role signs — the builder can
neither sign nor rush (internal/engine/proposals.go).

Knowledge-layer discipline (the load-bearing part):
  - layer 2 (frameworks): loaded via frameworks.py, sha256-verified, injected
    into prompts ONLY as SYSTEM text with provenance labels;
  - layer 3 (company facts): only user-confirmed statements and journaled
    records — an LLM assumption is never journaled as fact/decision;
  - layer 4 (executable model): never touched here except via
    validate_dsl/propose_change under the same bearer.

The conversation goes UNDERSTAND -> DISCOVER -> RECOMMEND -> CONFIRM (this
file) -> MODEL -> VALIDATE -> EXPLAIN -> SIMULATE -> APPROVE (synthesizer.py,
next step). State lives in packs/builder records (DesignSession/SessionMessage/
DesignDecision), so the session is journaled, ABAC-scoped and auditable —
transient working state never reaches the journal.

Zero non-stdlib dependencies. Run tests: pytest test_builder.py

Run:
    KALITA_URL=http://127.0.0.1:8095 \
    KALITA_BUILDER_LISTEN=127.0.0.1:8400 \
    python builder.py
"""
import json
import os
import sys
import time
import urllib.error
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
_WORKERS_DIR = os.path.dirname(_HERE)
sys.path.insert(0, os.path.join(_WORKERS_DIR, "agent_runner"))
import promptbox  # noqa: E402  (prompt-assembly contract, H4 layer 2)
import frameworks  # noqa: E402  (layer-2 trust loader, product mission A)
import synthesizer  # noqa: E402  (MODEL/VALIDATE/EXPLAIN/SIMULATE, P4)

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095").rstrip("/")
LISTEN = os.environ.get("KALITA_BUILDER_LISTEN", "127.0.0.1:8400")
LLM_URL = os.environ.get("KALITA_LLM_URL", "")
LLM_MODEL = os.environ.get("KALITA_LLM_MODEL", "openai/gpt-oss-20b")
LLM_KEY = os.environ.get("KALITA_LLM_KEY", "lm-studio")
MAX_BODY = 64 * 1024
MAX_QUESTIONS_PER_TURN = 3   # question discipline: no 50-question interviews
MAX_TURNS_BEFORE_FALLBACK = 8

# framework matching keywords: cheap deterministic pre-pass before the LLM
# classifier; a matched domain is only ever a PROPOSAL to the user (CONFIRM).
_FW_HINTS = {
    "ap": ["invoice", "invoices", "accounts payable", "bill", "billing",
           "payment", "vendor", "supplier", "creditor"],
    "itsm": ["incident", "service desk", "sla", "ticket", "helpdesk",
             "change", "request"],
    "crm": ["lead", "pipeline", "crm", "opportunity", "sales", "deal",
            "client", "customer"],
}


class BuilderError(Exception):
    pass


# --- MCP pass-through client (identical contract to chat_runner) ------------

def mcp_call(bearer, tool, arguments):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                        "params": {"name": tool, "arguments": arguments}}).encode()
    headers = {"Content-Type": "application/json",
               "Authorization": "Bearer " + bearer}
    req = urllib.request.Request(NODE + "/mcp", data=body, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            out = json.load(resp)
    except urllib.error.HTTPError as ex:
        try:
            out = json.loads(ex.read())
        except ValueError:
            raise BuilderError("HTTP %s from node" % (ex.code,))
    except urllib.error.URLError as ex:
        raise BuilderError("node unreachable: %s" % (ex,))
    if "error" in out and out["error"] is not None:
        return out["error"], True
    res = out.get("result") or {}
    content = res.get("content") or []
    payload = json.loads(content[0]["text"]) if content else {}
    return payload, bool(res.get("isError"))


def _idem(*parts):
    """Deterministic idempotency key (Architecture Skeptic MINOR-2): a
    network-retried turn re-derives the SAME key and the node dedupes the
    replay instead of double-writing. New conversations are distinct
    OPERATIONS — their call site passes an explicit nonce part."""
    return str(uuid.uuid5(uuid.NAMESPACE_URL, "|".join(str(p) for p in parts)))


# --- LLM boundary (model independence: env-only, isolated for tests) --------

def llm(messages, max_tokens=900):
    body = json.dumps({"model": LLM_MODEL, "messages": messages,
                        "temperature": 0.2, "max_tokens": max_tokens}).encode()
    req = urllib.request.Request(LLM_URL.rstrip("/") + "/chat/completions",
                                  data=body, headers={
                                      "Content-Type": "application/json",
                                      "Authorization": "Bearer " + LLM_KEY})
    with urllib.request.urlopen(req, timeout=180) as resp:
        out = json.load(resp)
    return out["choices"][0]["message"]["content"].strip()


def _extract_obj(text):
    t = (text or "").strip()
    if "```" in t:
        for part in t.split("```"):
            p = part.strip()
            if p[:4].lower() == "json":
                p = p[4:].strip()
            if p.startswith("{"):
                t = p
                break
    i, j = t.find("{"), t.rfind("}")
    if i >= 0 and j > i:
        try:
            o = json.loads(t[i:j + 1])
            return o if isinstance(o, dict) else None
        except ValueError:
            return None
    return None


# --- statement classification (CONVERSATIONAL_BUILDER.md §3) -----------------
# The Expert must distinguish: user fact / preference / answer, Kalita
# recommendation / risk, required decision, unknown. Classification is a MODEL
# decision over the message TEXT ONLY — the resulting label is data, and facts
# become model input only after DesignDecision(accepted) from the user.

_CLASSIFY_SYSTEM = (
    "You are the classifier inside the Kalita Business Expert. Read the user's "
    "latest message and label it with EXACTLY ONE kind. Reply with ONLY JSON: "
    '{"kind": "<kind>", "topic": "<short topic id or empty>"}. Kinds: '
    '"fact" (the user states something about their company: roles, rules, '
    "systems, numbers), \"preference\" (the user expresses a taste or an "
    "opinion), \"answer\" (a direct response to a question we asked), "
    "\"goal\" (the user describes what they want to achieve/build), "
    "\"decision\" (the user accepts or rejects an offered option), "
    "\"unknown\". Never invent facts the user did not state."
)


def classify_statement(question_asked, user_text):
    """Deterministic fallback when no model is configured; with a model, the
    LLM classifies. NEVER treats an assumption as a fact (the kind stays what
    the classifier said — the protocol requires CONFIRM before MODEL)."""
    if LLM_URL:
        messages = promptbox.build_messages(
            system=_CLASSIFY_SYSTEM,
            task="Classify the user-message data block below.",
            data=[("question-asked", question_asked or "(none)"),
                  ("user-message", user_text)],
        )
        obj = _extract_obj(llm(messages, max_tokens=120))
        if obj and obj.get("kind") in _KINDS:
            return obj["kind"], obj.get("topic") or ""
    # deterministic fallback (no model configured)
    if question_asked:
        return "answer", ""
    low = user_text.lower()
    if any(w in low for w in ("yes", "no", "ok", "okay", "sure", "agree",
                              "accept", "approve", "confirm", "build", "create",
                              "make", "need", "want", "please", "automate")):
        return ("decision" if any(w in low for w in ("yes", "no", "ok", "okay",
                                                     "sure", "agree", "accept",
                                                     "confirm", "approve")) else "goal", "")
    return "goal", ""


_KINDS = {"fact", "preference", "answer", "goal", "decision", "unknown"}


# --- framework matching (UNDERSTAND) ----------------------------------------

def match_framework(goal_text):
    """Deterministic keyword pre-pass; with a model, the LLM picks from the
    verified framework list. Returns a framework id that HAS a spec builder
    (synthesizer.BUILDABLE) or None — never a knowledge-only or made-up id:
    the result is validated against the loaded registry AND buildability
    (Architecture Skeptic MINOR-3)."""
    low = goal_text.lower()
    hits = {fid: sum(1 for w in words if w in low)
            for fid, words in _FW_HINTS.items()}
    best = max(hits.items(), key=lambda kv: kv[1])
    if best[1] > 0 and best[0] in synthesizer.BUILDABLE:
        return best[0]
    if LLM_URL:
        choices = "\n".join("- %s: %s" % (fid, fw["meta"]["name"])
                            for fid, fw in FRAMEWORKS.items()
                            if fid in synthesizer.BUILDABLE)
        messages = promptbox.build_messages(
            system=("You match a business goal to ONE Kalita business "
                    "framework. Reply with ONLY JSON "
                    '{"framework": "<id>"} or {"framework": null}.\n'
                    "Available frameworks:\n" + choices),
            task="Match the goal data block to a framework.",
            data=[("goal", goal_text)],
        )
        obj = _extract_obj(llm(messages, max_tokens=80))
        fid = (obj or {}).get("framework")
        if fid in FRAMEWORKS and fid in synthesizer.BUILDABLE:
            return fid
    return None


# --- session state (reads journaled records only) ---------------------------

def _get_session(bearer, session_id):
    payload, is_err = mcp_call(bearer, "get_record", {
        "entity": "DesignSession", "id": session_id})
    if is_err:
        raise BuilderError("cannot read design session: %r" % (payload,))
    return payload


def _messages(bearer, session_id):
    payload, is_err = mcp_call(bearer, "query", {
        "entity": "SessionMessage",
        "where": 'session = "%s"' % session_id,
        "sort": ["turn", "created_at"]})
    if is_err:
        raise BuilderError("cannot read session messages: %r" % (payload,))
    return (payload or {}).get("records") or []


def _decisions(bearer, session_id):
    payload, is_err = mcp_call(bearer, "query", {
        "entity": "DesignDecision",
        "where": 'session = "%s"' % session_id})
    if is_err:
        raise BuilderError("cannot read session decisions: %r" % (payload,))
    return (payload or {}).get("records") or []


def _post_message(bearer, session_id, author, kind, body, provenance="", turn=0, topic=""):
    payload, is_err = mcp_call(bearer, "create_record", {
        "entity": "SessionMessage",
        "values": {"session": session_id, "author": author, "kind": kind,
                   "body": body, "provenance": provenance, "turn": turn,
                   "topic": topic},
        "basis": {"type": "design_session", "id": session_id},
        "idempotency_key": _idem("msg", session_id, author, kind, turn, body)})
    if is_err:
        raise BuilderError("could not record a session message: %r" % (payload,))


def _post_decision(bearer, session_id, topic, decision, status, rationale=""):
    _, is_err = mcp_call(bearer, "create_record", {
        "entity": "DesignDecision",
        "values": {"session": session_id, "topic": topic,
                   "decision": decision, "status": status,
                   "rationale": rationale},
        "basis": {"type": "design_session", "id": session_id},
        "idempotency_key": _idem("dec", session_id, topic, decision, status)})
    if is_err:
        raise BuilderError("could not record a design decision")


# --- expert planning (DISCOVER/RECOMMEND/CONFIRM) ----------------------------

def next_blocking_question(fw, decisions):
    """The FIRST unanswered required question — blocking questions gate MODEL.
    Deterministic: no model may reorder or skip them."""
    decided = {d["values"].get("topic") for d in decisions
               if d["values"].get("status") == "accepted"}
    for q in frameworks.discovery_questions(fw):
        if q["class"] == "required" and q["id"] not in decided:
            return q
    return None


def next_important_question(fw, decisions, asked):
    decided = {d["values"].get("topic") for d in decisions
               if d["values"].get("status") == "accepted"}
    for q in frameworks.discovery_questions(fw):
        if q["class"] == "important" and q["id"] not in decided and q["id"] not in asked:
            return q
    return None


def plan_expert(bearer, session, fw, messages, decisions, user_text):
    """Decide the Expert's next action. Isolated for tests. Returns a dict:
    {"kind": "ask"|"ask_confirm"|"wait", ...}. The deterministic ladder below
    is the backbone; the LLM (when configured) may refine the WORDING and add
    recommendations — never reorder blocking questions."""
    mode = (session.get("values") or {}).get("mode", "create")
    # CHANGE mode (mission C): the model already exists — the ONLY discovery
    # is the new rule itself. The diff base (everything else) comes from the
    # original build session's journaled decisions, not from the user again.
    if mode == "change":
        topics = [d["values"].get("topic") for d in decisions
                  if d["values"].get("status") == "accepted"]
        topic = synthesizer.change_topic_for(fw["meta"]["id"])
        if topic and topic not in topics:
            q = frameworks.question_by_topic(fw, topic) or (
                "What are the new process rules?")
            return {"kind": "ask",
                    "question": q,
                    "topic": topic, "class": "required",
                    "why": "This is the model change — without a new rule there is nothing to change",
                    "provenance": "%s@%s" % (fw["meta"]["id"], fw["meta"]["version"])}
        return {"kind": "ask_confirm",
                "question": ("Understood the change. Build the updated model, "
                             "show the business diff and the impact on current "
                             "data BEFORE signing?"),
                "topic": "build_confirm",
                "provenance": "%s@%s" % (fw["meta"]["id"], fw["meta"]["version"])}
    # 1. blocking questions first, one at a time
    bq = next_blocking_question(fw, decisions)
    if bq:
        return {"kind": "ask", "question": bq["question"],
                "topic": bq["id"], "class": bq["class"],
                "why": bq["why"],
                "provenance": "%s@%s" % (fw["meta"]["id"], fw["meta"]["version"])}
    # 2. important questions next (with default shown)
    iq = next_important_question(fw, decisions, set())
    if iq:
        default = iq.get("default")
        q_text = iq["question"]
        if default:
            q_text += " (unless you say otherwise, I'll do this: %s)" % default
        return {"kind": "ask", "question": q_text, "topic": iq["id"],
                "class": iq["class"], "why": iq["why"],
                "default": default,
                "provenance": "%s@%s" % (fw["meta"]["id"], fw["meta"]["version"])}
    # 3. discovery satisfied: recommend and offer to build the model — the
    # recommendation is LABELED (framework@version) and the build is a
    # question, never an automatic apply (CONFIRM gate before MODEL)
    return {"kind": "ask_confirm",
            "question": (
                "Discovery complete. For framework %s@%s I recommend: %s. "
                "This is a recommendation, not a rule. Build the model and "
                "show it to you BEFORE signing?"
                % (fw["meta"]["id"], fw["meta"]["version"], fw["meta"]["name"])),
            "topic": "build_confirm",
            "provenance": "%s@%s" % (fw["meta"]["id"], fw["meta"]["version"])}


def _status_of(session):
    return (session.get("values") or {}).get("status", "open")


_YES_WORDS = (
    "yes", "ok", "okay", "sure", "build", "go", "proceed", "propose",
    "agree", "correct", "right", "please", "continue", "confirm", "approve",
    "accept", "yeah", "yep",
    # Russian aliases kept as harmless fallbacks
    "да", "ок", "построй", "строй", "предлож", "соглас", "подтвержд",
    "принят", "делай", "сделай",
)

_CHANGE_WORDS = (
    "change", "instead", "rather", "now", "update", "modify", "revise",
    "replace", "instead of", "no longer", "actually", "add", "remove",
    "different", "adjust", "move", "increase", "decrease",
    # Russian aliases kept as harmless fallbacks
    "теперь", "измени", "измен", "дополнительн", "вместо", "поменя",
    "убери", "добавь", "повысь", "понизь", "перенес", "по-новому",
)


def _is_yes(text):
    low = (text or "").lower()
    return any(w in low for w in _YES_WORDS)


def _is_change_intent(text):
    low = (text or "").lower()
    return any(w in low for w in _CHANGE_WORDS)


def _live_has_entity(bearer, entity_name):
    """Does the live definition already carry this entity? (change-mode
    detection: an existing company model, not a fresh build.)"""
    sys, is_err = mcp_call(bearer, "describe_system", {})
    if is_err:
        return False
    names = {e.get("name") for e in (sys or {}).get("entities", [])}
    return entity_name in names


_LIVE_ENTITY = {"ap": "VendorInvoice", "itsm": "Incident"}


def _live_has(bearer, fwid):
    return _live_has_entity(bearer, _LIVE_ENTITY.get(fwid, ""))


def _live_invoices(bearer):
    """Live invoice records for impact analysis (read-only, ABAC-scoped).
    Returns (records, limit_reason) — limit_reason is set when the caller
    cannot see the entity or the engine fails closed (empty result without an
    error): the report says so instead of guessing."""
    payload, is_err = mcp_call(bearer, "query", {"entity": "VendorInvoice"})
    if is_err:
        code = (payload or {}).get("code", "") if isinstance(payload, dict) else ""
        return [], "insufficient rights for your role to read invoices (%s)" % code
    rows = (payload or {}).get("records") or []
    if not rows:
        # the engine fails closed: an empty result may mean "no records" OR
        # "your role cannot read them" — say the ambiguity, never assume.
        return [], ("no invoices visible (0): either there are none, or your "
                    "role lacks read rights — impact on live records is "
                    "undetermined")
    return ([{"id": r.get("id", ""), "values": r.get("values", r)} for r in rows], "")


# Per-process call-budget caches (the node rate-limits 240 calls/min per
# actor; a long design conversation legitimately exceeds that unless the
# worker economizes). Cached values are DERIVED read-only facts; the journal
# stays the only source of truth — a cache hit only skips a re-read within
# its TTL, and every mutation still round-trips the node.
_SYNC_CACHE = {}   # bearer -> (ts, )  — session-status sync throttled
_BASE_CACHE = {}   # bearer -> (ts, decisions) — union base throttled
_SYNC_TTL = 10.0
_BASE_TTL = 30.0


def _sync_sessions(bearer, force=False):
    """Reflect the engine's truth into session statuses: a session parked at
    `proposed` is marked applied/rejected when its proposal was decided. The
    journal (get_proposal) is the only source — the worker never guesses.
    Throttled per bearer for the per-turn display path (F3); the CHANGE path
    always forces a fresh sync — a conversation started right after a decide
    must see the applied history."""
    now = time.monotonic()
    last = _SYNC_CACHE.get(bearer)
    if not force and last is not None and now - last < _SYNC_TTL:
        return
    _SYNC_CACHE[bearer] = now
    payload, is_err = mcp_call(bearer, "query", {
        "entity": "DesignSession",
        "where": 'status = "proposed"'})
    if is_err:
        return
    for r in (payload or {}).get("records") or []:
        vals = r.get("values", {})
        pid, rid = vals.get("proposal_id") or "", r.get("id") or ""
        if not pid or not rid:
            continue
        st, is_err = mcp_call(bearer, "get_proposal", {"proposal_id": pid})
        if is_err or not isinstance(st, dict):
            continue
        status = st.get("status")
        if status == "applied":
            _update_session(bearer, rid, {"status": "applied",
                                          "def_after": int(st.get("def_version") or 0)})
        elif status == "rejected":
            _update_session(bearer, rid, {"status": "rejected"})


def _base_decisions(bearer):
    """The diff base for a CHANGE: the UNION of accepted decisions across ALL
    applied ap build/change sessions, oldest-first with later values winning
    per topic. A change session's decisions are only its DELTA — the complete
    current model is the merge of every applied session's decisions (clean-
    room N3: taking only the latest session silently dropped optional
    elements like contract matching). The journal is the only source."""
    now = time.monotonic()
    cached = _BASE_CACHE.get(bearer)
    if cached is not None and now - cached[0] < _BASE_TTL:
        return cached[1]
    _sync_sessions(bearer, force=True)  # the change path needs FRESH truth
    payload, is_err = mcp_call(bearer, "query", {
        "entity": "DesignSession",
        "where": 'status = "applied"',
        "sort": ["created_at"], "limit": 100})
    if is_err:
        raise BuilderError("cannot find the original build session: %r" % (payload,))
    rows = [r for r in ((payload or {}).get("records") or [])
            if (r.get("values", {}).get("framework") or "").startswith("ap@")]
    if not rows:
        raise BuilderError("no applied AP build session found — the change "
                           "needs the original design history; build the "
                           "model first, then change it conversationally")
    merged = []  # oldest-first; decision_map-style later-wins by topic
    for r in sorted(rows, key=lambda x: x.get("values", {}).get("created_at", 0)):
        sid = r.get("id", "")
        payload, is_err = mcp_call(bearer, "query", {
            "entity": "DesignDecision",
            "where": 'session = "%s"' % sid})
        if is_err:
            raise BuilderError("cannot read a build session's decisions: %r" % (payload,))
        merged.extend(x for x in ((payload or {}).get("records") or [])
                      if x.get("values", {}).get("status") == "accepted")
    _BASE_CACHE[bearer] = (now, merged)
    return merged


def _update_session(bearer, session_id, values):
    _, is_err = mcp_call(bearer, "update_record", {
        "entity": "DesignSession", "id": session_id, "values": values,
        "basis": {"type": "design_session", "id": session_id},
        "idempotency_key": _idem("upd", session_id, json.dumps(values, sort_keys=True))})
    if is_err:
        raise BuilderError("could not update the design session")


def _compose_and_validate(bearer, spec, fwid=""):
    """MODEL→VALIDATE: structured spec → node-rendered DSL + curated top-level
    fragments (declared events, dashboards) → compile-check. Returns
    (dsl_text, errors_list). The assembler is deterministic; the node is the
    only renderer and the only validator."""
    clean = {k: v for k, v in spec.items() if not k.startswith("_")}
    composed, is_err = mcp_call(bearer, "compose_pack", clean)
    if is_err:
        return None, [{"code": "COMPOSE", "message": str(composed)}]
    dsl = (composed or {}).get("dsl")
    if not dsl:
        return None, [{"code": "COMPOSE", "message": "no DSL rendered"}]
    # append the curated TOP-LEVEL fragments: declared events and dashboards
    # (the compose schema cannot express them; they come from the golden-
    # derived templates with explicit provenance)
    if fwid == "ap":
        frag = synthesizer.ap_fragments(spec)
        dsl += "\n" + frag["events"] + "\n" + frag["dashboard"] + "\n"
    elif fwid == "itsm":
        frag = synthesizer.itsm_fragments(spec)
        dsl += "\n" + frag["dashboard"] + "\n"
    validated, is_err = mcp_call(bearer, "validate_dsl",
                                 {"files": {"ap.dsl": dsl}})
    if is_err:
        return dsl, [{"code": "VALIDATE", "message": str(validated)}]
    errs = (validated or {}).get("errors") or []
    return dsl, errs


def _model_phase(bearer, session, session_id, decs, turn):
    """MODEL→VALIDATE→EXPLAIN→SIMULATE (P4/P5). Re-runs deterministically from
    the decision set — no transient worker state between turns. In CHANGE mode
    the diff base is the original build session's journaled decisions; the
    EXPLAIN becomes a business diff + impact analysis over live records."""
    fwid = (session.get("values") or {}).get("framework", "").split("@")[0]
    mode = (session.get("values") or {}).get("mode", "create")
    dmap = synthesizer.decision_map(decs)

    if mode == "change":
        base = _base_decisions(bearer)
        old_spec = synthesizer.build_spec(fwid, synthesizer.decision_map(base))
        new_spec = synthesizer.build_spec(fwid, synthesizer.decision_map(base + decs))
        dsl, errs = _compose_and_validate(bearer, new_spec, fwid)
        if errs:
            brief = "; ".join("%s: %s" % (e.get("code"), e.get("message")) for e in errs[:5])
            reply = ("The updated model fails formal validation: %s. "
                     "The change will not be proposed." % brief)
            _post_message(bearer, session_id, "expert", "validation_error", reply,
                          provenance="assembler v1", turn=turn)
            return reply, session_id, {"phase": "validate_failed", "framework": fwid}
        summary_lines = ["**Model change (%s)**" % fwid]
        diff_lines = synthesizer.business_diff(fwid, old_spec, new_spec)
        summary_lines.extend(diff_lines or ["The rule did not change — check the wording."])
        # permission/approval impact (structural): roles and gates delta
        old_roles = {r["name"] for r in old_spec["roles"]}
        new_roles = {r["name"] for r in new_spec["roles"]}
        if new_roles - old_roles:
            summary_lines.append("New roles: %s" % ", ".join(sorted(new_roles - old_roles)))
        if old_roles - new_roles:
            summary_lines.append("Roles that lost a gate: %s"
                                 % ", ".join(sorted(old_roles - new_roles)))
        old_ents = {e["name"] for e in old_spec["entities"]}
        new_ents = {e["name"] for e in new_spec["entities"]}
        if old_ents == new_ents:
            summary_lines.append("Entities do not change.")
        summary_lines.append("Integrations do not change (same events).")
        if fwid == "ap":
            invoices, limit_reason = _live_invoices(bearer)
            if limit_reason:
                summary_lines.append("Impact on live invoices not calculated: %s."
                                     % limit_reason)
            else:
                summary_lines.extend(synthesizer.impact_report_ladder(
                    old_spec, new_spec, invoices))
        else:
            summary_lines.append("Impact on live records: changing the SLA "
                                 "policy changes timings, not routes — it will "
                                 "be checked on new records.")
        summary = "\n".join(summary_lines)
        _post_message(bearer, session_id, "expert", "proposal_summary", summary,
                      provenance="assembler v1 (diff: old vs new)",
                      turn=turn)
        _update_session(bearer, session_id, {"status": "modeling"})
        reply = (summary + "\n\nPropose this change? It is applied only after "
                 "your signature (Owner role).")
        _post_message(bearer, session_id, "expert", "question", reply,
                      provenance="assembler v1", turn=turn,
                      topic="propose_confirm")
        return reply, session_id, {"phase": "modeling", "framework": fwid}

    spec = synthesizer.build_spec(fwid, dmap)
    dsl, errs = _compose_and_validate(bearer, spec, fwid)
    if errs:
        brief = "; ".join("%s: %s" % (e.get("code"), e.get("message")) for e in errs[:5])
        reply = ("The model fails formal validation: %s. This is an assembler "
                 "error — the process will not be proposed until it is green."
                 % brief)
        _post_message(bearer, session_id, "expert", "validation_error", reply,
                      provenance="assembler v1", turn=turn)
        return reply, session_id, {"phase": "validate_failed", "framework": fwid}
    # EXPLAIN — deterministic human summary (what/whom/process/rights/agent/
    # gates/integrations/open questions), never raw DSL on the business level
    if fwid == "itsm":
        fragments = synthesizer.itsm_fragments(spec)
        sections = synthesizer.explain_itsm(spec, fragments)
        summary_lines = []
        for title, lines in sections.items():
            summary_lines.append("**%s**" % title)
            summary_lines.extend("- %s" % l for l in lines)
        summary = "\n".join(summary_lines)
        sim_lines = ["SLA policy (per your decisions):"]
        sim_lines.extend("- %s: response %dh / resolve %dh"
                         % (s["priority"], s["response_hours"], s["resolve_hours"])
                         for s in spec.get("_sla", []))
    else:
        fragments = synthesizer.ap_fragments(spec)
        sections = synthesizer.explain_spec(spec, fragments)
        summary_lines = []
        for title, lines in sections.items():
            summary_lines.append("**%s**" % title)
            summary_lines.extend("- %s" % l for l in lines)
        summary = "\n".join(summary_lines)
        # SIMULATE — honest walker: evaluates the GENERATED tier guards (the
        # same `when` strings the engine runs) over the subset grammar
        traces = synthesizer.walk_ladder(spec, [
            {"values": {"amount": a}} for a in (50_000, 100_000, 500_000, 1_500_000)])
        sim_lines = ["Scenario run over thresholds (walker approximation; the "
                     "engine will check at runtime):"]
        for t in traces:
            if t["ok"]:
                sim_lines.append("- invoice %s → approved by %s" % (t["amount"], t["tier_role"]))
            else:
                sim_lines.append("- invoice %s → route undetermined (I'll ask a human)" % t["amount"])
    _post_message(bearer, session_id, "expert", "proposal_summary", summary,
                  provenance="assembler v1 (from confirmed decisions)",
                  turn=turn)
    _post_message(bearer, session_id, "expert", "simulation", "\n".join(sim_lines),
                  provenance="deterministic (from decisions)", turn=turn)
    _update_session(bearer, session_id, {"status": "modeling"})
    reply = (summary + "\n\n" + "\n".join(sim_lines) + "\n\n"
             "Propose this system change? It will be applied only after your "
             "signature (Owner role).")
    _post_message(bearer, session_id, "expert", "question", reply,
                  provenance="assembler v1", turn=turn,
                  topic="propose_confirm")
    return reply, session_id, {"phase": "modeling", "framework": fwid}


def _approve_phase(bearer, session, session_id, decs, turn):
    """APPROVE: re-synthesize deterministically (change mode: base decisions +
    the new rule), merge into the CURRENT definition sources (a proposal
    REPLACES the whole definition — the additive gate would otherwise reject
    us for 'removing' existing packs), then propose_change — parked for the
    human signature. The builder NEVER applies."""
    fwid = (session.get("values") or {}).get("framework", "").split("@")[0]
    mode = (session.get("values") or {}).get("mode", "create")
    if mode == "change":
        base = _base_decisions(bearer)
        spec = synthesizer.build_spec(
            fwid, synthesizer.decision_map(base + decs))
    else:
        spec = synthesizer.build_spec(fwid, synthesizer.decision_map(decs))
    dsl, errs = _compose_and_validate(bearer, spec, fwid)
    if errs:
        reply = ("The model is no longer valid (%s) — the decisions changed "
                 "since it was shown. Tell me what changed and I'll rebuild."
                 % errs[0].get("code"))
        _post_message(bearer, session_id, "expert", "validation_error", reply,
                      provenance="assembler v1", turn=turn)
        return reply, session_id, {"phase": "validate_failed", "framework": ""}
    sys, is_err = mcp_call(bearer, "describe_system", {})
    if is_err:
        raise BuilderError("describe_system failed: %r" % (sys,))
    base_def = int((sys or {}).get("def_version", 0))
    # merge: current sources + the new pack; the engine re-validates ALL of it
    defn, is_err = mcp_call(bearer, "get_definition", {})
    if is_err:
        raise BuilderError("get_definition failed: %r" % (defn,))
    merged = dict((defn or {}).get("files") or {})
    merged[fwid + ".dsl"] = dsl
    fw_label = (FRAMEWORKS.get(fwid, {}).get("meta", {}).get("version", ""))
    prop, is_err = mcp_call(bearer, "propose_change", {
        "files": merged,
        "base_def_version": base_def,
        # F4: the audit trail names BOTH authorities — the human initiator
        # (journal actor) and the Business Expert that designed the model
        "description": ("[builder session %s] %s process "
                        "(designed by Business Expert, framework %s@%s)"
                        % (session_id, fwid, fwid, fw_label)),
        "basis": {"type": "design_session", "id": session_id},
        "idempotency_key": _idem("prop", session_id, base_def),
    })
    if is_err:
        reply = "The proposal was rejected by the node: %s" % (prop,)
        _post_message(bearer, session_id, "expert", "validation_error", reply,
                      provenance="node", turn=turn)
        return reply, session_id, {"phase": "propose_failed", "framework": ""}
    proposal_id = (prop or {}).get("proposal_id", "")
    _update_session(bearer, session_id, {
        "status": "proposed", "proposal_id": proposal_id, "def_before": base_def})
    reply = ("Proposal %s created and AWAITING your signature (Approvals "
             "section, Owner role). Nothing will be applied without a "
             "signature."
             % proposal_id)
    _post_message(bearer, session_id, "expert", "status", reply,
                  provenance="node", turn=turn)
    return reply, session_id, {
        "phase": "proposed", "framework": "", "proposal_id": proposal_id}


def handle_turn(bearer, session_id, text):
    """One builder turn (public). Wraps _handle_turn with a structured log
    line: one JSON per turn, correlated by session_id — the observability
    contract user request → design session → model proposal → signed change
    (PRODUCT_ARCHITECTURE.md §7)."""
    t0 = time.monotonic()
    reply, sid, state = _handle_turn(bearer, session_id, text)
    print(json.dumps({
        "event": "builder.turn",
        "session_id": sid,
        "phase": state.get("phase", ""),
        "framework": state.get("framework", ""),
        "proposal_id": state.get("proposal_id", ""),
        "turn_ms": round((time.monotonic() - t0) * 1000, 1),
    }, ensure_ascii=False))
    return reply, sid, state


def _handle_turn(bearer, session_id, text):
    """One builder turn. Returns (reply, session_id, state) where state is a
    structured dict for the UI (phase, framework, open questions). Every node
    call carries `bearer` — no other identity exists in this closure."""
    text = (text or "").strip()
    if not text:
        raise BuilderError("empty message")

    # F3: reflect the engine's truth into session statuses on EVERY turn (not
    # only inside change flows) — a session whose proposal was decided shows
    # applied/rejected in the panel instead of stuck at `proposed`
    _sync_sessions(bearer)

    # UNDERSTAND: open the design session on the first turn. A CHANGE intent
    # ("now ...") against an existing model opens a change session — the
    # diff base is the original build session's journaled decisions.
    if not session_id:
        fwid = match_framework(text)
        framework_label = (fwid + "@" + FRAMEWORKS[fwid]["meta"]["version"]) if fwid else ""
        mode = "create"
        if _is_change_intent(text) and fwid in _LIVE_ENTITY:
            if _live_has(bearer, fwid):
                mode = "change"
            else:
                reply = ("It looks like you want to CHANGE the process, but it "
                         "does not exist in the system yet. Say \"create …\" — "
                         "we'll build it from scratch, and then any changes — "
                         "conversationally.")
                # no session opened: return the honest answer as state
                return reply, "", {"phase": "understand", "framework": fwid or ""}
        rec, is_err = mcp_call(bearer, "create_record", {
            "entity": "DesignSession",
            "values": {"goal": text[:200], "framework": framework_label,
                       "mode": mode},
            "basis": {"type": "human", "id": "operator"},
            # a NEW conversation is a NEW operation: explicit nonce part
            "idempotency_key": _idem("session", text, uuid.uuid4().hex)})
        if is_err:
            raise BuilderError("could not open a design session: %r" % (rec,))
        session_id = rec["id"]
        session = {"values": rec.get("values", rec) or {}}
        session_is_new = True
    else:
        session = _get_session(bearer, session_id)
        session_is_new = False
    msgs = _messages(bearer, session_id)
    decs = _decisions(bearer, session_id)
    turn = len(msgs) + 1

    # classify the user statement (kind is DATA, recorded for the audit trail)
    last_q = None
    last_topic = ""
    for m in reversed(msgs):
        if m["values"].get("kind") == "question":
            last_q = m["values"].get("body")
            last_topic = m["values"].get("topic") or ""
            break
    kind, topic = classify_statement(last_q, text)
    _post_message(bearer, session_id, "user", kind, text,
                  provenance="user statement", turn=turn, topic=topic)

    # CONFIRM: an answer/decision to the Expert's question is the user's
    # confirmation — record it as a DesignDecision. ONLY accepted decisions
    # feed MODEL (protocol rule); facts alone never do. Re-read afterwards:
    # the expert plan must see THIS turn's decision (otherwise the answered
    # question would be asked again).
    if kind in ("answer", "decision") and last_topic:
        _post_decision(bearer, session_id, last_topic, text, "accepted",
                       rationale="user statement: " + text[:200])
        # see THIS turn's decision without a re-query: append it locally for
        # the CURRENT turn's planning only — the journal holds it for all
        # later turns (worker stays stateless between turns)
        decs = decs + [{"values": {"topic": last_topic, "decision": text,
                                   "status": "accepted"}}]

    fwid = (session.get("values") or {}).get("framework", "").split("@")[0]
    fw = FRAMEWORKS.get(fwid) if fwid else None
    if fw is None:
        # no framework matched: ask the user which domain, honestly
        reply = ("I don't yet understand which domain this belongs to. "
                 "Describe the goal in more detail — for example, \"invoice "
                 "approval\", \"service desk\", or \"sales\".")
        _post_message(bearer, session_id, "expert", "question", reply,
                      provenance="builder (no framework)", turn=turn)
        return reply, session_id, {"phase": "understand", "framework": ""}

    # CONFIRM gates first: answers to the build/propose questions trigger the
    # MODEL and APPROVE phases — never plan_expert's discovery ladder.
    if last_topic == "build_confirm" and _is_yes(text):
        return _model_phase(bearer, session, session_id, decs, turn)
    if last_topic == "propose_confirm" and _is_yes(text):
        return _approve_phase(bearer, session, session_id, decs, turn)
    if last_topic in ("build_confirm", "propose_confirm"):
        reply = "Understood, I won't change anything. Tell me what to adjust."
        _post_message(bearer, session_id, "expert", "status", reply, turn=turn)
        return reply, session_id, {"phase": "discover", "framework": fwid}

    plan = plan_expert(bearer, session, fw, msgs, decs, text)

    # question budget: never more than MAX_QUESTIONS_PER_TURN new questions
    if plan["kind"] in ("ask", "ask_confirm"):
        reply = plan["question"]
        if plan.get("why"):
            reply += "\n\n(Why I'm asking: %s)" % plan["why"]
        _post_message(bearer, session_id, "expert", "question", reply,
                      provenance=plan.get("provenance", ""), turn=turn,
                      topic=plan.get("topic", ""))
        return reply, session_id, {
            "phase": "discover", "framework": fwid,
            "topic": plan["topic"], "class": plan.get("class", "")}

    reply = "Noted. What's next?"
    _post_message(bearer, session_id, "expert", "status", reply, turn=turn)
    return reply, session_id, {"phase": "understand", "framework": fwid}


# --- HTTP surface (same contract shape as chat_runner) ----------------------

class Handler(BaseHTTPRequestHandler):
    def _cors_headers(self):
        """Same-host CORS: the dev UI runs on another PORT of the SAME host
        (or loopback); production serves the worker same-origin behind a
        proxy (PRODUCT_UX §2.3). Any other origin gets no CORS grant —
        fail-closed."""
        origin = self.headers.get("Origin") or ""
        host = (self.headers.get("Host") or "").split(":")[0]
        ohost = ""
        try:
            from urllib.parse import urlsplit
            ohost = urlsplit(origin).hostname or ""
        except Exception:
            ohost = ""
        if ohost and (ohost in ("127.0.0.1", "localhost") or ohost == host):
            return {"Access-Control-Allow-Origin": origin,
                    "Access-Control-Allow-Headers": "Content-Type, Authorization",
                    "Access-Control-Allow-Methods": "POST, GET, OPTIONS",
                    "Access-Control-Max-Age": "600"}
        return {}

    def _send(self, obj, code=200):
        body = json.dumps(obj).encode()
        self.send_response(code)
        for k, v in self._cors_headers().items():
            self.send_header(k, v)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_OPTIONS(self):
        self.send_response(204)
        for k, v in self._cors_headers().items():
            self.send_header(k, v)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_GET(self):
        if self.path in ("/healthz", "/readyz"):
            self._send({"ok": True, "frameworks": sorted(FRAMEWORKS)})
            return
        self._send({"code": "NOT_FOUND"}, 404)

    def do_POST(self):
        if self.path != "/build/turn":
            self._send({"code": "NOT_FOUND"}, 404)
            return
        auth = self.headers.get("Authorization") or ""
        bearer = auth[len("Bearer "):] if auth.startswith("Bearer ") else ""
        if not bearer:
            self._send({"code": "AUTH_REQUIRED",
                        "message": "missing bearer token — the builder has none of its own"}, 401)
            return
        length = int(self.headers.get("Content-Length") or 0)
        if length <= 0 or length > MAX_BODY:
            self._send({"code": "VALIDATION_ERROR", "message": "bad content-length"}, 400)
            return
        try:
            req = json.loads(self.rfile.read(length))
        except ValueError:
            self._send({"code": "VALIDATION_ERROR", "message": "bad json"}, 400)
            return
        text = (req.get("message") or "").strip()
        if not text:
            self._send({"code": "VALIDATION_ERROR", "message": "message is empty"}, 400)
            return
        try:
            reply, session_id, state = handle_turn(bearer, req.get("session_id"), text)
        except BuilderError as ex:
            self._send({"code": "UPSTREAM_ERROR", "message": str(ex)}, 502)
            return
        self._send({"session_id": session_id, "reply": reply, "state": state})

    def log_message(self, *_args):
        pass


FRAMEWORKS = {}


def main():
    global FRAMEWORKS
    FRAMEWORKS = frameworks.load_frameworks()  # fail-closed at startup
    host, _, port = LISTEN.partition(":")
    srv = ThreadingHTTPServer((host, int(port)), Handler)
    print("builder online at %s, node=%s, frameworks=%s%s"
          % (LISTEN, NODE, ",".join(sorted(FRAMEWORKS)),
             " with model " + LLM_MODEL if LLM_URL else " (deterministic)"))
    srv.serve_forever()


if __name__ == "__main__":
    main()

"""Agent runner — the last mile of the opus magnum.

A long-lived worker that logs in to a kalita node over MCP, takes tasks from its
role's pool, does the work, and completes them. It IS an agent employee: it has
an identity, a bearer token, permissions and an audit trail, and a human accepts
its critical output behind a signature (HITL).

The "do the work" step is pluggable. The default `handle` performs the task's
declared workflow action — enough to drive a pipeline. For open-ended work
(write code, draft a reply, analyze a record) plug an LLM: read the record with
`mcp(... "get_record" ...)`, produce output, write it back with `update_record`,
then `act` the transition. Swap `handle` for your handler; the loop is the same.

Run:
    KALITA_URL=http://127.0.0.1:8095 \
    KALITA_TOKEN=<engineer bearer token> \
    python runner.py
or self-register with KALITA_BOOTSTRAP_SECRET + KALITA_WORKER_ID + _ROLE.
"""
import json
import os
import sys
import time
import urllib.request
import urllib.error

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from bootstrap import get_token  # noqa: E402
import promptbox  # noqa: E402  (prompt-assembly contract, LLD-01 §6)

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
MCP_URL = NODE + "/mcp"
# Local, on-prem model (LM Studio / Ollama, OpenAI-compatible). Empty = the agent
# only drives the workflow deterministically; set it and the agent does the work.
LLM_URL = os.environ.get("KALITA_LLM_URL", "")
LLM_MODEL = os.environ.get("KALITA_LLM_MODEL", "openai/gpt-oss-20b")
LLM_KEY = os.environ.get("KALITA_LLM_KEY", "lm-studio")
_id = 0


def llm(messages, max_tokens=400):
    """One chat completion against the local OpenAI-compatible endpoint."""
    body = json.dumps({"model": LLM_MODEL, "messages": messages,
                       "temperature": 0.3, "max_tokens": max_tokens}).encode()
    req = urllib.request.Request(LLM_URL.rstrip("/") + "/chat/completions", data=body, headers={
        "Content-Type": "application/json", "Authorization": "Bearer " + LLM_KEY})
    with urllib.request.urlopen(req, timeout=180) as resp:
        out = json.load(resp)
    return out["choices"][0]["message"]["content"].strip()


def mcp(token, name, arguments):
    """Call one MCP tool; return (payload, is_error). Payload is the tool's JSON."""
    global _id
    _id += 1
    body = json.dumps({
        "jsonrpc": "2.0", "id": _id, "method": "tools/call",
        "params": {"name": name, "arguments": arguments},
    }).encode()
    req = urllib.request.Request(MCP_URL, data=body, headers={
        "Authorization": "Bearer " + token, "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=60) as resp:
        out = json.load(resp)
    if "error" in out:
        return out["error"], True
    res = out.get("result", {})
    content = res.get("content", [])
    payload = json.loads(content[0]["text"]) if content else {}
    return payload, bool(res.get("isError"))


_LABEL_KEYS = ("name", "title", "subject", "full_name", "label", "number")


def _label(values):
    """A short human label for a record: a name-like field, else the first
    non-uuid string value (so a client ref reads 'Vector LLC', not its email)."""
    for k in _LABEL_KEYS:
        v = values.get(k)
        if isinstance(v, str) and v and not _is_uuid(v):
            return v
    for v in values.values():
        if isinstance(v, str) and v and not _is_uuid(v):
            return v
    return ""


def _is_uuid(s):
    return len(s) == 36 and s.count("-") == 4


def resolve_context(token, desc, values):
    """Replace ref ids with the referenced record's name, so the model writes
    real names ('Vector LLC') instead of placeholders ('[Client Name]')."""
    fields = {f["name"]: f for f in desc.get("fields", [])}
    ctx = {}
    for k, v in values.items():
        f = fields.get(k, {})
        if f.get("type") == "ref" and isinstance(v, str) and v and f.get("ref"):
            rec, err = mcp(token, "get_record", {"entity": f["ref"], "id": v})
            if not err:
                ctx[k] = _label(rec.get("values", {})) or v
                continue
        ctx[k] = v
    return ctx


# Free-text fields an agent writes its work product into, in preference order —
# the deliverable for a tracker issue (result) vs. the input it reads (description).
_RESULT_KEYS = ("result", "output", "solution", "answer", "draft", "work",
                "summary", "assessment", "review", "analysis", "report", "notes")


def _output_field(desc):
    """The text field the agent should write its work product into, or None
    (then the product goes in a comment, as for an invoice with no such field)."""
    by_name = {f["name"]: f for f in desc.get("fields", [])}
    for k in _RESULT_KEYS:
        f = by_name.get(k)
        if f and f.get("type") == "text" and not f.get("computed"):
            return k
    return None


def _solo_step(desc, state):
    """The action if EXACTLY ONE manual, non-approval transition leaves `state`
    — the unambiguous 'submit' an agent should take to finish and reach the human
    gate. 0 or >1 means the agent must not guess (permissions backstop anyway)."""
    wf = desc.get("workflow") or {}
    nxt = [t for t in wf.get("transitions", [])
           if t.get("from") == state and not t.get("auto") and not t.get("requires_approval")]
    return (wf.get("field"), nxt[0]["action"]) if len(nxt) == 1 else (wf.get("field"), None)


def handle(token, task):
    """Do the work for one task. Returns (ok, message).

    With a model configured, the agent reads the record (refs resolved to names),
    produces the work product (a client message, code, a summary), writes it to
    the record's result field or a comment, performs the assigned action, and then
    advances along any unambiguous non-approval transitions — stopping the moment
    the next step needs a human signature. Without a model it just acts.
    """
    entity, rid, action = task.get("entity"), task.get("record_id"), task.get("action")
    if not (entity and rid and action):
        return True, "nothing to do"
    basis = {"type": "task", "id": task["id"]}
    if LLM_URL:
        desc, _ = mcp(token, "describe_entity", {"entity": entity})
        rec, _ = mcp(token, "get_record", {"entity": entity, "id": rid})
        ctx = resolve_context(token, desc, rec.get("values", {}))
        me = os.environ.get("KALITA_WORKER_ID", "agent")
        role = task.get("role", os.environ.get("KALITA_WORKER_ROLE", "agent"))
        note = llm(promptbox.build_messages(
            system=
                "You are %s, the %s on the Kalita business platform. You handle one work item and "
                "produce a complete, ready-to-deliver work product — a message to a client, a piece "
                "of work, or a summary. Be concise and professional. Use ONLY the real values given; "
                "never write placeholders like [Name] or [date], and never invent units or currency "
                "symbols that are not in the data. Sign as the %s. No markdown headers."
                % (me, role, role),
            task="This is a %s record. Your assigned step is '%s'. Produce the work product." % (entity, action),
            data=[("%s/%s" % (entity, rid), json.dumps(ctx, ensure_ascii=False, indent=2))],
        ))
        out = _output_field(desc)
        if out:  # the deliverable goes in the record's result field, journaled
            mcp(token, "update_record", {"entity": entity, "id": rid, "basis": basis,
                                         "values": {out: note}})
            mcp(token, "comment", {"entity": entity, "id": rid, "basis": basis,
                                   "body": "Work product written to '%s'; submitting for review." % out})
        else:  # no result field — the message itself is the deliverable
            mcp(token, "comment", {"entity": entity, "id": rid, "body": note, "basis": basis})

    field = (desc.get("workflow") or {}).get("field", "status") if LLM_URL else "status"
    last, seen = None, set()
    next_act = action
    for _ in range(5):  # the assigned action, then any forced non-HITL follow-ups
        res, err = mcp(token, "act", {"entity": entity, "id": rid, "action": next_act, "basis": basis})
        if err:
            return (last is not None), (last or json.dumps(res))
        if res.get("status") == "pending_approval":
            return True, "work done, submitted for human approval"
        last = ("worked + " if LLM_URL else "performed ") + next_act
        if not LLM_URL:
            break
        state = ((res.get("record") or {}).get("values", {})).get(field)
        if state in seen:  # never re-enter a state — no oscillation
            break
        seen.add(state)
        _, next_act = _solo_step(desc, state)  # the one forced step, or None
        if not next_act:
            break  # ambiguous or human-gated from here — stop for the human
    return True, last or "nothing to do"


_RISK = {"Low": 1, "Medium": 2, "High": 3}


def _extract_json(text):
    """Pull a JSON array out of a model's reply — tolerant of markdown fences and
    stray prose. Returns a list (the findings) or [] if nothing parseable."""
    t = text.strip()
    if "```" in t:  # ```json ... ``` fences
        for part in t.split("```"):
            p = part.strip()
            if p[:4].lower() == "json":
                p = p[4:].strip()
            if p.startswith("[") or p.startswith("{"):
                t = p
                break
    i, j = t.find("["), t.rfind("]")
    if i >= 0 and j > i:
        try:
            v = json.loads(t[i:j + 1])
            return v if isinstance(v, list) else []
        except ValueError:
            pass
    try:  # maybe an object {"findings": [...]}
        o = json.loads(t)
        return o.get("findings", []) if isinstance(o, dict) else []
    except ValueError:
        return []


def handle_legal(token, task):
    """The legal reviewer: read the contract and the firm's playbook, raise
    structured clause-level Finding records against it, set the overall risk and a
    short narrative, then submit. The lawyer triages the findings and signs."""
    entity, rid, action = task.get("entity"), task.get("record_id"), task.get("action")
    if not (entity and rid and action):
        return True, "nothing to do"
    basis = {"type": "task", "id": task["id"]}
    if not LLM_URL:
        res, err = mcp(token, "act", {"entity": entity, "id": rid, "action": action, "basis": basis})
        return (not err), ("submitted (no model)" if not err else json.dumps(res))

    desc, _ = mcp(token, "describe_entity", {"entity": entity})
    rec, _ = mcp(token, "get_record", {"entity": entity, "id": rid})
    ctx = resolve_context(token, desc, rec.get("values", {}))
    pb, _ = mcp(token, "query", {"entity": "Playbook"})
    rules = [r for r in pb.get("records", []) if r.get("values", {}).get("active", True)]
    rules_text = "\n".join(
        "- %s: %s [severity %s]" % (r["values"].get("topic"), r["values"].get("our_position"),
                                    r["values"].get("severity")) for r in rules) or "(no playbook rules)"
    rule_id = {str(r["values"].get("topic", "")).lower(): r["id"] for r in rules}

    out = llm(promptbox.build_messages(
        system=
            "You are a contract lawyer reviewing a contract AGAINST the firm's playbook. "
            "Return ONLY a JSON array; each element is "
            '{"clause": str, "risk": "Low"|"Medium"|"High", "issue": str, "redline": str, "rule_topic": str|null}. '
            "rule_topic must equal a playbook topic exactly, or be null. Raise a finding for every real "
            "deviation or risk. No prose, no markdown — only the JSON array.",
        task="Return the findings as a JSON array.",
        data=[("Playbook", rules_text), ("%s/%s" % (entity, rid), json.dumps(ctx, ensure_ascii=False, indent=2))],
    ), max_tokens=1500)
    findings = _extract_json(out)

    worst, made = 0, 0
    for f in findings:
        if not isinstance(f, dict):
            continue
        risk = f.get("risk") if f.get("risk") in _RISK else "Medium"
        worst = max(worst, _RISK[risk])
        vals = {"contract": rid, "clause": (f.get("clause") or "Clause")[:200], "risk": risk,
                "issue": f.get("issue") or "", "redline": f.get("redline") or ""}
        rid_rule = rule_id.get(str(f.get("rule_topic") or "").lower())
        if rid_rule:
            vals["rule"] = rid_rule
        _, err = mcp(token, "create_record", {"entity": "Finding", "values": vals, "basis": basis})
        if not err:
            made += 1
    overall = {0: "Low", 1: "Low", 2: "Medium", 3: "High"}[worst]
    mcp(token, "update_record", {"entity": entity, "id": rid, "basis": basis, "values": {
        "risk": overall,
        "assessment": "Reviewed against the playbook: %d finding(s) raised, overall risk %s." % (made, overall)}})

    res, err = mcp(token, "act", {"entity": entity, "id": rid, "action": action, "basis": basis})
    if err:
        return False, json.dumps(res)
    return True, "raised %d finding(s), overall risk %s, submitted for the lawyer" % (made, overall)


def _extract_obj(text):
    """Pull a single JSON object out of a model reply, tolerant of fences/prose."""
    t = text.strip()
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
            return o if isinstance(o, dict) else {}
        except ValueError:
            return {}
    return {}


def handle_sales(token, task):
    """The sales agent: triage a fresh opportunity. Read it, draft a one-paragraph
    qualification, BOOK the first follow-up as a real Activity (system of action,
    not just a note), then advance the stage — stopping well before the win, which
    only a human signs. The booked activity and the moved stage are the work, not
    a wall of text."""
    entity, rid, action = task.get("entity"), task.get("record_id"), task.get("action")
    if not (entity and rid and action):
        return True, "nothing to do"
    basis = {"type": "task", "id": task["id"]}
    if not LLM_URL:
        res, err = mcp(token, "act", {"entity": entity, "id": rid, "action": action, "basis": basis})
        return (not err), ("advanced (no model)" if not err else json.dumps(res))

    desc, _ = mcp(token, "describe_entity", {"entity": entity})
    rec, _ = mcp(token, "get_record", {"entity": entity, "id": rid})
    ctx = resolve_context(token, desc, rec.get("values", {}))
    out = llm(promptbox.build_messages(
        system=
            "You are a sales development agent on the Kalita platform triaging a new opportunity. "
            "Return ONLY a JSON object: "
            '{"qualification": str, "first_activity": str}. '
            "qualification: one short paragraph — is this worth pursuing, what to confirm, the risk. "
            "first_activity: the subject line of the first follow-up CALL to book (imperative, <=80 chars). "
            "Use only the real values given; no placeholders, no markdown.",
        task="Triage it.",
        data=[("%s/%s" % (entity, rid), json.dumps(ctx, ensure_ascii=False, indent=2))],
    ), max_tokens=500)
    obj = _extract_obj(out)
    qualification = (obj.get("qualification") or "Triaged; pursue and confirm budget/authority/need.").strip()
    subject = (obj.get("first_activity") or "Intro call: confirm need, budget and timeline").strip()[:200]

    _, aerr = mcp(token, "create_record", {"entity": "Activity", "basis": basis, "values": {
        "subject": subject, "kind": "Call", "opportunity": rid}})
    mcp(token, "comment", {"entity": entity, "id": rid, "basis": basis,
                           "body": "Qualification: " + qualification})

    res, err = mcp(token, "act", {"entity": entity, "id": rid, "action": action, "basis": basis})
    if err:
        return False, json.dumps(res)
    booked = "booked follow-up + " if not aerr else "(activity not booked) "
    return True, booked + "qualified and advanced for the rep"


# KALITA_HANDLER switches the work function: legal=contract reviewer, sales=SDR triage.
def _work(token, task):
    h = os.environ.get("KALITA_HANDLER")
    if h == "legal":
        return handle_legal(token, task)
    if h == "sales":
        return handle_sales(token, task)
    return handle(token, task)


def run_once(token):
    tasks, _ = mcp(token, "wait_for_task", {"timeout_sec": 25})
    for t in tasks.get("tasks", []):
        _, err = mcp(token, "take_task", {"task_id": t["id"]})
        if err:
            continue  # lost the lease to another worker — fine, skip it
        try:
            ok, msg = _work(token, t)
        except Exception as ex:  # never silently hang on the lease
            ok, msg = False, "handler crashed: " + str(ex)
        if ok:
            mcp(token, "report_progress", {"task_id": t["id"], "note": msg})
            mcp(token, "complete_task", {"task_id": t["id"], "result": msg})
        else:
            mcp(token, "fail_task", {"task_id": t["id"], "reason": msg})
        print(("done " if ok else "failed ") + t["id"] + ": " + msg)


def main():
    token = get_token()
    role = os.environ.get("KALITA_WORKER_ROLE", "?")
    print("agent-runner online for role", role, "at", NODE,
          ("with model " + LLM_MODEL) if LLM_URL else "(deterministic)")
    if os.environ.get("KALITA_ONCE"):  # one cycle then exit — for demos and cron
        run_once(token)
        return
    while True:
        try:
            run_once(token)
        except urllib.error.URLError as ex:
            print("node unreachable, retrying:", ex)
            time.sleep(5)
        except Exception as ex:
            print("loop error:", ex)
            time.sleep(5)


if __name__ == "__main__":
    main()

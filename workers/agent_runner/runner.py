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

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
MCP_URL = NODE + "/mcp"
# Local, on-prem model (LM Studio / Ollama, OpenAI-compatible). Empty = the agent
# only drives the workflow deterministically; set it and the agent does the work.
LLM_URL = os.environ.get("KALITA_LLM_URL", "")
LLM_MODEL = os.environ.get("KALITA_LLM_MODEL", "openai/gpt-oss-20b")
LLM_KEY = os.environ.get("KALITA_LLM_KEY", "lm-studio")
_id = 0


def llm(messages):
    """One chat completion against the local OpenAI-compatible endpoint."""
    body = json.dumps({"model": LLM_MODEL, "messages": messages,
                       "temperature": 0.3, "max_tokens": 400}).encode()
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


def resolve_context(token, entity, values):
    """Replace ref ids with the referenced record's name, so the model writes
    real names ('Vector LLC') instead of placeholders ('[Client Name]')."""
    desc, _ = mcp(token, "describe_entity", {"entity": entity})
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


def handle(token, task):
    """Do the work for one task. Returns (ok, message).

    With a model configured, the agent reads the record (refs resolved to names),
    produces the work product (a client message, a status note, a summary), posts
    it, then drives the transition. Without a model it just performs the action.
    """
    entity, rid, action = task.get("entity"), task.get("record_id"), task.get("action")
    if not (entity and rid and action):
        return True, "nothing to do"
    if LLM_URL:
        rec, _ = mcp(token, "get_record", {"entity": entity, "id": rid})
        ctx = resolve_context(token, entity, rec.get("values", {}))
        me = os.environ.get("KALITA_WORKER_ID", "agent")
        role = task.get("role", os.environ.get("KALITA_WORKER_ROLE", "agent"))
        note = llm([
            {"role": "system", "content":
                "You are %s, the %s on the Kalita business platform. You handle one work item and "
                "produce a complete, ready-to-send work product — a message to a client, a status "
                "note, or a summary. Be concise and professional. Use ONLY the real values given; "
                "never write placeholders like [Name] or [date], and never invent units or currency "
                "symbols that are not in the data. Sign as the %s. No markdown headers."
                % (me, role, role)},
            {"role": "user", "content":
                "%s record:\n%s\n\nYour assigned step is '%s'. Write the work product."
                % (entity, json.dumps(ctx, ensure_ascii=False, indent=2), action)},
        ])
        mcp(token, "comment", {"entity": entity, "id": rid, "body": note,
                               "basis": {"type": "task", "id": task["id"]}})
    res, err = mcp(token, "act", {
        "entity": entity, "id": rid, "action": action,
        "basis": {"type": "task", "id": task["id"]}})
    if err:
        return False, json.dumps(res)
    if res.get("status") == "pending_approval":
        return True, "work done, submitted for human approval"
    return True, ("worked + " if LLM_URL else "performed ") + action


def run_once(token):
    tasks, _ = mcp(token, "wait_for_task", {"timeout_sec": 25})
    for t in tasks.get("tasks", []):
        _, err = mcp(token, "take_task", {"task_id": t["id"]})
        if err:
            continue  # lost the lease to another worker — fine, skip it
        try:
            ok, msg = handle(token, t)
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

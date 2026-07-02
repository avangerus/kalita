#!/usr/bin/env python3
"""CoverageBot — packs/people's УРОВЕНЬ 3 ("свой код в воркере"), the third
of the three behaviour levels the pack demonstrates end to end:

  Уровень 1 (действие/переход) — packs/people/workflows.dsl: workflow
  Employee on status (deactivate/activate).
  Уровень 2 (событие)          — packs/people/entities.dsl: event
  OutOfOffice, + the `on event OutOfOffice` / `on create Coverage`
  automation in packs/people/workflows.dsl.
  Уровень 3 (свой код)         — ЭТОТ файл: automation only gets the node to
  the point of "a Coverage record exists and CoverageBot has a task". What
  automation CANNOT do — read the manager's name and write a human-readable
  sentence — is exactly the sliver of real code a worker exists for.

Loop: log in as CoverageBot (least-privilege, see packs/people/security.dsl —
read-only on Employee/Coverage, explicit deny on deactivate/activate and on
every mutating verb but comment), take one `arrange_coverage` task from the
pool, read the Coverage record the automation created, resolve the covering
manager's name, and post exactly one comment on the Coverage record:
"замена организована: <manager name>". Then complete the task. Every write
lands in the journal under the CoverageBot actor — this worker never touches
Employee.status; permissions deny it even if it tried (see security.dsl).

Zero dependencies: stdlib only, same shape as every other kalita worker
(workers/agent_runner/runner.py, workers/connectors/inbound.py).

Run:
    KALITA_URL=http://127.0.0.1:8095 \
    KALITA_TOKEN=<CoverageBot bearer token> \
    python bot.py
or self-register with KALITA_BOOTSTRAP_SECRET + KALITA_WORKER_ID +
KALITA_WORKER_ROLE=CoverageBot. KALITA_ONCE=1 runs a single take/act/complete
cycle then exits (demos, cron, tests).
"""
import json
import os
import sys
import time
import urllib.request
import urllib.error
import uuid

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from bootstrap import get_token  # noqa: E402

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
MCP_URL = NODE + "/mcp"
_id = 0


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


def idem_key(task_id, *parts):
    """Deterministic idempotency_key for one mutating call within one task
    attempt (LLD-01 §5) — same recipe as workers/agent_runner/runner.py: a
    pure function of the task id + a call-site label, so a retry of the same
    attempt reproduces the same key and dedups instead of double-writing."""
    name = str(task_id) + "|" + "|".join(str(p) for p in parts)
    return str(uuid.uuid5(uuid.NAMESPACE_URL, name))


def manager_name(token, coverage_values):
    """Resolve the covering manager's display name for the comment. Coverage
    is created by automation with `manager` already set to the Employee id
    one ref-hop from the event payload (employee.manager) — this is the bit
    of "own code" automation cannot do: fetch the referenced Employee record
    and turn its id into a name a human can read."""
    manager_id = coverage_values.get("manager")
    if not manager_id:
        return "(without a manager on file)"
    rec, err = mcp(token, "get_record", {"entity": "Employee", "id": manager_id})
    if err:
        return "(manager record unavailable: %s)" % manager_id
    return rec.get("values", {}).get("name") or manager_id


def handle(token, task):
    """Do the work for one arrange_coverage task. Returns (ok, message).

    The task carries entity=Coverage / record_id=<the record automation just
    created> (packs/people/workflows.dsl: `on create Coverage: agent
    CoverageBot: arrange_coverage()` — a record trigger, so unlike the
    event-only leg above, this task DOES have a real record to read). Read
    it, resolve the manager's name, write the comment, done — CoverageBot
    never calls `act` on Employee at all, and its security.dsl grant would
    deny it if it tried.
    """
    entity, rid = task.get("entity"), task.get("record_id")
    if entity != "Coverage" or not rid:
        return True, "nothing to do (not a Coverage task)"
    basis = {"type": "task", "id": task["id"]}

    cov, err = mcp(token, "get_record", {"entity": "Coverage", "id": rid})
    if err:
        return False, "could not read Coverage %s: %s" % (rid, json.dumps(cov))
    name = manager_name(token, cov.get("values", {}))

    body = "замена организована: %s" % name
    _, err = mcp(token, "comment", {
        "entity": "Coverage", "id": rid, "body": body, "basis": basis,
        "idempotency_key": idem_key(task["id"], "comment"),
    })
    if err:
        return False, "could not post the coverage comment"
    return True, body


def run_once(token):
    tasks, _ = mcp(token, "wait_for_task", {"timeout_sec": 25})
    for t in tasks.get("tasks", []):
        _, err = mcp(token, "take_task", {"task_id": t["id"],
                                          "idempotency_key": idem_key(t["id"], "take")})
        if err:
            continue  # lost the lease to another worker — fine, skip it
        try:
            ok, msg = handle(token, t)
        except Exception as ex:  # never silently hang on the lease
            ok, msg = False, "handler crashed: " + str(ex)
        if ok:
            mcp(token, "complete_task", {"task_id": t["id"], "result": msg,
                                         "idempotency_key": idem_key(t["id"], "complete")})
        else:
            mcp(token, "fail_task", {"task_id": t["id"], "reason": msg,
                                     "idempotency_key": idem_key(t["id"], "fail")})
        print(("done " if ok else "failed ") + t["id"] + ": " + msg)


def main():
    token = get_token()
    print("coverage-bot online at", NODE)
    if os.environ.get("KALITA_ONCE"):  # one cycle then exit — demos and tests
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

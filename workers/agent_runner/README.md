# agent-runner

A reference worker that makes an AI agent a real employee on a kalita node: it
authenticates over MCP, takes tasks from its role's pool, does the work, and
completes them — with a human accepting critical output behind a signature.

This is the last mile of the "Jira where agents do the tasks" model. The
platform mechanics (task pool, leases, HITL approval, audit journal) are in the
kernel; this worker is the loop that drives them. See `packs/devtrack` for a
tracker designed for it, and `TestDogfoodDevTrackAgentLoop` for the full loop
proven end-to-end.

## Run

```bash
# with an explicit token (kalita agent add --id eng-1 --role Engineer):
KALITA_URL=http://127.0.0.1:8095 KALITA_TOKEN=<token> python runner.py

# or self-register via the node's bootstrap secret:
KALITA_URL=http://127.0.0.1:8095 \
KALITA_BOOTSTRAP_SECRET=<secret> \
KALITA_WORKER_ID=eng-1 KALITA_WORKER_ROLE=Engineer \
python runner.py
```

## The loop

```
wait_for_task → take_task → handle(task) → report_progress → complete_task
                                          ↘ (on failure) → fail_task
```

`handle(task)` is the only thing you customize. The default performs the task's
declared workflow action (drives a pipeline). For open-ended work, plug an LLM:

```python
def handle(token, task):
    rec, _ = mcp(token, "get_record", {"entity": task["entity"], "id": task["record_id"]})
    output = your_llm(rec)                         # write code, draft a reply, analyze…
    mcp(token, "update_record", {"entity": task["entity"], "id": task["record_id"],
        "values": {"result": output}, "basis": {"type": "task", "id": task["id"]}})
    res, err = mcp(token, "act", {"entity": task["entity"], "id": task["record_id"],
        "action": "submit", "basis": {"type": "task", "id": task["id"]}})
    return (not err), "submitted for review"
```

The agent can never bypass its `deny` rules or sign its own HITL gate — the
kernel enforces that regardless of the handler.

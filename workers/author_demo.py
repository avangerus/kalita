#!/usr/bin/env python3
"""author_demo — real-model synthesis of a kalita pack from a business outcome.

Prompts a local OpenAI-compatible model (llama.cpp / LM Studio) for a
compose_pack JSON, then validates it through the node's MCP compose_pack:
user describes a result -> the model builds the process -> kalita validates it.
Stdlib only, same shape as every other worker.

Env: KALITA_LLM_URL (default http://127.0.0.1:1234/v1),
     KALITA_LLM_MODEL (default gemma-4-E4B-it-Q4_K_M.gguf),
     KALITA_URL, KALITA_TOKEN (the node + an agent bearer token).
"""
import json
import os
import sys
import urllib.request
import uuid

LLM_URL = os.environ.get("KALITA_LLM_URL", "http://127.0.0.1:1234/v1")
LLM_MODEL = os.environ.get("KALITA_LLM_MODEL", "gemma-4-E4B-it-Q4_K_M.gguf")
NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8080")
TOKEN = os.environ.get("KALITA_TOKEN", "")

DESCRIPTION = os.environ.get("KALITA_DESCRIPTION",
    "Процесс заявки на отпуск: сотрудник подаёт заявку, менеджер одобряет или отклоняет.")

FIELD_TYPES = ("string,text,int,float,money,bool,date,datetime,file,email,url,phone,"
               "duration,percent,color,decimal,json,serial,enum(+values),ref(+ref),"
               "array_ref(+ref),tags,multiselect(+values),array_file")

EXAMPLE = {
    "pack": "mini",
    "entities": [{"name": "Ticket", "fields": [
        {"name": "title", "type": "string", "required": True},
        {"name": "status", "type": "enum", "values": ["Open", "Approved", "Rejected"], "default": "Open"}]}],
    "workflows": [{"entity": "Ticket", "field": "status", "transitions": [
        {"from": "Open", "to": "Approved", "action": "approve_ticket", "requires_approval": "Manager"},
        {"from": "Open", "to": "Rejected", "action": "reject_ticket", "requires_approval": "Manager"}]}],
    "roles": [{"name": "Manager"}, {"name": "Worker", "agent": True}],
    "permissions": [
        {"role": "Manager", "rules": ["full [Ticket]", "approve [approve_ticket, reject_ticket]"]},
        {"role": "Worker", "rules": ["create [Ticket]", "read [Ticket]", "deny [delete *]"]}],
}

PROMPT = (
    "You are a business-process engineer on the kalita platform. "
    "Build a kalita pack (compose_pack JSON) for this business outcome:\n\n"
    f'"{DESCRIPTION}"\n\n'
    "Rules:\n"
    "- Output ONLY one JSON object, no prose, no markdown fences.\n"
    '- Schema: {"pack": str, "entities": [{"name": str, "fields": [{"name": str, "type": str, '
    '"values": [str], "ref": str, "required": bool, "default": str}]}], '
    '"workflows": [{"entity": str, "field": str, "transitions": [{"from": str, "to": str, '
    '"action": str, "requires_approval": str}]}], "roles": [{"name": str, "agent": bool}], '
    '"permissions": [{"role": str, "rules": [str]}]}\n'
    f"- Field types (closed): {FIELD_TYPES}\n"
    "- Permission verbs (closed): read, create, update, delete, act, approve, full, deny, call, emit, tail, flag.\n"
    '- People references use "core.User" (e.g. {"type":"ref","ref":"core.User"}), never a bare "User".\n'
    "- `approve [..]` and `act [..]` take ACTION names (the workflow transition actions), not entity names.\n"
    '- A rule like "approve [approve, reject]" grants signing those two transitions; "deny [delete *]" blocks deletes.\n'
    "- Add a requires_approval gate on every approve/reject transition.\n"
    '- Every agent role must carry a deny rule, e.g. "deny [delete *]".\n'
    "- Humans hold the approving role; agents hold the working role.\n"
    f"- Shape example: {json.dumps(EXAMPLE)}\n\n"
    "Return only the JSON object."
)


def _chat(content):
    body = json.dumps({"model": LLM_MODEL, "messages": [{"role": "user", "content": content}],
                       "temperature": 0.2, "max_tokens": 2000, "stream": False}).encode()
    req = urllib.request.Request(LLM_URL + "/chat/completions", data=body,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=300) as r:
        return json.load(r)["choices"][0]["message"]["content"].strip()


def llm_spec():
    """The author loop: ask the model, and on a malformed reply feed the error
    back for a corrected JSON — exactly what the kalita-author skill does with
    compose_pack's structured errors."""
    text = _chat(PROMPT)
    for _ in range(2):
        i, j = text.find("{"), text.rfind("}")
        if i >= 0 and j > i:
            try:
                return json.loads(text[i:j + 1])
            except json.JSONDecodeError as ex:
                text = _chat(
                    "Your JSON is invalid: %s. Here was your output:\n%s\n"
                    "Return ONLY the corrected JSON object, no prose." % (ex, text))
        else:
            text = _chat(
                "You returned no JSON object. Here was your output:\n%s\n"
                "Return ONLY a valid JSON object." % text[:300])
    sys.exit("model failed to produce valid JSON after retries; raw: %r" % text[:500])


def mcp(name, arguments):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                       "params": {"name": name, "arguments": arguments}}).encode()
    req = urllib.request.Request(NODE + "/mcp", data=body, headers={
        "Authorization": "Bearer " + TOKEN, "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as r:
        out = json.load(r)
    res = out.get("result", {})
    content = res.get("content", [])
    return (json.loads(content[0]["text"]) if content else {}), bool(res.get("isError"))


def main():
    """The full author loop: model -> compose_pack -> feed structured errors back
    -> regenerate -> repeat to green (bounded), exactly like kalita-author."""
    spec = llm_spec()
    for attempt in range(4):
        print("== attempt %d: model's pack ==" % (attempt + 1))
        print("  pack:", spec.get("pack"), "| entities:", [e["name"] for e in spec.get("entities", [])],
              "| roles:", [r["name"] for r in spec.get("roles", [])],
              "| workflows:", [w["entity"] for w in spec.get("workflows", [])])
        res, is_err = mcp("compose_pack", spec)
        if is_err:
            sys.exit("compose_pack ERROR: %s" % res)
        if res.get("ok"):
            print("  compose_pack ok: True")
            print("--- rendered DSL ---")
            print(res.get("dsl"))
            sys_spec, _ = mcp("describe_system", {})
            prop, perr = mcp("propose_change", {
                "files": {spec.get("pack", "pack") + ".dsl": res["dsl"]},
                "base_def_version": sys_spec.get("def_version"),
                "description": "auto-built from a natural-language description",
                "basis": {"type": "human", "id": "demo-Admin"},
                "idempotency_key": "author-demo-" + str(uuid.uuid4()),
            })
            if perr:
                sys.exit("propose_change ERROR: %s" % prop)
            print("== proposed (parked for human signature) ==")
            print("  proposal_id:", prop.get("proposal_id"), "| status:", prop.get("status"),
                  "| migration_plan steps:", len(prop.get("migration_plan", [])))
            print("== RESULT: model built a VALID pack and proposed it; a human signs next ==")
            return
        errs = res.get("errors", [])
        print("  compose_pack ok: False — %d structured error(s):" % len(errs))
        for e in errs:
            print("    -", e.get("code"), e.get("message"))
        spec = _fix(errs, spec)
    sys.exit("model did not converge to a valid pack in 4 attempts")


def _fix(errors, prev_spec):
    """Feed kalita's structured errors back to the model for a corrected pack."""
    brief = "\n".join("%s: %s (fix: %s)" % (e.get("code"), e.get("message"), e.get("fix_hint", ""))
                      for e in errors)
    text = _chat(
        "Your compose_pack spec failed kalita validation:\n%s\n"
        "Your previous spec was:\n%s\n"
        "Fix ONLY the reported problems and return the corrected JSON object, no prose."
        % (brief, json.dumps(prev_spec)))
    i, j = text.find("{"), text.rfind("}")
    if i < 0 or j <= i:
        sys.exit("model returned no JSON on fix: %r" % text[:300])
    return json.loads(text[i:j + 1])


if __name__ == "__main__":
    main()

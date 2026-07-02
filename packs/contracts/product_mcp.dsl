# product.mcp — domain-facing MCP tools the façade worker exposes over the
# node MCP (LLD-06 §1, PK-2). Pure manifest DATA: the façade worker itself
# (LLD-06 PR-2, Wave B — not built yet) reads this section and executes each
# tool as a composition of node calls under the CALLER's own bearer token.
# The façade has zero write authority of its own (LLD-06 §1: strict
# pass-through, no service-account tokens) — ABAC/HITL apply exactly as if
# the caller invoked create_record/act directly. `compose` is declarative,
# uninterpreted-by-this-compiler text; the façade worker is the interpreter.

product.mcp:
    tool legal.submit_contract "Create a contract from title/counterparty/terms and send it to legal review":
        in:
            title:        string required
            counterparty: ref[Counterparty] required
            terms:        text required
        compose:
            create Contract(title: $title, counterparty: $counterparty, terms: $terms) -> $contract
            act send_to_legal on $contract
        out: contract

    tool legal.findings "List the clause-level findings raised on a contract":
        in:
            contract: ref[Contract] required
        compose:
            query Finding where contract = $contract -> $findings
        out: findings

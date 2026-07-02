# assistant — the operator chat (KChat, LLD-06 §2 PK-3). A Session/Message
# pack like any other: ordinary governed entities, not a bespoke store. This
# is the whole point (LLD-06 §2): "диалог governed по построению — аудит,
# retention, ABAC, undo приходят бесплатно из ядра; чат-история не выходит за
# периметр и не является отдельным нетронутым хранилищем." chat_runner
# (LLD-06 PR-3) writes Session/Message through the SAME MCP create_record
# calls, under the CALLER'S bearer, that any other product write uses — there
# is no special-cased "chat storage" code path anywhere in the kernel.

entity Session "Chat session":
    owner:      ref[core.User] default=$me label="Owner"
    title:      string label="Title"
    started_at: datetime default=$now label="Started"

# role here is the CHAT message role (user/assistant/tool-result) — an
# ordinary enum field, unrelated to the RBAC "role" concept. body is the
# rendered TEXT of the turn (LLD-14 §4: even a fully "jailbroken" model can
# only ever produce Message.body text — never markup — because the UI-schema
# reistry, not the chat pack, is what would turn text into presentation, and
# it never does that for a plain field value).
entity Message "Chat message":
    session:    ref[Session] required on_delete=cascade label="Session"
    role:       enum[user, assistant, tool] required label="Role"
    body:       text required label="Body"
    created_at: datetime default=$now label="Sent"

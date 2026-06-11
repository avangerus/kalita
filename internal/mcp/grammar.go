package mcp

// Machine-readable grammar summary served by get_grammar. The full normative
// text is docs/DSL-SPEC-v0.md; this is the working subset an agent needs to
// generate a valid pack. Keep in lockstep with the compiler.

const grammarText = `kalita DSL v0 — grammar summary
Indentation: 4 spaces, no tabs. Comments: # to end of line. One pack = pack.kal manifest + *.kal modules.

MANIFEST:    pack <name> / version <semver> / requires kalita >= 0.1 / depends core >= 0.1
ENTITY:      entity Name:            # or: entity Name singleton:  (at most one record — settings)
                 field: type [required] [unique] [default=<expr>] [computed=<expr>] [on_delete=restrict|set_null|cascade]
TYPES:       string text int float money bool date datetime file
             email url phone duration(2d4h) percent(0-100) color(#RRGGBB) decimal json
             enum[A, B] ref[Entity] ref[core.User]
             array[ref[Entity]] array[string](tags) array[enum[A, B]](multiselect)
CONSTRAINTS: constraints:            # immediately after its entity
                 unique(field1, field2)
WORKFLOW:    workflow Entity on enum_field:
                 From -> To: action [when <expr>] [assignee=agent(Role)|Role] [requires approval(Role)]
                 From -> To: auto when <expr>
                 any  -> To: ...
ROLES:       roles:
                 HumanRole
                 BotRole agent       # agent roles MUST have a deny block in permissions
PERMISSIONS: permissions:
                 Role:
                     read|create|update|delete|full [Entity, Entity2] | all | Entity where <expr>
                     act [action1, action2]        # workflow actions the role may execute
                     approve [action]              # actions the role signs (HITL)
                     deny [update Entity.field, delete *, read Entity where <expr>, act [a]]
AUTOMATION:  automation:
                 on schedule <text> [for Entity] [when <expr>]:   # when requires for
                 on create|update|delete Entity [when <expr>]:
                 on stuck Entity in State for 10d:
                     agent Role: task_name(args)
                     notify email(field)
                     webhook out "https://..."
                     escalate_to Role
UI:          ui Entity:
                 list: [f1, f2] sort=-f1
                     filters: [f1]
                     view "Name": where <expr>
                 form:
                     section "Title": [f1, f2]
                 board: by enum_field
EXPR:        field cmp literal | field in [a, b] | clauses joined by and; cmp: = != > < >= <=; literals: 42, "str", true, $me; days_since(path)
RULES:       agent role without deny does not compile; workflow state field cannot be written directly; mutations require basis; only additive migrations.`

const grammarExample = `pack example
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1

entity Ticket:
    title: string required
    assignee: ref[core.User] default=$me
    priority: enum[Low, High] default=Low
    status: enum[New, InWork, Done] default=New

workflow Ticket on status:
    New    -> InWork: take_ticket assignee=agent(Helper)
    InWork -> Done:   close requires approval(Lead)

roles:
    Lead
    Helper agent

permissions:
    Lead:
        full    [Ticket]
        approve [close]
    Helper:
        read   [Ticket]
        create [Ticket]
        act    [take_ticket, close]
        deny   [delete *, update Ticket.priority]

ui Ticket:
    list: [title, priority, status]
    board: by status`

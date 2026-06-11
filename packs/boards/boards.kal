# Boards — a Trello-like kanban pack. Cards move freely (no workflow: the
# status is a plain enum, so drag-and-drop is just an update within rights).
# v0 limitation: columns are a fixed enum; per-board custom columns need
# dynamic enums (v1).

entity Board:
    name: string required unique
    description: text
    owner: ref[core.User] default=$me

entity Card:
    board: ref[Board] on_delete=restrict
    title: string required
    details: text
    assignee: ref[core.User]
    due_date: date
    priority: enum[Low, Normal, High, Urgent] default=Normal
    status: enum[Backlog, Todo, Doing, Review, Done] default=Backlog

roles:
    Lead
    Member
    Assistant agent

permissions:
    Lead:
        full [Board, Card]
    Member:
        read   [Board]
        create [Card]
        full   [Card]
        deny   [update Board.owner]
    Assistant:
        read   [Board, Card]
        create [Card]
        update [Card]
        deny   [delete *, update Board.*, update Card.status where status = Done]

ui Card:
    list: [title, board, assignee, priority, due_date, status] sort=-priority
        filters: [board, status, assignee, priority]
        view "My cards": where assignee = $me
    form:
        section "Card":     [title, details, board]
        section "Planning": [assignee, due_date, priority]
    board: by status

ui Board:
    list: [name, owner, description]

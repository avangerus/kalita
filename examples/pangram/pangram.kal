# THE PANGRAM PACK — one example that uses every type and construct.
# Read once, write any pack by analogy. Indent 4 spaces. # = comment.
# Lines marked  !INVARIANT  are NOT style — break them and it WON'T COMPILE / RUN.
# The five invariants that make kalita safe (an agent must respect them):
#   1. an agent role MUST have a `deny [...]` block
#   2. the workflow state field is set ONLY by transitions, never written directly
#   3. a transition with `requires approval(Role)` does not happen until a human signs
#   4. every data change needs a basis (the API supplies it) — no silent writes
#   5. default-deny: a role sees/does nothing unless a permission grants it

pack pangram
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1

# --- a singleton: settings, at most one record ---
entity Settings singleton:
    company_name: string required
    default_currency: string default="RUB"

# --- a normal entity showing EVERY field type and modifier ---
entity Order:
    number:      serial format="ORD-{year}-{seq:5}"   # auto document number, read-only
    title:       string required                       # short text, indexed
    notes:       text                                  # long text, full-text searchable
    qty:         int                                   # whole number
    weight:      float                                 # real number
    amount:      money                                 # number, or {amount, currency}
    paid:        bool default=false
    due:         date                                  # YYYY-MM-DD
    placed_at:   datetime                              # RFC3339
    contract:    file                                  # uploaded document (content-addressed)
    contact:     email                                 # validated
    site:        url
    phone:       phone
    sla:         duration                              # 2d4h
    discount:    percent                               # 0..100
    color:       color                                 # #RRGGBB
    customer:    ref[Customer] on_delete=restrict      # link to one record
    manager:     ref[core.User] default=$me            # built-in user; $me = current actor
    watchers:    array[ref[core.User]]                 # many refs
    labels:      array[string]                         # free tags
    channels:    array[enum[Web, Phone, Email]]        # multiselect
    priority:    enum[Low, Normal, High] default=Normal
    status:      enum[Draft, Confirmed, Shipped, Done] default=Draft
    # computed: arithmetic between fields, and an aggregate over related rows (read-only)
    net:         float computed = amount - amount * discount / 100
    line_count:  int   computed = count(OrderLine where order = $self)
    total:       money computed = sum(OrderLine.sum where order = $self)
    # !INVARIANT 2: `status` above is the workflow field — never write it directly; use act(...)

constraints:
    unique(number)

# --- master-detail: order lines roll up into the order's total ---
entity OrderLine:
    order:   ref[Order] on_delete=cascade
    product: string required
    price:   money
    qty:     int default=1
    sum:     float computed = price * qty

entity Customer:
    name:  string required unique
    user:  ref[core.User]              # the portal login behind this customer

# --- named bidirectional relations (like Jira issue links) ---
link Order -> Order as blocks / blocked_by
link Order -> Order as relates_to / relates_to     # symmetric

# --- workflow: states of an enum field; transitions with guards, agents, HITL ---
workflow Order on status:
    Draft     -> Confirmed: confirm when amount > 0          # guard
    Confirmed -> Shipped:   ship assignee=agent(Logistics)   # an agent does this step
    Shipped   -> Done:      close requires approval(Manager)  # !INVARIANT 3: HITL — won't happen without a human signature
    any       -> Draft:     reopen                            # `any` matches all states

roles:
    Manager
    Clerk
    Customer
    Logistics agent          # an AGENT role — !INVARIANT 1: must have a deny block (see below) or it WON'T COMPILE

permissions:
    Manager:
        full    [Order, OrderLine, Customer, Settings]
        approve [close]
        act     [confirm, ship, close, reopen]
    Clerk:
        read   [Order, OrderLine, Customer]
        create [Order, OrderLine]
        update [Order]
        act    [confirm]
        deny   [delete *, update Order.amount where status != Draft]
    Customer:
        # ABAC: a customer sees only their own orders (ref-path + $me)
        read Order where customer.user = $me
        read [OrderLine]
        deny [delete *, update Order.*]
    Logistics:
        read [Order]
        act  [ship]
        deny [delete *, update Order.* where status = Done]   # required deny for agents

# --- automation: triggers + actions ---
automation:
    on create Order:
        notify email(manager)
    on schedule daily at 09:00 for Order when status = Confirmed and due in [1, 3]:
        agent Logistics: remind_to_ship(urgency = high if due = 1 else normal)
    on stuck Order in Shipped for 5d:
        escalate_to Manager
    on update Order when status changed to Done:
        webhook out "https://erp.example.com/order-done"

# --- ui: which views exist, configured (the renderer knows how to draw them) ---
ui Order:
    list: [number, title, customer, amount, status] sort=-placed_at
        filters: [status, priority, manager]
        view "My orders": where manager = $me
    form:
        section "Order":   [title, customer, amount, due]
        section "Detail":  [notes, labels, channels, contract]
    board: by status

ui Customer:
    list: [name, user]

# --- dashboard: table-wide aggregates (count/sum/avg/min/max) over ALL records,
#     filtered by `where` and/or broken down by `group by`. Totals respect each
#     reader's row permissions, so a scoped user sees totals of only their rows ---
dashboard OrderStats "Orders":
    tile "Open orders": count Order where status != Done
    tile "Revenue":     sum amount Order where status = Done
    tile "Avg ticket":  avg amount Order where status = Done
    tile "By status":   count Order group by status

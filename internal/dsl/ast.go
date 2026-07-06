package dsl

// Raw parse tree. The parser builds this; semantic analysis turns it into a
// Model. Workflow / automation / ui blocks are parsed structurally but
// analyzed in week 4 (BACKLOG-MVP); they are carried as RawBlock so files
// using them still compile their entity/permission parts today.

type TypeKind int

const (
	TyScalar TypeKind = iota // string text int float money bool date datetime file + the rich scalars below
	TyEnum
	TyRef
	TyArrayRef
	TyTags       // array[string] — free labels
	TyMultiEnum  // array[enum[...]] — components/multiselect
	TyArrayFile  // array[file] — multiple attachments
)

// scalarTypes is the closed list of scalar field types. The rich ones
// (email/url/.../decimal/duration/percent/color) are validated by the kernel;
// the grammar stays closed so an agent cannot invent a type.
var scalarTypes = map[string]bool{
	"string": true, "text": true, "int": true, "float": true, "money": true,
	"bool": true, "date": true, "datetime": true, "file": true,
	// rich scalars (v1):
	"email": true, "url": true, "phone": true, "duration": true,
	"percent": true, "color": true, "decimal": true, "json": true,
	// serial: auto-assigned human-readable document number (INV-2026-00042)
	"serial": true,
}

type TypeRef struct {
	Kind       TypeKind
	Scalar     string   // TyScalar
	EnumValues []string // TyEnum
	RefTarget  string   // TyRef / TyArrayRef, possibly "core.User"
}

type FieldDecl struct {
	Name     string
	Line     int
	Type     TypeRef
	Required bool
	Unique   bool
	Default  string // raw expression text, "" if absent
	Computed string // raw expression text, "" if absent
	OnDelete string // restrict | set_null | cascade, "" if absent
	Format   string // serial format, e.g. "INV-{year}-{seq:5}", "" if absent
	Label    string // human label (i18n), "" = use the field name
	Picker   string // "<integration>.<fn>" — field is filled by a connector capability, "" if absent
}

type EntityDecl struct {
	Name        string
	Label       string // human label (i18n), "" = use the entity name
	File        string
	Line        int
	Singleton   bool // at most one record (settings-style entities)
	Includes    []IncludeDecl // `include core.<Mixin>` lines (LLD-22 §1), before any own field
	Fields      []*FieldDecl
	Constraints []ConstraintDecl
}

// IncludeDecl is one `include core.<Mixin>` line at the top of an entity body
// (LLD-22 §1) — a field-mixin composition directive. The semantic phase
// inlines the mixin's fields into EntityDecl.Fields (LLD-22 §2) before any
// other check runs, so the resulting Model is indistinguishable from an
// entity whose fields were written out by hand.
type IncludeDecl struct {
	Mixin string
	Line  int
}

type ConstraintDecl struct {
	Line   int
	Kind   string // unique
	Fields []string
}

type RoleDecl struct {
	Name    string
	Line    int
	IsAgent bool
}

// PermItem is one target inside a permission rule:
//
//	read [Debtor, Contract]          → verb=read, items: {Entity:Debtor}, {Entity:Contract}
//	deny [update Debtor.debt, ...]   → verb=deny, items carry their own Verb
//	act [send_claim]                 → names
//	emit [OrderPlaced]               → names (LLD-01 §4.2 perm_verb)
//	read Debtor where manager = $me  → entity + where
type PermItem struct {
	Verb   string // for deny sub-items; otherwise inherits rule verb
	Entity string // "" for pure-name verbs (act/approve)
	Field  string // "" or field name or "*"
	All    bool   // `all` / `*` target
	Names  []string
	Where  string // raw expression
	Line   int
}

type PermRule struct {
	Verb  string // read create update delete act approve full deny
	Items []PermItem
	Line  int
}

type PermBlock struct {
	Role  string
	Line  int
	Rules []PermRule
}

type TransitionDecl struct {
	From, To string // From may be "any"
	Action   string
	Auto     bool
	When     string // raw guard expression
	// assignee=agent(Role) → AssigneeAgent=true; assignee=Role → human role
	AssigneeAgent bool
	AssigneeRole  string
	ApprovalRole  string // requires approval(Role)
	Label         string // human button label (i18n), "" = humanized action name
	Emits         []EmitClause // `emits Event(...)` tail(s) (LLD-01 §4)
	Line          int
}

type WorkflowDecl struct {
	Entity      string
	Field       string // the enum field the workflow governs
	Transitions []TransitionDecl
	File        string
	Line        int
}

// EventDecl is a domain event a pack owns (LLD-01 §4): a named payload schema
// that transitions/automations `emit` and other rules subscribe to via
// `on event`. Payload fields reuse FieldDecl but a closed subset of types is
// allowed (scalar + ref; NOT computed/serial/file/array[file]).
type EventDecl struct {
	Name   string
	Label  string
	Fields []*FieldDecl
	File   string
	Line   int
}

// Assign is one `field = expr` pair in an emit payload / create / update
// action. Expr stays raw text (evaluated at runtime in the record/payload
// context, like automation args).
type Assign struct {
	Field string
	Expr  string
	Line  int
}

// EmitClause is an `emits Event(field=expr, ...)` tail on a transition line
// (LLD-01 §4.1). Event may be short (`X`, own pack) or qualified (`pack.X`).
type EmitClause struct {
	Event string // possibly "pack.Event"
	Args  []Assign
	Line  int
}

type AutomationAction struct {
	Kind string // agent | notify | webhook | escalate | emit | create | update
	Role string // agent / escalate_to target
	Task string // agent task name
	Args string // raw args text
	Raw  string
	Line int

	// LLD-01 §4/§4b action payloads:
	Event   string   // emit: event name (possibly "pack.Event")
	Entity  string   // create: target entity
	RefPath string   // update: one-hop ref field of the trigger record ("" = self)
	Assigns []Assign // emit / create / update field=expr pairs
}

type AutomationRule struct {
	Trigger    string // schedule | create | update | delete | stuck | event
	Schedule   string // raw schedule text for schedule triggers
	Entity     string // bound entity ("" for global schedule rules)
	Event      string // event trigger: subscribed event ("pack.Event" or "Event")
	StuckState string
	StuckFor   string
	When       string // raw condition
	Actions    []AutomationAction
	File       string
	Line       int
}

type UIDecl struct {
	Entity    string
	FieldRefs []PermItem // line-tagged field references to validate (Entity.field)
	BoardBy   string
	File      string
	Line      int
}

type Manifest struct {
	Name     string
	Version  string
	Requires string
	Depends  []string
}

// ProductDecl is the product{} manifest section (LLD-05 §3, PK-4): white-label
// identity and default auth mode for a pack shipped as a standalone product.
// Backend-agnostic — the DSL has no notion of a "Face" (a frontend/renderer
// concept, LLD-05 §1); Home is carried through as an opaque string for the
// renderer to interpret. Node --brand* flags default from these values
// (cmd/kalita); the frontend themes off them via /api/meta (ADR-004: zero
// build step, override CSS custom properties).
type ProductDecl struct {
	Name   string
	Logo   string
	Colors map[string]string // token name -> value, e.g. "accent" -> "#4da3ff"
	Home   string            // face name shown as the landing screen
	Auth   string            // password | invite | token
	File   string
	Line   int
}

// LinkDecl is a named bidirectional relation between two entities (Jira issue
// links): `link Task -> Task as blocks / blocked_by`. The runtime keeps both
// sides consistent: adding a forward link creates the inverse automatically.
type LinkDecl struct {
	From    string
	To      string
	Forward string // e.g. blocks
	Inverse string // e.g. blocked_by
	File    string
	Line    int
}

// DashboardTile is one metric on a dashboard: an aggregate over ALL records of
// an entity (not row-scoped like a computed field), optionally filtered by a
// where-condition and/or grouped by a field (a breakdown like "deals by stage").
type DashboardTile struct {
	Label   string
	Func    string // count | sum | avg | min | max
	Entity  string
	Field   string // for sum/avg/min/max
	GroupBy string // "" = single number; else a per-value breakdown
	Where   string // raw filter expression over the whole table, "" = all rows
	Line    int
}

type DashboardDecl struct {
	Name  string
	Title string
	Tiles []DashboardTile
	File  string
	Line  int
}

// IntegrationParam is one typed parameter of a connector read-function. Type
// reuses the field type grammar; Default is raw expression text ("" if absent).
type IntegrationParam struct {
	Name    string
	Type    TypeRef
	Default string
	Line    int
}

// IntegrationFunc is one read-only capability of a connector:
//
//	read list_dirs(path: string = "") -> tree
//
// Result is the shape the worker returns (tree | list). Only `read` (no side
// effects) is allowed in v0.
type IntegrationFunc struct {
	Name   string
	Params []IntegrationParam
	Result string // tree | list
	Line   int
}

// IntegrationDecl is a connector contract — the typed read-functions a connector
// exposes. The backend URL is NOT here; it lives in a runtime record so a
// connector can be added or repointed without a rebuild.
type IntegrationDecl struct {
	Name  string
	Funcs []*IntegrationFunc
	File  string
	Line  int
}

// ProductMCPTool is one domain-facing MCP tool declared for the product
// façade worker (LLD-06 §1, PK-2 PR-2 — the façade itself is not built yet).
// Pure manifest DATA: a dotted name, typed inputs (reusing the entity field
// grammar via FieldDecl), and a declarative composition of node calls
// (query/create/act/emit_event/search_perimeter) as raw, uninterpreted text.
// This compiler does not execute or validate the composition — the façade
// worker will, reading it as a pass-through recipe under the caller's own
// bearer token (LLD-06 §1: strict pass-through, no service-account writes).
type ProductMCPTool struct {
	Name    string // dotted tool name, e.g. legal.submit_contract
	Desc    string
	In      []*FieldDecl // typed inputs, same grammar as entity fields
	Compose []string     // one node-call composition step per line, raw text
	Out     string       // raw output binding, e.g. "contract"
	File    string
	Line    int
}

// ProductMCPDecl is the product.mcp{} manifest section: the tool registry a
// generic façade worker reads instead of being coded per product (LLD-06 §1).
type ProductMCPDecl struct {
	Tools []*ProductMCPTool
	File  string
	Line  int
}

type AST struct {
	Manifest     *Manifest
	Product      *ProductDecl
	ProductMCP   *ProductMCPDecl
	Entities     []*EntityDecl
	Events       []*EventDecl
	Roles        []*RoleDecl
	Permissions  []*PermBlock
	Workflows    []*WorkflowDecl
	Automations  []*AutomationRule
	UIs          []*UIDecl
	Links        []*LinkDecl
	Dashboards   []*DashboardDecl
	Integrations []*IntegrationDecl
}

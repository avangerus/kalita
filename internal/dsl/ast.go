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
}

type EntityDecl struct {
	Name        string
	File        string
	Line        int
	Singleton   bool // at most one record (settings-style entities)
	Fields      []*FieldDecl
	Constraints []ConstraintDecl
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
	Line          int
}

type WorkflowDecl struct {
	Entity      string
	Field       string // the enum field the workflow governs
	Transitions []TransitionDecl
	File        string
	Line        int
}

type AutomationAction struct {
	Kind string // agent | notify | webhook | escalate
	Role string // agent / escalate_to target
	Task string // agent task name
	Args string // raw args text
	Raw  string
	Line int
}

type AutomationRule struct {
	Trigger    string // schedule | create | update | delete | stuck
	Schedule   string // raw schedule text for schedule triggers
	Entity     string // bound entity ("" for global schedule rules)
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

// DashboardTile is one metric on a dashboard: an aggregate over an entity,
// optionally grouped by a field (a breakdown like "deals by stage").
type DashboardTile struct {
	Label   string
	Func    string // count | sum
	Entity  string
	Field   string // for sum
	GroupBy string // "" = single number; else a per-value breakdown
	Line    int
}

type DashboardDecl struct {
	Name  string
	Title string
	Tiles []DashboardTile
	File  string
	Line  int
}

type AST struct {
	Manifest    *Manifest
	Entities    []*EntityDecl
	Roles       []*RoleDecl
	Permissions []*PermBlock
	Workflows   []*WorkflowDecl
	Automations []*AutomationRule
	UIs         []*UIDecl
	Links       []*LinkDecl
	Dashboards  []*DashboardDecl
}

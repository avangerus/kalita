package dsl

// Raw parse tree. The parser builds this; semantic analysis turns it into a
// Model. Workflow / automation / ui blocks are parsed structurally but
// analyzed in week 4 (BACKLOG-MVP); they are carried as RawBlock so files
// using them still compile their entity/permission parts today.

type TypeKind int

const (
	TyScalar TypeKind = iota // string text int float money bool date datetime file
	TyEnum
	TyRef
	TyArrayRef
)

var scalarTypes = map[string]bool{
	"string": true, "text": true, "int": true, "float": true, "money": true,
	"bool": true, "date": true, "datetime": true, "file": true,
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
}

type EntityDecl struct {
	Name        string
	File        string
	Line        int
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

type RawBlock struct {
	Kind  string // workflow | automation | ui
	Lines []Line
}

type Manifest struct {
	Name     string
	Version  string
	Requires string
	Depends  []string
}

type AST struct {
	Manifest    *Manifest
	Entities    []*EntityDecl
	Roles       []*RoleDecl
	Permissions []*PermBlock
	RawBlocks   []*RawBlock
}

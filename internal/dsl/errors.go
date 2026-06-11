// Package dsl is the Kalita DSL compiler: lexer, parser, semantic model.
// Grammar: docs/DSL-SPEC-v0.md. Design rules: closed lists everywhere, one way
// to express one thing, and every error carries a machine-readable fix hint —
// errors are the agent's self-correction loop, not log noise.
package dsl

import "fmt"

// Code is the closed list of compile error codes. Codes are append-only and
// never renamed: agents learn them.
type Code string

const (
	ETab            Code = "E001_TAB_INDENT"
	EUnexpectedChar Code = "E002_UNEXPECTED_CHAR"
	EExpectedColon  Code = "E003_EXPECTED_COLON"
	EBadIndent      Code = "E004_BAD_INDENT"
	EUnknownBlock   Code = "E005_UNKNOWN_BLOCK"
	EBadTypeSyntax  Code = "E006_BAD_TYPE_SYNTAX"
	EUnknownType    Code = "E007_UNKNOWN_TYPE"
	EBadModifier    Code = "E008_BAD_MODIFIER"
	EDupEntity      Code = "E009_DUPLICATE_ENTITY"
	EDupField       Code = "E010_DUPLICATE_FIELD"
	EUnknownRef     Code = "E011_UNKNOWN_REF_TARGET"
	EBadEnumDefault Code = "E012_DEFAULT_NOT_IN_ENUM"
	EConstraint     Code = "E013_CONSTRAINT_UNKNOWN_FIELD"
	EDupRole        Code = "E014_DUPLICATE_ROLE"
	EUnknownRole    Code = "E015_UNKNOWN_ROLE"
	EUnknownEntity  Code = "E016_UNKNOWN_ENTITY"
	EUnknownField   Code = "E017_UNKNOWN_FIELD"
	EBadVerb        Code = "E018_UNKNOWN_PERMISSION_VERB"
	EAgentNoDeny    Code = "E019_AGENT_ROLE_WITHOUT_DENY"
	EEmptyBlock     Code = "E020_EMPTY_BLOCK"
	EBadManifest    Code = "E021_BAD_MANIFEST"
	EOrphanBlock    Code = "E022_BLOCK_WITHOUT_ENTITY"
	EBadTransition  Code = "E023_BAD_TRANSITION"
	EBadAutomation  Code = "E024_BAD_AUTOMATION"
	EWorkflowField  Code = "E025_WORKFLOW_FIELD_NOT_ENUM"
	EUnknownState   Code = "E026_UNKNOWN_STATE"
	EDupAction      Code = "E027_DUPLICATE_ACTION"
	EUnknownAction  Code = "E028_UNKNOWN_ACTION"
	EUIUnknownField Code = "E029_UI_UNKNOWN_FIELD"
	EDupWorkflow    Code = "E030_DUPLICATE_WORKFLOW"
	ENotAgentRole   Code = "E031_ROLE_IS_NOT_AGENT"
	EBadLink        Code = "E032_BAD_LINK"
	ELinkEntity     Code = "E033_LINK_UNKNOWN_ENTITY"
	EDupLinkName    Code = "E034_DUPLICATE_LINK_NAME"
)

// Error is a single compile diagnostic. FixHint tells an agent (or a human)
// what to change; it must always be actionable.
type Error struct {
	Code    Code   `json:"code"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
	FixHint string `json:"fix_hint"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s %s:%d: %s (fix: %s)", e.Code, e.File, e.Line, e.Message, e.FixHint)
}

// Errors collects diagnostics across compilation; compilation continues past
// errors where possible so an agent gets the full picture in one pass.
type Errors struct {
	List []*Error
}

func (es *Errors) add(code Code, file string, line int, msg, hint string) {
	es.List = append(es.List, &Error{Code: code, File: file, Line: line, Message: msg, FixHint: hint})
}

func (es *Errors) Empty() bool { return len(es.List) == 0 }

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
	ETab               Code = "E001_TAB_INDENT"
	EUnexpectedChar    Code = "E002_UNEXPECTED_CHAR"
	EExpectedColon     Code = "E003_EXPECTED_COLON"
	EBadIndent         Code = "E004_BAD_INDENT"
	EUnknownBlock      Code = "E005_UNKNOWN_BLOCK"
	EBadTypeSyntax     Code = "E006_BAD_TYPE_SYNTAX"
	EUnknownType       Code = "E007_UNKNOWN_TYPE"
	EBadModifier       Code = "E008_BAD_MODIFIER"
	EDupEntity         Code = "E009_DUPLICATE_ENTITY"
	EDupField          Code = "E010_DUPLICATE_FIELD"
	EUnknownRef        Code = "E011_UNKNOWN_REF_TARGET"
	EBadEnumDefault    Code = "E012_DEFAULT_NOT_IN_ENUM"
	EConstraint        Code = "E013_CONSTRAINT_UNKNOWN_FIELD"
	EDupRole           Code = "E014_DUPLICATE_ROLE"
	EUnknownRole       Code = "E015_UNKNOWN_ROLE"
	EUnknownEntity     Code = "E016_UNKNOWN_ENTITY"
	EUnknownField      Code = "E017_UNKNOWN_FIELD"
	EBadVerb           Code = "E018_UNKNOWN_PERMISSION_VERB"
	EAgentNoDeny       Code = "E019_AGENT_ROLE_WITHOUT_DENY"
	EEmptyBlock        Code = "E020_EMPTY_BLOCK"
	EBadManifest       Code = "E021_BAD_MANIFEST"
	EOrphanBlock       Code = "E022_BLOCK_WITHOUT_ENTITY"
	EBadTransition     Code = "E023_BAD_TRANSITION"
	EBadAutomation     Code = "E024_BAD_AUTOMATION"
	EWorkflowField     Code = "E025_WORKFLOW_FIELD_NOT_ENUM"
	EUnknownState      Code = "E026_UNKNOWN_STATE"
	EDupAction         Code = "E027_DUPLICATE_ACTION"
	EUnknownAction     Code = "E028_UNKNOWN_ACTION"
	EUIUnknownField    Code = "E029_UI_UNKNOWN_FIELD"
	EDupWorkflow       Code = "E030_DUPLICATE_WORKFLOW"
	ENotAgentRole      Code = "E031_ROLE_IS_NOT_AGENT"
	EBadLink           Code = "E032_BAD_LINK"
	ELinkEntity        Code = "E033_LINK_UNKNOWN_ENTITY"
	EDupLinkName       Code = "E034_DUPLICATE_LINK_NAME"
	EBadDashboardTile  Code = "E035_BAD_DASHBOARD_TILE"
	EDashboardEntity   Code = "E036_DASHBOARD_UNKNOWN_ENTITY"
	EDashboardField    Code = "E037_DASHBOARD_UNKNOWN_FIELD"
	EBadIntegration    Code = "E038_BAD_INTEGRATION"
	EUnknownCapability Code = "E039_UNKNOWN_CAPABILITY"

	// LLD-01 §4/§4b/§4c: the `event` primitive, automation create/update
	// actions, and permission action-name validation.
	EUnknownEvent    Code = "E040_UNKNOWN_EVENT"         // emits/on event references a non-existent event
	EEventFieldType  Code = "E041_EVENT_FIELD_TYPE"      // event payload field is computed/serial/file/array[file]
	EEmitForeignPack Code = "E042_EMIT_FOREIGN_PACK"     // emits an event owned by another pack
	EEventPackAbsent Code = "E043_EVENT_PACK_ABSENT"     // on event <pack>.X where <pack> is not in this build
	EDupEvent        Code = "E044_DUPLICATE_EVENT"       // event name declared twice in a pack
	EEventPayload    Code = "E045_EVENT_PAYLOAD"         // emit payload field unknown / type mismatch / missing required
	EUpdateWorkflow  Code = "E046_UPDATE_WORKFLOW_FIELD" // update ... set <workflow-field> bypasses HITL
	EBadActionExpr   Code = "E047_BAD_ACTION_TARGET"     // create/update names an unknown entity/field/ref-path

	// LLD-01 §4.2/§5 (PR-5): the `emit` permission verb.
	EUnknownEmitGrant Code = "E048_UNKNOWN_EMIT_GRANT" // emit [Name] grant names an event this pack does not declare

	// LLD-08 §2 (PR-1): the `tail` permission verb (node-wide journal read).
	EUnknownTailKind Code = "E049_UNKNOWN_TAIL_KIND" // tail [kind,...] grant names a kind outside the closed eventstore.Kind taxonomy

	// LLD-05 §3 (PR-3): the `product` manifest block (PK-4 delivery wrapper).
	EBadProduct Code = "E050_BAD_PRODUCT" // malformed product{} field or a second product block

	// LLD-06 §1 (PK-2): the `product.mcp` manifest block (façade tool registry).
	EBadProductMCP Code = "E051_BAD_PRODUCT_MCP" // malformed product.mcp block, tool or a second product.mcp block

	// I-3 ("error is a signal, never silence"): a computed-field aggregate
	// (count/sum/avg/min/max) whose where-clause is not the one form the
	// runtime understands used to fail SILENTLY at read time — the field just
	// vanished from results (internal/engine/computed.go's evalAggregate
	// returns ok=false, withComputed drops the key). Now caught at compile time.
	EBadAggregateWhere Code = "E052_BAD_AGGREGATE_WHERE" // aggregate where-clause is not `reffield = $self`

	// LLD-22 §1/§2 (PR-1): `include core.<Mixin>` field-mixin composition.
	EUnknownMixin        Code = "E053_UNKNOWN_MIXIN"         // include names a mixin outside the core.* registry
	EMixinFieldCollision Code = "E054_MIXIN_FIELD_COLLISION" // a mixin field collides with an own field or another include
	EIncludePosition     Code = "E055_INCLUDE_POSITION"      // include appears after the first field
	EBadInclude          Code = "E056_BAD_INCLUDE"           // malformed `include` line

	// LLD-21 §5 (PR-2): the `subscription` top-level block — reactive wiring
	// (worker/endpoint reacts to node events) declared in the pack, born-
	// governed rather than hidden in worker config. Additive grammar (I-1):
	// covers a malformed block/trigger/sink, a name declared twice, and (once
	// PR-3 lands the egress whitelist) an undeclared external sink domain.
	EBadSubscription Code = "E057_BAD_SUBSCRIPTION"

	// TECHDEBT "Безопасность LLM-периметра" (b): the `flag` permission verb —
	// the mirror-image WRITE counterpart of `tail`'s read. Its target list is
	// its own closed, deliberately narrower set (flaggableKinds in
	// semantic_blocks.go), not the whole eventstore taxonomy `tail` accepts,
	// because a `flag [kind]` grant is the power to Append a core-reserved
	// kind directly, bypassing the normal create/update/act mutation surface.
	EUnknownFlagKind Code = "E058_UNKNOWN_FLAG_KIND" // flag [kind] grant names a kind outside the closed flaggable set

	// TECHDEBT "Ядро: automation / schedule" (P1): `on schedule ...` trigger
	// text used to be carried as free-form, unvalidated text — the runtime
	// (engine.Tick) understood only `every <dur>` and silently treated
	// anything else (including a typo'd `daily at 9:0` or outright garbage)
	// as "fires once a day", exactly the silent-degradation shape E052 closed
	// for aggregates (I-3: "error is a signal, never silence"). Now caught at
	// compile time by the same one-parser-two-layers discipline:
	// dsl.ParseSchedule / engine.Tick share the parser (see schedule.go).
	EBadSchedule Code = "E059_BAD_SCHEDULE_FORM" // schedule trigger is not `every <dur>` or `daily [at HH:MM]`

	// RELEASE-1.0 N8 §7.7 (panic-safety): an internal compiler panic recovered
	// inside Compile itself. This is NOT a DSL-authoring mistake — it means a
	// bug in the lexer/parser/analyzer crashed instead of returning a normal
	// diagnostic. It exists so Compile can never propagate a panic to its
	// callers, one of which is engine fold-replay: every node restart replays
	// eventstore.DefinitionApplied events (the pack directory is only the
	// genesis seed, definitions live in the journal), so an unrecovered panic
	// there would brick the node on every future boot, not just crash one
	// request. See compile.go.
	EInternalPanic Code = "E060_INTERNAL_COMPILER_ERROR"
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

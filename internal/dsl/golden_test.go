package dsl

import "testing"

// Golden suite: 20 deliberately broken sources. Week 2 DoD: every one of them
// produces a structured error with the expected code — and every error carries
// a non-empty fix hint (the agent self-correction contract).
func TestBrokenSources(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want Code
	}{
		{"tab indent", "entity A:\n\tname: string", ETab},
		{"unterminated string", "ui A:\n    list: \"oops", EUnexpectedChar},
		{"weird char", "entity A:\n    name: string @", EUnexpectedChar},
		{"entity without colon", "entity Debtor\n    name: string", EExpectedColon},
		{"field without colon", "entity A:\n    name string", EExpectedColon},
		{"top-level indent", "    entity A:", EBadIndent},
		{"unknown block", "banana A:\n    x: int", EUnknownBlock},
		{"missing type", "entity A:\n    name:", EBadTypeSyntax},
		{"unknown type", "entity A:\n    name: varchar", EUnknownType},
		{"empty enum", "entity A:\n    s: enum[]", EBadTypeSyntax},
		{"bad array", "entity A:\n    xs: array[int]", EBadTypeSyntax},
		{"unknown modifier", "entity A:\n    name: string mandatory", EBadModifier},
		{"bad on_delete", "entity A:\n    b: ref[A] on_delete=explode", EBadModifier},
		{"duplicate entity", "entity A:\n    x: int\n\nentity A:\n    y: int", EDupEntity},
		{"duplicate field", "entity A:\n    x: int\n    x: string", EDupField},
		{"unknown ref", "entity A:\n    b: ref[Ghost]", EUnknownRef},
		{"default not in enum", "entity A:\n    s: enum[On, Off] default=Maybe", EBadEnumDefault},
		{"constraint unknown field", "entity A:\n    x: int\n\nconstraints:\n    unique(x, ghost)", EConstraint},
		{"orphan constraints", "constraints:\n    unique(x)", EOrphanBlock},
		{"duplicate role", "roles:\n    Owner\n    Owner", EDupRole},
		{"perm for unknown role", "entity A:\n    x: int\n\npermissions:\n    Ghost:\n        read [A]", EUnknownRole},
		{"perm unknown entity", "roles:\n    Owner\n\npermissions:\n    Owner:\n        read [Ghost]", EUnknownEntity},
		{"perm unknown field", "entity A:\n    x: int\n\nroles:\n    Owner\n\npermissions:\n    Owner:\n        deny [update A.ghost]", EUnknownField},
		{"unknown verb", "entity A:\n    x: int\n\nroles:\n    Owner\n\npermissions:\n    Owner:\n        steal [A]", EBadVerb},
		{"agent without deny", "entity A:\n    x: int\n\nroles:\n    Bot agent\n\npermissions:\n    Bot:\n        read [A]", EAgentNoDeny},
		{"agent without permissions at all", "entity A:\n    x: int\n\nroles:\n    Bot agent", EAgentNoDeny},
		{"empty entity", "entity A:", EEmptyBlock},
		{"manifest without value", "pack", EBadManifest},
		{"bad transition syntax", "entity A:\n    s: enum[X, Y]\n\nworkflow A on s:\n    X Y: go", EBadTransition},
		{"workflow on non-enum", "entity A:\n    s: string\n\nworkflow A on s:\n    X -> Y: go", EWorkflowField},
		{"unknown state", "entity A:\n    s: enum[X, Y]\n\nworkflow A on s:\n    X -> Z: go", EUnknownState},
		{"duplicate action", "entity A:\n    s: enum[X, Y, Z]\n\nworkflow A on s:\n    X -> Y: go\n    Y -> Z: go", EDupAction},
		{"duplicate workflow", "entity A:\n    s: enum[X, Y]\n\nworkflow A on s:\n    X -> Y: go\n\nworkflow A on s:\n    Y -> X: back", EDupWorkflow},
		{"approval by unknown role", "entity A:\n    s: enum[X, Y]\n\nworkflow A on s:\n    X -> Y: go requires approval(Ghost)", EUnknownRole},
		{"agent assignee not agent role", "entity A:\n    s: enum[X, Y]\n\nroles:\n    Human\n\nworkflow A on s:\n    X -> Y: go assignee=agent(Human)", ENotAgentRole},
		{"act perm unknown action", "entity A:\n    s: enum[X, Y]\n\nroles:\n    Owner\n\nworkflow A on s:\n    X -> Y: go\n\npermissions:\n    Owner:\n        act [fly]", EUnknownAction},
		{"automation unknown trigger", "automation:\n    on explode A:\n        escalate_to Owner", EBadAutomation},
		{"schedule when without for", "entity A:\n    s: enum[X, Y]\n\nautomation:\n    on schedule daily when s = X:\n        escalate_to Owner", EBadAutomation},
		{"stuck without workflow", "entity A:\n    s: enum[X, Y]\n\nroles:\n    Owner\n\nautomation:\n    on stuck A in X for 2d:\n        escalate_to Owner", EBadAutomation},
		{"ui unknown field", "entity A:\n    x: int\n\nui A:\n    list: [x, ghost]", EUIUnknownField},
		{"ui board unknown field", "entity A:\n    x: int\n\nui A:\n    board: by ghost", EUIUnknownField},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := Compile(map[string]string{"bad.kal": tc.src})
			if len(errs) == 0 {
				t.Fatalf("must not compile clean")
			}
			found := false
			for _, e := range errs {
				if e.FixHint == "" {
					t.Errorf("error %s has empty fix_hint — hints are mandatory", e.Code)
				}
				if e.Code == tc.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("want code %s, got: %v", tc.want, errs)
			}
		})
	}
}

// A file with several independent mistakes reports them all in one pass —
// the agent fixes everything in a single round trip.
func TestMultipleErrorsInOnePass(t *testing.T) {
	src := `entity A:
    x: varchar
    y: ref[Ghost]
    z: enum[On, Off] default=Maybe
`
	_, errs := Compile(map[string]string{"bad.kal": src})
	if len(errs) < 3 {
		t.Fatalf("want at least 3 independent diagnostics, got %d: %v", len(errs), errs)
	}
}

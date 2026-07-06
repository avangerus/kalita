package dsl

import (
	"fmt"
	"strings"
	"testing"
)

// RELEASE-1.0 N8 §7.7 (panic-safety): Compile must never panic into its
// caller — the catastrophic case is engine fold-replay, which runs on every
// node restart (see compile.go's doc comment on Compile). This file proves
// the recover barrier itself works (by injection, since no real panicking
// input is known to exist — see TestMaliciousCorpusNeverPanics below) and
// that a large corpus of deliberately hostile inputs — the golden broken-
// source suite plus additional adversarial constructs — never hits it.

// TestCompileRecoversInternalPanic proves the barrier in Compile: substitute
// the inner pipeline with a stand-in that always panics, and check Compile
// converts that into an EInternalPanic compile error instead of letting the
// panic escape. This is the only way to test the barrier deterministically:
// it must hold even if (as far as we can tell — see below) no real DSL input
// can trigger it today.
func TestCompileRecoversInternalPanic(t *testing.T) {
	orig := compileImpl
	compileImpl = func(files map[string]string) (*Model, []*Error) {
		panic("simulated internal compiler panic")
	}
	defer func() { compileImpl = orig }()

	var model *Model
	var errs []*Error
	func() {
		// Outer recover is the negative proof: if Compile's own barrier were
		// ever removed or broken, this test must fail loudly instead of the
		// panic silently killing the test binary.
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Compile must recover its own panic, but it escaped: %v", r)
			}
		}()
		model, errs = Compile(map[string]string{"x.dsl": "entity A:\n    x: int"})
	}()

	if model != nil {
		t.Errorf("model must be nil when the panic barrier fires, got %+v", model)
	}
	if len(errs) != 1 || errs[0].Code != EInternalPanic {
		t.Fatalf("want exactly one EInternalPanic error, got %v", errs)
	}
	if errs[0].FixHint == "" {
		t.Error("even the internal-panic error must carry a fix_hint (the agent self-correction contract)")
	}
}

// brokenSourceCorpus is deliberately malformed/hostile DSL text: the golden
// broken-source suite (see TestBrokenSources in golden_test.go) plus
// additional adversarial constructs aimed at classic parser panic sources —
// out-of-range indexing, unbounded recursion/nesting, and malformed brackets
// — that the golden suite does not already cover verbatim.
var brokenSourceCorpus = []string{
	// --- golden broken sources (TestBrokenSources) ---
	"entity A:\n\tname: string",
	"ui A:\n    list: \"oops",
	"entity A:\n    name: string @",
	"entity Debtor\n    name: string",
	"entity A:\n    name string",
	"    entity A:",
	"banana A:\n    x: int",
	"entity A:\n    name:",
	"entity A:\n    name: varchar",
	"entity A:\n    s: enum[]",
	"entity A:\n    xs: array[int]",
	"entity A:\n    name: string mandatory",
	"entity A:\n    b: ref[A] on_delete=explode",
	"entity A:\n    x: int\n\nentity A:\n    y: int",
	"entity A:\n    x: int\n    x: string",
	"entity A:\n    b: ref[Ghost]",
	"entity A:\n    s: enum[On, Off] default=Maybe",
	"entity A:\n    x: int\n\nconstraints:\n    unique(x, ghost)",
	"constraints:\n    unique(x)",
	"roles:\n    Owner\n    Owner",
	"entity A:\n    x: int\n\npermissions:\n    Ghost:\n        read [A]",
	"roles:\n    Owner\n\npermissions:\n    Owner:\n        read [Ghost]",
	"entity A:\n    x: int\n\nroles:\n    Owner\n\npermissions:\n    Owner:\n        deny [update A.ghost]",
	"entity A:\n    x: int\n\nroles:\n    Owner\n\npermissions:\n    Owner:\n        steal [A]",
	"entity A:\n    x: int\n\nroles:\n    Bot agent\n\npermissions:\n    Bot:\n        read [A]",
	"entity A:\n    x: int\n\nroles:\n    Bot agent",
	"entity A:",
	"pack",
	"entity A:\n    s: enum[X, Y]\n\nworkflow A on s:\n    X Y: go",
	"entity A:\n    s: string\n\nworkflow A on s:\n    X -> Y: go",
	"entity A:\n    s: enum[X, Y]\n\nworkflow A on s:\n    X -> Z: go",
	"entity A:\n    s: enum[X, Y, Z]\n\nworkflow A on s:\n    X -> Y: go\n    Y -> Z: go",
	"entity A:\n    s: enum[X, Y]\n\nworkflow A on s:\n    X -> Y: go\n\nworkflow A on s:\n    Y -> X: back",
	"entity A:\n    s: enum[X, Y]\n\nworkflow A on s:\n    X -> Y: go requires approval(Ghost)",
	"entity A:\n    s: enum[X, Y]\n\nroles:\n    Human\n\nworkflow A on s:\n    X -> Y: go assignee=agent(Human)",
	"entity A:\n    s: enum[X, Y]\n\nroles:\n    Owner\n\nworkflow A on s:\n    X -> Y: go\n\npermissions:\n    Owner:\n        act [fly]",
	"entity A:\n    x: int\n\nroles:\n    Bridge agent\n\npermissions:\n    Bridge:\n        tail [event.exploded]\n        deny [delete *, create *, update *, act *, emit *, read *]",
	"automation:\n    on explode A:\n        escalate_to Owner",
	"entity A:\n    s: enum[X, Y]\n\nautomation:\n    on schedule daily when s = X:\n        escalate_to Owner",
	"entity A:\n    s: enum[X, Y]\n\nroles:\n    Owner\n\nautomation:\n    on stuck A in X for 2d:\n        escalate_to Owner",
	"entity A:\n    x: int\n\nui A:\n    list: [x, ghost]",
	"entity A:\n    x: int\n\nui A:\n    board: by ghost",
	"entity Finding:\n    status: string\n\nentity Contract:\n    n: int\n    open: int computed = count(Finding where contract = $self and status = Open)",
	"entity B:\n    x: int\n\nentity A:\n    c: int computed = count(B where $self = x)",
	"entity B:\n    x: int\n\nentity A:\n    c: int computed = count(B)",
	"automation:\n    on schedule every 5x:\n        escalate_to Owner",
	"automation:\n    on schedule daily at 25:00:\n        escalate_to Owner",
	"automation:\n    on schedule 0 9 * * 1-5:\n        escalate_to Owner",

	// --- additional adversarial constructs: bracket/paren/index abuse ---
	"entity A:\n    x: enum[",
	"entity A:\n    x: ref[",
	"entity A:\n    x: array[",
	"entity A:\n    x: array[enum[",
	"entity A:\n    x: array[ref[",
	"entity A:\n    x: mdg[",
	"entity A:\n    x: enum[,,,,]",
	"entity A:\n    x: enum[a][b][c]",
	"link A -> ",
	"link A -> B as",
	"link A -> B as x /",
	"link -> -> -> -> as / /",
	"include core.",
	"include",
	"entity A:\n    include core.Named\n    x: int\n    include core.Contact",
	"workflow A on s:",
	"workflow",
	"workflow A on s:\n    -> : go",
	"workflow A on s:\n    X -> : go",
	"workflow A on s:\n    X -> Y:",
	"workflow A on s:\n    X -> Y: go when",
	"workflow A on s:\n    X -> Y: go assignee=",
	"workflow A on s:\n    X -> Y: go assignee=agent(",
	"workflow A on s:\n    X -> Y: go requires",
	"workflow A on s:\n    X -> Y: go requires approval(",
	"workflow A on s:\n    X -> Y: go label=",
	"integration X:\n    read Y(",
	"integration",
	"dashboard X:\n    tile \"T\":",
	"dashboard X:\n    tile:",
	"roles:\n    A rate",
	"roles:\n    A rate abc",
	"permissions:\n    A:\n        deny",
	"permissions:\n    A:\n        deny [",
	"permissions:\n    A:\n        read [A.",
	"permissions:\n    A:\n        read [A. ] where",
	"product.mcp:",
	"product:",
	"subscription X:",
	"event E:",
	"entity A \"",
	"entity A \"unterminated:\n    x: int",

	// --- degenerate / whitespace / empty ---
	"",
	"\n\n\n\n",
	"# just a comment\n",
	"    \n\t\n",
	strings.Repeat("\n", 500),

	// --- deep / repeated nesting (bounded — real stack overflow is not
	// recoverable by any Go program, so this stays within a size the parser
	// is expected to actually handle, not an attempt to blow the stack) ---
	deepIndentSource(200),
	manyEntitiesSource(500),
	longIdentSource(5000),
	manyEnumValuesSource(2000),

	// --- unicode / control characters in identifiers and strings ---
	"entity A:\n    x: string default=\"日本語テキスト\"",
	"entity A:\n    x: string default=\"\x00\x01\x02\"",
	"entity データ:\n    x: int",
	"entity A:\n    x: string @#$%^&*()",
	"entity A:\n    x: string\r\n\r\n",
}

func deepIndentSource(n int) string {
	var b strings.Builder
	b.WriteString("entity A:\n")
	for i := 0; i < n; i++ {
		b.WriteString(strings.Repeat(" ", 4+i) + "# comment at depth\n")
	}
	b.WriteString("    x: int\n")
	return b.String()
}

func manyEntitiesSource(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "entity E%d:\n    x: int\n\n", i)
	}
	return b.String()
}

func longIdentSource(n int) string {
	return "entity A:\n    " + strings.Repeat("x", n) + ": int\n"
}

func manyEnumValuesSource(n int) string {
	vals := make([]string, n)
	for i := range vals {
		vals[i] = fmt.Sprintf("V%d", i)
	}
	return "entity A:\n    s: enum[" + strings.Join(vals, ", ") + "]\n"
}

// TestMaliciousCorpusNeverPanics runs the whole hostile corpus through the
// real, public Compile entrypoint. The outer per-case recover is the
// negative proof (a panic here must fail the test loudly, never crash the
// binary); the assertion that no result ever carries EInternalPanic is the
// positive proof that the corpus contains NO input that actually reaches
// Compile's recover barrier — i.e. the parser is, as far as this corpus can
// tell, totally panic-safe on its own, and the barrier is pure defense in
// depth for constructs not in this corpus (or not yet written).
func TestMaliciousCorpusNeverPanics(t *testing.T) {
	for i, src := range brokenSourceCorpus {
		src := src
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			var model *Model
			var errs []*Error
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("Compile panicked (must never happen — Compile is documented panic-safe): %v\nsource:\n%s", r, src)
					}
				}()
				model, errs = Compile(map[string]string{"fuzz.dsl": src})
			}()
			for _, e := range errs {
				if e.Code == EInternalPanic {
					t.Errorf("input hit Compile's internal-panic recovery barrier — a REAL panicking construct exists: %s\nsource:\n%s", e.Message, src)
				}
			}
			_ = model // compiling successfully or with ordinary errors is fine either way
		})
	}
}

// FuzzCompile fuzzes compileImpl DIRECTLY (bypassing Compile's recover) so a
// real parser bug surfaces as a fuzz failure pinpointing the actual crashing
// construct, rather than being silently absorbed by the barrier. Compile
// itself (the production entrypoint) stays safe regardless of what fuzzing
// finds, because of the recover in compile.go — this target exists purely to
// search for a genuine panic-inducing input, honestly reporting whichever
// way it goes.
func FuzzCompile(f *testing.F) {
	for _, src := range brokenSourceCorpus {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		_, _ = compileImpl(map[string]string{"fuzz.dsl": src})
	})
}

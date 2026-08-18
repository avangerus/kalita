package dsl

import (
	"fmt"
	"sort"
	"strings"
)

// compileImpl is the actual compilation pipeline (lex → parse → analyze).
// Compile below wraps it with a recover barrier; the indirection through this
// package-private var exists ONLY so that barrier is unit-testable by
// substituting a deliberately panicking stand-in (see
// TestCompileRecoversInternalPanic in compile_test.go) without needing a real
// panic-inducing DSL construct to exist. Nothing outside this package can see
// or touch this var — it is not part of the public API.
var compileImpl = func(files map[string]string) (*Model, []*Error) {
	errs := &Errors{}
	ast := &AST{}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths) // deterministic compile order

	for _, path := range paths {
		lines := lex(path, files[path], errs)
		parse(lines, errs, ast)
	}

	model := analyze(ast, errs)
	return model, errs.List
}

// Compile parses and analyzes a set of .dsl sources (path → content) as one
// pack. It returns the model and ALL diagnostics found — compilation does not
// stop at the first error, because an agent fixing the file needs the full
// picture in a single round trip.
//
// Panic-safety (RELEASE-1.0 N8 §7.7): Compile is called from two places —
// when an agent proposes a change (engine.ProposeChange/DecideProposal, live
// request) AND on every node restart, when engine fold replays
// eventstore.DefinitionApplied events (the pack directory is only the
// genesis seed; definitions replay from the journal, see engine.go's
// applyEvent). A panic reaching the SECOND caller is unrecoverable by any
// retry — the poison is IN the journal, so the node fails to start on every
// future restart until someone edits history by hand, which is worse than
// any single request failing. Compile therefore recovers any internal panic
// itself, once, here, turning it into an ordinary compile error
// (EInternalPanic) instead of letting it escape. Both call sites inherit the
// fix uniformly; neither needs (or should grow) its own recover.
func Compile(files map[string]string) (model *Model, errs []*Error) {
	defer func() {
		if r := recover(); r != nil {
			model = nil
			errs = []*Error{{
				Code:    EInternalPanic,
				Message: fmt.Sprintf("internal compiler error (recovered panic): %v", r),
				FixHint: "this is a kalita DSL compiler bug, not a mistake in your source — report it upstream; nothing was applied",
			}}
		}
	}()
	return compileImpl(files)
}

// packHeaderOf extracts the `pack <name>` header a .dsl file declares — the
// file's own identity line (first line of the file), never the path. Returns
// "" when the file carries no header (such files group together and compile
// as one pack, same as a single headerless compile today).
func packHeaderOf(src string) string {
	for _, line := range strings.Split(src, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "pack" {
			return fields[1]
		}
		if strings.HasPrefix(strings.TrimSpace(line), "pack") {
			break // a `pack` line that is not `pack <name>` — treat as headerless
		}
	}
	return ""
}

// CompileMulti compiles a flat sources map that may contain SEVERAL packs
// (the product-mission shape: a proposal can carry the whole multi-pack
// definition — add a pack while keeping every existing one). Files are
// grouped by their own `pack <name>` header; each group compiles exactly as
// Compile would, then dsl.MergeModels merges the results with the same
// collision loudness as the node's boot loader. A single-group map is
// byte-identical to Compile(files) (I-1): existing single-pack proposals and
// journals replay unchanged.
func CompileMulti(files map[string]string) (model *Model, errs []*Error) {
	defer func() {
		if r := recover(); r != nil {
			model = nil
			errs = []*Error{{
				Code:    EInternalPanic,
				Message: fmt.Sprintf("internal compiler error (recovered panic): %v", r),
				FixHint: "this is a kalita DSL compiler bug, not a mistake in your source — report it upstream; nothing was applied",
			}}
		}
	}()
	groups := map[string]map[string]string{}
	var order []string
	for _, path := range sortedPaths(files) {
		h := packHeaderOf(files[path])
		if _, ok := groups[h]; !ok {
			groups[h] = map[string]string{}
			order = append(order, h)
		}
		groups[h][path] = files[path]
	}
	if len(order) == 1 {
		return Compile(files)
	}
	models := make([]*Model, 0, len(order))
	for _, h := range order {
		m, errs := Compile(groups[h])
		if len(errs) > 0 {
			return nil, errs
		}
		models = append(models, m)
	}
	return MergeModels(models)
}

func sortedPaths(files map[string]string) []string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths) // deterministic group order, same rule as compileImpl
	return paths
}

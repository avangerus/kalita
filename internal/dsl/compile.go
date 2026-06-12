package dsl

import "sort"

// Compile parses and analyzes a set of .dsl sources (path → content) as one
// pack. It returns the model and ALL diagnostics found — compilation does not
// stop at the first error, because an agent fixing the file needs the full
// picture in a single round trip.
func Compile(files map[string]string) (*Model, []*Error) {
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

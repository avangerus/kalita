package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/avangerus/kalita/internal/dsl"
)

// Serial fields: auto-assigned human-readable document numbers (INV-2026-00042,
// PROJ-7). The kernel hands out a monotonic per-entity-per-field sequence; the
// counter is derived from existing records on replay, so it survives restarts
// without a separate stored cursor. Gaps are fine (a failed create may burn a
// number) — sequences guarantee uniqueness and order, not density.

var reSeqToken = regexp.MustCompile(`\{seq(?::(\d+))?\}`)

// assignSerials fills any unset serial fields with the next formatted value.
// Called inside Create, under the lock, before the record is journaled.
func (e *Engine) assignSerials(decl *dsl.EntityDecl, values map[string]any) {
	for _, f := range decl.Fields {
		if f.Type.Kind != dsl.TyScalar || f.Type.Scalar != "serial" {
			continue
		}
		if v, present := values[f.Name]; present && v != nil && v != "" {
			continue // explicit value wins (e.g. import)
		}
		next := e.nextSerial(decl.Name, f.Name)
		values[f.Name] = formatSerial(f.Format, next, e.now().Year())
	}
}

// nextSerial returns the next sequence for an entity.field, scanning existing
// records for the current maximum (replay-safe, no stored cursor).
func (e *Engine) nextSerial(entity, field string) int {
	max := 0
	decl := e.model.Entities[entity]
	var format string
	for _, f := range decl.Fields {
		if f.Name == field {
			format = f.Format
		}
	}
	for _, rec := range e.records[entity] {
		s, _ := rec.Values[field].(string)
		if n := parseSeq(s, format); n > max {
			max = n
		}
	}
	return max + 1
}

// formatSerial renders a format with {seq[:width]} and {year}. Default "{seq}".
func formatSerial(format string, seq, year int) string {
	if format == "" {
		format = "{seq}"
	}
	out := strings.ReplaceAll(format, "{year}", strconv.Itoa(year))
	out = reSeqToken.ReplaceAllStringFunc(out, func(tok string) string {
		m := reSeqToken.FindStringSubmatch(tok)
		if m[1] != "" {
			w, _ := strconv.Atoi(m[1])
			return fmt.Sprintf("%0*d", w, seq)
		}
		return strconv.Itoa(seq)
	})
	return out
}

// parseSeq extracts the sequence number from a formatted serial value by
// turning the format into a regex: literals are escaped, {year} -> \d{4},
// {seq[:w]} -> the (\d+) capture. Replay-safe counter resume.
func parseSeq(value, format string) int {
	if value == "" {
		return 0
	}
	if format == "" {
		format = "{seq}"
	}
	// split format into literal segments around {year} and {seq...} tokens
	tokenRe := regexp.MustCompile(`\{year\}|\{seq(?::\d+)?\}`)
	var pat strings.Builder
	pat.WriteString("^")
	last := 0
	for _, loc := range tokenRe.FindAllStringIndex(format, -1) {
		pat.WriteString(regexp.QuoteMeta(format[last:loc[0]]))
		if strings.HasPrefix(format[loc[0]:loc[1]], "{year}") {
			pat.WriteString(`\d{4}`)
		} else {
			pat.WriteString(`(\d+)`)
		}
		last = loc[1]
	}
	pat.WriteString(regexp.QuoteMeta(format[last:]))
	pat.WriteString("$")

	re, err := regexp.Compile(pat.String())
	if err != nil {
		return 0
	}
	m := re.FindStringSubmatch(value)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

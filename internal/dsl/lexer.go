package dsl

import (
	"strings"
	"unicode"
)

// The lexer is line-oriented: a .kal file is a sequence of logical lines with
// an indent level and a flat token list. Block structure comes from indents,
// which keeps both the parser and the constrained-decoding grammar small.

type TokKind int

const (
	TIdent TokKind = iota // identifiers, keywords, $me, durations (10d)
	TNum                  // 42, 3.14
	TStr                  // "quoted"
	TPunct                // : , [ ] ( ) = . * -> < > <= >= != +
)

type Tok struct {
	Kind TokKind
	Text string
}

type Line struct {
	File   string
	Num    int // 1-based
	Indent int // count of leading spaces
	Raw    string
	Toks   []Tok
}

// lex splits source into logical lines. Tabs in indentation are an error:
// one way to indent, no mixed-whitespace drift.
func lex(file, src string, errs *Errors) []Line {
	var out []Line
	for i, raw := range strings.Split(src, "\n") {
		num := i + 1
		line := strings.TrimRight(raw, " \r")
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") || strings.TrimLeft(line, " \t") == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "\t") || strings.Contains(line[:len(line)-len(trimmed)], "\t") {
			errs.add(ETab, file, num, "tab character in indentation", "indent with spaces only (4 per level)")
			continue
		}
		indent := len(line) - len(trimmed)
		toks := tokenize(file, num, trimmed, errs)
		if len(toks) == 0 {
			continue
		}
		out = append(out, Line{File: file, Num: num, Indent: indent, Raw: trimmed, Toks: toks})
	}
	return out
}

func isIdentRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$'
}

func tokenize(file string, num int, s string, errs *Errors) []Tok {
	var toks []Tok
	i := 0
	for i < len(s) {
		c := rune(s[i])
		switch {
		case c == ' ':
			i++
		case c == '#':
			return toks // comment to end of line
		case c == '"':
			j := i + 1
			for j < len(s) && s[j] != '"' {
				j++
			}
			if j >= len(s) {
				errs.add(EUnexpectedChar, file, num, "unterminated string", `close the string with "`)
				return toks
			}
			toks = append(toks, Tok{TStr, s[i+1 : j]})
			i = j + 1
		case unicode.IsDigit(c):
			j := i
			for j < len(s) && (unicode.IsDigit(rune(s[j])) || s[j] == '.') {
				j++
			}
			// trailing unit (10d, 48h) folds into one ident-like token
			for j < len(s) && unicode.IsLetter(rune(s[j])) {
				j++
			}
			toks = append(toks, Tok{TNum, s[i:j]})
			i = j
		case isIdentRune(c):
			j := i
			for j < len(s) && isIdentRune(rune(s[j])) {
				j++
			}
			toks = append(toks, Tok{TIdent, s[i:j]})
			i = j
		case c == '-' && i+1 < len(s) && s[i+1] == '>':
			toks = append(toks, Tok{TPunct, "->"})
			i += 2
		case c == '-' && i+1 < len(s) && unicode.IsDigit(rune(s[i+1])):
			// negative number / descending sort prefix: keep '-' as punct
			toks = append(toks, Tok{TPunct, "-"})
			i++
		case strings.ContainsRune("<>!", c) && i+1 < len(s) && s[i+1] == '=':
			toks = append(toks, Tok{TPunct, s[i:i+2]})
			i += 2
		case strings.ContainsRune(":,[]()=.*<>+-/", c):
			toks = append(toks, Tok{TPunct, string(c)})
			i++
		default:
			errs.add(EUnexpectedChar, file, num, "unexpected character "+string(c),
				"remove it; the DSL allows identifiers, numbers, strings and : , [ ] ( ) = . * -> < > <= >= !=")
			i++
		}
	}
	return toks
}

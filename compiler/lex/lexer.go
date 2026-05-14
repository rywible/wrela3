package lex

import (
	"strings"
	"unicode"

	"github.com/ryanwible/wrela3/compiler/diag"
)

// All tokenizes a source string.
func All(src string) ([]Token, []diag.Diagnostic) {
	var toks []Token
	var ds []diag.Diagnostic

	for i := 0; i < len(src); {
		ch := src[i]

		if isWhitespace(ch) {
			i++
			continue
		}

		if ch == '\n' {
			toks = append(toks, Token{Kind: Newline, Text: "\n", Start: i, End: i + 1})
			i++
			continue
		}

		if isLineComment(src, i) {
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}

		if i+1 < len(src) {
			if kind, ok := twoCharOperatorAt(src[i:]); ok {
				toks = append(toks, Token{Kind: kind, Text: src[i : i+2], Start: i, End: i + 2})
				i += 2
				continue
			}
		}

		if kind, ok := singleCharTokens[ch]; ok {
			toks = append(toks, Token{Kind: kind, Text: src[i : i+1], Start: i, End: i + 1})
			i++
			continue
		}

		if ch == '"' {
			tok, next, err := lexString(src, i)
			toks = append(toks, tok)
			if err != "" {
				ds = append(ds, diag.Diagnostic{Phase: "lex", Code: BadStringDiagnosticCode, FilePath: "", Start: i, End: next, Message: err})
			}
			i = next
			continue
		}

		if unicode.IsDigit(rune(ch)) {
			tok, next := lexInt(src, i)
			toks = append(toks, tok)
			i = next
			continue
		}

		if isIdentStart(ch) {
			tok, next := lexIdent(src, i)
			if kind, ok := keywordKinds[tok.Text]; ok {
				tok.Kind = kind
			}
			toks = append(toks, tok)
			i = next
			continue
		}

		ds = append(ds, diag.Diagnostic{
			Phase:    "lex",
			Code:     BadStringDiagnosticCode,
			FilePath: "",
			Start:    i,
			End:      i + 1,
			Message:  "invalid character",
		})
		i++
	}

	toks = append(toks, Token{Kind: EOF, Text: "", Start: len(src), End: len(src)})
	return toks, ds
}

func isWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\f'
}

func isLineComment(src string, i int) bool {
	if src[i] != '/' || i+1 >= len(src) {
		return false
	}
	return src[i+1] == '/'
}

func twoCharOperatorAt(s string) (Kind, bool) {
	if len(s) < 2 {
		return 0, false
	}
	kind, ok := twoCharOperators[s[0:2]]
	return kind, ok
}

func lexIdent(src string, i int) (Token, int) {
	start := i
	for i < len(src) && isIdentContinue(src[i]) {
		i++
	}
	return Token{Kind: Identifier, Text: src[start:i], Start: start, End: i}, i
}

func isIdentStart(ch byte) bool {
	return ch == '_' || unicode.IsLetter(rune(ch))
}

func isIdentContinue(ch byte) bool {
	return isIdentStart(ch) || unicode.IsDigit(rune(ch))
}

func lexInt(src string, i int) (Token, int) {
	start := i
	if strings.HasPrefix(src[i:], "0x") || strings.HasPrefix(src[i:], "0X") {
		i += 2
		for i < len(src) && isHexDigit(src[i]) {
			i++
		}
		return Token{Kind: Integer, Text: src[start:i], Start: start, End: i}, i
	}
	for i < len(src) && unicode.IsDigit(rune(src[i])) {
		i++
	}
	return Token{Kind: Integer, Text: src[start:i], Start: start, End: i}, i
}

func isHexDigit(ch byte) bool {
	return unicode.IsDigit(rune(ch)) ||
		(ch >= 'a' && ch <= 'f') ||
		(ch >= 'A' && ch <= 'F')
}

func lexString(src string, i int) (Token, int, string) {
	start := i
	i++
	for i < len(src) {
		if src[i] == '\\' {
			i++
			if i >= len(src) {
				return Token{Kind: String, Text: src[start:i], Start: start, End: i}, i, "unterminated string literal"
			}
			i++
			continue
		}
		if src[i] == '"' {
			i++
			return Token{Kind: String, Text: src[start+1 : i-1], Start: start, End: i}, i, ""
		}
		if src[i] == '\n' {
			return Token{Kind: String, Text: src[start+1 : i], Start: start, End: i}, i, "unterminated string literal"
		}
		i++
	}
	return Token{Kind: String, Text: src[start+1:], Start: start, End: len(src)}, len(src), "unterminated string literal"
}

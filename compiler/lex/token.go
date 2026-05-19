package lex

import "github.com/ryanwible/wrela3/compiler/diag"

// Kind identifies lexical token kinds used by parser and tests.
type Kind int

const (
	EOF Kind = iota
	Identifier
	Integer
	String

	LParen
	RParen
	LBrace
	RBrace
	LBracket
	RBracket
	Colon
	Comma
	Dot
	Plus
	Minus
	Star
	Slash
	Percent
	Less
	LessEqual
	Greater
	GreaterEqual
	Equal
	EqualEqual
	Bang
	BangEqual
	Amp
	Pipe
	Caret
	Arrow
	FatArrow
	ShiftLeft
	ShiftRight

	KeywordModule
	KeywordUse
	KeywordFrom
	KeywordData
	KeywordClass
	KeywordUnique
	KeywordDriver
	KeywordPath
	KeywordExecutor
	KeywordInterrupt
	KeywordReceiver
	KeywordOn
	KeywordImage
	KeywordTransitions
	KeywordPhase
	KeywordFn
	KeywordAsm
	KeywordStart
	KeywordLet
	KeywordReturn
	KeywordIf
	KeywordElse
	KeywordWhile
	KeywordWith
	KeywordFor
	KeywordIn
	KeywordEnum
	KeywordTrait
	KeywordImpl
	KeywordWhere
	KeywordConst
	KeywordStaticAssert
	KeywordEvent
	KeywordProjection
	KeywordMatch
	KeywordSizeof
	KeywordAlignof
	KeywordTrue
	KeywordFalse
	KeywordNever

	Semicolon
	Newline
)

// Token is a span-bearing lexical token.
type Token struct {
	Kind  Kind
	Text  string
	Start int
	End   int
}

var keywordKinds = map[string]Kind{
	"module":        KeywordModule,
	"use":           KeywordUse,
	"from":          KeywordFrom,
	"data":          KeywordData,
	"class":         KeywordClass,
	"unique":        KeywordUnique,
	"driver":        KeywordDriver,
	"path":          KeywordPath,
	"executor":      KeywordExecutor,
	"interrupt":     KeywordInterrupt,
	"receiver":      KeywordReceiver,
	"on":            KeywordOn,
	"image":         KeywordImage,
	"transitions":   KeywordTransitions,
	"phase":         KeywordPhase,
	"fn":            KeywordFn,
	"asm":           KeywordAsm,
	"start":         KeywordStart,
	"let":           KeywordLet,
	"return":        KeywordReturn,
	"if":            KeywordIf,
	"else":          KeywordElse,
	"while":         KeywordWhile,
	"with":          KeywordWith,
	"for":           KeywordFor,
	"enum":          KeywordEnum,
	"trait":         KeywordTrait,
	"impl":          KeywordImpl,
	"in":            KeywordIn,
	"where":         KeywordWhere,
	"const":         KeywordConst,
	"static_assert": KeywordStaticAssert,
	"event":         KeywordEvent,
	"projection":    KeywordProjection,
	"match":         KeywordMatch,
	"sizeof":        KeywordSizeof,
	"alignof":       KeywordAlignof,
	"true":          KeywordTrue,
	"false":         KeywordFalse,
	"never":         KeywordNever,
}

var twoCharOperators = map[string]Kind{
	"->": Arrow,
	"=>": FatArrow,
	"==": EqualEqual,
	"!=": BangEqual,
	"<=": LessEqual,
	">=": GreaterEqual,
	"<<": ShiftLeft,
	">>": ShiftRight,
}

var singleCharTokens = map[byte]Kind{
	'(': LParen,
	')': RParen,
	'{': LBrace,
	'}': RBrace,
	'[': LBracket,
	']': RBracket,
	':': Colon,
	',': Comma,
	'.': Dot,
	'+': Plus,
	'-': Minus,
	'*': Star,
	'/': Slash,
	'%': Percent,
	'<': Less,
	'>': Greater,
	'=': Equal,
	'!': Bang,
	'&': Amp,
	'|': Pipe,
	'^': Caret,
	';': Semicolon,
}

// BadStringDiagnosticCode is the lexer error code for malformed string literals.
const BadStringDiagnosticCode = diag.PAR0001

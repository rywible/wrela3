package compiler

import "github.com/ryanwible/wrela3/compiler/internal/codeerr"

type CodeError = codeerr.CodeError

func NewCodeError(code, message string) CodeError {
	return codeerr.New(code, message)
}

package ir

import "github.com/ryanwible/wrela3/compiler/diag"

func Lower(checked any) (*Program, []diag.Diagnostic) {
	return nil, []diag.Diagnostic{{
		Phase:   "cg",
		Code:    diag.CG0001,
		Message: "lowering requires a semantic program; sem package is not yet available in this branch",
	}}
}

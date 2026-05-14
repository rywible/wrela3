package ir

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestLowerReturnsCG0001WithoutSem(t *testing.T) {
	_, diags := Lower(struct{}{})
	if len(diags) != 1 || diags[0].Code != diag.CG0001 {
		t.Fatalf("diags = %#v, want one CG0001", diags)
	}
}

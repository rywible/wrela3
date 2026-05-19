package codegen

import "testing"

func TestStorageEncoderCodegenHarnessBuildsProgram(t *testing.T) {
	program := storageEncoderProgramForCodegenTest()
	if len(program.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(program.Functions))
	}
	if program.Functions[0].Symbol == "" {
		t.Fatalf("encoder symbol must not be empty")
	}
}

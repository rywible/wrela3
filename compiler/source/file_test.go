package source_test

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/source"
)

func TestLineColumn(t *testing.T) {
	f := source.NewFile(1, "main.wrela", "a\nbc\n")
	line, col := f.LineColumn(3)
	if line != 2 || col != 2 {
		t.Fatalf("LineColumn(3) = %d:%d, want 2:2", line, col)
	}
}

func TestLineColumnEmptyFile(t *testing.T) {
	f := source.NewFile(1, "empty.wrela", "")
	line, col := f.LineColumn(0)
	if line != 1 || col != 1 {
		t.Fatalf("LineColumn(0) = %d:%d, want 1:1", line, col)
	}
}

func TestLineColumnTrailingNewline(t *testing.T) {
	f := source.NewFile(1, "main.wrela", "a\n")
	if line, col := f.LineColumn(2); line != 2 || col != 1 {
		t.Fatalf("LineColumn(2) = %d:%d, want 2:1", line, col)
	}
}

func TestLineColumnLineStartIsStable(t *testing.T) {
	f := source.NewFile(1, "main.wrela", "a\nbc\ndef")
	line, col := f.LineColumn(2)
	if line != 2 || col != 1 {
		t.Fatalf("LineColumn(2) = %d:%d, want 2:1", line, col)
	}
}

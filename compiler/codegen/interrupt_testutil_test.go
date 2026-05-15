package codegen

import (
	"sort"
	"testing"
)

func symbolBytes(t *testing.T, image *Image, symbol string) []byte {
	t.Helper()
	rva, ok := image.Symbols[symbol]
	if !ok {
		t.Fatalf("missing symbol %s", symbol)
	}
	text := image.Sections[0]
	start := int(rva - text.RVA)
	end := len(text.Data)
	var starts []int
	for _, other := range image.Symbols {
		if other > rva {
			starts = append(starts, int(other-text.RVA))
		}
	}
	if len(starts) != 0 {
		sort.Ints(starts)
		end = starts[0]
	}
	if start < 0 || start > len(text.Data) || end < start || end > len(text.Data) {
		t.Fatalf("invalid symbol span for %s: %d..%d in %d bytes", symbol, start, end, len(text.Data))
	}
	return text.Data[start:end]
}

func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}

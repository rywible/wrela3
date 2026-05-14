package codegen

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestCompileEntryAdapterCallOrderAndHalt(t *testing.T) {
	delegatedHw := &ir.ConstInt{
		Symbol: "owned",
		Value:  1,
		Type:   ir.Type{Name: "U64"},
	}
	delegated := ir.Function{
		Symbol: "image_delegated",
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					delegatedHw,
					&ir.Return{Value: delegatedHw},
				},
			},
		},
	}

	owned := ir.Function{
		Symbol: "image_owned",
		Params: []ir.Value{
			&ir.Param{Symbol: "owned", Type: ir.Type{Name: "OwnedHardware"}},
		},
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					&ir.Return{},
				},
			},
		},
	}

	program := &ir.Program{
		Functions: []ir.Function{delegated, owned},
		Entry: ir.EntryAdapter{
			Symbol:                "_wrela_efi_entry",
			DelegatedPhaseSymbol:  "image_delegated",
			OwnedPhaseSymbol:      "image_owned",
			DelegatedHardwareType: "DelegatedHardware",
			OwnedHardwareType:     "OwnedHardware",
		},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	entryRVA := image.Symbols["_wrela_efi_entry"]
	if entryRVA == 0 {
		t.Fatal("missing _wrela_efi_entry symbol")
	}
	entryOffset := int(entryRVA - 0x1000)
	entryCode := image.Sections[0].Data[entryOffset:]
	for name, pattern := range map[string][]byte{
		"stack frame":                    {0x55, 0x48, 0x89, 0xE5},
		"delegated hardware allocation":  {0x48, 0x83, 0xEC, 0x40},
		"store image handle":             {0x48, 0x89, 0x4D, 0xC0},
		"load boot services table":       {0x48, 0x8B, 0x42, 0x60},
		"store boot services table":      {0x48, 0x89, 0x45, 0xC8},
		"pass delegated hardware record": {0x48, 0x8B, 0xFD, 0x48, 0x83, 0xC7, 0xC0},
	} {
		if !bytes.Contains(entryCode, pattern) {
			t.Fatalf("entry adapter missing %s pattern %#x", name, pattern)
		}
	}

	setupEnd := bytes.Index(entryCode, []byte{0x48, 0x8B, 0xFD, 0x48, 0x83, 0xC7, 0xC0})
	if setupEnd < 0 {
		t.Fatal("missing final delegated hardware argument setup")
	}
	setupEnd += 7
	callOffsets := allOffsets(entryCode[setupEnd:], 0xE8)
	if len(callOffsets) < 2 {
		t.Fatalf("expected two adapter calls, got %d", len(callOffsets))
	}
	for i := range callOffsets {
		callOffsets[i] += setupEnd
	}

	firstCall := callOffsets[0]
	firstRel := int32(binary.LittleEndian.Uint32(entryCode[firstCall+1 : firstCall+5]))
	gotFirst := int64(firstRel)
	wantFirst := int64(int64(image.Symbols["image_delegated"]) - int64(entryRVA+uint64(firstCall)+5))
	if gotFirst != wantFirst {
		t.Fatalf("first rel32 = %d, want %d", gotFirst, wantFirst)
	}
	if len(entryCode) < firstCall+5+2 || !bytes.Equal(entryCode[firstCall+5:firstCall+8], []byte{0x48, 0x8B, 0xF8}) {
		t.Fatalf("delegated return value should move rax to rdi: %#x", entryCode[firstCall:firstCall+8])
	}

	secondCall := callOffsets[1]
	secondRel := int32(binary.LittleEndian.Uint32(entryCode[secondCall+1 : secondCall+5]))
	gotSecond := int64(secondRel)
	wantSecond := int64(int64(image.Symbols["image_owned"]) - int64(entryRVA+uint64(secondCall)+5))
	if gotSecond != wantSecond {
		t.Fatalf("second rel32 = %d, want %d", gotSecond, wantSecond)
	}

	if secondCall <= firstCall {
		t.Fatalf("owned phase call should come after delegated call")
	}
	if !bytes.Contains(entryCode, []byte{0xF4, 0xE9}) {
		t.Fatalf("entry adapter should end in hlt loop")
	}
}

func allOffsets(data []byte, value byte) []int {
	var offsets []int
	for i, b := range data {
		if b == value {
			offsets = append(offsets, i)
		}
	}
	return offsets
}

package codegen

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/ir"
	"github.com/ryanwible/wrela3/compiler/layout"
)

func TestCompileEntryAdapterMaterializesRecordsAndNestedOffsets(t *testing.T) {
	delegated := ir.Function{Symbol: "image_delegated", Blocks: []ir.Block{{
		Label: "entry",
		Ops:   []ir.Operation{&ir.Return{}},
	}}}
	owned := ir.Function{Symbol: "image_owned", Params: []ir.Value{&ir.Param{Symbol: "owned", Type: ir.Type{Name: "OwnedHardware"}}}, Blocks: []ir.Block{{
		Label: "entry",
		Ops:   []ir.Operation{&ir.Return{}},
	}}}
	program := &ir.Program{
		Functions: []ir.Function{delegated, owned},
		Entry: ir.EntryAdapter{
			Symbol:               "_wrela_efi_entry",
			DelegatedPhaseSymbol: "image_delegated",
			OwnedPhaseSymbol:     "image_owned",
		},
	}

	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", ds)
	}
	entry := image.Sections[0].Data[int(image.Symbols["_wrela_efi_entry"]-0x1000):]
	adapterLayout := buildEntryAdapterLayout()

	if adapterLayout.FrameSize != 176 {
		t.Fatalf("entry adapter frame size = %d, want %d", adapterLayout.FrameSize, 176)
	}
	if adapterLayout.UefiHandleOffset != -8 ||
		adapterLayout.UefiBootServicesOffset != -16 ||
		adapterLayout.UefiBootServicesCallsOffset != -24 ||
		adapterLayout.DelegatedMemoryOffset != -88 ||
		adapterLayout.DelegatedHardwareOffset != -168 {
		t.Fatalf("unexpected base offsets: %#v", adapterLayout)
	}

	assertContainsInstruction(t, entry, asm.Instruction{Mnemonic: "push", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rbp")},
	}})
	if !bytes.Contains(entry, []byte{0x48, 0x89, 0xE5}) {
		t.Fatal("missing mov rbp, rsp prologue")
	}
	assertContainsInstruction(t, entry, asm.Instruction{Mnemonic: "sub", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rsp")},
		asm.ImmOperand{Value: int64(adapterLayout.FrameSize)},
	}})

	assertContainsInstruction(t, entry, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.RegOperand{Reg: asm.MustLookup("rcx")},
	}})
	assertContainsInstruction(t, entry, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(adapterLayout.UefiHandleOffset), Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})

	assertContainsInstruction(t, entry, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.MemOperand{Base: asm.MustLookup("rdx"), Disp: 96, Width: 64},
	}})
	assertContainsInstruction(t, entry, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(adapterLayout.UefiBootServicesOffset), Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})

	assertContainsInstruction(t, entry, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(adapterLayout.UefiBootServicesOffset), Width: 64},
	}})
	assertContainsInstruction(t, entry, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(adapterLayout.UefiBootServicesCallsOffset), Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})

	delegatedMemoryRecord := buildEntryRecordLayout("DelegatedMemory", entryRecordFields["DelegatedMemory"], map[string]layout.Record{})
	assertStoresImmediateSlot(t, entry, 0x200000, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["arena_base"].Offset)
	assertStoresImmediateSlot(t, entry, 0x200000, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["arena_length"].Offset)
	assertStoresImmediateSlot(t, entry, 0, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["next_offset"].Offset)

	uefiHandleRecord := buildEntryRecordLayout("UefiHandle", entryRecordFields["UefiHandle"], map[string]layout.Record{})
	bootCallsRecord := buildEntryRecordLayout("UefiBootServicesCalls", entryRecordFields["UefiBootServicesCalls"], map[string]layout.Record{})
	assertCopiesStackRange(t, entry, adapterLayout.DelegatedHardwareImageOffset, adapterLayout.UefiHandleOffset, uefiHandleRecord.Size)
	assertCopiesStackRange(t, entry, adapterLayout.DelegatedHardwareBootOffset, adapterLayout.UefiBootServicesCallsOffset, bootCallsRecord.Size)
	assertCopiesStackRange(t, entry, adapterLayout.DelegatedHardwareMemoryOffset, adapterLayout.DelegatedMemoryOffset, delegatedMemoryRecord.Size)
}

func TestCompileEntryAdapterCallOrderAndHalt(t *testing.T) {
	delegatedResult := &ir.ConstInt{
		Symbol: "owned",
		Value:  1,
		Type:   ir.Type{Name: "U64"},
	}
	delegated := ir.Function{Symbol: "image_delegated", Blocks: []ir.Block{{
		Label: "entry",
		Ops:   []ir.Operation{delegatedResult, &ir.Return{Value: delegatedResult}},
	}}}
	owned := ir.Function{Symbol: "image_owned", Params: []ir.Value{&ir.Param{Symbol: "owned", Type: ir.Type{Name: "OwnedHardware"}}}, Blocks: []ir.Block{{
		Label: "entry",
		Ops:   []ir.Operation{&ir.Return{}},
	}}}
	program := &ir.Program{Functions: []ir.Function{delegated, owned}, Entry: ir.EntryAdapter{
		Symbol:               "_wrela_efi_entry",
		DelegatedPhaseSymbol: "image_delegated",
		OwnedPhaseSymbol:     "image_owned",
	}}
	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", ds)
	}

	entry := image.Sections[0].Data[int(image.Symbols["_wrela_efi_entry"]-0x1000):]
	rig := mustEncode(t, asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.ImmOperand{Value: int64(buildEntryAdapterLayout().DelegatedHardwareOffset)},
	}})
	setupEnd := bytes.Index(entry, rig)
	if setupEnd < 0 {
		t.Fatal("missing delegated hardware argument setup")
	}

	callOffsets := allOffsets(entry[setupEnd:], 0xE8)
	if len(callOffsets) < 2 {
		t.Fatalf("expected two adapter calls, got %d", len(callOffsets))
	}
	for i := range callOffsets {
		callOffsets[i] += setupEnd
	}

	firstCall := callOffsets[0]
	gotFirst := int64(int32(binary.LittleEndian.Uint32(entry[firstCall+1 : firstCall+5])))
	wantFirst := int64(int64(image.Symbols["image_delegated"]) - int64(image.Symbols["_wrela_efi_entry"]+uint64(firstCall)+5))
	if gotFirst != wantFirst {
		t.Fatalf("first rel32 = %d, want %d", gotFirst, wantFirst)
	}
	if !bytes.Equal(entry[firstCall+5:firstCall+8], []byte{0x48, 0x8B, 0xF8}) {
		t.Fatalf("delegated return value should move rax to rdi")
	}

	secondCall := callOffsets[1]
	gotSecond := int64(int32(binary.LittleEndian.Uint32(entry[secondCall+1 : secondCall+5])))
	wantSecond := int64(int64(image.Symbols["image_owned"]) - int64(image.Symbols["_wrela_efi_entry"]+uint64(secondCall)+5))
	if gotSecond != wantSecond {
		t.Fatalf("second rel32 = %d, want %d", gotSecond, wantSecond)
	}

	if secondCall <= firstCall {
		t.Fatalf("owned phase call should come after delegated call")
	}
	if !bytes.Contains(entry, []byte{0xF4, 0xE9}) {
		t.Fatalf("entry adapter should end in hlt loop")
	}
}

func TestCompileCallPassesRecordReturnSlotInR10(t *testing.T) {
	const mapResult = "UefiMemoryMapResult"
	call := &ir.Call{Symbol: "get_map", Type: ir.Type{Name: mapResult}}
	caller := ir.Function{
		Symbol: "caller",
		Blocks: []ir.Block{{
			Label: "entry",
			Ops:   []ir.Operation{call, &ir.Return{Value: call}},
		}},
	}
	asmMethod := ir.AsmMethod{
		Symbol: "get_map",
		Return: ir.Type{Name: mapResult},
		Body:   "mov rax, 0x10\nret",
	}
	program := &ir.Program{Functions: []ir.Function{caller}, AsmMethods: []ir.AsmMethod{asmMethod}}

	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", ds)
	}

	callerCode := image.Sections[0].Data[int(image.Symbols["caller"]-0x1000):]
	callOffset := bytes.Index(callerCode, []byte{0xE8})
	if callOffset < 0 {
		t.Fatal("missing get_map call")
	}
	if callOffset < 4 {
		t.Fatal("expected r10 setup and record-return call")
	}
	encodedR10 := append(
		mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
			asm.RegOperand{Reg: asm.MustLookup("r10")},
			asm.RegOperand{Reg: asm.MustLookup("rbp")},
		}}),
		mustEncode(t, asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
			asm.RegOperand{Reg: asm.MustLookup("r10")},
			asm.ImmOperand{Value: -8},
		}})...,
	)
	if callOffset < len(encodedR10) || !bytes.Equal(callerCode[callOffset-len(encodedR10):callOffset], encodedR10) {
		t.Fatalf("r10 setup should occur immediately before call")
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

func mustEncode(t *testing.T, instr asm.Instruction) []byte {
	t.Helper()
	encoded, ds := asm.Encode([]asm.Instruction{instr})
	if len(ds) != 0 {
		t.Fatalf("encoding %s failed: %#v", instr.Mnemonic, ds)
	}
	return encoded
}

func assertContainsInstruction(t *testing.T, code []byte, instr asm.Instruction) {
	t.Helper()
	if !bytes.Contains(code, mustEncode(t, instr)) {
		t.Fatalf("missing instruction %s %v", instr.Mnemonic, instr.Operands)
	}
}

func assertStoresImmediateSlot(t *testing.T, code []byte, value int64, slot int) {
	t.Helper()
	sequence := append(
		mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
			asm.RegOperand{Reg: asm.MustLookup("rax")},
			asm.ImmOperand{Value: value},
		}}),
		mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
			asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(slot), Width: 64},
			asm.RegOperand{Reg: asm.MustLookup("rax")},
		}})...,
	)
	if !bytes.Contains(code, sequence) {
		t.Fatalf("missing immediate store value %d to stack slot %d", value, slot)
	}
}

func assertCopiesStackRange(t *testing.T, code []byte, dstSlot, srcSlot, size int) {
	t.Helper()
	for i := 0; i < size; i += 8 {
		assertContainsInstruction(t, code, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
			asm.RegOperand{Reg: asm.MustLookup("rax")},
			asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(srcSlot + i), Width: 64},
		}})
		assertContainsInstruction(t, code, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
			asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(dstSlot + i), Width: 64},
			asm.RegOperand{Reg: asm.MustLookup("rax")},
		}})
	}
}

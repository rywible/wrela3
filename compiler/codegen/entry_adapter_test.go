package codegen

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/ir"
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
		Types:     entryAdapterTestTypes(104),
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
	adapterLayout, ok := buildEntryAdapterLayout(compileContext{types: program.Types})
	if !ok {
		t.Fatal("buildEntryAdapterLayout() failed")
	}

	if adapterLayout.FrameSize != 144 {
		t.Fatalf("entry adapter frame size = %d, want %d", adapterLayout.FrameSize, 144)
	}
	if adapterLayout.UefiHandleOffset != -8 ||
		adapterLayout.UefiBootServicesOffset != -16 ||
		adapterLayout.UefiBootServicesCallsOffset != -24 ||
		adapterLayout.DelegatedBytesOffset != -40 ||
		adapterLayout.UefiMemoryMapOffset != -72 ||
		adapterLayout.DelegatedMemoryOffset != -104 ||
		adapterLayout.DelegatedHardwareOffset != -136 {
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
		asm.MemOperand{Base: asm.MustLookup("rdx"), Disp: 104, Width: 64},
	}})
	assertContainsInstruction(t, entry, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(adapterLayout.UefiBootServicesOffset), Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})

	bootCallsRecord, ok := recordLayoutFromTypeInfo(program.Types["UefiBootServicesCalls"])
	if !ok {
		t.Fatal("UefiBootServicesCalls TypeInfo did not produce a record layout")
	}
	if bootCallsRecord.Size != 8 || bootCallsRecord.Fields["boot_services"].Size != 8 {
		t.Fatalf("UefiBootServicesCalls layout = %#v, want handle field", bootCallsRecord)
	}

	delegatedMemoryRecord, ok := recordLayoutFromTypeInfo(program.Types["DelegatedMemory"])
	if !ok {
		t.Fatal("DelegatedMemory TypeInfo did not produce a record layout")
	}
	if delegatedMemoryRecord.Size != 32 || delegatedMemoryRecord.Fields["last_memory_map"].Offset != 24 || delegatedMemoryRecord.Fields["last_memory_map"].Size != 8 {
		t.Fatalf("DelegatedMemory layout = %#v, want last_memory_map handle at +24", delegatedMemoryRecord)
	}
	assertStoresImmediateSlot(t, entry, 0x200000, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["arena_base"].Offset)
	assertStoresImmediateSlot(t, entry, 0x200000, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["arena_length"].Offset)
	assertStoresImmediateSlot(t, entry, 0, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["next_offset"].Offset)

	memoryMapRecord, ok := recordLayoutFromTypeInfo(program.Types["UefiMemoryMap"])
	if !ok {
		t.Fatal("UefiMemoryMap TypeInfo did not produce a record layout")
	}
	if memoryMapRecord.Size != 32 || memoryMapRecord.Fields["descriptors"].Size != 8 || memoryMapRecord.Fields["key"].Offset != 24 {
		t.Fatalf("UefiMemoryMap layout = %#v, want descriptors handle and key at +24", memoryMapRecord)
	}
	hardwareRecord, ok := recordLayoutFromTypeInfo(program.Types["DelegatedHardware"])
	if !ok {
		t.Fatal("DelegatedHardware TypeInfo did not produce a record layout")
	}
	if hardwareRecord.Size != 32 ||
		hardwareRecord.Fields["image_handle"].Offset != 0 ||
		hardwareRecord.Fields["boot_services"].Offset != 8 ||
		hardwareRecord.Fields["system_table"].Offset != 16 ||
		hardwareRecord.Fields["delegated_memory"].Offset != 24 {
		t.Fatalf("DelegatedHardware layout = %#v, want four handle fields", hardwareRecord)
	}

	assertStoresSlotValue(t, entry, adapterLayout.UefiBootServicesCallsOffset, adapterLayout.UefiBootServicesOffset)
	assertStoresSlotAddress(t, entry, adapterLayout.UefiMemoryMapDescriptorsOffset, adapterLayout.DelegatedBytesOffset)
	assertStoresSlotAddress(t, entry, adapterLayout.DelegatedMemoryMapOffset, adapterLayout.UefiMemoryMapOffset)
	assertStoresSlotAddress(t, entry, adapterLayout.DelegatedHardwareImageOffset, adapterLayout.UefiHandleOffset)
	assertStoresSlotAddress(t, entry, adapterLayout.DelegatedHardwareBootOffset, adapterLayout.UefiBootServicesCallsOffset)
	assertContainsInstruction(t, entry, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(adapterLayout.DelegatedHardwareSystemOffset), Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rdx")},
	}})
	assertStoresSlotAddress(t, entry, adapterLayout.DelegatedHardwareMemoryOffset, adapterLayout.DelegatedMemoryOffset)
}

func TestCompileEntryAdapterUsesTypeInfoForMaterializedRecordOffsets(t *testing.T) {
	delegated := ir.Function{Symbol: "image_delegated", Blocks: []ir.Block{{
		Label: "entry",
		Ops:   []ir.Operation{&ir.Return{}},
	}}}
	owned := ir.Function{Symbol: "image_owned", Params: []ir.Value{&ir.Param{Symbol: "owned", Type: ir.Type{Name: "OwnedHardware"}}}, Blocks: []ir.Block{{
		Label: "entry",
		Ops:   []ir.Operation{&ir.Return{}},
	}}}
	types := entryAdapterTestTypes(96)
	types["UefiMemoryMap"] = ir.TypeInfo{
		Name:  "UefiMemoryMap",
		Kind:  ir.TypeKindData,
		Size:  48,
		Align: 8,
		Fields: map[string]ir.FieldInfo{
			"descriptors":        {Name: "descriptors", Offset: 16, Size: 8, Align: 8},
			"descriptor_size":    {Name: "descriptor_size", Offset: 24, Size: 8, Align: 8},
			"descriptor_version": {Name: "descriptor_version", Offset: 32, Size: 4, Align: 4},
			"key":                {Name: "key", Offset: 40, Size: 8, Align: 8},
		},
	}
	program := &ir.Program{Functions: []ir.Function{delegated, owned}, Types: types, Entry: ir.EntryAdapter{
		Symbol:               "_wrela_efi_entry",
		DelegatedPhaseSymbol: "image_delegated",
		OwnedPhaseSymbol:     "image_owned",
	}}

	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", ds)
	}

	entry := image.Sections[0].Data[int(image.Symbols["_wrela_efi_entry"]-0x1000):]
	adapterLayout, ok := buildEntryAdapterLayout(compileContext{types: types})
	if !ok {
		t.Fatal("buildEntryAdapterLayout() failed")
	}
	if got := adapterLayout.UefiMemoryMapDescriptorsOffset - adapterLayout.UefiMemoryMapOffset; got != 16 {
		t.Fatalf("descriptors offset = %d, want TypeInfo offset 16", got)
	}
	assertStoresSlotAddress(t, entry, adapterLayout.UefiMemoryMapDescriptorsOffset, adapterLayout.DelegatedBytesOffset)
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
	program := &ir.Program{Functions: []ir.Function{delegated, owned}, Types: entryAdapterTestTypes(96), Entry: ir.EntryAdapter{
		Symbol:               "_wrela_efi_entry",
		DelegatedPhaseSymbol: "image_delegated",
		OwnedPhaseSymbol:     "image_owned",
	}}
	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", ds)
	}

	entry := image.Sections[0].Data[int(image.Symbols["_wrela_efi_entry"]-0x1000):]
	adapterLayout, ok := buildEntryAdapterLayout(compileContext{types: program.Types})
	if !ok {
		t.Fatal("buildEntryAdapterLayout() failed")
	}
	rig := mustEncode(t, asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.ImmOperand{Value: int64(adapterLayout.DelegatedHardwareOffset)},
	}})
	setupEnd := bytes.Index(entry, rig)
	if setupEnd < 0 {
		t.Fatal("missing delegated hardware argument setup")
	}

	callOffsets := allOffsets(entry[setupEnd:], 0xE8)
	if len(callOffsets) < 3 {
		t.Fatalf("expected three adapter calls, got %d", len(callOffsets))
	}
	for i := range callOffsets {
		callOffsets[i] += setupEnd
	}

	firstCall := callOffsets[0]
	if gotFirst := entryAdapterCallTarget(entry, image, firstCall); gotFirst != image.Symbols["image_delegated"] {
		t.Fatalf("first call target = %#x, want image_delegated %#x", gotFirst, image.Symbols["image_delegated"])
	}

	installCall := callOffsets[1]
	if gotInstall := entryAdapterCallTarget(entry, image, installCall); gotInstall != image.Symbols[apTrampolineInstallSymbol] {
		t.Fatalf("second call target = %#x, want AP trampoline install %#x", gotInstall, image.Symbols[apTrampolineInstallSymbol])
	}

	ownedCall := callOffsets[2]
	if gotOwned := entryAdapterCallTarget(entry, image, ownedCall); gotOwned != image.Symbols["image_owned"] {
		t.Fatalf("third call target = %#x, want image_owned %#x", gotOwned, image.Symbols["image_owned"])
	}

	moveReturnToOwned := bytes.Index(entry[installCall+5:ownedCall], []byte{0x48, 0x8B, 0xF8})
	if moveReturnToOwned < 0 {
		t.Fatalf("delegated return value should move rax to rdi after AP trampoline install")
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
	program := &ir.Program{
		Functions:  []ir.Function{caller},
		AsmMethods: []ir.AsmMethod{asmMethod},
		Types: map[string]ir.TypeInfo{
			mapResult: {Name: mapResult, Kind: ir.TypeKindData},
		},
	}

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
			asm.ImmOperand{Value: -16},
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

func entryAdapterCallTarget(entry []byte, image *Image, callOffset int) uint64 {
	rel := int32(binary.LittleEndian.Uint32(entry[callOffset+1 : callOffset+5]))
	return uint64(int64(image.Symbols["_wrela_efi_entry"]) + int64(callOffset) + 5 + int64(rel))
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

func assertStoresSlotAddress(t *testing.T, code []byte, dstSlot, srcSlot int) {
	t.Helper()
	sequence := append(
		mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
			asm.RegOperand{Reg: asm.MustLookup("rax")},
			asm.RegOperand{Reg: asm.MustLookup("rbp")},
		}}),
		mustEncode(t, asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
			asm.RegOperand{Reg: asm.MustLookup("rax")},
			asm.ImmOperand{Value: int64(srcSlot)},
		}})...,
	)
	sequence = append(sequence, mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(dstSlot), Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})...)
	if !bytes.Contains(code, sequence) {
		t.Fatalf("missing address store of stack slot %d to stack slot %d", srcSlot, dstSlot)
	}
}

func assertStoresSlotValue(t *testing.T, code []byte, dstSlot, srcSlot int) {
	t.Helper()
	sequence := append(
		mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
			asm.RegOperand{Reg: asm.MustLookup("rax")},
			asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(srcSlot), Width: 64},
		}}),
		mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
			asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(dstSlot), Width: 64},
			asm.RegOperand{Reg: asm.MustLookup("rax")},
		}})...,
	)
	if !bytes.Contains(code, sequence) {
		t.Fatalf("missing store of slot value from %d into %d", srcSlot, dstSlot)
	}
}

func entryAdapterTestTypes(bootServicesOffset int) map[string]ir.TypeInfo {
	return map[string]ir.TypeInfo{
		"UefiHandle": entryAdapterTypeInfo("UefiHandle", 8, 8,
			entryAdapterField("address", 0, 8, 8),
		),
		"UefiBootServicesCalls": entryAdapterTypeInfo("UefiBootServicesCalls", 8, 8,
			entryAdapterField("boot_services", 0, 8, 8),
		),
		"DelegatedBytes": entryAdapterTypeInfo("DelegatedBytes", 16, 8,
			entryAdapterField("address", 0, 8, 8),
			entryAdapterField("length", 8, 8, 8),
		),
		"UefiMemoryMap": entryAdapterTypeInfo("UefiMemoryMap", 32, 8,
			entryAdapterField("descriptors", 0, 8, 8),
			entryAdapterField("descriptor_size", 8, 8, 8),
			entryAdapterField("descriptor_version", 16, 4, 4),
			entryAdapterField("key", 24, 8, 8),
		),
		"DelegatedMemory": entryAdapterTypeInfo("DelegatedMemory", 32, 8,
			entryAdapterField("arena_base", 0, 8, 8),
			entryAdapterField("arena_length", 8, 8, 8),
			entryAdapterField("next_offset", 16, 8, 8),
			entryAdapterField("last_memory_map", 24, 8, 8),
		),
		"DelegatedHardware": entryAdapterTypeInfo("DelegatedHardware", 32, 8,
			entryAdapterField("image_handle", 0, 8, 8),
			entryAdapterField("boot_services", 8, 8, 8),
			entryAdapterField("system_table", 16, 8, 8),
			entryAdapterField("delegated_memory", 24, 8, 8),
		),
		"UefiSystemTable": {
			Name: "UefiSystemTable",
			Kind: ir.TypeKindClass,
			Fields: map[string]ir.FieldInfo{
				"boot_services": {Name: "boot_services", Offset: bootServicesOffset, Size: 8, Align: 8},
			},
		},
	}
}

func entryAdapterTypeInfo(name string, size, align int, fields ...ir.FieldInfo) ir.TypeInfo {
	info := ir.TypeInfo{
		Name:   name,
		Kind:   ir.TypeKindData,
		Size:   size,
		Align:  align,
		Fields: map[string]ir.FieldInfo{},
	}
	for _, field := range fields {
		info.Fields[field.Name] = field
	}
	return info
}

func entryAdapterField(name string, offset, size, align int) ir.FieldInfo {
	return ir.FieldInfo{Name: name, Offset: offset, Size: size, Align: align}
}

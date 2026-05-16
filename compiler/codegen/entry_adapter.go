package codegen

import (
	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
	"github.com/ryanwible/wrela3/compiler/layout"
)

var entryRecordNames = []string{
	"UefiHandle",
	"UefiBootServicesCalls",
	"DelegatedBytes",
	"UefiMemoryMap",
	"DelegatedMemory",
	"DelegatedHardware",
}

type entryFrameLayout struct {
	FrameSize                      int
	UefiHandleOffset               int
	UefiBootServicesOffset         int
	UefiBootServicesCallsOffset    int
	DelegatedBytesOffset           int
	UefiMemoryMapOffset            int
	DelegatedMemoryOffset          int
	DelegatedHardwareOffset        int
	UefiMemoryMapDescriptorsOffset int
	DelegatedMemoryMapOffset       int
	DelegatedHardwareImageOffset   int
	DelegatedHardwareBootOffset    int
	DelegatedHardwareMemoryOffset  int
}

func buildEntryAdapterLayout(ctx compileContext) (entryFrameLayout, bool) {
	records, ok := entryAdapterRecords(ctx)
	if !ok {
		return entryFrameLayout{}, false
	}

	ordered := []string{
		"UefiHandle",
		"UefiBootServices",
		"UefiBootServicesCalls",
		"DelegatedBytes",
		"UefiMemoryMap",
		"DelegatedMemory",
		"DelegatedHardware",
	}
	offsets := map[string]int{}
	cursor := 0
	for _, name := range ordered {
		record := entryAdapterSlotLayout(name, records)
		cursor += record.Size
		cursor = layout.AlignUp(cursor, record.Align)
		offsets[name] = -cursor
	}

	frameSize := layout.AlignUp(cursor, 16)
	hw := records["DelegatedHardware"]
	mem := records["DelegatedMemory"]
	memoryMap := records["UefiMemoryMap"]
	if !recordHasFields(mem, "arena_base", "arena_length", "next_offset", "last_memory_map") ||
		!recordHasFields(hw, "image_handle", "boot_services", "delegated_memory") ||
		!recordHasFields(memoryMap, "descriptors") {
		return entryFrameLayout{}, false
	}
	memoryMapDescriptors, ok := recordField(memoryMap, "descriptors")
	if !ok {
		return entryFrameLayout{}, false
	}
	delegatedHardwareImage, ok := recordField(hw, "image_handle")
	if !ok {
		return entryFrameLayout{}, false
	}
	delegatedHardwareBoot, ok := recordField(hw, "boot_services")
	if !ok {
		return entryFrameLayout{}, false
	}
	delegatedHardwareMemory, ok := recordField(hw, "delegated_memory")
	if !ok {
		return entryFrameLayout{}, false
	}
	delegatedMemoryMap, ok := recordField(mem, "last_memory_map")
	if !ok {
		return entryFrameLayout{}, false
	}

	return entryFrameLayout{
		FrameSize:                      frameSize,
		UefiHandleOffset:               offsets["UefiHandle"],
		UefiBootServicesOffset:         offsets["UefiBootServices"],
		UefiBootServicesCallsOffset:    offsets["UefiBootServicesCalls"],
		DelegatedBytesOffset:           offsets["DelegatedBytes"],
		UefiMemoryMapOffset:            offsets["UefiMemoryMap"],
		DelegatedMemoryOffset:          offsets["DelegatedMemory"],
		DelegatedHardwareOffset:        offsets["DelegatedHardware"],
		UefiMemoryMapDescriptorsOffset: offsets["UefiMemoryMap"] + memoryMapDescriptors.Offset,
		DelegatedHardwareImageOffset:   offsets["DelegatedHardware"] + delegatedHardwareImage.Offset,
		DelegatedHardwareBootOffset:    offsets["DelegatedHardware"] + delegatedHardwareBoot.Offset,
		DelegatedHardwareMemoryOffset:  offsets["DelegatedHardware"] + delegatedHardwareMemory.Offset,
		DelegatedMemoryMapOffset:       offsets["DelegatedMemory"] + delegatedMemoryMap.Offset,
	}, true
}

func entryAdapterSlotLayout(name string, records map[string]layout.Record) layout.Record {
	if name == "UefiBootServices" {
		return layout.Record{Size: 8, Align: 8, Fields: map[string]layout.FieldLayout{}}
	}
	return records[name]
}

func entryAdapterRecords(ctx compileContext) (map[string]layout.Record, bool) {
	records := map[string]layout.Record{}
	for _, name := range entryRecordNames {
		record, ok := recordLayoutFromTypeInfo(ctx.types[name])
		if !ok {
			return nil, false
		}
		records[name] = record
	}
	return records, true
}

func recordLayoutFromTypeInfo(info ir.TypeInfo) (layout.Record, bool) {
	if info.Size <= 0 || info.Align <= 0 || len(info.Fields) == 0 {
		return layout.Record{}, false
	}
	record := layout.Record{
		Size:   info.Size,
		Align:  info.Align,
		Fields: map[string]layout.FieldLayout{},
	}
	for name, field := range info.Fields {
		if field.Size <= 0 || field.Align <= 0 {
			return layout.Record{}, false
		}
		record.Fields[name] = layout.FieldLayout{
			Offset: field.Offset,
			Size:   field.Size,
			Align:  field.Align,
		}
	}
	return record, true
}

func recordField(record layout.Record, name string) (layout.FieldLayout, bool) {
	field, ok := record.Fields[name]
	return field, ok
}

func recordHasFields(record layout.Record, names ...string) bool {
	for _, name := range names {
		if _, ok := record.Fields[name]; !ok {
			return false
		}
	}
	return true
}

func emitSlotFromBase(e *Emitter, dest asm.Reg, base asm.Reg, slot int) {
	emitRegRegMove(e, dest, base)
	e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
		asm.RegOperand{Reg: dest},
		asm.ImmOperand{Value: int64(slot)},
	}})
}

func emitStoreImmediateSlot(e *Emitter, value int64, slot int) {
	emitMovImmToReg(e, asm.MustLookup("rax"), value)
	emitStoreSlotFromReg(e, asm.MustLookup("rax"), slot, 64)
}

func emitStoreSlotAddress(e *Emitter, dstSlot, srcSlot int) {
	emitSlotFromBase(e, asm.MustLookup("rax"), asm.MustLookup("rbp"), srcSlot)
	emitStoreSlotFromReg(e, asm.MustLookup("rax"), dstSlot, 64)
}

func emitStoreSlotValue(e *Emitter, dstSlot, srcSlot int) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(srcSlot), Width: 64},
	}})
	emitStoreSlotFromReg(e, asm.MustLookup("rax"), dstSlot, 64)
}

func emitInstallAPTrampoline(e *Emitter) {
	emitMovImmToReg(e, asm.MustLookup("rdi"), apTrampolineBase)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), "_wrela_ap_trampoline_blob")
	emitRegRegMove(e, asm.MustLookup("rsi"), asm.MustLookup("rax"))
	emitMovImmToReg(e, asm.MustLookup("rcx"), int64(len(apTrampolineBlob())))
	emitCallReloc(e, "_wrela_method_platform_uefi_types_DelegatedMemory_install_ap_trampoline")
}

func emitEntryAdapter(e *Emitter, entry ir.EntryAdapter, ctx compileContext) {
	adapterLayout, ok := buildEntryAdapterLayout(ctx)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{
			Phase:   diagnosticPhase,
			Code:    diag.CG0001,
			Message: "entry adapter requires source-declared record layouts",
		})
		return
	}
	records, ok := entryAdapterRecords(ctx)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{
			Phase:   diagnosticPhase,
			Code:    diag.CG0001,
			Message: "entry adapter requires source-declared record layouts",
		})
		return
	}
	delegatedBytesRecord := records["DelegatedBytes"]
	uefiMemoryMapRecord := records["UefiMemoryMap"]
	delegatedMemoryRecord := records["DelegatedMemory"]
	systemBootServicesOffset, ok := systemTableBootServicesOffset(ctx)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{
			Phase:   diagnosticPhase,
			Code:    diag.CG0001,
			Message: "entry adapter requires source-declared UefiSystemTable.boot_services layout",
		})
		return
	}

	e.emit(0x55)
	e.emit(0x48, 0x89, 0xE5)
	e.emitInstruction(asm.Instruction{Mnemonic: "sub", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rsp")},
		asm.ImmOperand{Value: int64(adapterLayout.FrameSize)},
	}})

	// UefiHandle.
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.RegOperand{Reg: asm.MustLookup("rcx")},
	}})
	emitStoreSlotFromReg(e, asm.MustLookup("rax"), adapterLayout.UefiHandleOffset, 64)

	// UefiBootServices.
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.MemOperand{Base: asm.MustLookup("rdx"), Disp: int64(systemBootServicesOffset), Width: 64},
	}})
	emitStoreSlotFromReg(e, asm.MustLookup("rax"), adapterLayout.UefiBootServicesOffset, 64)

	for i := 0; i < delegatedMemoryRecord.Size; i += 8 {
		emitStoreImmediateSlot(e, 0, adapterLayout.DelegatedMemoryOffset+i)
	}
	emitStoreImmediateSlot(e, 0x200000, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["arena_base"].Offset)
	emitStoreImmediateSlot(e, 0x200000, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["arena_length"].Offset)
	emitStoreImmediateSlot(e, 0, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["next_offset"].Offset)

	for i := 0; i < delegatedBytesRecord.Size; i += 8 {
		emitStoreImmediateSlot(e, 0, adapterLayout.DelegatedBytesOffset+i)
	}
	for i := 0; i < uefiMemoryMapRecord.Size; i += 8 {
		emitStoreImmediateSlot(e, 0, adapterLayout.UefiMemoryMapOffset+i)
	}

	emitStoreSlotValue(e, adapterLayout.UefiBootServicesCallsOffset, adapterLayout.UefiBootServicesOffset)
	emitStoreSlotAddress(e, adapterLayout.UefiMemoryMapDescriptorsOffset, adapterLayout.DelegatedBytesOffset)
	emitStoreSlotAddress(e, adapterLayout.DelegatedMemoryMapOffset, adapterLayout.UefiMemoryMapOffset)
	emitStoreSlotAddress(e, adapterLayout.DelegatedHardwareImageOffset, adapterLayout.UefiHandleOffset)
	emitStoreSlotAddress(e, adapterLayout.DelegatedHardwareBootOffset, adapterLayout.UefiBootServicesCallsOffset)
	emitStoreSlotAddress(e, adapterLayout.DelegatedHardwareMemoryOffset, adapterLayout.DelegatedMemoryOffset)

	emitInstallAPTrampoline(e)

	emitSlotFromBase(e, asm.MustLookup("rdi"), asm.MustLookup("rbp"), adapterLayout.DelegatedHardwareOffset)
	emitSymbolCall(e, entry.DelegatedPhaseSymbol)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	emitSymbolCall(e, entry.OwnedPhaseSymbol)

	loop := e.newLabel("entry_halt")
	e.bindLabel(loop)
	e.emit(0xF4)
	e.emitJmp(loop)
}

func systemTableBootServicesOffset(ctx compileContext) (int, bool) {
	info, ok := ctx.types["UefiSystemTable"]
	if !ok {
		return 0, false
	}
	field, ok := info.Fields["boot_services"]
	if !ok {
		return 0, false
	}
	return field.Offset, true
}

func emitSymbolCall(e *Emitter, symbol string) {
	e.emit(0xE8, 0, 0, 0, 0)
	e.CallReloc = append(e.CallReloc, internalReloc{Offset: uint64(len(e.Code) - 4), Symbol: symbol})
}

package codegen

import (
	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/ir"
	"github.com/ryanwible/wrela3/compiler/layout"
)

type entryRecordField struct {
	Name string
	Type string
}

var entryRecordFields = map[string][]entryRecordField{
	"UefiHandle": {
		{Name: "address", Type: "U64"},
	},
	"UefiBootServices": {
		{Name: "table_address", Type: "VirtualAddress"},
	},
	"UefiBootServicesCalls": {
		{Name: "boot_services", Type: "UefiBootServices"},
	},
	"DelegatedBytes": {
		{Name: "address", Type: "VirtualAddress"},
		{Name: "length", Type: "U64"},
	},
	"UefiMemoryMap": {
		{Name: "descriptors", Type: "DelegatedBytes"},
		{Name: "descriptor_size", Type: "U64"},
		{Name: "key", Type: "U64"},
		{Name: "descriptor_version", Type: "U32"},
	},
	"DelegatedMemory": {
		{Name: "arena_base", Type: "VirtualAddress"},
		{Name: "arena_length", Type: "U64"},
		{Name: "next_offset", Type: "U64"},
		{Name: "last_memory_map", Type: "UefiMemoryMap"},
	},
	"DelegatedHardware": {
		{Name: "image_handle", Type: "UefiHandle"},
		{Name: "boot_services", Type: "UefiBootServicesCalls"},
		{Name: "delegated_memory", Type: "DelegatedMemory"},
	},
}

type entryFrameLayout struct {
	FrameSize                     int
	UefiHandleOffset              int
	UefiBootServicesOffset        int
	UefiBootServicesCallsOffset   int
	DelegatedMemoryOffset         int
	DelegatedHardwareOffset       int
	DelegatedMemoryMapOffset      int
	DelegatedHardwareImageOffset  int
	DelegatedHardwareBootOffset   int
	DelegatedHardwareMemoryOffset int
}

func buildEntryAdapterLayout() entryFrameLayout {
	records := map[string]layout.Record{}
	for name, fields := range entryRecordFields {
		records[name] = buildEntryRecordLayout(name, fields, records)
	}

	ordered := []string{"UefiHandle", "UefiBootServices", "UefiBootServicesCalls", "DelegatedMemory", "DelegatedHardware"}
	offsets := map[string]int{}
	cursor := 0
	for _, name := range ordered {
		record := records[name]
		cursor += record.Size
		cursor = layout.AlignUp(cursor, record.Align)
		offsets[name] = -cursor
	}

	frameSize := layout.AlignUp(cursor, 16)
	hw := records["DelegatedHardware"]
	mem := records["DelegatedMemory"]

	return entryFrameLayout{
		FrameSize:                     frameSize,
		UefiHandleOffset:              offsets["UefiHandle"],
		UefiBootServicesOffset:        offsets["UefiBootServices"],
		UefiBootServicesCallsOffset:   offsets["UefiBootServicesCalls"],
		DelegatedMemoryOffset:         offsets["DelegatedMemory"],
		DelegatedHardwareOffset:       offsets["DelegatedHardware"],
		DelegatedHardwareImageOffset:  offsets["DelegatedHardware"] + hw.Fields["image_handle"].Offset,
		DelegatedHardwareBootOffset:   offsets["DelegatedHardware"] + hw.Fields["boot_services"].Offset,
		DelegatedHardwareMemoryOffset: offsets["DelegatedHardware"] + hw.Fields["delegated_memory"].Offset,
		DelegatedMemoryMapOffset:      offsets["DelegatedMemory"] + mem.Fields["last_memory_map"].Offset,
	}
}

func buildEntryRecordLayout(name string, fields []entryRecordField, cache map[string]layout.Record) layout.Record {
	if layout, ok := cache[name]; ok && (layout.Size != 0 || layout.Align != 0 || len(layout.Fields) != 0) {
		return layout
	}
	if len(fields) == 0 {
		cache[name] = layout.Record{Fields: map[string]layout.FieldLayout{}}
		return cache[name]
	}

	record := layout.Record{Fields: map[string]layout.FieldLayout{}}
	offset := 0
	recordAlign := 1
	for _, field := range fields {
		size, align := entryTypeSizeAlign(field.Type, cache)
		offset = layout.AlignUp(offset, align)
		record.Fields[field.Name] = layout.FieldLayout{Offset: offset, Size: size, Align: align}
		offset += size
		if align > recordAlign {
			recordAlign = align
		}
	}
	record.Size = layout.AlignUp(offset, recordAlign)
	record.Align = recordAlign
	cache[name] = record
	return record
}

func entryTypeSizeAlign(name string, cache map[string]layout.Record) (int, int) {
	if record, ok := cache[name]; ok && (record.Size != 0 || record.Align != 0 || len(record.Fields) != 0) {
		return record.Size, record.Align
	}
	if fields, ok := entryRecordFields[name]; ok {
		record := buildEntryRecordLayout(name, fields, cache)
		return record.Size, record.Align
	}
	if s, a, err := layout.SizeAlign(name); err == nil {
		return s, a
	}
	return 8, 8
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

func emitCopyStackBytes(e *Emitter, dstSlot, srcSlot, size int) {
	for i := 0; i < size; i += 8 {
		e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
			asm.RegOperand{Reg: asm.MustLookup("rax")},
			asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(srcSlot + i), Width: 64},
		}})
		emitStoreSlotFromReg(e, asm.MustLookup("rax"), dstSlot+i, 64)
	}
}

func emitEntryAdapter(e *Emitter, entry ir.EntryAdapter) {
	adapterLayout := buildEntryAdapterLayout()
	records := map[string]layout.Record{}
	uefiHandleRecord := buildEntryRecordLayout("UefiHandle", entryRecordFields["UefiHandle"], records)
	bootCallsRecord := buildEntryRecordLayout("UefiBootServicesCalls", entryRecordFields["UefiBootServicesCalls"], records)
	delegatedMemoryRecord := buildEntryRecordLayout("DelegatedMemory", entryRecordFields["DelegatedMemory"], records)

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
		asm.MemOperand{Base: asm.MustLookup("rdx"), Disp: 96, Width: 64},
	}})
	emitStoreSlotFromReg(e, asm.MustLookup("rax"), adapterLayout.UefiBootServicesOffset, 64)

	// UefiBootServicesCalls.
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(adapterLayout.UefiBootServicesOffset), Width: 64},
	}})
	emitStoreSlotFromReg(e, asm.MustLookup("rax"), adapterLayout.UefiBootServicesCallsOffset, 64)

	for i := 0; i < delegatedMemoryRecord.Size; i += 8 {
		emitStoreImmediateSlot(e, 0, adapterLayout.DelegatedMemoryOffset+i)
	}
	emitStoreImmediateSlot(e, 0x200000, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["arena_base"].Offset)
	emitStoreImmediateSlot(e, 0x200000, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["arena_length"].Offset)
	emitStoreImmediateSlot(e, 0, adapterLayout.DelegatedMemoryOffset+delegatedMemoryRecord.Fields["next_offset"].Offset)

	emitCopyStackBytes(e, adapterLayout.DelegatedHardwareImageOffset, adapterLayout.UefiHandleOffset, uefiHandleRecord.Size)
	emitCopyStackBytes(e, adapterLayout.DelegatedHardwareBootOffset, adapterLayout.UefiBootServicesCallsOffset, bootCallsRecord.Size)
	emitCopyStackBytes(e, adapterLayout.DelegatedHardwareMemoryOffset, adapterLayout.DelegatedMemoryOffset, delegatedMemoryRecord.Size)

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

func emitSymbolCall(e *Emitter, symbol string) {
	e.emit(0xE8, 0, 0, 0, 0)
	e.CallReloc = append(e.CallReloc, internalReloc{Offset: uint64(len(e.Code) - 4), Symbol: symbol})
}

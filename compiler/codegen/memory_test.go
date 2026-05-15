package codegen

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestArenaReserveEmitsBoundsTrapAndBump(t *testing.T) {
	program := testProgramWithArenaReserve(t)
	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("compile diagnostics: %#v", diags)
	}
	code := symbolBytes(t, image, "_wrela_method_test_Worker_run")
	if _, ok := image.Symbols["_wrela_memory_oom"]; !ok {
		t.Fatalf("missing memory trap symbol")
	}
	if !codeCallsSymbol(t, image, "_wrela_method_test_Worker_run", "_wrela_memory_oom") {
		t.Fatalf("reserve/frame code must call _wrela_memory_oom on bounds failure: %x", code)
	}
	for name, want := range map[string][]byte{
		"frame length 64":   {0x40, 0x00, 0x00, 0x00},
		"reserve length 32": {0x20, 0x00, 0x00, 0x00},
		"reserve align 8":   {0x08, 0x00, 0x00, 0x00},
	} {
		if !containsBytes(code, want) {
			t.Fatalf("reserve code missing %s constant %x in %x", name, want, code)
		}
	}
}

func TestArenaReserveEmitsOverflowTraps(t *testing.T) {
	program := testProgramWithArenaReserve(t)
	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("compile diagnostics: %#v", diags)
	}
	code := symbolBytes(t, image, "_wrela_method_test_Worker_run")
	if got := countBytes(code, []byte{0x0F, 0x81}); got < 3 {
		t.Fatalf("reserve/frame code must skip OOM only when arithmetic does not overflow, got %d jno branches in %x", got, code)
	}
	if got := countBytes(code, []byte{0x0F, 0x80}); got != 0 {
		t.Fatalf("reserve/frame overflow guard must not skip OOM on overflow, got %d jo branches in %x", got, code)
	}
}

func TestArenaPlaceWritesConstructedFields(t *testing.T) {
	program := testProgramWithArenaPlace(t)
	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("compile diagnostics: %#v", diags)
	}
	code := symbolBytes(t, image, "_wrela_method_test_Worker_run")
	if !containsBytes(code, []byte{0x39, 0x30, 0x00, 0x00}) {
		t.Fatalf("place must store Message.id immediate 12345 into arena storage: %x", code)
	}
	if !containsBytes(code, []byte{0x10, 0x00, 0x00, 0x00}) {
		t.Fatalf("place must reserve Message storage size 16: %x", code)
	}
}

func TestArenaPlaceStoresNestedDataFieldHandle(t *testing.T) {
	program := testProgramWithArenaPlaceNestedBytes(t)
	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("compile diagnostics: %#v", diags)
	}
	code := symbolBytes(t, image, "_wrela_method_test_Worker_run")
	want := []byte{0x48, 0x8B, 0xC6, 0x48, 0x83, 0xC0, 0x08, 0x48, 0x89, 0x06}
	if !bytes.Contains(code, want) {
		t.Fatalf("place must store nested Bytes handle into arena record field: want %x in %x", want, code)
	}
}

func TestArenaPlaceCopiesNestedDataFieldWithEightByteStorage(t *testing.T) {
	program := testProgramWithArenaPlaceNestedEightByteData(t)
	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("compile diagnostics: %#v", diags)
	}
	code := symbolBytes(t, image, "_wrela_method_test_Worker_run")
	wantHandle := []byte{0x48, 0x8B, 0xC6, 0x48, 0x83, 0xC0, 0x08, 0x48, 0x89, 0x06}
	if !bytes.Contains(code, wantHandle) {
		t.Fatalf("place must store nested Message handle into arena record field: want %x in %x", wantHandle, code)
	}
	wantStorageCopy := []byte{0x49, 0x8B, 0x03, 0x48, 0x89, 0x46, 0x08}
	if !bytes.Contains(code, wantStorageCopy) {
		t.Fatalf("place must copy nested Message storage, not its handle: want %x in %x", wantStorageCopy, code)
	}
}

func codeCallsSymbol(t *testing.T, image *Image, caller, target string) bool {
	t.Helper()
	callerRVA := image.Symbols[caller]
	targetRVA := image.Symbols[target]
	code := symbolBytes(t, image, caller)
	for i := 0; i+5 <= len(code); i++ {
		if code[i] != 0xE8 {
			continue
		}
		rel := int32(binary.LittleEndian.Uint32(code[i+1 : i+5]))
		got := uint64(int64(callerRVA) + int64(i) + 5 + int64(rel))
		if got == targetRVA {
			return true
		}
	}
	return false
}

func countBytes(haystack, needle []byte) int {
	count := 0
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			count++
		}
	}
	return count
}

func testProgramWithArenaReserve(t *testing.T) *ir.Program {
	t.Helper()
	memoryType := ir.Type{Name: "ExecutorMemory", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindClass}
	frameType := ir.Type{Name: "ArenaFrame", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindClass}
	mutableBytes := ir.Type{Name: "MutableBytes", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindData}
	memory := &ir.Local{Symbol: "memory", Type: memoryType}
	frameLen := &ir.ConstInt{Symbol: "frame_len", Value: 64, Type: ir.Type{Name: "U64"}}
	reserveLen := &ir.ConstInt{Symbol: "reserve_len", Value: 32, Type: ir.Type{Name: "U64"}}
	reserveAlign := &ir.ConstInt{Symbol: "reserve_align", Value: 8, Type: ir.Type{Name: "U64"}}
	frame := &ir.FrameBegin{Symbol: "tick", Parent: memory, Length: frameLen, Type: frameType}
	reserve := &ir.ArenaReserve{Arena: frame, Length: reserveLen, Align: reserveAlign, Type: mutableBytes}
	return &ir.Program{
		Types: arenaTestTypes(),
		Functions: []ir.Function{{
			Symbol: "_wrela_method_test_Worker_run",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				memory,
				frameLen,
				reserveLen,
				reserveAlign,
				frame,
				reserve,
				&ir.FrameEnd{Frame: frame},
				&ir.Return{},
			}}},
		}},
	}
}

func testProgramWithArenaPlace(t *testing.T) *ir.Program {
	t.Helper()
	program := testProgramWithArenaReserve(t)
	messageType := ir.Type{Name: "Message", Module: "test", Kind: ir.TypeKindData}
	program.Types["test.Message"] = ir.TypeInfo{
		Name: "Message", Module: "test", Kind: ir.TypeKindData, Size: 16, Align: 8, StorageSize: 16,
		Fields: map[string]ir.FieldInfo{
			"id": {Name: "id", Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8, Type: ir.Type{Name: "U64"}},
		},
		FieldOrder: []string{"id"},
	}
	id := &ir.ConstInt{Symbol: "message_id", Value: 12345, Type: ir.Type{Name: "U64"}}
	frame := program.Functions[0].Blocks[0].Ops[4].(*ir.FrameBegin)
	place := &ir.ArenaPlace{
		Arena: frame,
		Type:  messageType,
		Fields: []ir.FieldValue{{
			Name:  "id",
			Value: id,
		}},
	}
	ops := []ir.Operation{
		program.Functions[0].Blocks[0].Ops[0],
		program.Functions[0].Blocks[0].Ops[1],
		id,
		frame,
		place,
		&ir.FrameEnd{Frame: frame},
		&ir.Return{},
	}
	program.Functions[0].Blocks[0].Ops = ops
	return program
}

func testProgramWithArenaPlaceNestedBytes(t *testing.T) *ir.Program {
	t.Helper()
	program := testProgramWithArenaReserve(t)
	bytesType := ir.Type{Name: "Bytes", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindData}
	outputType := ir.Type{Name: "OutputLine", Module: "test", Kind: ir.TypeKindData}
	program.Types["machine.x86_64.executor_memory.Bytes"] = ir.TypeInfo{
		Name: "Bytes", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindData, Size: 16, Align: 8, StorageSize: 16,
		Fields: map[string]ir.FieldInfo{
			"address": {Name: "address", Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8, Type: ir.Type{Name: "PhysicalAddress"}},
			"length":  {Name: "length", Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8, Type: ir.Type{Name: "U64"}},
		},
		FieldOrder: []string{"address", "length"},
	}
	program.Types["test.OutputLine"] = ir.TypeInfo{
		Name: "OutputLine", Module: "test", Kind: ir.TypeKindData, Size: 8, Align: 8, StorageSize: 24,
		Fields: map[string]ir.FieldInfo{
			"bytes": {
				Name:          "bytes",
				Offset:        0,
				Size:          8,
				Align:         8,
				StorageOffset: 8,
				StorageSize:   16,
				Type:          bytesType,
			},
		},
		FieldOrder: []string{"bytes"},
	}
	frame := program.Functions[0].Blocks[0].Ops[4].(*ir.FrameBegin)
	reserve := program.Functions[0].Blocks[0].Ops[5].(*ir.ArenaReserve)
	place := &ir.ArenaPlace{
		Arena: frame,
		Type:  outputType,
		Fields: []ir.FieldValue{{
			Name:  "bytes",
			Value: reserve,
		}},
	}
	ops := []ir.Operation{
		program.Functions[0].Blocks[0].Ops[0],
		program.Functions[0].Blocks[0].Ops[1],
		program.Functions[0].Blocks[0].Ops[2],
		program.Functions[0].Blocks[0].Ops[3],
		frame,
		reserve,
		place,
		&ir.FrameEnd{Frame: frame},
		&ir.Return{},
	}
	program.Functions[0].Blocks[0].Ops = ops
	return program
}

func testProgramWithArenaPlaceNestedEightByteData(t *testing.T) *ir.Program {
	t.Helper()
	program := testProgramWithArenaReserve(t)
	messageType := ir.Type{Name: "Message", Module: "test", Kind: ir.TypeKindData}
	boxType := ir.Type{Name: "Box", Module: "test", Kind: ir.TypeKindData}
	messageInfo := ir.TypeInfo{
		Name: "Message", Module: "test", Kind: ir.TypeKindData, Size: 8, Align: 8, StorageSize: 8,
		Fields: map[string]ir.FieldInfo{
			"id": {Name: "id", Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8, Type: ir.Type{Name: "U64"}},
		},
		FieldOrder: []string{"id"},
	}
	program.Types["Message"] = messageInfo
	program.Types["test.Message"] = messageInfo
	boxInfo := ir.TypeInfo{
		Name: "Box", Module: "test", Kind: ir.TypeKindData, Size: 8, Align: 8, StorageSize: 16,
		Fields: map[string]ir.FieldInfo{
			"message": {
				Name:          "message",
				Offset:        0,
				Size:          8,
				Align:         8,
				StorageOffset: 8,
				StorageSize:   8,
				Type:          messageType,
			},
		},
		FieldOrder: []string{"message"},
	}
	program.Types["Box"] = boxInfo
	program.Types["test.Box"] = boxInfo
	id := &ir.ConstInt{Symbol: "message_id", Value: 12345, Type: ir.Type{Name: "U64"}}
	message := &ir.Construct{
		Symbol: "message",
		Type:   messageType,
		Fields: []ir.FieldValue{{
			Name:  "id",
			Value: id,
		}},
	}
	frame := program.Functions[0].Blocks[0].Ops[4].(*ir.FrameBegin)
	place := &ir.ArenaPlace{
		Arena: frame,
		Type:  boxType,
		Fields: []ir.FieldValue{{
			Name:  "message",
			Value: message,
		}},
	}
	ops := []ir.Operation{
		program.Functions[0].Blocks[0].Ops[0],
		program.Functions[0].Blocks[0].Ops[1],
		id,
		message,
		frame,
		place,
		&ir.FrameEnd{Frame: frame},
		&ir.Return{},
	}
	program.Functions[0].Blocks[0].Ops = ops
	return program
}

func arenaTestTypes() map[string]ir.TypeInfo {
	return map[string]ir.TypeInfo{
		"machine.x86_64.executor_memory.ExecutorMemory": {
			Name: "ExecutorMemory", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindClass, Size: 24, Align: 8, StorageSize: 24,
			Fields: map[string]ir.FieldInfo{
				"arena_base":   {Name: "arena_base", Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8, Type: ir.Type{Name: "PhysicalAddress"}},
				"arena_length": {Name: "arena_length", Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8, Type: ir.Type{Name: "U64"}},
				"next_offset":  {Name: "next_offset", Offset: 16, Size: 8, Align: 8, StorageOffset: 16, StorageSize: 8, Type: ir.Type{Name: "U64"}},
			},
			FieldOrder: []string{"arena_base", "arena_length", "next_offset"},
		},
		"machine.x86_64.executor_memory.ArenaFrame": {
			Name: "ArenaFrame", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindClass, Size: 24, Align: 8, StorageSize: 24,
			Fields: map[string]ir.FieldInfo{
				"arena_base":   {Name: "arena_base", Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8, Type: ir.Type{Name: "PhysicalAddress"}},
				"arena_length": {Name: "arena_length", Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8, Type: ir.Type{Name: "U64"}},
				"next_offset":  {Name: "next_offset", Offset: 16, Size: 8, Align: 8, StorageOffset: 16, StorageSize: 8, Type: ir.Type{Name: "U64"}},
			},
			FieldOrder: []string{"arena_base", "arena_length", "next_offset"},
		},
		"machine.x86_64.executor_memory.MutableBytes": {
			Name: "MutableBytes", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindData, Size: 16, Align: 8, StorageSize: 16,
			Fields: map[string]ir.FieldInfo{
				"address": {Name: "address", Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8, Type: ir.Type{Name: "PhysicalAddress"}},
				"length":  {Name: "length", Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8, Type: ir.Type{Name: "U64"}},
			},
			FieldOrder: []string{"address", "length"},
		},
	}
}

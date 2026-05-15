package codegen

import (
	"encoding/binary"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func interruptProgramForCodegenTest(t *testing.T) *ir.Program {
	t.Helper()
	u8 := ir.Type{Name: "U8", Module: "builtin", Kind: ir.TypeKindPrimitive}
	eventType := ir.Type{Name: "SerialPathInterrupt", Module: "machine.x86_64.serial", Kind: ir.TypeKindData}
	executorType := ir.Type{Name: "HelloWorld", Module: "examples.hello.program", Kind: ir.TypeKindExecutor}

	eventByte := &ir.ConstInt{Symbol: "event_byte", Value: 0, Type: u8}
	eventRet := &ir.Construct{
		Symbol: "event_value",
		Type:   eventType,
		Fields: []ir.FieldValue{{Name: "byte", Value: eventByte}},
	}
	eventFn := ir.Function{
		Symbol: "_wrela_event_fn_machine_x86_64_serial_SerialConsolePath_interrupt",
		Return: eventType,
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			eventByte,
			eventRet,
			&ir.Return{Value: eventRet},
		}}},
	}
	handlerFn := ir.Function{
		Symbol: "_wrela_on_fn_examples_hello_program_HelloWorld_serial_path_interrupt",
		Return: ir.Type{Name: "void", Module: "builtin", Kind: ir.TypeKindPrimitive},
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{&ir.Return{}}}},
	}

	return &ir.Program{
		Functions: []ir.Function{eventFn, handlerFn},
		Types: map[string]ir.TypeInfo{
			"SerialPathInterrupt": {
				Name:        "SerialPathInterrupt",
				Module:      "machine.x86_64.serial",
				Kind:        ir.TypeKindData,
				Size:        8,
				Align:       8,
				StorageSize: 8,
				Fields: map[string]ir.FieldInfo{
					"byte": {
						Name:          "byte",
						Type:          u8,
						Offset:        0,
						StorageOffset: 0,
						Size:          1,
						StorageSize:   1,
						Align:         1,
					},
				},
				FieldOrder: []string{"byte"},
			},
			"HelloWorld": {
				Name:        "HelloWorld",
				Module:      "examples.hello.program",
				Kind:        ir.TypeKindExecutor,
				Size:        32,
				Align:       8,
				StorageSize: 32,
				Fields: map[string]ir.FieldInfo{
					"serial_path": {
						Name:          "serial_path",
						Type:          ir.Type{Name: "SerialConsolePath", Module: "machine.x86_64.serial", Kind: ir.TypeKindDriverPath},
						Offset:        16,
						StorageOffset: -1,
						Size:          16,
						StorageSize:   0,
						Align:         8,
					},
				},
				FieldOrder: []string{"serial_path"},
			},
		},
		InterruptBindings: []ir.InterruptBinding{
			{
				EventSymbol:           "interrupt_event::machine.x86_64.serial::SerialConsolePath::interrupt",
				HandlerSymbol:         "on_handler::examples.hello.program::HelloWorld::serial_path::interrupt",
				EventFunctionSymbol:   eventFn.Symbol,
				HandlerFunctionSymbol: handlerFn.Symbol,
				ExecutorType:          executorType,
				PathField:             "serial_path",
				PathFieldOffset:       16,
				ContextSymbol:         "_wrela_interrupt_context_0",
				EventStorageSymbol:    "_wrela_interrupt_event_40",
				EventStorageSize:      8,
				Vector:                0x40,
			},
			{
				EventSymbol:           "interrupt_event::machine.x86_64.edu::EduMsiPath::interrupt",
				HandlerSymbol:         "on_handler::examples.hello.program::HelloWorld::edu_path::interrupt",
				EventFunctionSymbol:   eventFn.Symbol,
				HandlerFunctionSymbol: handlerFn.Symbol,
				ExecutorType:          executorType,
				PathField:             "edu_path",
				PathFieldOffset:       16,
				ContextSymbol:         "_wrela_interrupt_context_0",
				EventStorageSymbol:    "_wrela_interrupt_event_41",
				EventStorageSize:      8,
				Vector:                0x41,
			},
			{
				EventSymbol:           "interrupt_event::machine.x86_64.ivshmem::IvshmemMsixPath::interrupt",
				HandlerSymbol:         "on_handler::examples.hello.program::HelloWorld::ivshmem_rx::interrupt",
				EventFunctionSymbol:   eventFn.Symbol,
				HandlerFunctionSymbol: handlerFn.Symbol,
				ExecutorType:          executorType,
				PathField:             "ivshmem_rx",
				PathFieldOffset:       16,
				ContextSymbol:         "_wrela_interrupt_context_0",
				EventStorageSymbol:    "_wrela_interrupt_event_42",
				EventStorageSize:      8,
				Vector:                0x42,
			},
		},
		InterruptContexts: []ir.InterruptContext{{
			Symbol:       "_wrela_interrupt_context_0",
			ExecutorType: executorType,
			Size:         32,
			PathFields: []ir.InterruptContextPathField{{
				FieldName: "serial_path",
				Offset:    16,
				Type:      ir.Type{Name: "SerialConsolePath", Module: "machine.x86_64.serial", Kind: ir.TypeKindDriverPath},
			}},
		}},
	}
}

func TestAsmMethodExternalBranchRelocation(t *testing.T) {
	method := ir.AsmMethod{
		Symbol: "_wrela_method_platform_uefi_transition_DelegatedHardware_capture_vector40_serial_handler",
		Body:   "call _wrela_interrupt_vector40_serial\njmp _wrela_interrupt_vector41_edu_msi\nret",
	}

	unit, diags := compileAsmMethodUnit(method)
	if len(diags) != 0 {
		t.Fatalf("compileAsmMethodUnit() diagnostics = %#v", diags)
	}
	if len(unit.CallReloc) != 2 {
		t.Fatalf("compileAsmMethodUnit() call relocs = %#v, want 2", unit.CallReloc)
	}

	wantRelocs := []internalReloc{
		{Offset: 1, Symbol: "_wrela_interrupt_vector40_serial"},
		{Offset: 6, Symbol: "_wrela_interrupt_vector41_edu_msi"},
	}
	for i, want := range wantRelocs {
		if unit.CallReloc[i] != want {
			t.Fatalf("compileAsmMethodUnit() call reloc %d = %#v, want %#v", i, unit.CallReloc[i], want)
		}
	}

	if !containsBytes(unit.Bytes, []byte{0xE8, 0, 0, 0, 0}) {
		t.Fatalf("external call must encode as zero rel32 before relocation: %#x", unit.Bytes)
	}
	if !containsBytes(unit.Bytes, []byte{0xE9, 0, 0, 0, 0}) {
		t.Fatalf("external jmp must encode as zero rel32 before relocation: %#x", unit.Bytes)
	}
}

func TestCompileGeneratesInterruptDispatchStubs(t *testing.T) {
	program := interruptProgramForCodegenTest(t)
	img, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	for _, symbol := range []string{
		"_wrela_interrupt_vector40_serial",
		"_wrela_interrupt_vector41_edu_msi",
		"_wrela_interrupt_vector42_ivshmem_msix",
	} {
		if _, ok := img.Symbols[symbol]; !ok {
			t.Fatalf("missing %s symbol", symbol)
		}
		code := symbolBytes(t, img, symbol)
		if !containsBytes(code, []byte{0x48, 0xCF}) {
			t.Fatalf("%s missing iretq", symbol)
		}
	}
}

func TestInterruptContextSymbolStoresActiveExecutor(t *testing.T) {
	program := interruptProgramForCodegenTest(t)
	img, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	if _, ok := img.Symbols["_wrela_interrupt_context_0"]; !ok {
		t.Fatalf("missing interrupt context symbol")
	}
	data := sectionByName(img, ".data")
	if data == nil || data.Characteristics&0x80000000 == 0 || data.Characteristics&0x40000000 == 0 {
		t.Fatalf("interrupt context must live in writable .data section: %#v", data)
	}

	img2, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("second Compile() diagnostics = %#v", diags)
	}
	data2 := sectionByName(img2, ".data")
	if len(program.WritableData) != 0 {
		t.Fatalf("Compile() must not mutate Program.WritableData: %#v", program.WritableData)
	}
	if data2 == nil || len(data2.Data) != len(data.Data) {
		t.Fatalf("Compile() must be idempotent for interrupt runtime data: first %d second %d", len(data.Data), len(data2.Data))
	}
}

func TestInterruptDispatchUsesContextRelocation(t *testing.T) {
	img, diags := Compile(interruptProgramForCodegenTest(t))
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	found := map[string]bool{}
	for _, rel := range img.Relocs {
		if rel.Symbol != "_wrela_interrupt_vector40_serial" {
			continue
		}
		locationRVA := img.Symbols[rel.Symbol] + rel.Offset
		start := int(locationRVA - img.Sections[0].RVA)
		if start < 0 || start+8 > len(img.Sections[0].Data) {
			t.Fatalf("relocation outside .text: %#v", rel)
		}
		got := binary.LittleEndian.Uint64(img.Sections[0].Data[start : start+8])
		for _, target := range []string{"_wrela_interrupt_context_0", "_wrela_interrupt_event_40"} {
			if got == uint64(runtimeImageBase+img.Symbols[target]) {
				found[target] = true
			}
		}
	}
	if !found["_wrela_interrupt_context_0"] || !found["_wrela_interrupt_event_40"] {
		t.Fatalf("missing context/event relocation: found %#v relocs %#v", found, img.Relocs)
	}
}

func sectionByName(img *Image, name string) *Section {
	for i := range img.Sections {
		if img.Sections[i].Name == name {
			return &img.Sections[i]
		}
	}
	return nil
}

package codegen

import (
	"encoding/binary"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func interruptProgramForCodegenTest(t *testing.T) *ir.Program {
	t.Helper()
	boolType := ir.Type{Name: "Bool", Module: "builtin", Kind: ir.TypeKindPrimitive}
	u8 := ir.Type{Name: "U8", Module: "builtin", Kind: ir.TypeKindPrimitive}
	eventType := ir.Type{Name: "SerialPathInterrupt", Module: "machine.x86_64.serial", Kind: ir.TypeKindData}
	executorType := ir.Type{Name: "HelloWorld", Module: "examples.hello.program", Kind: ir.TypeKindExecutor}

	eventHasByte := &ir.ConstInt{Symbol: "event_has_byte", Value: 1, Type: boolType}
	eventByte := &ir.ConstInt{Symbol: "event_byte", Value: 0, Type: u8}
	eventRet := &ir.Construct{
		Symbol: "event_value",
		Type:   eventType,
		Fields: []ir.FieldValue{{Name: "has_byte", Value: eventHasByte}, {Name: "byte", Value: eventByte}},
	}
	eventFn := ir.Function{
		Symbol: "_wrela_event_fn_machine_x86_64_serial_SerialConsolePath_interrupt",
		Return: eventType,
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			eventHasByte,
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
					"has_byte": {
						Name:          "has_byte",
						Type:          boolType,
						Offset:        0,
						StorageOffset: 0,
						Size:          1,
						StorageSize:   1,
						Align:         1,
					},
					"byte": {
						Name:          "byte",
						Type:          u8,
						Offset:        1,
						StorageOffset: 1,
						Size:          1,
						StorageSize:   1,
						Align:         1,
					},
				},
				FieldOrder: []string{"has_byte", "byte"},
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

func interruptTopicProgramForCodegenTest(t *testing.T) *ir.Program {
	t.Helper()
	program := interruptProgramForCodegenTest(t)
	program.Topics = []ir.TopicLayout{{
		Label:       "console.com1.rx",
		Kind:        "serial_rx",
		Depth:       64,
		Subscribers: []string{"console"},
	}}
	program.VcpuStarts = []ir.VcpuStartPlan{{
		VcpuID:    2,
		SlotLabel: "console",
		ExecutorType: ir.Type{
			Name:   "HelloWorld",
			Module: "examples.hello.program",
			Kind:   ir.TypeKindExecutor,
		},
	}}
	program.InterruptBindings = program.InterruptBindings[:1]
	program.InterruptBindings[0].HandlerSymbol = ""
	program.InterruptBindings[0].HandlerFunctionSymbol = ""
	program.InterruptBindings[0].TopicLabel = "console.com1.rx"
	program.InterruptBindings[0].TopicKind = "serial_rx"
	program.InterruptBindings[0].PublisherOwnerKind = "driver_path"
	program.InterruptBindings[0].PublisherOwnerLabel = "console.com1"
	program.InterruptBindings[0].SubscriberSlots = []string{"console"}
	return program
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
	program := interruptTopicProgramForCodegenTest(t)
	img, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	for _, symbol := range []string{
		"_wrela_interrupt_vector40_serial",
		"_wrela_interrupt_vector41_edu_msi",
		"_wrela_interrupt_vector42_ivshmem_msix",
		"_wrela_interrupt_vectorf0_wake",
	} {
		if _, ok := img.Symbols[symbol]; !ok {
			t.Fatalf("missing %s symbol", symbol)
		}
	}
	for _, symbol := range []string{
		"_wrela_interrupt_vector40_serial",
		"_wrela_interrupt_vectorf0_wake",
	} {
		code := symbolBytes(t, img, symbol)
		if !containsBytes(code, []byte{0x48, 0xCF}) {
			t.Fatalf("%s missing iretq", symbol)
		}
	}
}

func TestInterruptDispatchRejectsHandlerOnlyBinding(t *testing.T) {
	_, diags := Compile(interruptProgramForCodegenTest(t))

	if !hasCode(diags, diag.CG0001) {
		t.Fatalf("Compile() diagnostics = %#v, want CG0001 for handler-only interrupt binding", diags)
	}
}

func TestInterruptDispatchRejectsDuplicateVectorBindings(t *testing.T) {
	program := interruptTopicProgramForCodegenTest(t)
	duplicate := program.InterruptBindings[0]
	program.InterruptBindings = append(program.InterruptBindings, duplicate)

	_, diags := Compile(program)
	if !hasDiagnostic(diags, diag.CG0001, "duplicate interrupt binding vector 0x40") {
		t.Fatalf("Compile() diagnostics = %#v, want CG0001 duplicate interrupt vector binding", diags)
	}
}

func hasDiagnostic(ds []diag.Diagnostic, code, message string) bool {
	for _, d := range ds {
		if d.Code == code && d.Message == message {
			return true
		}
	}
	return false
}

func TestCompileGeneratesTrapForUnboundKnownInterruptVector(t *testing.T) {
	program := interruptTopicProgramForCodegenTest(t)
	img, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	bound := symbolBytes(t, img, "_wrela_interrupt_vector40_serial")
	if !containsBytes(bound, []byte{0x48, 0xCF}) {
		t.Fatalf("bound vector missing iretq: %#x", bound)
	}
	for _, symbol := range []string{
		"_wrela_interrupt_vector41_edu_msi",
		"_wrela_interrupt_vector42_ivshmem_msix",
	} {
		code := symbolBytes(t, img, symbol)
		if !containsBytes(code, []byte{0xFA, 0xF4, 0xEB, 0xFD}) {
			t.Fatalf("%s missing fatal fallback trap: %#x", symbol, code)
		}
	}
	wake := symbolBytes(t, img, "_wrela_interrupt_vectorf0_wake")
	if !containsBytes(wake, []byte{0x48, 0xCF}) || containsBytes(wake, []byte{0xFA, 0xF4, 0xEB, 0xFD}) {
		t.Fatalf("wake vector must EOI and return instead of trapping: %#x", wake)
	}
	if got := len(img.InterruptBindings); got != 1 {
		t.Fatalf("image interrupt bindings = %d, want 1", got)
	}
}

func TestInterruptContextSymbolStoresActiveExecutor(t *testing.T) {
	program := interruptTopicProgramForCodegenTest(t)
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
	img, diags := Compile(interruptTopicProgramForCodegenTest(t))
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	found := map[string]bool{}
	for _, rel := range img.Relocs {
		if rel.Symbol != "_wrela_interrupt_vector40_serial" {
			continue
		}
		locationRVA := img.Symbols[rel.Symbol] + rel.Offset
		text := sectionByName(img, ".text")
		start := int(locationRVA - text.RVA)
		if start < 0 || start+8 > len(text.Data) {
			t.Fatalf("relocation outside .text: %#v", rel)
		}
		got := binary.LittleEndian.Uint64(text.Data[start : start+8])
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

func TestInterruptTopicDispatchPublishesWithoutHandlerCall(t *testing.T) {
	program := interruptTopicProgramForCodegenTest(t)
	img, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	targets := callTargetsForSymbol(t, img, "_wrela_interrupt_vector40_serial")
	if !targets[program.InterruptBindings[0].EventFunctionSymbol] {
		t.Fatalf("interrupt topic dispatch did not call event function: targets %#v", targets)
	}
	handler := "_wrela_on_fn_examples_hello_program_HelloWorld_serial_path_interrupt"
	if targets[handler] {
		t.Fatalf("interrupt topic dispatch must not call handler function: targets %#v", targets)
	}

	code := symbolBytes(t, img, "_wrela_interrupt_vector40_serial")
	topicAddress := make([]byte, 8)
	binary.LittleEndian.PutUint64(topicAddress, runtimeImageBase+img.Symbols["_wrela_topic_console_com1_rx"])
	if !containsBytes(code, topicAddress) {
		t.Fatalf("interrupt topic dispatch missing topic data address: %#x", code)
	}
}

func TestInterruptTopicDispatchWakesNonBspSubscriber(t *testing.T) {
	img, diags := Compile(interruptTopicProgramForCodegenTest(t))
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := symbolBytes(t, img, "_wrela_interrupt_vector40_serial")

	destination := make([]byte, 4)
	binary.LittleEndian.PutUint32(destination, 2<<24)
	vector := make([]byte, 4)
	binary.LittleEndian.PutUint32(vector, 0x00004000|0xF0)
	if !containsBytes(code, destination) || !containsBytes(code, vector) {
		t.Fatalf("interrupt topic dispatch missing LAPIC wake for vCPU 2/vector F0: %#x", code)
	}
}

func TestInterruptTopicDispatchSkipsBspSelfWake(t *testing.T) {
	program := interruptTopicProgramForCodegenTest(t)
	program.VcpuStarts[0].VcpuID = 0
	img, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := symbolBytes(t, img, "_wrela_interrupt_vector40_serial")

	vector := make([]byte, 4)
	binary.LittleEndian.PutUint32(vector, 0x00004000|0xF0)
	if containsBytes(code, vector) {
		t.Fatalf("interrupt topic dispatch must not self-IPI BSP subscriber with unbound wake vector: %#x", code)
	}
}

func TestInterruptTopicDispatchResolvesConditionalJumps(t *testing.T) {
	img, diags := Compile(interruptTopicProgramForCodegenTest(t))
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := symbolBytes(t, img, "_wrela_interrupt_vector40_serial")
	if containsBytes(code, []byte{0x0f, 0x84, 0, 0, 0, 0}) {
		t.Fatalf("interrupt topic dispatch contains unresolved JE fixup: %#x", code)
	}
}

func TestSerialRxInterruptTopicDispatchChecksHasByteField(t *testing.T) {
	program := interruptTopicProgramForCodegenTest(t)
	info := program.Types["SerialPathInterrupt"]
	hasByte := info.Fields["has_byte"]
	hasByte.Offset = 1
	hasByte.StorageOffset = 1
	info.Fields["has_byte"] = hasByte
	eventByte := info.Fields["byte"]
	eventByte.Offset = 0
	eventByte.StorageOffset = 0
	info.Fields["byte"] = eventByte
	info.FieldOrder = []string{"byte", "has_byte"}
	program.Types["SerialPathInterrupt"] = info

	img, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := symbolBytes(t, img, "_wrela_interrupt_vector40_serial")
	if !containsBytes(code, []byte{0x8a, 0x56, 0x01}) {
		t.Fatalf("serial rx dispatch must load has_byte at payload offset 1 before publishing: %#x", code)
	}
	if containsBytes(code, []byte{0x8a, 0x16}) {
		t.Fatalf("serial rx dispatch must not guard against zero byte payloads at offset 0: %#x", code)
	}
}

func TestInterruptRuntimeDataSynthesizesTopicContextObject(t *testing.T) {
	program := interruptTopicProgramForCodegenTest(t)
	program.InterruptContexts = nil
	img, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	if _, ok := img.Symbols["_wrela_interrupt_context_0"]; !ok {
		t.Fatalf("topic interrupt context data object was not synthesized")
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

func callTargetsForSymbol(t *testing.T, img *Image, symbol string) map[string]bool {
	t.Helper()
	text := sectionByName(img, ".text")
	if text == nil {
		t.Fatal("missing .text section")
	}
	startRVA, ok := img.Symbols[symbol]
	if !ok {
		t.Fatalf("missing symbol %s", symbol)
	}
	start := int(startRVA - text.RVA)
	end := len(text.Data)
	for _, rva := range img.Symbols {
		offset := int(rva - text.RVA)
		if offset > start && offset < end {
			end = offset
		}
	}
	code := text.Data[start:end]
	byRVA := map[uint64]string{}
	for name, rva := range img.Symbols {
		byRVA[rva] = name
	}
	out := map[string]bool{}
	for i := 0; i+5 <= len(code); i++ {
		if code[i] != 0xE8 {
			continue
		}
		rel := int32(binary.LittleEndian.Uint32(code[i+1 : i+5]))
		target := uint64(int64(startRVA) + int64(i+5) + int64(rel))
		if name := byRVA[target]; name != "" {
			out[name] = true
		}
	}
	return out
}

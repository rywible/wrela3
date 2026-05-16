package codegen

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestVcpuStartupDataSymbolsAreDeterministic(t *testing.T) {
	program := &ir.Program{
		VcpuStarts: []ir.VcpuStartPlan{
			{VcpuID: 1, SlotLabel: "worker", Terminal: false},
			{VcpuID: 0, SlotLabel: "hello", Terminal: true},
		},
	}

	objects := vcpuStartupData(program)

	wantSymbols := []string{
		"_wrela_ap_trampoline_blob",
		"_wrela_vcpu0_stack",
		"_wrela_vcpu0_ready",
		"_wrela_vcpu0_entry",
		"_wrela_vcpu0_stack_top",
		"_wrela_vcpu0_context",
		"_wrela_vcpu0_apic_id_command",
		"_wrela_vcpu1_stack",
		"_wrela_vcpu1_ready",
		"_wrela_vcpu1_entry",
		"_wrela_vcpu1_stack_top",
		"_wrela_vcpu1_context",
		"_wrela_vcpu1_apic_id_command",
	}
	if len(objects) != len(wantSymbols) {
		t.Fatalf("len(vcpuStartupData) = %d, want %d", len(objects), len(wantSymbols))
	}
	for i, obj := range objects {
		if obj.Symbol != wantSymbols[i] {
			t.Fatalf("object %d symbol = %q, want %q", i, obj.Symbol, wantSymbols[i])
		}
		if obj.Symbol == "_wrela_ap_trampoline_blob" {
			if obj.Align != 4096 {
				t.Fatalf("object %s Align = %d, want 4096", obj.Symbol, obj.Align)
			}
			continue
		}
		if obj.Symbol == "_wrela_vcpu0_stack" || obj.Symbol == "_wrela_vcpu1_stack" {
			if len(obj.Bytes) != apTrampolineVcpuStackSize {
				t.Fatalf("object %s size = %d, want %d", obj.Symbol, len(obj.Bytes), apTrampolineVcpuStackSize)
			}
		}
		if obj.Align != 64 {
			t.Fatalf("object %s Align = %d, want 64", obj.Symbol, obj.Align)
		}
	}
}

func TestAPTrampolineBlobContract(t *testing.T) {
	blob := apTrampolineBlob()
	if len(blob) > 4096 {
		t.Fatalf("AP trampoline must fit in one 4KiB SIPI page, got %d bytes", len(blob))
	}
	for _, want := range [][]byte{
		{0xFA},                               // cli
		{0x0F, 0x01},                         // lgdt shape
		{0x0F, 0x01, 0x1D},                   // lidt owned IDT shape
		{0x0F, 0x22},                         // mov to control register shape
		{0x0F, 0x30},                         // wrmsr
		{0x66, 0xB8, 0x33, 0x00, 0x01, 0x80}, // CR0 owned-mode value
		{0x48, 0x8B, 0x3D},                   // 64-bit context pointer load
		{0x48, 0x8B, 0x05},                   // 64-bit entry/stack pointer loads
		{0x4C, 0x8B, 0x1D, 0x20, 0x00, 0x00, 0x00}, // AP local APIC base metadata load
		{0xB8, 0xFF, 0x01, 0x00, 0x00},             // AP local APIC SVR enable value
		{0xFB},                                     // sti before executor handoff
		{0xFF, 0xD0},                               // call rax handoff
		{0xF4},                                     // hlt fallback
	} {
		if !bytes.Contains(blob, want) {
			t.Fatalf("trampoline missing byte shape %x in %x", want, blob)
		}
	}
	if bytes.Contains(blob, u32le(0xFEE00000)) {
		t.Fatalf("trampoline must not embed the default LAPIC base: %x", blob)
	}
	if len(blob) <= apTrampolineLocalApicBaseOffset+8 {
		t.Fatalf("trampoline too short for local APIC base metadata slot: %d", len(blob))
	}
	if len(blob) <= apTrampolineReadyOffset+8 {
		t.Fatalf("trampoline too short for ready metadata slot: %d", len(blob))
	}
	if len(blob) < apTrampolineIDTDescriptorOffset+10 {
		t.Fatalf("trampoline too short for IDT descriptor metadata slot: %d", len(blob))
	}
	if !bytes.Equal(blob[0x10:0x12], []byte{0xC8, 0x80}) {
		t.Fatalf("trampoline lgdt must address installed SIPI page metadata, got %x", blob[0x10:0x12])
	}
	if !bytes.Equal(blob[0x1e:0x20], []byte{0xA0, 0x80}) {
		t.Fatalf("trampoline CR3 load must address installed SIPI page metadata, got %x", blob[0x1e:0x20])
	}
	if !bytes.Equal(blob[0x8b:0x92], []byte{0x48, 0x8B, 0x05, 0x16, 0x00, 0x00, 0x00}) {
		t.Fatalf("trampoline entry load must address entry metadata, got %x", blob[0x8b:0x92])
	}
	if !bytes.Equal(blob[apTrampolineLocalApicBaseOffset:apTrampolineLocalApicBaseOffset+8], make([]byte, 8)) {
		t.Fatalf("trampoline local APIC base metadata slot must start at %#x", apTrampolineLocalApicBaseOffset)
	}
	if !bytes.Equal(blob[apTrampolinePML4Offset:apTrampolinePML4Offset+16], make([]byte, 16)) {
		t.Fatalf("trampoline handoff metadata must start at %#x", apTrampolinePML4Offset)
	}
	if !bytes.Equal(blob[apTrampolineReadyOffset+8:apTrampolineReadyOffset+14], []byte{0x17, 0x00, 0xd0, 0x80, 0x00, 0x00}) {
		t.Fatalf("trampoline ready offset must precede the checked-in GDT descriptor, got %x", blob[apTrampolineReadyOffset+8:apTrampolineReadyOffset+14])
	}
	if !bytes.Equal(blob[apTrampolineReadyOffset+24:apTrampolineReadyOffset+30], []byte{0xff, 0xff, 0x00, 0x00, 0x00, 0x9a}) {
		t.Fatalf("trampoline GDT code descriptor must remain checked in, got %x", blob[apTrampolineReadyOffset+24:apTrampolineReadyOffset+30])
	}
	if !bytes.Equal(blob[apTrampolineIDTDescriptorOffset:apTrampolineIDTDescriptorOffset+10], make([]byte, 10)) {
		t.Fatalf("trampoline IDT descriptor slot must start at %#x", apTrampolineIDTDescriptorOffset)
	}
}

func TestVcpuEnterCallsExecutorStartAndHaltsIfReturned(t *testing.T) {
	execType := ir.Type{Name: "Hello", Module: "test", Kind: ir.TypeKindExecutor}
	hello := &ir.Local{Symbol: "hello", Type: execType}
	program := &ir.Program{
		VcpuStarts: []ir.VcpuStartPlan{{VcpuID: 0, SlotLabel: "hello", ExecutorType: execType, Terminal: true}},
		Functions: []ir.Function{{
			Symbol: "enter_hello",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				hello,
				&ir.VcpuEnter{VcpuID: 0, SlotLabel: "hello", Executor: hello},
			}}},
		}, {
			Symbol: "_wrela_method_test_Hello_run",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				&ir.Return{},
			}}},
		}},
	}
	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, image, "enter_hello")
	if !bytes.Contains(code, []byte{0xB8, 0xFF, 0x01, 0x00, 0x00}) {
		t.Fatalf("enter must enable the BSP local APIC before executor handoff: %x", code)
	}
	if !bytes.Contains(code, []byte{0xFB}) {
		t.Fatalf("enter must enable interrupts before executor handoff: %x", code)
	}
	if !bytes.Contains(code, []byte{0xF4}) {
		t.Fatalf("enter must contain hlt fallback if executor returns: %x", code)
	}
	if !codeReferencesSymbol(t, image, "enter_hello", "_wrela_vcpu0_apic_id_command") {
		t.Fatalf("enter_hello must store vCPU0 APIC ID command for wake IPIs")
	}
	if !hasRelocTo(t, image, "enter_hello", "_wrela_method_test_Hello_run") {
		t.Fatalf("enter_hello missing relocation to executor run")
	}
}

func TestVcpuStartEmitsLapicIcrWrites(t *testing.T) {
	execType := ir.Type{Name: "Worker", Module: "test", Kind: ir.TypeKindExecutor}
	worker := &ir.Local{Symbol: "worker", Type: execType}
	statusType := ir.Type{Name: "VcpuStartStatus", Module: "machine.x86_64.cpu_state", Kind: ir.TypeKindData}
	program := &ir.Program{
		VcpuStarts: []ir.VcpuStartPlan{{
			VcpuID:        1,
			APICID:        7,
			LocalApicBase: 0xfee01000,
			SlotLabel:     "worker",
			Terminal:      false,
		}},
		Functions: []ir.Function{{
			Symbol: "start_worker",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				worker,
				&ir.VcpuStart{
					VcpuID:        1,
					APICID:        7,
					LocalApicBase: 0xfee01000,
					SlotLabel:     "worker",
					Type:          statusType,
					Executor:      worker,
				},
			}}},
		}, {
			Symbol: "_wrela_method_test_Worker_run",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				&ir.Return{},
			}}},
		}},
	}
	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, image, "start_worker")
	if !bytes.Contains(code, u32le(7<<24)) {
		t.Fatalf("start_worker must target APIC ID 7 in ICR high dword: %x", code)
	}
	if !bytes.Contains(code, u32le(0xfee01000)) {
		t.Fatalf("start_worker must use discovered LAPIC base 0xfee01000: %x", code)
	}
	if bytes.Contains(code, u32le(0xFEE00000)) {
		t.Fatalf("start_worker must not embed the default LAPIC base: %x", code)
	}
	for _, want := range [][]byte{
		u32le(apicICRInitAssert),
		u32le(0x00004608),
		{0x0F, 0x20, 0xD8}, // mov rax, cr3 before patching trampoline PML4 slot
	} {
		if !bytes.Contains(code, want) {
			t.Fatalf("start missing LAPIC command %x in %x", want, code)
		}
	}
	for _, symbol := range []string{"_wrela_vcpu1_ready", "_wrela_vcpu1_stack", "_wrela_vcpu1_apic_id_command", "_wrela_method_test_Worker_run"} {
		if !codeReferencesSymbol(t, image, "start_worker", symbol) {
			t.Fatalf("start_worker missing reference to %s", symbol)
		}
	}
	if !bytes.Contains(code, []byte{0x49, 0x89, 0x81, apTrampolinePML4Offset, 0x00, 0x00, 0x00}) {
		t.Fatalf("start_worker must patch 64-bit trampoline CR3 metadata through stable r9 base: %x", code)
	}
	for _, broadcast := range [][]byte{u32le(0x000CC500), u32le(0x000C4608)} {
		if bytes.Contains(code, broadcast) {
			t.Fatalf("start_worker must not use destination-shorthand broadcast ICR command %x in %x", broadcast, code)
		}
	}
	for _, offset := range []byte{apTrampolineLocalApicBaseOffset, apTrampolineEntryOffset, apTrampolineStackOffset, apTrampolineContextOffset, apTrampolineReadyOffset} {
		wantStore := []byte{0x49, 0x89, 0x81, offset, 0x00, 0x00, 0x00}
		if !bytes.Contains(code, wantStore) {
			t.Fatalf("start_worker must patch 64-bit trampoline offset %#x through stable r9 base; missing %x in %x", offset, wantStore, code)
		}
	}
	if !bytes.Contains(code, []byte{0x41, 0x0F, 0x01, 0x89, apTrampolineIDTDescriptorOffset, 0x00, 0x00, 0x00}) {
		t.Fatalf("start_worker must patch AP trampoline IDT descriptor through stable r9 base: %x", code)
	}
}

func TestVcpuStartLoadsRuntimeReceiverApicFields(t *testing.T) {
	execType := ir.Type{Name: "Worker", Module: "test", Kind: ir.TypeKindExecutor}
	worker := &ir.Local{Symbol: "worker", Type: execType}
	vcpuType := ir.Type{Name: "Vcpu", Module: "machine.x86_64.cpu_state", Kind: ir.TypeKindData}
	vcpu := &ir.Param{Symbol: "vcpu", Type: vcpuType}
	statusType := ir.Type{Name: "VcpuStartStatus", Module: "machine.x86_64.cpu_state", Kind: ir.TypeKindData}
	program := &ir.Program{
		Types: runtimeVcpuCodegenTypes(),
		VcpuStarts: []ir.VcpuStartPlan{{
			VcpuID:       1,
			SlotLabel:    "worker",
			ExecutorType: execType,
			Terminal:     false,
		}},
		Functions: []ir.Function{{
			Symbol: "start_runtime_worker",
			Params: []ir.Value{vcpu},
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				worker,
				&ir.VcpuStart{
					VcpuID:    1,
					Vcpu:      vcpu,
					SlotLabel: "worker",
					Type:      statusType,
					Executor:  worker,
				},
			}}},
		}, {
			Symbol: "_wrela_method_test_Worker_run",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				&ir.Return{},
			}}},
		}},
	}
	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, image, "start_runtime_worker")
	if !bytes.Contains(code, []byte{0x48, 0xC1, 0xE0, 0x18}) {
		t.Fatalf("start must shift the runtime APIC ID into the ICR destination field: %x", code)
	}
	if !bytes.Contains(code, []byte{0x49, 0x8B, 0x5B, 0x10}) && !bytes.Contains(code, []byte{0x4D, 0x8B, 0x5B, 0x10}) {
		t.Fatalf("start must load the runtime local_apic_base field instead of using only immediates: %x", code)
	}
	globalStore := bytes.Index(code, []byte{0x4C, 0x89, 0x18})
	shift := bytes.Index(code, []byte{0x48, 0xC1, 0xE0, 0x18})
	firstICRHigh := bytes.Index(code, []byte{0x41, 0x89, 0x83, 0x10, 0x03, 0x00, 0x00})
	if globalStore < 0 || shift < 0 || firstICRHigh < 0 || !(globalStore < shift && shift < firstICRHigh) {
		t.Fatalf("start must reload APIC destination after storing local APIC global; global=%d shift=%d icr=%d code=%x", globalStore, shift, firstICRHigh, code)
	}
	localBaseReload := lastIndexAnyBefore(code, firstICRHigh,
		[]byte{0x49, 0x8B, 0x5B, 0x10},
		[]byte{0x4D, 0x8B, 0x5B, 0x10},
	)
	if localBaseReload < shift {
		t.Fatalf("start must reload the LAPIC base after APIC ID field load clobbers r11; baseReload=%d shift=%d icr=%d code=%x", localBaseReload, shift, firstICRHigh, code)
	}
}

func TestVcpuStartAndEnterCompileTogether(t *testing.T) {
	program := twoVcpuProgramForCodegenTest()
	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	for _, symbol := range []string{
		"_wrela_vcpu0_context",
		"_wrela_vcpu1_context",
		"_wrela_vcpu1_ready",
	} {
		if _, ok := image.Symbols[symbol]; !ok {
			t.Fatalf("missing symbol %s", symbol)
		}
	}
}

func runtimeVcpuCodegenTypes() map[string]ir.TypeInfo {
	vcpu := ir.TypeInfo{
		Name:        "Vcpu",
		Module:      "machine.x86_64.cpu_state",
		Kind:        ir.TypeKindData,
		Size:        24,
		Align:       8,
		StorageSize: 24,
		Fields: map[string]ir.FieldInfo{
			"id":              {Name: "id", Type: ir.Type{Name: "U64"}, Offset: 0, Size: 8, Align: 8},
			"apic_id":         {Name: "apic_id", Type: ir.Type{Name: "U32"}, Offset: 8, Size: 4, Align: 4},
			"local_apic_base": {Name: "local_apic_base", Type: ir.Type{Name: "PhysicalAddress"}, Offset: 16, Size: 8, Align: 8},
		},
		FieldOrder: []string{"id", "apic_id", "local_apic_base"},
	}
	return map[string]ir.TypeInfo{
		"Vcpu":                          vcpu,
		"machine.x86_64.cpu_state.Vcpu": vcpu,
		"machine.x86_64.cpu_state.VcpuStartStatus": {Name: "VcpuStartStatus", Module: "machine.x86_64.cpu_state", Kind: ir.TypeKindData, Size: 16, Align: 8, StorageSize: 16},
	}
}

func codeReferencesSymbol(t *testing.T, image *Image, ownerSymbol string, targetSymbol string) bool {
	t.Helper()
	ownerStart, ok := image.Symbols[ownerSymbol]
	if !ok {
		t.Fatalf("missing owner symbol %s", ownerSymbol)
	}
	target, ok := image.Symbols[targetSymbol]
	if !ok {
		t.Fatalf("missing target symbol %s", targetSymbol)
	}
	code := symbolBytes(t, image, ownerSymbol)
	want := make([]byte, 8)
	binary.LittleEndian.PutUint64(want, runtimeImageBase+target)
	if !bytes.Contains(code, want) {
		return false
	}
	_ = ownerStart
	return true
}

func hasRelocTo(t *testing.T, image *Image, ownerSymbol string, targetSymbol string) bool {
	t.Helper()
	return codeCallsSymbol(t, image, ownerSymbol, targetSymbol)
}

func lastIndexAnyBefore(haystack []byte, before int, needles ...[]byte) int {
	if before < 0 || before > len(haystack) {
		before = len(haystack)
	}
	best := -1
	prefix := haystack[:before]
	for _, needle := range needles {
		if idx := bytes.LastIndex(prefix, needle); idx > best {
			best = idx
		}
	}
	return best
}

func u32le(v uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, v)
	return out
}

func twoVcpuProgramForCodegenTest() *ir.Program {
	execType := ir.Type{Name: "Worker", Module: "test", Kind: ir.TypeKindExecutor}
	hello := &ir.Local{Symbol: "hello", Type: execType}
	worker := &ir.Local{Symbol: "worker", Type: execType}
	statusType := ir.Type{Name: "VcpuStartStatus", Module: "machine.x86_64.cpu_state", Kind: ir.TypeKindData}
	return &ir.Program{
		VcpuStarts: []ir.VcpuStartPlan{
			{VcpuID: 0, SlotLabel: "hello", ExecutorType: execType, Terminal: true},
			{VcpuID: 1, SlotLabel: "worker", ExecutorType: execType, Terminal: false},
		},
		Functions: []ir.Function{{
			Symbol: "start_and_enter",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				worker,
				hello,
				&ir.VcpuStart{VcpuID: 1, SlotLabel: "worker", Type: statusType, Executor: worker},
				&ir.VcpuEnter{VcpuID: 0, SlotLabel: "hello", Executor: hello},
			}}},
		}, {
			Symbol: "_wrela_method_test_Worker_run",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				&ir.Return{},
			}}},
		}},
	}
}

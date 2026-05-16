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
		"_wrela_vcpu0_ready",
		"_wrela_vcpu0_entry",
		"_wrela_vcpu0_stack_top",
		"_wrela_vcpu0_context",
		"_wrela_vcpu1_ready",
		"_wrela_vcpu1_entry",
		"_wrela_vcpu1_stack_top",
		"_wrela_vcpu1_context",
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
		{0xFA},       // cli
		{0x0F, 0x22}, // mov to control register shape
		{0x0F, 0x30}, // wrmsr
		{0xF4},       // hlt fallback
	} {
		if !bytes.Contains(blob, want) {
			t.Fatalf("trampoline missing byte shape %x in %x", want, blob)
		}
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
	if !bytes.Contains(code, []byte{0xF4}) {
		t.Fatalf("enter must contain hlt fallback if executor returns: %x", code)
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
		VcpuStarts: []ir.VcpuStartPlan{{VcpuID: 1, SlotLabel: "worker", Terminal: false}},
		Functions: []ir.Function{{
			Symbol: "start_worker",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				worker,
				&ir.VcpuStart{VcpuID: 1, SlotLabel: "worker", Type: statusType, Executor: worker},
			}}},
		}},
	}
	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, image, "start_worker")
	for _, want := range [][]byte{
		u32le(0x000C4500),
		u32le(0x000C4608),
	} {
		if !bytes.Contains(code, want) {
			t.Fatalf("start missing LAPIC command %x in %x", want, code)
		}
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

func hasRelocTo(t *testing.T, image *Image, ownerSymbol string, targetSymbol string) bool {
	t.Helper()
	return codeCallsSymbol(t, image, ownerSymbol, targetSymbol)
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

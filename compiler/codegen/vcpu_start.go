package codegen

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

const apTrampolineBase = 0x8000
const apTrampolineInstallSymbol = "_wrela_method_platform_uefi_types_DelegatedMemory_install_ap_trampoline"
const apTrampolineVcpuStackSize = 16 * 1024

const (
	apTrampolinePML4Offset    = 0x7c
	apTrampolineEntryOffset   = 0x80
	apTrampolineStackOffset   = 0x84
	apTrampolineContextOffset = 0x88
	apTrampolineReadyOffset   = 0x8c
)

//go:embed testdata/ap_trampoline.bin
var apTrampolineBlobBytes []byte

func apTrampolineBlob() []byte {
	out := make([]byte, len(apTrampolineBlobBytes))
	copy(out, apTrampolineBlobBytes)
	return out
}

func apTrampolineDataObject() ir.DataObject {
	return ir.DataObject{
		Symbol: "_wrela_ap_trampoline_blob",
		Bytes:  apTrampolineBlob(),
		Align:  4096,
	}
}

func hasAPTrampolineInstallMethod(program *ir.Program) bool {
	for _, method := range program.AsmMethods {
		if method.Symbol == apTrampolineInstallSymbol {
			return true
		}
	}
	return false
}

func compileAPTrampolineInstallUnit() (compiledUnit, []diag.Diagnostic) {
	return compileAsmMethodUnit(ir.AsmMethod{
		Symbol: apTrampolineInstallSymbol,
		Params: []ir.Value{
			&ir.Param{Symbol: "trampoline_base", Type: ir.Type{Name: "PhysicalAddress"}},
			&ir.Param{Symbol: "source", Type: ir.Type{Name: "PhysicalAddress"}},
			&ir.Param{Symbol: "length", Type: ir.Type{Name: "U64"}},
		},
		Body: `mov rdi, trampoline_base
mov rsi, source
mov rcx, length
copy:
cmp rcx, 0
je done
mov al, [rsi]
mov [rdi], al
add rsi, 1
add rdi, 1
sub rcx, 1
jmp copy
done:
ret`,
	})
}

func vcpuStartupData(program *ir.Program) []ir.DataObject {
	out := []ir.DataObject{apTrampolineDataObject()}
	if program == nil || len(program.VcpuStarts) == 0 {
		return out
	}

	plans := append([]ir.VcpuStartPlan{}, program.VcpuStarts...)
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].VcpuID < plans[j].VcpuID
	})

	suffixes := []string{"ready", "entry", "stack_top", "context"}
	for _, plan := range plans {
		out = append(out, ir.DataObject{
			Symbol: fmt.Sprintf("_wrela_vcpu%d_stack", plan.VcpuID),
			Bytes:  make([]byte, apTrampolineVcpuStackSize),
			Align:  64,
		})
		for _, suffix := range suffixes {
			out = append(out, ir.DataObject{
				Symbol: fmt.Sprintf("_wrela_vcpu%d_%s", plan.VcpuID, suffix),
				Bytes:  make([]byte, 8),
				Align:  64,
			})
		}
	}
	return out
}

func emitVcpuEnter(e *Emitter, op *ir.VcpuEnter, frame Frame, ctx compileContext) {
	emitAddressOfValue(e, frame, op.Executor, asm.MustLookup("rdi"))
	emitCallReloc(e, executorStartSymbol(ctx.VcpuPlans[op.SlotLabel].ExecutorType))
	emitHltLoop(e)
}

func emitVcpuStart(e *Emitter, op *ir.VcpuStart, frame Frame, ctx compileContext) {
	emitPrepareVcpuStartup(e, op, frame, ctx)
	emitLapicWrite(e, lapicICRHigh, uint32(op.VcpuID)<<24)
	emitLapicWrite(e, lapicICRLow, 0x000C4500)
	emitDelayLoop(e, 10000)
	emitLapicWrite(e, lapicICRHigh, uint32(op.VcpuID)<<24)
	emitLapicWrite(e, lapicICRLow, 0x000C4600|uint32(apTrampolineBase>>12))
	emitDelayLoop(e, 10000)
	emitLapicWrite(e, lapicICRHigh, uint32(op.VcpuID)<<24)
	emitLapicWrite(e, lapicICRLow, 0x000C4600|uint32(apTrampolineBase>>12))
	emitWaitForVcpuReady(e, op.VcpuID)
	emitStoreVcpuStartStatus(e, op, frame)
}

func emitPrepareVcpuStartup(e *Emitter, op *ir.VcpuStart, frame Frame, ctx compileContext) {
	readySymbol := fmt.Sprintf("_wrela_vcpu%d_ready", op.VcpuID)
	entrySymbol := fmt.Sprintf("_wrela_vcpu%d_entry", op.VcpuID)
	stackTopSymbol := fmt.Sprintf("_wrela_vcpu%d_stack_top", op.VcpuID)
	contextSymbol := fmt.Sprintf("_wrela_vcpu%d_context", op.VcpuID)
	stackSymbol := fmt.Sprintf("_wrela_vcpu%d_stack", op.VcpuID)
	executorType := ctx.VcpuPlans[op.SlotLabel].ExecutorType
	if executorType.Name == "" {
		executorType = valueType(op.Executor)
	}
	runSymbol := executorStartSymbol(executorType)

	// Reset and expose the startup record fields for debugging/inspection.
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), readySymbol)
	emitMovImmToReg(e, asm.MustLookup("r10"), 0)
	emitStoreMemFromReg(e, asm.MustLookup("rax"), 0, asm.MustLookup("r10"), 64)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), runSymbol)
	emitRegRegMove(e, asm.MustLookup("r10"), asm.MustLookup("rax"))
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), entrySymbol)
	emitStoreMemFromReg(e, asm.MustLookup("rax"), 0, asm.MustLookup("r10"), 64)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), stackSymbol)
	emitAddImm(e, asm.MustLookup("rax"), apTrampolineVcpuStackSize)
	emitRegRegMove(e, asm.MustLookup("r10"), asm.MustLookup("rax"))
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), stackTopSymbol)
	emitStoreMemFromReg(e, asm.MustLookup("rax"), 0, asm.MustLookup("r10"), 64)
	emitAddressOfValue(e, frame, op.Executor, asm.MustLookup("r10"))
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), contextSymbol)
	emitStoreMemFromReg(e, asm.MustLookup("rax"), 0, asm.MustLookup("r10"), 64)

	// Patch the SIPI-page handoff slots consumed by ap_trampoline.bin.
	emitMovImmToReg(e, asm.MustLookup("r11"), apTrampolineBase)
	e.emit(0x0F, 0x20, 0xD8) // mov rax, cr3
	emitStoreMemFromReg(e, asm.MustLookup("r11"), apTrampolinePML4Offset, asm.MustLookup("rax"), 32)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), runSymbol)
	emitStoreMemFromReg(e, asm.MustLookup("r11"), apTrampolineEntryOffset, asm.MustLookup("rax"), 32)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), stackSymbol)
	emitAddImm(e, asm.MustLookup("rax"), apTrampolineVcpuStackSize)
	emitStoreMemFromReg(e, asm.MustLookup("r11"), apTrampolineStackOffset, asm.MustLookup("rax"), 32)
	emitAddressOfValue(e, frame, op.Executor, asm.MustLookup("rax"))
	emitStoreMemFromReg(e, asm.MustLookup("r11"), apTrampolineContextOffset, asm.MustLookup("rax"), 32)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), readySymbol)
	emitStoreMemFromReg(e, asm.MustLookup("r11"), apTrampolineReadyOffset, asm.MustLookup("rax"), 32)
}

func executorStartSymbol(typ ir.Type) string {
	return codegenSymbolName("method", typ.Module, typ.Name, "run")
}

func emitHltLoop(e *Emitter) {
	loop := e.newLabel("hlt_loop")
	e.bindLabel(loop)
	emitHltWait(e)
	e.emitJmp(loop)
}

func emitDelayLoop(e *Emitter, count int64) {
	loop := e.newLabel("vcpu_delay")
	emitMovImmToReg(e, asm.MustLookup("rcx"), count)
	e.bindLabel(loop)
	e.emitInstruction(asm.Instruction{Mnemonic: "sub", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rcx")},
		asm.ImmOperand{Value: 1},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "cmp", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rcx")},
		asm.ImmOperand{Value: 0},
	}})
	e.emitJcc(0x85, loop)
}

func emitWaitForVcpuReady(e *Emitter, vcpuID int) {
	loop := e.newLabel("vcpu_ready")
	done := e.newLabel("vcpu_ready_done")
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), fmt.Sprintf("_wrela_vcpu%d_ready", vcpuID))
	e.bindLabel(loop)
	emitLoadMemToReg(e, asm.MustLookup("r10"), asm.MustLookup("rax"), 0, 64)
	e.emitInstruction(asm.Instruction{Mnemonic: "cmp", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("r10")},
		asm.ImmOperand{Value: 1},
	}})
	e.emitJcc(0x84, done)
	e.emitInstruction(asm.Instruction{Mnemonic: "pause"})
	e.emitJmp(loop)
	e.bindLabel(done)
}

func emitStoreVcpuStartStatus(e *Emitter, op *ir.VcpuStart, frame Frame) {
	slot, ok := frame.Slots[op]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing vcpu start status slot"})
		return
	}
	emitMovImmToReg(e, asm.MustLookup("rax"), 1)
	emitStoreSlotFromReg(e, asm.MustLookup("rax"), slot, 8)
	emitMovImmToReg(e, asm.MustLookup("rax"), int64(op.VcpuID))
	emitStoreSlotFromReg(e, asm.MustLookup("rax"), slot+8, 64)
}

func codegenSymbolName(parts ...string) string {
	var b strings.Builder
	b.WriteString("_wrela")
	for _, part := range parts {
		if part == "" {
			continue
		}
		b.WriteByte('_')
		for _, r := range part {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			} else {
				b.WriteByte('_')
			}
		}
	}
	return b.String()
}

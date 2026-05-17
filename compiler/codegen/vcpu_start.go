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
const apStartupReadyPollLimit = 10_000_000
const apicICRInitAssert = 0x00004500

const (
	apTrampolineLocalApicBaseOffset = 0x98
	apTrampolinePML4Offset          = 0xa0
	apTrampolineEntryOffset         = 0xa8
	apTrampolineStackOffset         = 0xb0
	apTrampolineContextOffset       = 0xb8
	apTrampolineReadyOffset         = 0xc0
	apTrampolineIDTDescriptorOffset = 0xe8
)

//go:embed testdata/ap_trampoline.bin
var apTrampolineBlobBytes []byte

func apTrampolineBlob() []byte {
	out := make([]byte, len(apTrampolineBlobBytes))
	copy(out, apTrampolineBlobBytes)
	return out
}

func validateAPStartupContract() []diag.Diagnostic {
	if apTrampolineBase >= 0x100000 {
		return []diag.Diagnostic{{Phase: diagnosticPhase, Code: diag.SEM0074, Message: "AP trampoline must be below 1 MiB"}}
	}
	if apTrampolineBase%4096 != 0 {
		return []diag.Diagnostic{{Phase: diagnosticPhase, Code: diag.SEM0074, Message: "AP trampoline must be 4 KiB aligned"}}
	}
	return nil
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

	suffixes := []string{"ready", "entry", "stack_top", "context", "apic_id_command"}
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
	mode := vcpuAPICMode(op.APICMode, op.SlotLabel, ctx)
	emitStoreVcpuAPICIDCommand(e, op.VcpuID, op.Vcpu, op.APICID, frame, mode)
	emitLoadVcpuLocalApicBase(e, op.Vcpu, op.LocalApicBase, frame, asm.MustLookup("r11"))
	emitStoreLocalApicBase(e, asm.MustLookup("r11"))
	emitLocalApicSVREnable(e, asm.MustLookup("r11"), mode)
	e.emit(0xFB)
	emitAddressOfValue(e, frame, op.Executor, asm.MustLookup("rdi"))
	emitCallReloc(e, executorStartSymbolForPlan(ctx.VcpuPlans[op.SlotLabel], valueType(op.Executor)))
	emitHltLoop(e)
}

func emitVcpuStart(e *Emitter, op *ir.VcpuStart, frame Frame, ctx compileContext) {
	mode := vcpuAPICMode(op.APICMode, op.SlotLabel, ctx)
	emitPrepareVcpuStartup(e, op, frame, ctx)
	emitStoreVcpuAPICIDCommand(e, op.VcpuID, op.Vcpu, op.APICID, frame, mode)
	emitLoadVcpuLocalApicBase(e, op.Vcpu, op.LocalApicBase, frame, asm.MustLookup("r11"))
	emitStoreLocalApicBase(e, asm.MustLookup("r11"))
	emitLocalApicSVREnable(e, asm.MustLookup("r11"), mode)
	emitLoadVcpuAPICIDCommand(e, op.Vcpu, op.APICID, frame, asm.MustLookup("rax"), mode)
	emitLoadVcpuLocalApicBase(e, op.Vcpu, op.LocalApicBase, frame, asm.MustLookup("r11"))
	emitSendIcrForMode(e, asm.MustLookup("r11"), asm.MustLookup("rax"), apicICRInitAssert, mode)
	emitLoadVcpuAPICIDCommand(e, op.Vcpu, op.APICID, frame, asm.MustLookup("rax"), mode)
	emitLoadVcpuLocalApicBase(e, op.Vcpu, op.LocalApicBase, frame, asm.MustLookup("r11"))
	emitSendIcrForMode(e, asm.MustLookup("r11"), asm.MustLookup("rax"), 0x00004600|uint32(apTrampolineBase>>12), mode)
	emitLoadVcpuAPICIDCommand(e, op.Vcpu, op.APICID, frame, asm.MustLookup("rax"), mode)
	emitLoadVcpuLocalApicBase(e, op.Vcpu, op.LocalApicBase, frame, asm.MustLookup("r11"))
	emitSendIcrForMode(e, asm.MustLookup("r11"), asm.MustLookup("rax"), 0x00004600|uint32(apTrampolineBase>>12), mode)
	emitWaitForVcpuReady(e, op.VcpuID)
	emitStoreVcpuStartStatus(e, op, frame)
}

func vcpuAPICMode(opMode string, slotLabel string, ctx compileContext) string {
	if opMode != "" {
		return opMode
	}
	if plan, ok := ctx.VcpuPlans[slotLabel]; ok && plan.APICMode != "" {
		return plan.APICMode
	}
	return ctx.APICMode
}

func emitLoadVcpuLocalApicBase(e *Emitter, vcpu ir.Value, fallback uint64, frame Frame, dst asm.Reg) {
	if emitLoadVcpuField(e, vcpu, frame, "local_apic_base", dst, 64) {
		return
	}
	emitMovImmToReg(e, dst, int64(fallback))
}

func emitLoadVcpuAPICIDCommand(e *Emitter, vcpu ir.Value, fallback uint32, frame Frame, dst asm.Reg, mode string) {
	if emitLoadVcpuField(e, vcpu, frame, "apic_id", dst, 32) {
		if !apicModeUsesRawDestination(mode) {
			emitShiftImm(e, 0x04, dst, 24)
		}
		return
	}
	if apicModeUsesRawDestination(mode) {
		emitMovImmToReg(e, dst, int64(fallback))
		return
	}
	emitMovImmToReg(e, dst, int64(fallback<<24))
}

func emitStoreVcpuAPICIDCommand(e *Emitter, vcpuID int, vcpu ir.Value, fallback uint32, frame Frame, mode string) {
	emitLoadVcpuAPICIDCommand(e, vcpu, fallback, frame, asm.MustLookup("r10"), mode)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), vcpuAPICIDCommandSymbol(vcpuID))
	emitStoreMemFromReg(e, asm.MustLookup("rax"), 0, asm.MustLookup("r10"), 64)
}

func vcpuAPICIDCommandSymbol(vcpuID int) string {
	return fmt.Sprintf("_wrela_vcpu%d_apic_id_command", vcpuID)
}

func emitLoadVcpuField(e *Emitter, vcpu ir.Value, frame Frame, field string, dst asm.Reg, width int) bool {
	if vcpu == nil {
		return false
	}
	info, ok := e.ctx.typeInfo(valueType(vcpu))
	if !ok {
		return false
	}
	fieldInfo, ok := info.Fields[field]
	if !ok {
		return false
	}
	base, disp, ok := emitValueAddress(e, frame, vcpu)
	if !ok {
		return false
	}
	emitLoadMemToReg(e, dst, base, disp+int64(fieldInfo.Offset), width)
	return true
}

func emitLapicWriteWithBaseReg(e *Emitter, base asm.Reg, offset uint32, value uint32) {
	emitMovImmToReg(e, asm.MustLookup("rax"), int64(value))
	emitLapicWriteRegWithBaseReg(e, base, offset, asm.MustLookup("rax"))
}

func emitLocalApicSVREnable(e *Emitter, base asm.Reg, mode string) {
	if usesX2APIC(mode) {
		emitEnableX2APIC(e)
		emitMovImmToReg(e, asm.MustLookup("r10"), 0x1ff)
		emitX2APICWriteMSR(e, x2apicSVRMSR, asm.MustLookup("r10"))
		return
	}
	if usesRuntimeX2APICFallback(mode) {
		xapic := e.newLabel("apic_svr_xapic")
		done := e.newLabel("apic_svr_done")
		emitJumpIfX2APICInactive(e, xapic)
		emitMovImmToReg(e, asm.MustLookup("r10"), 0x1ff)
		emitX2APICWriteMSR(e, x2apicSVRMSR, asm.MustLookup("r10"))
		e.emitJmp(done)
		e.bindLabel(xapic)
		emitLapicWriteWithBaseReg(e, base, lapicSVR, 0x1ff)
		e.bindLabel(done)
		return
	}
	emitLapicWriteWithBaseReg(e, base, lapicSVR, 0x1ff)
}

func emitLapicWriteRegWithBaseReg(e *Emitter, base asm.Reg, offset uint32, value asm.Reg) {
	emitStoreMemFromReg(e, base, int64(offset), value, 32)
}

func emitWaitForIcrDelivery(e *Emitter, base asm.Reg) {
	loop := e.newLabel("icr_delivery")
	done := e.newLabel("icr_delivery_done")
	e.bindLabel(loop)
	emitLoadMemToReg(e, asm.MustLookup("rax"), base, lapicICRLow, 32)
	emitMovImmToReg(e, asm.MustLookup("r10"), 0x1000)
	emitRegRegOp(e, 0x21, asm.MustLookup("rax"), asm.MustLookup("r10"))
	e.emitInstruction(asm.Instruction{Mnemonic: "cmp", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.ImmOperand{Value: 0},
	}})
	e.emitJcc(0x84, done)
	e.emitInstruction(asm.Instruction{Mnemonic: "pause"})
	e.emitJmp(loop)
	e.bindLabel(done)
}

func emitSendIcr(e *Emitter, base asm.Reg, destShifted asm.Reg, low uint32) {
	emitRegRegMove(e, asm.MustLookup("rax"), destShifted)
	emitLapicWriteRegWithBaseReg(e, base, lapicICRHigh, asm.MustLookup("rax"))
	emitLapicWriteWithBaseReg(e, base, lapicICRLow, low)
	emitWaitForIcrDelivery(e, base)
}

func emitSendIcrForMode(e *Emitter, base asm.Reg, destCommand asm.Reg, low uint32, mode string) {
	switch {
	case usesX2APIC(mode):
		emitSendX2APICIcr(e, destCommand, low)
	case usesRuntimeX2APICFallback(mode):
		xapic := e.newLabel("apic_icr_xapic")
		done := e.newLabel("apic_icr_done")
		emitRegRegMove(e, asm.MustLookup("r10"), destCommand)
		emitJumpIfX2APICInactive(e, xapic)
		emitSendX2APICIcr(e, asm.MustLookup("r10"), low)
		e.emitJmp(done)
		e.bindLabel(xapic)
		emitShiftImm(e, 0x04, asm.MustLookup("r10"), 24)
		emitSendIcr(e, base, asm.MustLookup("r10"), low)
		e.bindLabel(done)
	default:
		emitSendIcr(e, base, destCommand, low)
	}
}

func emitSendX2APICIcr(e *Emitter, destAPICID asm.Reg, low uint32) {
	emitRegRegMove(e, asm.MustLookup("r10"), destAPICID)
	emitShiftImm(e, 0x04, asm.MustLookup("r10"), 32)
	emitMovImmToReg(e, asm.MustLookup("rax"), int64(low))
	emitRegRegOp(e, 0x09, asm.MustLookup("rax"), asm.MustLookup("r10"))
	emitX2APICWriteMSR(e, x2apicICRMSR, asm.MustLookup("rax"))
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
	runSymbol := executorStartSymbolForPlan(ctx.VcpuPlans[op.SlotLabel], executorType)

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
	trampolineBase := asm.MustLookup("r9")
	emitMovImmToReg(e, trampolineBase, apTrampolineBase)
	emitLoadVcpuLocalApicBase(e, op.Vcpu, op.LocalApicBase, frame, asm.MustLookup("rax"))
	emitStoreMemFromReg(e, trampolineBase, apTrampolineLocalApicBaseOffset, asm.MustLookup("rax"), 64)
	emitStoreIDTDescriptor(e, trampolineBase, apTrampolineIDTDescriptorOffset)
	e.emit(0x0F, 0x20, 0xD8) // mov rax, cr3
	emitStoreMemFromReg(e, trampolineBase, apTrampolinePML4Offset, asm.MustLookup("rax"), 64)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), runSymbol)
	emitStoreMemFromReg(e, trampolineBase, apTrampolineEntryOffset, asm.MustLookup("rax"), 64)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), stackSymbol)
	emitAddImm(e, asm.MustLookup("rax"), apTrampolineVcpuStackSize)
	emitStoreMemFromReg(e, trampolineBase, apTrampolineStackOffset, asm.MustLookup("rax"), 64)
	emitAddressOfValue(e, frame, op.Executor, asm.MustLookup("rax"))
	emitStoreMemFromReg(e, trampolineBase, apTrampolineContextOffset, asm.MustLookup("rax"), 64)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), readySymbol)
	emitStoreMemFromReg(e, trampolineBase, apTrampolineReadyOffset, asm.MustLookup("rax"), 64)
}

func emitStoreIDTDescriptor(e *Emitter, base asm.Reg, offset int64) {
	if base.Name != "r9" {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "AP trampoline IDT descriptor patch requires r9 base"})
		return
	}
	e.emit(0x41, 0x0F, 0x01, 0x89)
	e.emitInt32(int32(offset))
}

func executorStartSymbol(typ ir.Type) string {
	return codegenSymbolName("method", typ.Module, typ.Name, "run")
}

func executorStartSymbolForPlan(plan ir.VcpuStartPlan, fallback ir.Type) string {
	if plan.EntrySymbol != "" {
		return plan.EntrySymbol
	}
	if plan.ExecutorType.Name != "" {
		return executorStartSymbol(plan.ExecutorType)
	}
	return executorStartSymbol(fallback)
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
	timeout := e.newLabel("vcpu_ready_timeout")
	emitMovImmToReg(e, asm.MustLookup("rcx"), apStartupReadyPollLimit)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), fmt.Sprintf("_wrela_vcpu%d_ready", vcpuID))
	e.bindLabel(loop)
	emitLoadMemToReg(e, asm.MustLookup("r10"), asm.MustLookup("rax"), 0, 64)
	e.emitInstruction(asm.Instruction{Mnemonic: "cmp", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("r10")},
		asm.ImmOperand{Value: 1},
	}})
	e.emitJcc(0x84, done)
	e.emitInstruction(asm.Instruction{Mnemonic: "sub", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rcx")},
		asm.ImmOperand{Value: 1},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "cmp", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rcx")},
		asm.ImmOperand{Value: 0},
	}})
	e.emitJcc(0x84, timeout)
	e.emitInstruction(asm.Instruction{Mnemonic: "pause"})
	e.emitJmp(loop)
	e.bindLabel(timeout)
	emitCallReloc(e, "_wrela_ap_startup_timeout")
	e.bindLabel(done)
}

func emitStoreVcpuStartStatus(e *Emitter, op *ir.VcpuStart, frame Frame) {
	valueSlot, ok := frameSlot(frame, op)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing vcpu start status slot"})
		return
	}
	slot, ok := frameObjectSlot(frame, op)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing vcpu start status object slot"})
		return
	}
	emitSlotFromBase(e, asm.MustLookup("r10"), asm.MustLookup("rbp"), slot)
	emitStoreSlotFromReg(e, asm.MustLookup("r10"), valueSlot, 64)
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

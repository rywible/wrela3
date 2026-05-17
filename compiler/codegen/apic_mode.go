package codegen

import "github.com/ryanwible/wrela3/compiler/asm"

const (
	ia32APICBaseMSR = 0x01B
	x2apicEOIMSR    = 0x80B
	x2apicSVRMSR    = 0x80F
	x2apicICRMSR    = 0x830
)

func usesX2APIC(mode string) bool {
	switch mode {
	case "x2apic_required", "x2apic_preferred":
		return true
	default:
		return false
	}
}

func usesRuntimeX2APICFallback(mode string) bool {
	return mode == "x2apic_with_xapic_fallback"
}

func apicModeUsesRawDestination(mode string) bool {
	return usesX2APIC(mode) || usesRuntimeX2APICFallback(mode)
}

func emitJumpIfX2APICUnavailable(e *Emitter, target string) {
	emitPushReg(e, asm.MustLookup("rbx"))
	e.emit(0xB8)
	e.emitUint32(1)
	e.emit(0x0F, 0xA2) // cpuid
	emitPopReg(e, asm.MustLookup("rbx"))
	e.emit(0xF7, 0xC1)
	e.emitUint32(1 << 21)
	e.emitJcc(0x84, target)
}

func emitJumpIfX2APICInactive(e *Emitter, target string) {
	e.emit(0xB9)
	e.emitUint32(ia32APICBaseMSR)
	e.emit(0x0F, 0x32) // rdmsr
	e.emit(0xA9)
	e.emitUint32(1 << 10)
	e.emitJcc(0x84, target)
}

func emitEnableX2APIC(e *Emitter) {
	e.emit(0xB9)
	e.emitUint32(ia32APICBaseMSR)
	e.emit(0x0F, 0x32) // rdmsr
	e.emit(0x0D)
	e.emitUint32(0xC00) // APIC global enable | x2APIC enable
	e.emit(0x0F, 0x30)  // wrmsr
}

func emitX2APICWriteMSR(e *Emitter, msr uint32, valueReg asm.Reg) {
	e.emit(0xB9)
	e.emitUint32(msr)
	emitRegRegMove(e, asm.MustLookup("rax"), valueReg)
	emitRegRegMove(e, asm.MustLookup("rdx"), valueReg)
	emitShiftImm(e, 0x05, asm.MustLookup("rdx"), 32)
	e.emit(0x0F, 0x30)
}

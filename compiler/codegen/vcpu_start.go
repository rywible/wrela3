package codegen

import (
	_ "embed"
	"fmt"
	"sort"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

const apTrampolineBase = 0x8000
const apTrampolineInstallSymbol = "_wrela_method_platform_uefi_types_DelegatedMemory_install_ap_trampoline"

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

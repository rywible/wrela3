package codegen

import (
	"fmt"
	"sort"

	"github.com/ryanwible/wrela3/compiler/ir"
)

const apTrampolineBase = 0x8000

func vcpuStartupData(program *ir.Program) []ir.DataObject {
	if program == nil || len(program.VcpuStarts) == 0 {
		return nil
	}

	plans := append([]ir.VcpuStartPlan{}, program.VcpuStarts...)
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].VcpuID < plans[j].VcpuID
	})

	suffixes := []string{"ready", "entry", "stack_top", "context"}
	out := make([]ir.DataObject, 0, len(plans)*len(suffixes))
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

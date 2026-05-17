package codegen

import (
	"bytes"
	"encoding/binary"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func encodeImm32ForTest(value int64) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, uint32(value))
	return out
}

func compileVcpuStartForTest(t testingT) compiledUnit {
	program := &ir.Program{
		Types: map[string]ir.TypeInfo{},
		VcpuStarts: []ir.VcpuStartPlan{{VcpuID: 1, APICID: 1, SlotLabel: "worker"}},
	}
	ctx := compileContext{types: program.Types, VcpuPlans: vcpuPlanMap(program)}
	e := &Emitter{Labels: map[string]int{}, ctx: ctx}
	emitWaitForVcpuReady(e, 1)
	return compiledUnit{Symbol: "vcpu_start_test", Bytes: e.Code, CallReloc: e.CallReloc}
}

type testingT interface {
	Helper()
}

func bytesContainForTest(haystack, needle []byte) bool {
	return bytes.Contains(haystack, needle)
}

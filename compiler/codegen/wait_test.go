package codegen

import (
	"bytes"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
	"github.com/ryanwible/wrela3/compiler/report"
)

func TestWaitFallbackEmitsStiHlt(t *testing.T) {
	unit := compileWaitFallbackUnitForTest()
	if !bytes.Contains(unit.Bytes, []byte{0xFB, 0xF4}) {
		t.Fatalf("fallback wait must emit sti+hlt: %x", unit.Bytes)
	}
}

func TestTopicWaitFallbackUsesStiHlt(t *testing.T) {
	unit := compileTopicWaitUnitForTest(ir.TopicWait{
		SlotLabel:       "worker",
		UseMonitorMwait: false,
		Fallback:        "sti_hlt",
	})
	if !bytes.Contains(unit.Bytes, []byte{0xFB, 0xF4}) {
		t.Fatalf("topic wait fallback must emit sti+hlt: %x", unit.Bytes)
	}
}

func TestMonitorMwaitBytesAreAvailable(t *testing.T) {
	unit := compileMonitorMwaitUnitForTest()
	for _, want := range [][]byte{
		{0x0F, 0x01, 0xC8},
		{0x0F, 0x01, 0xC9},
	} {
		if !bytes.Contains(unit.Bytes, want) {
			t.Fatalf("monitor/mwait unit missing %x in %x", want, unit.Bytes)
		}
	}
}

func TestTopicWaitUsesMonitorMwaitWhenSelected(t *testing.T) {
	unit := compileTopicWaitUnitForTest(ir.TopicWait{SlotLabel: "worker", UseMonitorMwait: true, Fallback: "sti_hlt"})
	for _, want := range [][]byte{
		{0x0F, 0x01, 0xC8},
		{0x0F, 0x01, 0xC9},
	} {
		if !bytes.Contains(unit.Bytes, want) {
			t.Fatalf("monitor/mwait wait missing %x in %x", want, unit.Bytes)
		}
	}
}

func TestWakeStrategyReportsFallback(t *testing.T) {
	r := report.ImageReport{}
	appendWakePathReport(&r, ir.TopicWait{SlotLabel: "worker", UseMonitorMwait: false, Fallback: "sti_hlt"})
	if len(r.Runtime.WakePaths) != 1 || r.Runtime.WakePaths[0].Fallback != "sti_hlt" {
		t.Fatalf("wake path report = %#v", r.Runtime.WakePaths)
	}
}

func TestWakeStrategyUsesDiscoveredMonitorMwaitFact(t *testing.T) {
	wait := topicWaitFromFeaturesForTest(ir.CpuFeatureFacts{MonitorMwaitAvailable: true})
	if !wait.UseMonitorMwait || wait.Fallback != "sti_hlt" {
		t.Fatalf("wait strategy = %#v, want monitor/mwait with hlt fallback", wait)
	}
}

func TestWakeStrategyReportIncludesMonitorMwaitBranch(t *testing.T) {
	r := report.ImageReport{}
	appendWakePathReport(&r, topicWaitFromFeaturesForTest(ir.CpuFeatureFacts{MonitorMwaitAvailable: true}))
	if len(r.Runtime.WakePaths) != 1 {
		t.Fatalf("wake path report missing: %#v", r.Runtime.WakePaths)
	}
	if r.Runtime.WakePaths[0].Strategy != "monitor_mwait" || r.Runtime.WakePaths[0].Fallback != "sti_hlt" {
		t.Fatalf("wake path report = %#v", r.Runtime.WakePaths)
	}
}

func appendWakePathReport(r *report.ImageReport, wait ir.TopicWait) {
	strategy := "sti_hlt"
	if wait.UseMonitorMwait {
		strategy = "monitor_mwait"
	}
	r.Runtime.WakePaths = append(r.Runtime.WakePaths, report.WakePathReport{
		SlotLabel: wait.SlotLabel,
		Strategy:  strategy,
		Fallback:  wait.Fallback,
	})
}

func topicWaitFromFeaturesForTest(features ir.CpuFeatureFacts) ir.TopicWait {
	wait := ir.TopicWait{SlotLabel: "worker", Fallback: "sti_hlt"}
	if features.MonitorMwaitAvailable {
		wait.UseMonitorMwait = true
	}
	return wait
}

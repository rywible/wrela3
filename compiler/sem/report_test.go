package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/report"
)

func TestBuildImageReport(t *testing.T) {
	checked := &CheckedProgram{
		Index: &Index{Images: []*ast.ImageDecl{{Name: "Hello"}}},
		ImageGraph: ImageGraph{
			MemoryRoots: []MemoryRootNode{{
				Label: "boot.root",
				Base:  0x200000,
				Bytes: 0x1000000,
			}},
			Arenas: []ArenaNode{{
				Label:  "main_arena",
				Parent: "",
				Base:   0x200000,
				Bytes:  0x10000,
				Owner:  "executor",
			}},
		},
	}
	reportImage := BuildImageReport(checked)
	if reportImage.Version != 1 {
		t.Fatalf("Version = %d, want 1", reportImage.Version)
	}
	if reportImage.Image != "Hello" {
		t.Fatalf("Image = %q, want %q", reportImage.Image, "Hello")
	}
	if reportImage.Memory.TotalBytes != 0x1000000 {
		t.Fatalf("TotalBytes = %d, want %d", reportImage.Memory.TotalBytes, 0x1000000)
	}
	if len(reportImage.Memory.RootRegions) != 1 {
		t.Fatalf("RootRegions = %d, want 1", len(reportImage.Memory.RootRegions))
	}
	if reportImage.Memory.RootRegions[0].Label != "boot.root" {
		t.Fatalf("root label = %q, want boot.root", reportImage.Memory.RootRegions[0].Label)
	}
	if len(reportImage.AuthorityAudit.MemoryRoots) != 1 || reportImage.AuthorityAudit.MemoryRoots[0].Kind != "memory_root" {
		t.Fatalf("missing memory root audit: %#v", reportImage.AuthorityAudit.MemoryRoots)
	}
	if len(reportImage.AuthorityAudit.Arenas) != 1 || reportImage.AuthorityAudit.Arenas[0].Kind != "arena" {
		t.Fatalf("missing arena audit: %#v", reportImage.AuthorityAudit.Arenas)
	}
}

func TestImageReportResolvesSeedSlotOwnersToClaimedLabels(t *testing.T) {
	checked := &CheckedProgram{ImageGraph: ImageGraph{
		ExecutorSlots: []ExecutorSlotNode{
			{Label: "console"},
			{Label: "worker"},
		},
		Arenas: []ArenaNode{
			{Label: "console.memory", Owner: "executor_slot.0", Bytes: 4096, Kind: "executor_memory"},
			{Label: "worker.memory", Owner: "executor_slot.1", Bytes: 8192, Kind: "executor_memory"},
			{Label: "orphan.memory", Owner: "executor_slot.2", Bytes: 1024, Kind: "executor_memory"},
		},
		InterruptQueues: []InterruptQueueNode{
			{Label: "irq.serial.rx", Owner: "executor_slot.0", Capacity: 64},
		},
		PlacementConstraints: []PlacementConstraintNode{
			{Kind: "separate_physical_cores", A: "executor_slot.0", B: "executor_slot.1", Required: true},
		},
	}}
	r := BuildImageReport(checked)
	if r.Memory.Arenas[0].Owner != "console" || r.Memory.Arenas[1].Owner != "worker" || r.Memory.Arenas[2].Owner != "executor_slot.2" {
		t.Fatalf("arena owners = %#v", r.Memory.Arenas)
	}
	if r.Memory.ExecutorBudgets[0].SlotLabel != "console" || r.Memory.ExecutorBudgets[1].SlotLabel != "worker" {
		t.Fatalf("executor budgets = %#v", r.Memory.ExecutorBudgets)
	}
	if r.Runtime.InterruptQueues[0].Owner != "console" {
		t.Fatalf("interrupt queue owner = %#v", r.Runtime.InterruptQueues[0])
	}
	if r.Runtime.Placement[0].SubjectA != "console" || r.Runtime.Placement[0].SubjectB != "worker" {
		t.Fatalf("placement report = %#v", r.Runtime.Placement)
	}
}

func TestImageReportIncludesDiscoveryFacts(t *testing.T) {
	checked := &CheckedProgram{ImageGraph: ImageGraph{
		HardwareClaims: []HardwareClaimNode{
			{Kind: "pci_bar", Key: "edu.bar0"},
		},
		APICFacts: []APICFactNode{
			{Mode: "xapic_fallback"},
		},
		TimerFacts: []TimerFactNode{
			{Label: "periodic.1000us", Source: "local_apic_pit_calibrated", PeriodUS: 1000},
		},
		LocalityFacts: []LocalityFactNode{
			{Subject: "cpu0", Kind: "numa_node", Value: "0", Known: false},
		},
		FramebufferFacts: []FramebufferFactNode{
			{Known: false},
		},
		InterruptQueues: []InterruptQueueNode{
			{Label: "irq.serial.rx", Owner: "console", Capacity: 64, Overflow: "drop_newest_and_set_flag"},
		},
	}}
	r := BuildImageReport(checked)
	if len(r.AuthorityAudit.HardwareClaims) != 1 || r.AuthorityAudit.HardwareClaims[0].Owner != "delegated_hardware" {
		t.Fatalf("hardware claims missing from report: %#v", r.AuthorityAudit.HardwareClaims)
	}
	if r.Hardware.APIC.Mode != "xapic_fallback" || r.Hardware.APIC.SelectedAPICMode != 1 {
		t.Fatalf("APIC mode missing from report: %#v", r.Hardware.APIC)
	}
	if len(r.Hardware.Timers) != 1 || r.Hardware.Timers[0].Source != "local_apic_pit_calibrated" {
		t.Fatalf("timer facts missing from report: %#v", r.Hardware.Timers)
	}
	if len(r.Hardware.Locality) != 1 || r.Hardware.Locality[0].Known {
		t.Fatalf("unknown locality fact missing from report: %#v", r.Hardware.Locality)
	}
	if r.Hardware.Framebuffer.Known {
		t.Fatalf("unknown framebuffer fact missing from report: %#v", r.Hardware.Framebuffer)
	}
	if len(r.Runtime.InterruptQueues) != 1 || r.Runtime.InterruptQueues[0].Label != "irq.serial.rx" {
		t.Fatalf("interrupt queues missing from report: %#v", r.Runtime.InterruptQueues)
	}
	if len(r.AuthorityAudit.Queues) != 1 || r.AuthorityAudit.Queues[0].Owner != "console" {
		t.Fatalf("queue audit missing from report: %#v", r.AuthorityAudit.Queues)
	}
}

func TestImageReportIncludesWakePaths(t *testing.T) {
	checked := &CheckedProgram{ImageGraph: ImageGraph{
		WakeTargets: []WakeTargetNode{
			{SlotLabel: "worker", Owner: "timer.periodic", Strategy: "sti_hlt", Fallback: "sti_hlt"},
		},
	}}
	r := BuildImageReport(checked)
	if len(r.Runtime.WakePaths) != 1 || r.Runtime.WakePaths[0].SlotLabel != "worker" || r.Runtime.WakePaths[0].Fallback != "sti_hlt" {
		t.Fatalf("wake paths missing from report: %#v", r.Runtime.WakePaths)
	}
	if len(r.AuthorityAudit.WakeTargets) != 1 || r.AuthorityAudit.WakeTargets[0].Owner != "timer.periodic" {
		t.Fatalf("wake target audit missing from report: %#v", r.AuthorityAudit.WakeTargets)
	}
}

func TestImageReportPreservesMonitorMwaitWakeStrategy(t *testing.T) {
	checked := &CheckedProgram{ImageGraph: ImageGraph{
		WakeTargets: []WakeTargetNode{
			{SlotLabel: "worker", Owner: "timer.periodic", Strategy: "monitor_mwait", Fallback: "sti_hlt"},
		},
	}}
	r := BuildImageReport(checked)
	if len(r.Runtime.WakePaths) != 1 || r.Runtime.WakePaths[0].Strategy != "monitor_mwait" || r.Runtime.WakePaths[0].Fallback != "sti_hlt" {
		t.Fatalf("wake paths missing monitor/mwait strategy: %#v", r.Runtime.WakePaths)
	}
}

func TestAuthorityAuditReportCompleteness(t *testing.T) {
	if !hasCode(ValidateAuthorityAudit(report.ImageReport{}), diag.SEM0075) {
		t.Fatalf("expected SEM0075 for nil authority audit sections")
	}
	r := report.ImageReport{AuthorityAudit: completeEmptyAuthorityAudit()}
	if ds := ValidateAuthorityAudit(r); len(ds) != 0 {
		t.Fatalf("audit diagnostics: %#v", ds)
	}
}

func TestAuthorityAuditRequiresTimerRecordWhenTimerIsUsed(t *testing.T) {
	r := report.ImageReport{
		Hardware: report.HardwareReport{
			Timers: []report.TimerReport{{Label: "periodic.1000us", Source: "local_apic_pit_calibrated", PeriodUS: 1000}},
		},
		AuthorityAudit: completeEmptyAuthorityAudit(),
	}
	if !hasCode(ValidateAuthorityAuditContent(r), diag.SEM0075) {
		t.Fatalf("expected SEM0075 when timer report has no timer authority audit record")
	}
	r.AuthorityAudit.Timers = []report.AuthorityRecord{{Kind: "timer", Label: "periodic.1000us"}}
	if ds := ValidateAuthorityAuditContent(r); len(ds) != 0 {
		t.Fatalf("unexpected content diagnostics: %#v", ds)
	}
}

func TestAuthorityAuditContentRequiresHardwareClaimsInterruptsAndDMABuffers(t *testing.T) {
	tests := []struct {
		name string
		r    report.ImageReport
		fill func(*report.ImageReport)
	}{
		{
			name: "hardware_claims",
			r: report.ImageReport{
				Hardware:       report.HardwareReport{Claims: []report.AuthorityRecord{{Kind: "isa_irq", Label: "4"}}},
				AuthorityAudit: completeEmptyAuthorityAudit(),
			},
			fill: func(r *report.ImageReport) {
				r.AuthorityAudit.HardwareClaims = []report.AuthorityRecord{{Kind: "pci_bar", Label: "edu.bar0"}}
			},
		},
		{
			name: "interrupts",
			r: report.ImageReport{
				Runtime:        report.RuntimeReport{Interrupts: []report.AuthorityRecord{{Kind: "shared_interrupt_source", Label: "serial.rx"}}},
				AuthorityAudit: completeEmptyAuthorityAudit(),
			},
			fill: func(r *report.ImageReport) {
				r.AuthorityAudit.Interrupts = []report.AuthorityRecord{{Kind: "interrupt_route", Label: "serial.rx"}}
			},
		},
		{
			name: "dma_buffers",
			r: report.ImageReport{
				Memory:         report.MemoryReport{Arenas: []report.ArenaReport{{Kind: "dma_buffer", Label: "edu.dma"}}},
				AuthorityAudit: completeEmptyAuthorityAudit(),
			},
			fill: func(r *report.ImageReport) {
				r.AuthorityAudit.Arenas = []report.AuthorityRecord{{Kind: "dma_buffer", Label: "edu.dma"}}
				r.AuthorityAudit.DMABuffers = []report.AuthorityRecord{{Kind: "dma_buffer", Label: "edu.dma"}}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !hasCode(ValidateAuthorityAuditContent(test.r), diag.SEM0075) {
				t.Fatalf("expected SEM0075 for missing %s audit records", test.name)
			}
			test.fill(&test.r)
			if ds := ValidateAuthorityAuditContent(test.r); len(ds) != 0 {
				t.Fatalf("unexpected content diagnostics after filling %s: %#v", test.name, ds)
			}
		})
	}
}

func TestImageReportIncludesCompleteAuthorityAuditMappings(t *testing.T) {
	checked := &CheckedProgram{ImageGraph: ImageGraph{
		InterruptTopicRoutes: []InterruptTopicRouteNode{
			{PathLabel: "serial", TopicLabel: "serial.rx"},
		},
		SharedInterruptSources: []SharedInterruptSourceNode{
			{RouteKey: "isa_irq:4/vector:0x40", SourceLabel: "serial.rx"},
		},
		TimerFacts: []TimerFactNode{
			{Label: "periodic.1000us", Source: "local_apic_pit_calibrated", PeriodUS: 1000},
		},
		Topics: []TopicNode{
			{Label: "timer.periodic", Kind: "timer_tick", PayloadType: "machine.x86_64.topic_payload.TimerTickPayload", PayloadSize: 24, PayloadAlign: 8, Depth: 64},
		},
		WakeTargets: []WakeTargetNode{
			{SlotLabel: "worker", Owner: "timer.periodic", Strategy: "sti_hlt", Fallback: "sti_hlt"},
		},
		DMABuffers: []DMABufferNode{
			{Label: "edu.dma", OwnerDevice: "edu"},
		},
	}}
	r := BuildImageReport(checked)
	if len(r.AuthorityAudit.Interrupts) != 2 {
		t.Fatalf("interrupt audit missing: %#v", r.AuthorityAudit.Interrupts)
	}
	if len(r.AuthorityAudit.Timers) != 1 {
		t.Fatalf("timer audit missing: %#v", r.AuthorityAudit.Timers)
	}
	if len(r.Runtime.Topics) != 1 || len(r.AuthorityAudit.Topics) != 1 {
		t.Fatalf("topic report/audit missing: runtime=%#v audit=%#v", r.Runtime.Topics, r.AuthorityAudit.Topics)
	}
	if len(r.AuthorityAudit.WakeTargets) != 1 {
		t.Fatalf("wake audit missing: %#v", r.AuthorityAudit.WakeTargets)
	}
	if len(r.AuthorityAudit.DMABuffers) != 1 {
		t.Fatalf("DMA audit missing: %#v", r.AuthorityAudit.DMABuffers)
	}
	if ds := append(ValidateAuthorityAudit(r), ValidateAuthorityAuditContent(r)...); len(ds) != 0 {
		t.Fatalf("authority audit diagnostics: %#v", ds)
	}
}

func completeEmptyAuthorityAudit() report.AuthorityAuditReport {
	return report.AuthorityAuditReport{
		MemoryRoots:    []report.AuthorityRecord{},
		Arenas:         []report.AuthorityRecord{},
		HardwareClaims: []report.AuthorityRecord{},
		Interrupts:     []report.AuthorityRecord{},
		Timers:         []report.AuthorityRecord{},
		Queues:         []report.AuthorityRecord{},
		Topics:         []report.AuthorityRecord{},
		WakeTargets:    []report.AuthorityRecord{},
		DMABuffers:     []report.AuthorityRecord{},
	}
}

func TestImageNameForReportDefaultsToImage(t *testing.T) {
	reportImage := BuildImageReport(nil)
	if reportImage.Image != "image" {
		t.Fatalf("Image = %q, want image", reportImage.Image)
	}
}

func TestImageReportWithNilDeclUsesDefaultImageName(t *testing.T) {
	checked := &CheckedProgram{Index: &Index{}, ImageGraph: ImageGraph{}}
	checked.Index.Images = []*ast.ImageDecl{}
	reportImage := BuildImageReport(checked)
	if reportImage.Image != "image" {
		t.Fatalf("Image = %q, want image", reportImage.Image)
	}

	checked = &CheckedProgram{
		Index:      &Index{Images: []*ast.ImageDecl{{}}},
		ImageGraph: ImageGraph{},
	}
	reportImage = BuildImageReport(checked)
	if reportImage.Image != "image" {
		t.Fatalf("Image = %q, want image", reportImage.Image)
	}

	checked = &CheckedProgram{
		Index:      &Index{Images: []*ast.ImageDecl{{Name: ""}}},
		ImageGraph: ImageGraph{},
	}
	reportImage = BuildImageReport(checked)
	if reportImage.Image != "image" {
		t.Fatalf("Image = %q, want image", reportImage.Image)
	}
}

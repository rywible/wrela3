package sem

import (
	"strconv"
	"strings"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/report"
)

func BuildImageReport(checked *CheckedProgram) report.ImageReport {
	r := report.NewImageReport(imageNameForReport(checked))
	if checked == nil {
		return r
	}
	resolveOwner := reportOwnerResolver(checked.ImageGraph)

	for _, root := range checked.ImageGraph.MemoryRoots {
		r.Memory.RootRegions = append(r.Memory.RootRegions, report.MemoryRootReport{
			Label: root.Label,
			Base:  root.Base,
			Bytes: root.Bytes,
		})
		r.Memory.TotalBytes += root.Bytes
		r.AuthorityAudit.MemoryRoots = append(r.AuthorityAudit.MemoryRoots, report.AuthorityRecord{
			Kind:       "memory_root",
			Label:      root.Label,
			Provenance: "firmware",
		})
	}

	for _, arena := range checked.ImageGraph.Arenas {
		r.Memory.Arenas = append(r.Memory.Arenas, report.ArenaReport{
			Label:  arena.Label,
			Parent: arena.Parent,
			Base:   arena.Base,
			Bytes:  arena.Bytes,
			Owner:  resolveOwner(arena.Owner),
			Kind:   arena.Kind,
		})
		r.AuthorityAudit.Arenas = append(r.AuthorityAudit.Arenas, report.AuthorityRecord{
			Kind:  "arena",
			Label: arena.Label,
			Owner: resolveOwner(arena.Owner),
		})
	}
	appendDiscoveryFacts(&r, checked.ImageGraph)
	appendExecutorMemoryAndLocality(&r, checked.ImageGraph, resolveOwner)
	appendWakePaths(&r, checked.ImageGraph)
	appendRuntimeFacts(&r, checked.ImageGraph, resolveOwner)
	appendInterruptsToAudit(&r, checked.ImageGraph)
	appendTimersToAudit(&r, checked.ImageGraph)
	appendTopicsToReportAndAudit(&r, checked.ImageGraph)
	appendDMABuffersToAudit(&r, checked.ImageGraph)

	return r
}

func appendDiscoveryFacts(r *report.ImageReport, g ImageGraph) {
	for _, claim := range g.HardwareClaims {
		record := report.AuthorityRecord{
			Kind:  claim.Kind,
			Label: claim.Key,
			Owner: "delegated_hardware",
		}
		r.Hardware.Claims = append(r.Hardware.Claims, record)
		r.AuthorityAudit.HardwareClaims = append(r.AuthorityAudit.HardwareClaims, record)
		appendPCIClaimReport(r, claim)
	}
	for _, fact := range g.APICFacts {
		r.Hardware.APIC.Mode = fact.Mode
		r.Hardware.APIC.Required = fact.Required
		r.Hardware.APIC.Fallback = fact.Fallback
	}
	for _, timer := range g.TimerFacts {
		r.Hardware.Timers = append(r.Hardware.Timers, report.TimerReport{
			Label:    timer.Label,
			Source:   timer.Source,
			PeriodUS: timer.PeriodUS,
		})
	}
	for _, locality := range g.LocalityFacts {
		r.Hardware.Locality = append(r.Hardware.Locality, report.LocalityReport{
			Subject: locality.Subject,
			Kind:    locality.Kind,
			Value:   locality.Value,
			Known:   locality.Known,
		})
	}
	for _, framebuffer := range g.FramebufferFacts {
		r.Hardware.Framebuffer = report.FramebufferReport{
			Base:   framebuffer.Base,
			Bytes:  framebuffer.Bytes,
			Width:  framebuffer.Width,
			Height: framebuffer.Height,
			Stride: framebuffer.Stride,
			Format: framebuffer.Format,
			Known:  framebuffer.Known,
		}
	}
}

func appendPCIClaimReport(r *report.ImageReport, claim HardwareClaimNode) {
	if !strings.HasPrefix(claim.Kind, "pci_") {
		return
	}
	identity := claim.Key
	barIndex := uint64(0)
	hasBAR := false
	if claim.Kind == "pci_bar" {
		if before, after, ok := strings.Cut(claim.Key, "."); ok {
			identity = before
			if parsed, err := strconv.ParseUint(after, 0, 8); err == nil {
				barIndex = parsed
				hasBAR = true
			}
		}
	}
	idx := -1
	for i := range r.Hardware.PCI {
		if r.Hardware.PCI[i].Identity == identity {
			idx = i
			break
		}
	}
	if idx == -1 {
		r.Hardware.PCI = append(r.Hardware.PCI, report.PCIReport{Identity: identity, BARs: []report.BARReport{}})
		idx = len(r.Hardware.PCI) - 1
	}
	if hasBAR {
		r.Hardware.PCI[idx].BARs = append(r.Hardware.PCI[idx].BARs, report.BARReport{
			Index: uint8(barIndex),
			Kind:  claim.Kind,
		})
	}
}

func appendRuntimeFacts(r *report.ImageReport, g ImageGraph, resolveOwner func(string) string) {
	for _, queue := range g.InterruptQueues {
		r.Runtime.InterruptQueues = append(r.Runtime.InterruptQueues, report.InterruptQueueReport{
			Label:    queue.Label,
			Owner:    resolveOwner(queue.Owner),
			Capacity: queue.Capacity,
			Overflow: queue.Overflow,
		})
		r.AuthorityAudit.Queues = append(r.AuthorityAudit.Queues, report.AuthorityRecord{
			Kind:  "interrupt_queue",
			Label: queue.Label,
			Owner: resolveOwner(queue.Owner),
		})
	}
}

func appendExecutorMemoryAndLocality(r *report.ImageReport, g ImageGraph, resolveOwner func(string) string) {
	for _, arena := range g.Arenas {
		if arena.Kind != "executor_memory" {
			continue
		}
		r.Memory.ExecutorBudgets = append(r.Memory.ExecutorBudgets, report.ExecutorBudgetReport{
			SlotLabel: resolveOwner(arena.Owner),
			Bytes:     arena.Bytes,
		})
	}
	for _, constraint := range g.PlacementConstraints {
		r.Runtime.Placement = append(r.Runtime.Placement, report.PlacementReport{
			Kind:      constraint.Kind,
			SubjectA:  resolveOwner(constraint.A),
			SubjectB:  resolveOwner(constraint.B),
			Required:  constraint.Required,
			Satisfied: constraint.Satisfied,
			Fallback:  constraint.Fallback,
		})
	}
	for _, placement := range g.PlacementDecisions {
		r.Runtime.Placement = append(r.Runtime.Placement, report.PlacementReport{
			Kind:      "cpu_for_slot",
			SubjectA:  resolveOwner(placement.SlotLabel),
			SubjectB:  placement.Target,
			Required:  false,
			Satisfied: placement.Satisfied,
			Fallback:  placement.Fallback,
		})
	}
}

func appendWakePaths(r *report.ImageReport, g ImageGraph) {
	for _, wake := range g.WakeTargets {
		r.Runtime.WakePaths = append(r.Runtime.WakePaths, report.WakePathReport{
			SlotLabel: wake.SlotLabel,
			Strategy:  wake.Strategy,
			Fallback:  wake.Fallback,
		})
		r.AuthorityAudit.WakeTargets = append(r.AuthorityAudit.WakeTargets, report.AuthorityRecord{
			Kind:  "wake_target",
			Label: wake.SlotLabel,
			Owner: wake.Owner,
		})
	}
}

func appendInterruptsToAudit(r *report.ImageReport, g ImageGraph) {
	for _, route := range g.InterruptTopicRoutes {
		record := report.AuthorityRecord{
			Kind:  "interrupt_route",
			Label: route.PathLabel,
			Owner: route.TopicLabel,
		}
		r.Runtime.Interrupts = append(r.Runtime.Interrupts, record)
		r.AuthorityAudit.Interrupts = append(r.AuthorityAudit.Interrupts, record)
	}
	for _, source := range g.SharedInterruptSources {
		record := report.AuthorityRecord{
			Kind:  "shared_interrupt_source",
			Label: source.SourceLabel,
			Owner: source.RouteKey,
		}
		r.Runtime.Interrupts = append(r.Runtime.Interrupts, record)
		r.AuthorityAudit.Interrupts = append(r.AuthorityAudit.Interrupts, record)
	}
}

func appendTimersToAudit(r *report.ImageReport, g ImageGraph) {
	seen := map[string]bool{}
	appendTimer := func(label, owner string) {
		if label == "" || seen[label] {
			return
		}
		seen[label] = true
		r.AuthorityAudit.Timers = append(r.AuthorityAudit.Timers, report.AuthorityRecord{
			Kind:  "timer",
			Label: label,
			Owner: owner,
		})
	}
	for _, timer := range g.TimerFacts {
		appendTimer(timer.Label, timer.Source)
	}
	for _, route := range g.TimerRoutes {
		appendTimer(route.Label, route.Source)
	}
}

func appendTopicsToReportAndAudit(r *report.ImageReport, g ImageGraph) {
	for _, topic := range g.Topics {
		r.Runtime.Topics = append(r.Runtime.Topics, report.TopicReport{
			Label:       topic.Label,
			PayloadType: reportPayloadType(topic.PayloadType),
			Bytes:       topic.PayloadSize,
			Align:       topic.PayloadAlign,
			Depth:       topic.Depth,
		})
		r.AuthorityAudit.Topics = append(r.AuthorityAudit.Topics, report.AuthorityRecord{
			Kind:  topic.Kind,
			Label: topic.Label,
			Owner: "topic_graph",
		})
	}
}

func reportPayloadType(payload string) string {
	if idx := strings.LastIndex(payload, "."); idx >= 0 {
		return payload[idx+1:]
	}
	return payload
}

func appendDMABuffersToAudit(r *report.ImageReport, g ImageGraph) {
	for _, dma := range g.DMABuffers {
		r.AuthorityAudit.DMABuffers = append(r.AuthorityAudit.DMABuffers, report.AuthorityRecord{
			Kind:  "dma_buffer",
			Label: dma.Label,
			Owner: dma.OwnerDevice,
		})
	}
}

func ValidateAuthorityAudit(r report.ImageReport) []diag.Diagnostic {
	var ds []diag.Diagnostic
	require := func(ok bool, name string) {
		if !ok {
			ds = append(ds, diag.Diagnostic{Phase: "sem", Code: diag.SEM0075, Severity: diag.Error, Message: "authority audit report missing " + name})
		}
	}
	require(r.AuthorityAudit.MemoryRoots != nil, "memory_roots")
	require(r.AuthorityAudit.Arenas != nil, "arenas")
	require(r.AuthorityAudit.HardwareClaims != nil, "hardware_claims")
	require(r.AuthorityAudit.Interrupts != nil, "interrupts")
	require(r.AuthorityAudit.Timers != nil, "timers")
	require(r.AuthorityAudit.Queues != nil, "queues")
	require(r.AuthorityAudit.Topics != nil, "topics")
	require(r.AuthorityAudit.WakeTargets != nil, "wake_targets")
	require(r.AuthorityAudit.DMABuffers != nil, "dma_buffers")
	return ds
}

func ValidateAuthorityAuditContent(r report.ImageReport) []diag.Diagnostic {
	var ds []diag.Diagnostic
	requireRecord := func(records []report.AuthorityRecord, name string, reportUses bool) {
		if reportUses && len(records) == 0 {
			ds = append(ds, diag.Diagnostic{Phase: "sem", Code: diag.SEM0075, Severity: diag.Error, Message: "authority audit report missing records for " + name})
		}
	}
	requireRecord(r.AuthorityAudit.MemoryRoots, "memory_roots", len(r.Memory.RootRegions) != 0)
	requireRecord(r.AuthorityAudit.Arenas, "arenas", len(r.Memory.Arenas) != 0)
	requireRecord(r.AuthorityAudit.HardwareClaims, "hardware_claims", len(r.Hardware.Claims) != 0 || len(r.Hardware.PCI) != 0)
	requireRecord(r.AuthorityAudit.Interrupts, "interrupts", len(r.Runtime.Interrupts) != 0)
	requireRecord(r.AuthorityAudit.Timers, "timers", len(r.Hardware.Timers) != 0)
	requireRecord(r.AuthorityAudit.Queues, "queues", len(r.Runtime.InterruptQueues) != 0)
	requireRecord(r.AuthorityAudit.Topics, "topics", len(r.Runtime.Topics) != 0)
	requireRecord(r.AuthorityAudit.WakeTargets, "wake_targets", len(r.Runtime.WakePaths) != 0)
	requireRecord(r.AuthorityAudit.DMABuffers, "dma_buffers", reportHasArenaKind(r.Memory.Arenas, "dma_buffer"))
	return ds
}

func reportOwnerResolver(g ImageGraph) func(string) string {
	seedLabels := map[string]string{}
	for i, slot := range g.ExecutorSlots {
		if slot.Label != "" {
			seedLabels["executor_slot."+strconv.Itoa(i)] = slot.Label
		}
	}
	return func(owner string) string {
		if label := seedLabels[owner]; label != "" {
			return label
		}
		return owner
	}
}

func reportHasArenaKind(arenas []report.ArenaReport, kind string) bool {
	for _, arena := range arenas {
		if arena.Kind == kind {
			return true
		}
	}
	return false
}

func imageNameForReport(checked *CheckedProgram) string {
	if checked == nil || checked.Index == nil || len(checked.Index.Images) == 0 {
		return "image"
	}
	if checked.Index.Images[0] == nil {
		return "image"
	}
	if checked.Index.Images[0].Name != "" {
		return checked.Index.Images[0].Name
	}
	return "image"
}

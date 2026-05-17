package sem

import "github.com/ryanwible/wrela3/compiler/report"

func BuildImageReport(checked *CheckedProgram) report.ImageReport {
	r := report.NewImageReport(imageNameForReport(checked))
	if checked == nil {
		return r
	}

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
			Owner:  arena.Owner,
		})
		r.AuthorityAudit.Arenas = append(r.AuthorityAudit.Arenas, report.AuthorityRecord{
			Kind:  "arena",
			Label: arena.Label,
			Owner: arena.Owner,
		})
	}
	appendDiscoveryFacts(&r, checked.ImageGraph)
	appendRuntimeFacts(&r, checked.ImageGraph)

	return r
}

func appendDiscoveryFacts(r *report.ImageReport, g ImageGraph) {
	for _, claim := range g.HardwareClaims {
		r.AuthorityAudit.HardwareClaims = append(r.AuthorityAudit.HardwareClaims, report.AuthorityRecord{
			Kind:  claim.Kind,
			Label: claim.Key,
			Owner: "delegated_hardware",
		})
	}
	for _, fact := range g.APICFacts {
		r.Hardware.APIC.Mode = fact.Mode
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

func appendRuntimeFacts(r *report.ImageReport, g ImageGraph) {
	for _, queue := range g.InterruptQueues {
		r.Runtime.InterruptQueues = append(r.Runtime.InterruptQueues, report.InterruptQueueReport{
			Label:    queue.Label,
			Owner:    queue.Owner,
			Capacity: queue.Capacity,
			Overflow: queue.Overflow,
		})
		r.AuthorityAudit.Queues = append(r.AuthorityAudit.Queues, report.AuthorityRecord{
			Kind:  "interrupt_queue",
			Label: queue.Label,
			Owner: queue.Owner,
		})
	}
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

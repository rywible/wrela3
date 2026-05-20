package sem

import (
	"strconv"
	"strings"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/nvmefmt"
	"github.com/ryanwible/wrela3/compiler/report"
	"github.com/ryanwible/wrela3/compiler/storagefmt"
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
	appendStorageFacts(&r, checked)
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
		r.Hardware.APIC.SelectedAPICMode = selectedAPICModeValue(fact.Mode)
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

func selectedAPICModeValue(mode string) uint32 {
	if mode == "x2apic_preferred" || mode == "x2apic_required" || mode == "x2apic_with_xapic_fallback" {
		return 2
	}
	if mode != "" {
		return 1
	}
	return 0
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
	for _, placement := range g.VcpuPlacements {
		r.Runtime.Executors = append(r.Runtime.Executors, report.ExecutorReport{
			SlotLabel: resolveOwner(placement.SlotLabel),
			VcpuID:    uint64(placement.VcpuID),
		})
	}
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
			Type:        topic.Type,
			TypeKey:     topic.TypeKey,
			PayloadType: reportPayloadType(topic.PayloadType),
			PayloadKey:  topic.PayloadKey,
			NextType:    topic.NextType,
			NextKey:     topic.NextKey,
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

func appendStorageFacts(r *report.ImageReport, checked *CheckedProgram) {
	if checked == nil || !imageUsesStorage(checked) {
		return
	}
	g := checked.ImageGraph
	metrics := storageMetricsFacts(checked)
	r.Storage.MetricFacts = storageMetricFactSet(metrics)
	activeLBASize := storageMetric(metrics, "active_lba_size")
	r.Storage.ActiveLBASize = activeLBASize
	r.Storage.NamespaceMode = "conventional"
	r.Storage.DurabilityMode = storageDurabilityModeForReport(checked, activeLBASize)
	r.Storage.InterruptMode, r.Storage.MsiFallbackSharesVector = storageInterruptModeForReport(g)
	r.Storage.EventSlotSize = storageMetricOrDefault(metrics, "event_slot_size", storagefmt.EventSlotSize)
	r.Storage.EventHeaderSize = storageMetricOrDefault(metrics, "event_header_size", storagefmt.EventHeaderSize)
	r.Storage.EventPayloadBytes = storageMetricOrDefault(metrics, "event_payload_bytes", storagefmt.EventPayloadBytes)
	r.Storage.EventSlotsWritten = storageMetricOrDefault(metrics, "event_slots_written", uint64(len(g.StorageAppendCalls)))
	r.Storage.EventSlotsReservedEmpty = storageMetricOrDefault(metrics, "event_slots_reserved_empty", storagefmt.FinishBatch(r.Storage.ActiveLBASize, storagefmt.StorageTargetBatchSlots).ReservedEmptySlots)
	r.Storage.ReservedEmptySlots = r.Storage.EventSlotsReservedEmpty
	r.Storage.EventSlotsRecovered = storageMetricOrDefault(metrics, "event_slots_recovered", 0)
	r.Storage.TargetBatchSlots = storageMetricOrDefault(metrics, "target_batch_slots", storagefmt.StorageTargetBatchSlots)
	r.Storage.MaxOverflowSlots = storagefmt.StorageMaxOverflowSlots
	r.Storage.MaxBatchSlots = storageMetricOrDefault(metrics, "max_batch_slots", storagefmt.StorageMaxBatchSlots)
	r.Storage.MaxAtomicGroupSlots = storagefmt.StorageMaxAtomicGroupSlots
	r.Storage.BatchesSubmitted = storageMetricOrDefault(metrics, "batches_submitted", uint64(len(g.StorageAppendCalls)))
	r.Storage.BatchOverflowCount = storageMetricOrDefault(metrics, "batch_overflow_count", 0)
	r.Storage.AppendLatencyP50US = storageMetric(metrics, "append_latency_us")
	r.Storage.AppendLatencyP99US = storageMetric(metrics, "durability_latency_us")
	r.Storage.DeviceReportedMediaWrites = storageMetric(metrics, "device_media_write_commands")
	if r.Storage.EventSlotsWritten == 0 {
		r.Storage.EventSlotsWritten = r.Storage.DeviceReportedMediaWrites
	}
	if r.Storage.BatchesSubmitted == 0 {
		r.Storage.BatchesSubmitted = r.Storage.DeviceReportedMediaWrites
	}
	r.Storage.MediaWriteBytes = storageMetric(metrics, "device_media_write_bytes")
	r.Storage.BlobOrphanBytes = storageMetric(metrics, "blob_orphan_bytes")
	r.Storage.AdminQueueDepth = storageMetricOrDefault(metrics, "admin_queue_depth", storagefmt.StorageAdminQueueDepth)
	r.Storage.ForegroundIOQueueDepth = storageMetricOrDefault(metrics, "foreground_io_queue_depth", storagefmt.StorageForegroundIOQueueDepth)
	r.Storage.BackgroundIOQueueDepth = storageMetricOrDefault(metrics, "background_io_queue_depth", storagefmt.StorageBackgroundIOQueueDepth)
	r.Storage.ProjectionLagEvents = storageMetric(metrics, "projection_lag_events")
	r.Storage.EventUpcastCount = storageMetricOrDefault(metrics, "event_upcast_count", storageEventUpcastCount(checked.Storage))
	r.Storage.ProjectionUpcastCount = storageMetric(metrics, "projection_upcast_count")
	r.Storage.ProjectionRebuildCount = storageMetric(metrics, "projection_rebuild_count")
	r.Storage.StreamDirectoryCacheHits = storageMetricOrDefault(metrics, "stream_directory_cache_hits", 1)
	r.Storage.StreamDirectoryCacheMisses = storageMetricOrDefault(metrics, "stream_directory_cache_misses", 0)
	r.Storage.StreamDirectoryCacheHitRateX1000 = storageCacheHitRateX1000(metrics)
	r.Storage.CoreLinkCommittedGroups = storageMetric(metrics, "core_link_committed_groups")
	r.Storage.CoreLinkBackpressureCount = storageMetric(metrics, "core_link_backpressure_count")
	for _, path := range g.StoragePaths {
		queueID := path.QueueID
		vector := path.Vector
		switch path.Role {
		case "foreground":
			queueID = uint16(storageMetricOrDefault(metrics, "foreground_path_queue_id", uint64(queueID)))
			vector = uint8(storageMetricOrDefault(metrics, "foreground_path_vector", uint64(vector)))
		case "background":
			queueID = uint16(storageMetricOrDefault(metrics, "background_path_queue_id", uint64(queueID)))
			vector = uint8(storageMetricOrDefault(metrics, "background_path_vector", uint64(vector)))
		}
		r.Storage.NvmePaths = append(r.Storage.NvmePaths, report.NvmePathReport{
			Label:      path.Label,
			Role:       path.Role,
			Owner:      path.Owner,
			QueueID:    queueID,
			Vector:     vector,
			QueueDepth: storagePathQueueDepth(path.Role, metrics),
		})
	}
	for _, endpoint := range g.CoreLinkEndpoints {
		r.Storage.CoreLinks = append(r.Storage.CoreLinks, report.CoreLinkReport{
			Label:     endpoint.Label,
			Direction: endpoint.Direction,
			Role:      endpoint.Role,
			Owner:     endpoint.Owner,
			Peer:      endpoint.Peer,
			Depth:     endpoint.Depth,
		})
	}
}

func storageMetricOrDefault(metrics map[string]uint64, name string, fallback uint64) uint64 {
	if value, ok := metrics[name]; ok {
		return value
	}
	return fallback
}

func storageMetric(metrics map[string]uint64, name string) uint64 {
	return storageMetricOrDefault(metrics, name, 0)
}

func storageMetricFactSet(metrics map[string]uint64) map[string]bool {
	facts := map[string]bool{}
	for name := range metrics {
		facts[name] = true
	}
	return facts
}

func storageCacheHitRateX1000(metrics map[string]uint64) uint64 {
	if ppm, ok := metrics["stream_directory_cache_hit_rate_ppm"]; ok {
		return ppm / 1000
	}
	hits, hasHits := metrics["stream_directory_cache_hits"]
	misses, hasMisses := metrics["stream_directory_cache_misses"]
	if hasHits || hasMisses {
		total := hits + misses
		if total == 0 {
			return 0
		}
		return hits * 1000 / total
	}
	return 1000
}

func storageMetricsFacts(checked *CheckedProgram) map[string]uint64 {
	facts := map[string]uint64{}
	if checked == nil {
		return facts
	}
	var visitExpr func(string, ast.Expr)
	var visitStmts func(string, []ast.Stmt)
	visitExpr = func(moduleName string, expr ast.Expr) {
		if expr == nil {
			return
		}
		switch e := expr.(type) {
		case *ast.ConstructorExpr:
			if e.Type.Name == "StorageMetrics" {
				for _, arg := range e.Args {
					if value, ok := storageMetricUint(checked, moduleName, arg.Value); ok {
						facts[arg.Name] = value
					}
				}
			}
			for _, arg := range e.Args {
				visitExpr(moduleName, arg.Value)
			}
		case *ast.VariantConstructorExpr:
			for _, arg := range e.Args {
				visitExpr(moduleName, arg.Value)
			}
		case *ast.CallExpr:
			visitExpr(moduleName, e.Receiver)
			for _, arg := range e.Args {
				visitExpr(moduleName, arg.Value)
			}
		case *ast.FieldExpr:
			visitExpr(moduleName, e.Base)
		case *ast.BinaryExpr:
			visitExpr(moduleName, e.Left)
			visitExpr(moduleName, e.Right)
		}
	}
	visitStmts = func(moduleName string, stmts []ast.Stmt) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.LetStmt:
				visitExpr(moduleName, s.Expr)
			case *ast.ReturnStmt:
				visitExpr(moduleName, s.Value)
			case *ast.IfStmt:
				visitExpr(moduleName, s.Cond)
				visitStmts(moduleName, s.Then)
				visitStmts(moduleName, s.Else)
			case *ast.IfLetStmt:
				visitExpr(moduleName, s.Value)
				visitStmts(moduleName, s.Body)
			case *ast.MatchStmt:
				visitExpr(moduleName, s.Value)
				for _, arm := range s.Arms {
					visitStmts(moduleName, arm.Body)
				}
			case *ast.WhileStmt:
				visitExpr(moduleName, s.Cond)
				visitStmts(moduleName, s.Body)
			case *ast.WithStmt:
				visitExpr(moduleName, s.Expr)
				visitStmts(moduleName, s.Body)
			case *ast.ForStmt:
				visitExpr(moduleName, s.InExpr)
				visitStmts(moduleName, s.Body)
			case *ast.AssignStmt:
				visitExpr(moduleName, s.Value)
			case *ast.ExprStmt:
				visitExpr(moduleName, s.Expr)
			}
		}
	}
	for _, mod := range checked.Modules {
		for _, decl := range mod.Decls {
			switch d := decl.(type) {
			case *ast.DataDecl:
				for _, method := range d.Methods {
					visitStmts(mod.Name, method.Body)
				}
			case *ast.ClassDecl:
				for _, method := range d.Methods {
					visitStmts(mod.Name, method.Body)
				}
			case *ast.DriverDecl:
				for _, method := range d.Methods {
					visitStmts(mod.Name, method.Body)
				}
			case *ast.ExecutorDecl:
				for _, method := range d.Methods {
					visitStmts(mod.Name, method.Body)
				}
				for _, handler := range d.OnHandlers {
					visitStmts(mod.Name, handler.Body)
				}
			case *ast.ImageDecl:
				for _, phase := range d.Phases {
					visitStmts(mod.Name, phase.Body)
				}
			}
		}
	}
	return facts
}

func storageMetricUint(checked *CheckedProgram, moduleName string, expr ast.Expr) (uint64, bool) {
	switch e := expr.(type) {
	case *ast.IntLiteral:
		value, err := strconv.ParseUint(e.Value, 0, 64)
		return value, err == nil
	case *ast.NameExpr:
		if checked.Index == nil {
			return 0, false
		}
		value, ok := checked.Index.LookupConst(moduleName, e.Name)
		if !ok || value.Type == nil {
			return 0, false
		}
		return value.Value, true
	case *ast.FieldExpr:
		return storagePathVectorValue(e)
	case *ast.CallExpr:
		return storageMetricCallUint(checked, e)
	}
	return 0, false
}

func storageMetricCallUint(checked *CheckedProgram, call *ast.CallExpr) (uint64, bool) {
	if call == nil || len(call.Args) != 0 {
		return 0, false
	}
	switch call.Method {
	case "first_append_reserved_empty_slots":
		return storagefmt.FinishBatch(512, storagefmt.StorageTargetBatchSlots).ReservedEmptySlots, true
	case "first_append_durability_mode_value":
		mode, err := nvmefmt.SelectDurability(nvmefmt.NamespaceFacts{
			LogicalBlockSize: 512,
			SupportsFUA:      nvmeIdentifyControllerSupportsFUA(checked),
		})
		if err != nil {
			return 0, false
		}
		if mode.Mode == nvmefmt.DurabilityFUA {
			return 1, true
		}
		if mode.Mode == nvmefmt.DurabilityWritePlusFlush {
			return 3, true
		}
		return 0, false
	default:
		return 0, false
	}
}

func storageDurabilityModeForReport(checked *CheckedProgram, activeLBASize uint64) string {
	if mode, ok := storageMetricsSelectedDurabilityMode(checked, activeLBASize); ok {
		return mode
	}
	mode, err := nvmefmt.SelectDurability(nvmefmt.NamespaceFacts{LogicalBlockSize: activeLBASize, SupportsFUA: true})
	if err != nil {
		return ""
	}
	return strings.ToLower(mode.Mode)
}

func storageInterruptModeForReport(g ImageGraph) (string, bool) {
	for _, claim := range g.HardwareClaims {
		if claim.Kind == "pci_nvme_interrupts" {
			return "msix_or_multimessage_msi", false
		}
	}
	for _, route := range g.InterruptConfigurators {
		if route.TopicKind == "nvme_completion" {
			return "msix_or_multimessage_msi", false
		}
	}
	if len(g.StoragePaths) != 0 {
		return "planned_vectors", false
	}
	return "", false
}

func storageMetricsSelectedDurabilityMode(checked *CheckedProgram, activeLBASize uint64) (string, bool) {
	if checked == nil {
		return "", false
	}
	var selected string
	var found bool
	record := func(moduleName string, expr ast.Expr) {
		if found {
			return
		}
		switch e := expr.(type) {
		case *ast.IntLiteral:
			if mode, ok := storageDurabilityModeNameForValue(e.Value); ok {
				selected, found = mode, true
			}
		case *ast.NameExpr:
			if value, ok := storageMetricUint(checked, moduleName, e); ok {
				if mode, ok := storageDurabilityModeNameForValue(strconv.FormatUint(value, 10)); ok {
					selected, found = mode, true
				}
			}
		case *ast.CallExpr:
			if e.Method == "first_append_durability_mode_value" {
				mode, err := nvmefmt.SelectDurability(nvmefmt.NamespaceFacts{
					LogicalBlockSize: activeLBASize,
					SupportsFUA:      nvmeIdentifyControllerSupportsFUA(checked),
				})
				if err == nil {
					selected, found = strings.ToLower(mode.Mode), true
				}
			}
		}
	}

	var visitExpr func(string, ast.Expr)
	var visitStmts func(string, []ast.Stmt)
	visitExpr = func(moduleName string, expr ast.Expr) {
		if expr == nil || found {
			return
		}
		switch e := expr.(type) {
		case *ast.ConstructorExpr:
			if e.Type.Name == "StorageMetrics" {
				for _, arg := range e.Args {
					if arg.Name == "selected_durability_mode" {
						record(moduleName, arg.Value)
						return
					}
				}
			}
			for _, arg := range e.Args {
				visitExpr(moduleName, arg.Value)
			}
		case *ast.VariantConstructorExpr:
			for _, arg := range e.Args {
				visitExpr(moduleName, arg.Value)
			}
		case *ast.CallExpr:
			visitExpr(moduleName, e.Receiver)
			for _, arg := range e.Args {
				visitExpr(moduleName, arg.Value)
			}
		case *ast.FieldExpr:
			visitExpr(moduleName, e.Base)
		case *ast.BinaryExpr:
			visitExpr(moduleName, e.Left)
			visitExpr(moduleName, e.Right)
		}
	}
	visitStmts = func(moduleName string, stmts []ast.Stmt) {
		for _, stmt := range stmts {
			if found {
				return
			}
			switch s := stmt.(type) {
			case *ast.LetStmt:
				visitExpr(moduleName, s.Expr)
			case *ast.ReturnStmt:
				visitExpr(moduleName, s.Value)
			case *ast.IfStmt:
				visitExpr(moduleName, s.Cond)
				visitStmts(moduleName, s.Then)
				visitStmts(moduleName, s.Else)
			case *ast.IfLetStmt:
				visitExpr(moduleName, s.Value)
				visitStmts(moduleName, s.Body)
			case *ast.MatchStmt:
				visitExpr(moduleName, s.Value)
				for _, arm := range s.Arms {
					visitStmts(moduleName, arm.Body)
				}
			case *ast.WhileStmt:
				visitExpr(moduleName, s.Cond)
				visitStmts(moduleName, s.Body)
			case *ast.WithStmt:
				visitExpr(moduleName, s.Expr)
				visitStmts(moduleName, s.Body)
			case *ast.ForStmt:
				visitExpr(moduleName, s.InExpr)
				visitStmts(moduleName, s.Body)
			case *ast.AssignStmt:
				visitExpr(moduleName, s.Value)
			case *ast.ExprStmt:
				visitExpr(moduleName, s.Expr)
			}
		}
	}
	for _, mod := range checked.Modules {
		for _, decl := range mod.Decls {
			switch d := decl.(type) {
			case *ast.DataDecl:
				for _, method := range d.Methods {
					visitStmts(mod.Name, method.Body)
				}
			case *ast.ClassDecl:
				for _, method := range d.Methods {
					visitStmts(mod.Name, method.Body)
				}
			case *ast.DriverDecl:
				for _, method := range d.Methods {
					visitStmts(mod.Name, method.Body)
				}
			case *ast.ExecutorDecl:
				for _, method := range d.Methods {
					visitStmts(mod.Name, method.Body)
				}
				for _, handler := range d.OnHandlers {
					visitStmts(mod.Name, handler.Body)
				}
			case *ast.ImageDecl:
				for _, phase := range d.Phases {
					visitStmts(mod.Name, phase.Body)
				}
			}
			if found {
				return selected, true
			}
		}
	}
	return "", false
}

func storageDurabilityModeNameForValue(value string) (string, bool) {
	mode, err := strconv.ParseUint(value, 0, 64)
	if err != nil {
		return "", false
	}
	switch mode {
	case 1:
		return strings.ToLower(nvmefmt.DurabilityFUA), true
	case 2:
		return "pfail_atomic_fua", true
	case 3:
		return strings.ToLower(nvmefmt.DurabilityWritePlusFlush), true
	default:
		return "", false
	}
}

func nvmeIdentifyControllerSupportsFUA(checked *CheckedProgram) bool {
	if checked == nil || len(checked.Modules) == 0 {
		return true
	}
	for _, mod := range checked.Modules {
		if mod.Name != "machine.x86_64.nvme" {
			continue
		}
		for _, decl := range mod.Decls {
			if d, ok := decl.(*ast.ClassDecl); ok && d.Name == "NvmeDirectStorage" {
				for _, method := range d.Methods {
					if method.Name == "identify_controller" {
						if value, ok := constructorBoolReturn(method.Body, "NvmeControllerFacts", "supports_fua"); ok {
							return value
						}
					}
				}
			}
		}
		return false
	}
	return false
}

func constructorBoolReturn(stmts []ast.Stmt, constructorName string, fieldName string) (bool, bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			if ctor, ok := s.Value.(*ast.ConstructorExpr); ok && ctor.Type.Name == constructorName {
				for _, arg := range ctor.Args {
					if arg.Name == fieldName {
						if literal, ok := arg.Value.(*ast.BoolLiteral); ok {
							return literal.Value, true
						}
					}
				}
			}
		case *ast.IfStmt:
			if value, ok := constructorBoolReturn(s.Then, constructorName, fieldName); ok {
				return value, true
			}
			if value, ok := constructorBoolReturn(s.Else, constructorName, fieldName); ok {
				return value, true
			}
		case *ast.IfLetStmt:
			if value, ok := constructorBoolReturn(s.Body, constructorName, fieldName); ok {
				return value, true
			}
		case *ast.MatchStmt:
			for _, arm := range s.Arms {
				if value, ok := constructorBoolReturn(arm.Body, constructorName, fieldName); ok {
					return value, true
				}
			}
		case *ast.WhileStmt:
			if value, ok := constructorBoolReturn(s.Body, constructorName, fieldName); ok {
				return value, true
			}
		case *ast.WithStmt:
			if value, ok := constructorBoolReturn(s.Body, constructorName, fieldName); ok {
				return value, true
			}
		case *ast.ForStmt:
			if value, ok := constructorBoolReturn(s.Body, constructorName, fieldName); ok {
				return value, true
			}
		}
	}
	return false, false
}

func imageUsesStorage(checked *CheckedProgram) bool {
	if checked == nil {
		return false
	}
	return len(checked.ImageGraph.StoragePaths) != 0 ||
		len(checked.ImageGraph.CoreLinkEndpoints) != 0 ||
		len(checked.ImageGraph.ProjectionFeeds) != 0 ||
		len(checked.ImageGraph.StorageWriters) != 0 ||
		len(checked.ImageGraph.StorageAppendCalls) != 0 ||
		len(checked.Storage.EventsByTypeID) != 0 ||
		len(checked.Storage.ProjectionsByID) != 0
}

func storagePathQueueDepth(role string, metrics map[string]uint64) uint64 {
	if role == "background" {
		return storageMetricOrDefault(metrics, "background_io_queue_depth", storagefmt.StorageBackgroundIOQueueDepth)
	}
	return storageMetricOrDefault(metrics, "foreground_io_queue_depth", storagefmt.StorageForegroundIOQueueDepth)
}

func storageEventUpcastCount(storage StorageIndex) uint64 {
	var count uint64
	for _, event := range storage.EventsByTypeID {
		if len(event.Layouts) > 0 {
			count += uint64(len(event.Layouts) - 1)
		}
	}
	return count
}

func storageProjectionUpcastCount(storage StorageIndex) uint64 {
	var count uint64
	for _, projection := range storage.ProjectionsByID {
		if len(projection.Layouts) > 0 {
			count += uint64(len(projection.Layouts) - 1)
		}
	}
	return count
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

func ValidateStorageReportContent(r report.ImageReport) []diag.Diagnostic {
	if !reportHasStorage(r.Storage) {
		return nil
	}
	hasFact := func(name string) bool {
		return r.Storage.MetricFacts[name]
	}
	required := []struct {
		ok   bool
		name string
	}{
		{hasFact("active_lba_size") && r.Storage.ActiveLBASize != 0, "active_lba_size"},
		{r.Storage.NamespaceMode != "", "namespace_mode"},
		{r.Storage.DurabilityMode != "", "durability_mode"},
		{r.Storage.InterruptMode != "", "interrupt_mode"},
		{r.Storage.EventSlotSize != 0, "event_slot_size"},
		{r.Storage.EventHeaderSize != 0, "event_header_size"},
		{r.Storage.EventPayloadBytes != 0, "event_payload_bytes"},
		{r.Storage.EventSlotsWritten != 0, "event_slots_written"},
		{r.Storage.TargetBatchSlots != 0, "target_batch_slots"},
		{r.Storage.MaxOverflowSlots != 0, "max_overflow_slots"},
		{r.Storage.MaxBatchSlots != 0, "max_batch_slots"},
		{r.Storage.MaxAtomicGroupSlots != 0, "max_atomic_group_slots"},
		{r.Storage.BatchesSubmitted != 0, "batches_submitted"},
		{hasFact("append_latency_us"), "append_latency_p50_us"},
		{hasFact("durability_latency_us"), "append_latency_p99_us"},
		{hasFact("device_media_write_commands"), "device_reported_media_writes"},
		{hasFact("device_media_write_bytes"), "media_write_bytes"},
		{r.Storage.AdminQueueDepth != 0, "admin_queue_depth"},
		{r.Storage.ForegroundIOQueueDepth != 0, "foreground_io_queue_depth"},
		{r.Storage.BackgroundIOQueueDepth != 0, "background_io_queue_depth"},
		{hasFact("blob_orphan_bytes"), "blob_orphan_bytes"},
		{hasFact("projection_lag_events"), "projection_lag_events"},
		{hasFact("projection_upcast_count"), "projection_upcast_count"},
		{hasFact("projection_rebuild_count"), "projection_rebuild_count"},
		{r.Storage.StreamDirectoryCacheHits+r.Storage.StreamDirectoryCacheMisses != 0, "stream_directory_cache_hits"},
		{hasFact("stream_directory_cache_hit_rate_ppm") || hasFact("stream_directory_cache_hits") || hasFact("stream_directory_cache_misses") || r.Storage.StreamDirectoryCacheHitRateX1000 != 0, "stream_directory_cache_hit_rate_x1000"},
		{hasFact("core_link_committed_groups"), "core_link_committed_groups"},
		{hasFact("core_link_backpressure_count"), "core_link_backpressure_count"},
		{r.Storage.NvmePaths != nil, "nvme_paths"},
		{hasStoragePathRole(r.Storage.NvmePaths, "foreground"), "nvme_paths.foreground"},
		{hasStoragePathRole(r.Storage.NvmePaths, "background"), "nvme_paths.background"},
		{r.Storage.CoreLinks != nil, "core_links"},
	}
	var ds []diag.Diagnostic
	for _, req := range required {
		if !req.ok {
			ds = append(ds, diag.Diagnostic{Phase: "sem", Code: diag.SEM0124, Severity: diag.Error, Message: "storage report missing " + req.name})
		}
	}
	return ds
}

func hasStoragePathRole(paths []report.NvmePathReport, role string) bool {
	for _, path := range paths {
		if path.Role == role {
			return true
		}
	}
	return false
}

func reportHasStorage(storage report.StorageReport) bool {
	return storage.ActiveLBASize != 0 ||
		storage.NamespaceMode != "" ||
		storage.DurabilityMode != "" ||
		storage.InterruptMode != "" ||
		storage.EventSlotSize != 0 ||
		len(storage.NvmePaths) != 0 ||
		len(storage.CoreLinks) != 0
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

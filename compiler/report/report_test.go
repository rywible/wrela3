package report

import (
	"encoding/json"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestImageReportJSONShape(t *testing.T) {
	r := NewImageReport("Hello")
	r.Memory.TotalBytes = 0x1000000
	r.Memory.RootRegions = []MemoryRootReport{{
		Label: "boot.root",
		Base:  0x200000,
		Bytes: 0x1000000,
	}}
	r.AuthorityAudit.MemoryRoots = []AuthorityRecord{{Kind: "memory_root", Label: "boot.root"}}
	r.Runtime.Topics = []TopicReport{{
		Label:       "timer.periodic",
		Type:        "Topic<TimerTickPayload>",
		TypeKey:     "machine.x86_64.topic.Topic<machine.x86_64.topic_payload.TimerTickPayload>",
		PayloadType: "TimerTickPayload",
		PayloadKey:  "machine.x86_64.topic_payload.TimerTickPayload",
		NextType:    "Option<TimerTickPayload>",
		NextKey:     "wrela.lang.core.Option<machine.x86_64.topic_payload.TimerTickPayload>",
		Bytes:       24,
		Align:       8,
		Depth:       64,
	}}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	for _, key := range []string{"version", "image", "memory", "hardware", "runtime", "authority_audit"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("report missing top-level key %q in %s", key, data)
		}
	}
	var shaped struct {
		Runtime struct {
			Topics []TopicReport `json:"topics"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(data, &shaped); err != nil {
		t.Fatalf("unmarshal shaped report: %v", err)
	}
	if len(shaped.Runtime.Topics) != 1 ||
		shaped.Runtime.Topics[0].TypeKey != "machine.x86_64.topic.Topic<machine.x86_64.topic_payload.TimerTickPayload>" ||
		shaped.Runtime.Topics[0].PayloadKey != "machine.x86_64.topic_payload.TimerTickPayload" ||
		shaped.Runtime.Topics[0].NextKey != "wrela.lang.core.Option<machine.x86_64.topic_payload.TimerTickPayload>" {
		t.Fatalf("generic topic JSON fields = %#v in %s", shaped.Runtime.Topics, data)
	}
}

func TestNewImageReportUsesEmptyArrays(t *testing.T) {
	r := NewImageReport("Hello")
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var decoded struct {
		Memory struct {
			RootRegions     []MemoryRootReport     `json:"root_regions"`
			Arenas          []ArenaReport          `json:"arenas"`
			ExecutorBudgets []ExecutorBudgetReport `json:"executor_budgets"`
		} `json:"memory"`
		Hardware struct {
			Claims      []AuthorityRecord `json:"claims"`
			PCI         []PCIReport       `json:"pci"`
			Timers      []TimerReport     `json:"timers"`
			Locality    []LocalityReport  `json:"locality"`
			Framebuffer FramebufferReport `json:"framebuffer"`
		} `json:"hardware"`
		Runtime struct {
			Executors       []ExecutorReport       `json:"executors"`
			Placement       []PlacementReport      `json:"placement"`
			Interrupts      []AuthorityRecord      `json:"interrupts"`
			Topics          []TopicReport          `json:"topics"`
			InterruptQueues []InterruptQueueReport `json:"interrupt_queues"`
			WakePaths       []WakePathReport       `json:"wake_paths"`
		} `json:"runtime"`
		AuthorityAudit struct {
			MemoryRoots    []AuthorityRecord `json:"memory_roots"`
			Arenas         []AuthorityRecord `json:"arenas"`
			HardwareClaims []AuthorityRecord `json:"hardware_claims"`
			Interrupts     []AuthorityRecord `json:"interrupts"`
			Timers         []AuthorityRecord `json:"timers"`
			Queues         []AuthorityRecord `json:"queues"`
			Topics         []AuthorityRecord `json:"topics"`
			WakeTargets    []AuthorityRecord `json:"wake_targets"`
			DMABuffers     []AuthorityRecord `json:"dma_buffers"`
		} `json:"authority_audit"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	checks := map[string]bool{
		"memory.root_regions":          decoded.Memory.RootRegions != nil,
		"memory.arenas":                decoded.Memory.Arenas != nil,
		"memory.executor_budgets":      decoded.Memory.ExecutorBudgets != nil,
		"hardware.claims":              decoded.Hardware.Claims != nil,
		"hardware.pci":                 decoded.Hardware.PCI != nil,
		"hardware.timers":              decoded.Hardware.Timers != nil,
		"hardware.locality":            decoded.Hardware.Locality != nil,
		"runtime.executors":            decoded.Runtime.Executors != nil,
		"runtime.placement":            decoded.Runtime.Placement != nil,
		"runtime.interrupts":           decoded.Runtime.Interrupts != nil,
		"runtime.topics":               decoded.Runtime.Topics != nil,
		"runtime.interrupt_queues":     decoded.Runtime.InterruptQueues != nil,
		"runtime.wake_paths":           decoded.Runtime.WakePaths != nil,
		"authority_audit.memory_roots": decoded.AuthorityAudit.MemoryRoots != nil,
		"authority_audit.arenas":       decoded.AuthorityAudit.Arenas != nil,
		"authority_audit.hardware":     decoded.AuthorityAudit.HardwareClaims != nil,
		"authority_audit.interrupts":   decoded.AuthorityAudit.Interrupts != nil,
		"authority_audit.timers":       decoded.AuthorityAudit.Timers != nil,
		"authority_audit.queues":       decoded.AuthorityAudit.Queues != nil,
		"authority_audit.topics":       decoded.AuthorityAudit.Topics != nil,
		"authority_audit.wake_targets": decoded.AuthorityAudit.WakeTargets != nil,
		"authority_audit.dma_buffers":  decoded.AuthorityAudit.DMABuffers != nil,
	}
	for name, ok := range checks {
		if !ok {
			t.Fatalf("%s decoded as nil from %s", name, data)
		}
	}
}

func TestStorageMetricsReportShape(t *testing.T) {
	r := NewImageReport("StorageImage")
	r.Storage = StorageReport{
		ActiveLBASize:                    512,
		NamespaceMode:                    "conventional",
		DurabilityMode:                   "fua",
		EventSlotSize:                    512,
		ReservedEmptySlots:               3,
		TargetBatchSlots:                 64,
		MaxOverflowSlots:                 8,
		MaxBatchSlots:                    72,
		MaxAtomicGroupSlots:              32,
		AppendLatencyP50US:               10,
		AppendLatencyP99US:               2000,
		DeviceReportedMediaWrites:        123,
		MediaWriteBytes:                  62976,
		AdminQueueDepth:                  32,
		ForegroundIOQueueDepth:           256,
		BackgroundIOQueueDepth:           128,
		BlobOrphanBytes:                  4096,
		ProjectionLagEvents:              5,
		ProjectionUpcastCount:            2,
		ProjectionRebuildCount:           1,
		StreamDirectoryCacheHitRateX1000: 875,
		NvmePaths: []NvmePathReport{{
			Label:      "nvme.foreground",
			Role:       "foreground",
			Owner:      "foreground",
			QueueID:    1,
			Vector:     80,
			QueueDepth: 256,
		}},
		CoreLinks: []CoreLinkReport{{
			Label:     "core_link.producer.0",
			Direction: "tx",
			Role:      "producer",
			Owner:     "foreground",
			Peer:      "maintenance",
			Depth:     64,
		}},
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var shaped struct {
		Storage StorageReport `json:"storage"`
	}
	if err := json.Unmarshal(data, &shaped); err != nil {
		t.Fatalf("unmarshal shaped report: %v", err)
	}
	if shaped.Storage.ActiveLBASize != 512 ||
		shaped.Storage.NamespaceMode != "conventional" ||
		shaped.Storage.DurabilityMode != "fua" ||
		shaped.Storage.EventSlotSize != 512 ||
		shaped.Storage.TargetBatchSlots != 64 ||
		shaped.Storage.AppendLatencyP99US != 2000 ||
		shaped.Storage.DeviceReportedMediaWrites != 123 ||
		shaped.Storage.ForegroundIOQueueDepth != 256 ||
		shaped.Storage.BlobOrphanBytes != 4096 ||
		shaped.Storage.ProjectionLagEvents != 5 ||
		shaped.Storage.ProjectionUpcastCount != 2 ||
		shaped.Storage.ProjectionRebuildCount != 1 ||
		shaped.Storage.StreamDirectoryCacheHitRateX1000 != 875 {
		t.Fatalf("storage report = %#v in %s", shaped.Storage, data)
	}
	if len(shaped.Storage.NvmePaths) != 1 || shaped.Storage.NvmePaths[0].QueueDepth != 256 {
		t.Fatalf("nvme paths = %#v in %s", shaped.Storage.NvmePaths, data)
	}
	if len(shaped.Storage.CoreLinks) != 1 || shaped.Storage.CoreLinks[0].Depth != 64 {
		t.Fatalf("core links = %#v in %s", shaped.Storage.CoreLinks, data)
	}
}

func TestZeroValueImageReportJSONShape(t *testing.T) {
	r := ImageReport{
		Version: 1,
		Image:   "Hello",
		Memory: MemoryReport{
			TotalBytes: 0x1000000,
			RootRegions: []MemoryRootReport{{
				Label: "boot.root",
				Base:  0x200000,
				Bytes: 0x1000000,
			}},
		},
		AuthorityAudit: AuthorityAuditReport{
			MemoryRoots: []AuthorityRecord{{Kind: "memory_root", Label: "boot.root"}},
		},
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	for _, key := range []string{"version", "image", "memory", "hardware", "runtime", "authority_audit"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("report missing top-level key %q in %s", key, data)
		}
	}
}

func TestConvergenceDiagnosticCodesExist(t *testing.T) {
	codes := []string{
		diag.SEM0056, diag.SEM0057, diag.SEM0058, diag.SEM0059, diag.SEM0060,
		diag.SEM0061, diag.SEM0062, diag.SEM0063, diag.SEM0064, diag.SEM0065,
		diag.SEM0066, diag.SEM0067, diag.SEM0068, diag.SEM0069, diag.SEM0070,
		diag.SEM0071, diag.SEM0072, diag.SEM0073, diag.SEM0074, diag.SEM0075,
	}
	for _, code := range codes {
		if code == "" {
			t.Fatalf("diagnostic code must not be empty")
		}
	}
}

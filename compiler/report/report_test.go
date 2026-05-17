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

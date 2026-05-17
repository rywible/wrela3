package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
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
	}}
	r := BuildImageReport(checked)
	if len(r.AuthorityAudit.HardwareClaims) != 1 || r.AuthorityAudit.HardwareClaims[0].Owner != "delegated_hardware" {
		t.Fatalf("hardware claims missing from report: %#v", r.AuthorityAudit.HardwareClaims)
	}
	if r.Hardware.APIC.Mode != "xapic_fallback" {
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

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

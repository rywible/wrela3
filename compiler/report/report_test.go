package report

import (
	"encoding/json"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestImageReportJSONShape(t *testing.T) {
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

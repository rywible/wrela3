package sem

import (
	"strings"
	"testing"
)

func TestNvmeSourceCompiles(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	driver := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeDriver")
	initialize := methodByName(t, driver, "initialize")
	assertMethodSignature(t, initialize, []string{"device:PciDevice"}, "NvmeDriver")
	claimController := methodByName(t, driver, "claim_controller")
	assertMethodSignature(t, claimController, []string{"devices:PciDeviceSet", "occurrence:U64"}, "PciDevice")

	path := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeIoPath")
	if path.Kind != KindDriverPath {
		t.Fatalf("NvmeIoPath kind = %s, want driver path", path.Kind)
	}
	event := checked.Index.InterruptEvent("machine.x86_64.nvme", "NvmeIoPath")
	if event == nil || event.EventType.Name != "NvmeCompletionInterrupt" {
		t.Fatalf("NvmeIoPath interrupt event = %#v, want NvmeCompletionInterrupt", event)
	}

	namespace := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeNamespace")
	assertTypeFields(t, namespace, map[string]string{
		"logical_block_size":                  "U64",
		"supports_zns":                        "Bool",
		"supports_fua":                        "Bool",
		"atomic_write_unit_blocks":            "U32",
		"power_fail_atomic_write_unit_blocks": "U32",
		"volatile_write_cache":                "Bool",
	})

	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	for _, want := range []string{
		"const NVME_CLASS_MASS_STORAGE: U64 = 0x01",
		"const NVME_SUBCLASS_NVM: U64 = 0x08",
		"const NVME_PROGIF_EXPRESS: U64 = 0x02",
		"candidate.identity.class_code == NVME_CLASS_MASS_STORAGE",
		"candidate.identity.subclass == NVME_SUBCLASS_NVM",
		"candidate.identity.prog_if == NVME_PROGIF_EXPRESS",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("nvme source missing %q", want)
		}
	}
}

func assertMethodSignature(t *testing.T, method *Method, params []string, returnType string) {
	t.Helper()
	gotParams := method.Params
	if len(gotParams) > 0 && gotParams[0].Name == "self" {
		gotParams = gotParams[1:]
	}
	if len(gotParams) != len(params) {
		t.Fatalf("%s params = %#v, want %#v", method.Name, method.Params, params)
	}
	for i, want := range params {
		got := gotParams[i].Name + ":" + gotParams[i].Type.Name
		if got != want {
			t.Fatalf("%s param %d = %s, want %s", method.Name, i, got, want)
		}
	}
	if method.Return == nil || method.Return.Name != returnType {
		t.Fatalf("%s return = %#v, want %s", method.Name, method.Return, returnType)
	}
}

package compiler

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestHardwareDiscoveryReportAPIBuilds(t *testing.T) {
	source := readRepoFile(t, "wrela/platform/hardware/discovery.wrela")
	for _, want := range []string{
		"data DiscoveryReport",
		"fn report(self, memory: MutableBytes, hardware_plan: HardwarePlan) -> DiscoveryReport",
		"pci_device_count = self.pci.count",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("discovery report source missing %q", want)
		}
	}
	hello := readRepoFile(t, "examples/hello/main.wrela")
	for _, want := range []string{
		"let root_region = discovery.memory.require_usable_region(",
		"let memory_region = root_region.bytes()",
		"discovery.report(memory = memory_region, hardware_plan = hardware_plan)",
		"discovery_report.memory_length < 0x600000",
		"discovery_report.pci_device_count == 0",
		"0xAC070001",
		"0xAC070002",
	} {
		if !strings.Contains(hello, want) {
			t.Fatalf("hello discovery report use missing %q", want)
		}
	}

	result, err := Build(BuildOptions{
		Mode:       ModeDev,
		RootPath:   "examples/hello/main.wrela",
		OutputPath: filepath.Join(t.TempDir(), "hello.efi"),
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("build hello: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%#v", result), "Q35PciInterruptConfigurator") {
		t.Fatalf("legacy q35 configurator leaked into build result")
	}
}

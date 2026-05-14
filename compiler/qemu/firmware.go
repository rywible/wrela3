package qemu

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Firmware struct {
	Code string
	Vars string
}

func ResolveFirmware(qemuBinary string) (Firmware, error) {
	if firmware, ok, err := firmwareFromEnv(); ok || err != nil {
		return firmware, err
	}

	for _, dir := range firmwareSearchDirs(qemuBinary) {
		if firmware, ok := firmwareFromQEMUShare(dir); ok {
			return firmware, nil
		}
	}
	for _, firmware := range directFirmwareCandidates() {
		if fileExists(firmware.Code) && fileExists(firmware.Vars) {
			return firmware, nil
		}
	}
	return Firmware{}, fmt.Errorf("could not find x86_64 OVMF/EDK2 firmware; set WRELA_OVMF_CODE and WRELA_OVMF_VARS")
}

func firmwareFromEnv() (Firmware, bool, error) {
	code := os.Getenv("WRELA_OVMF_CODE")
	vars := os.Getenv("WRELA_OVMF_VARS")
	if code == "" && vars == "" {
		return Firmware{}, false, nil
	}
	if code == "" || vars == "" {
		return Firmware{}, true, fmt.Errorf("WRELA_OVMF_CODE and WRELA_OVMF_VARS must both be set")
	}
	if !fileExists(code) {
		return Firmware{}, true, fmt.Errorf("WRELA_OVMF_CODE does not exist: %s", code)
	}
	if !fileExists(vars) {
		return Firmware{}, true, fmt.Errorf("WRELA_OVMF_VARS does not exist: %s", vars)
	}
	return Firmware{Code: code, Vars: vars}, true, nil
}

func firmwareSearchDirs(qemuBinary string) []string {
	bin := qemuBinary
	if bin == "" {
		bin = "qemu-system-x86_64"
	}
	if !strings.ContainsRune(bin, os.PathSeparator) {
		if found, err := exec.LookPath(bin); err == nil {
			bin = found
		}
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}

	var dirs []string
	if bin != "" {
		versionDir := filepath.Dir(filepath.Dir(bin))
		dirs = append(dirs, filepath.Join(versionDir, "share", "qemu"))
	}
	dirs = append(dirs,
		"/opt/homebrew/share/qemu",
		"/usr/local/share/qemu",
		"/usr/share/qemu",
	)
	return uniqueStrings(dirs)
}

func firmwareFromQEMUShare(shareDir string) (Firmware, bool) {
	entries, err := os.ReadDir(filepath.Join(shareDir, "firmware"))
	if err != nil {
		return Firmware{}, false
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.SliceStable(names, func(i, j int) bool {
		return firmwareMetadataRank(names[i]) < firmwareMetadataRank(names[j])
	})

	for _, name := range names {
		if firmware, ok := firmwareFromMetadata(filepath.Join(shareDir, "firmware", name)); ok {
			return firmware, true
		}
	}
	return Firmware{}, false
}

func firmwareMetadataRank(name string) int {
	rank := 10
	if strings.Contains(name, "x86_64") {
		rank -= 5
	}
	if strings.Contains(name, "secure") {
		rank += 2
	}
	return rank
}

type firmwareMetadata struct {
	Mapping struct {
		Executable struct {
			Filename string `json:"filename"`
		} `json:"executable"`
		NVRAMTemplate struct {
			Filename string `json:"filename"`
		} `json:"nvram-template"`
	} `json:"mapping"`
	Targets []struct {
		Architecture string   `json:"architecture"`
		Machines     []string `json:"machines"`
	} `json:"targets"`
}

func firmwareFromMetadata(path string) (Firmware, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Firmware{}, false
	}
	var metadata firmwareMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return Firmware{}, false
	}
	if !metadataSupportsQ35X86(metadata) {
		return Firmware{}, false
	}
	firmware := Firmware{
		Code: metadata.Mapping.Executable.Filename,
		Vars: metadata.Mapping.NVRAMTemplate.Filename,
	}
	if fileExists(firmware.Code) && fileExists(firmware.Vars) {
		return firmware, true
	}
	return Firmware{}, false
}

func metadataSupportsQ35X86(metadata firmwareMetadata) bool {
	for _, target := range metadata.Targets {
		if target.Architecture != "x86_64" {
			continue
		}
		for _, machine := range target.Machines {
			if strings.Contains(machine, "q35") {
				return true
			}
		}
	}
	return false
}

func directFirmwareCandidates() []Firmware {
	return []Firmware{
		{Code: "/usr/share/OVMF/OVMF_CODE.fd", Vars: "/usr/share/OVMF/OVMF_VARS.fd"},
		{Code: "/usr/share/edk2/ovmf/OVMF_CODE.fd", Vars: "/usr/share/edk2/ovmf/OVMF_VARS.fd"},
		{Code: "/usr/share/qemu/OVMF_CODE.fd", Vars: "/usr/share/qemu/OVMF_VARS.fd"},
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildHello(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "hello.efi")
	result, err := Build(BuildOptions{
		Mode:       ModeDev,
		RootPath:   "examples/hello/main.wrela",
		OutputPath: out,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("Build hello: %v", err)
	}
	if result.OutputPath != out {
		t.Fatalf("OutputPath = %q, want %q", result.OutputPath, out)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output image is empty")
	}
}

func TestBuildHelloImageContainsRuntimeSignals(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "hello.efi")
	_, err := Build(BuildOptions{
		Mode:       ModeDev,
		RootPath:   "examples/hello/main.wrela",
		OutputPath: out,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("Build hello: %v", err)
	}
	bytes, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	for name, pattern := range map[string][]byte{
		"PE MZ header":       []byte{'M', 'Z'},
		"hello newline data": []byte("hello from wrela\n\x00"),
		"serial out":         {0xEE},
		"cli":                {0xFA},
		"mov cr3":            {0x0F, 0x22, 0xDA},
		"lgdt":               {0x49, 0x0F, 0x01, 0x13},
		"lidt":               {0x49, 0x0F, 0x01, 0x1B},
		"retfq":              {0x48, 0xCB},
		"mov ds, ax":         {0x8E, 0xD8},
		"mov es, ax":         {0x8E, 0xC0},
		"mov ss, ax":         {0x8E, 0xD0},
		"mov fs, ax":         {0x8E, 0xE0},
		"mov gs, ax":         {0x8E, 0xE8},
	} {
		if !containsBytes(bytes, pattern) {
			t.Fatalf("image missing %s pattern %s", name, strings.ToUpper(hexBytes(pattern)))
		}
	}
}

func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}

func hexBytes(bytes []byte) string {
	const digits = "0123456789abcdef"
	var b strings.Builder
	for i, by := range bytes {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte(digits[by>>4])
		b.WriteByte(digits[by&0xf])
	}
	return b.String()
}

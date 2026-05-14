package compiler

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/codegen"
	"github.com/ryanwible/wrela3/compiler/ir"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
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
	imageBytes, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	for name, pattern := range map[string][]byte{
		"PE MZ header":       []byte{'M', 'Z'},
		"hello newline data": []byte("hello from wrela\n\x00"),
	} {
		if !containsBytes(imageBytes, pattern) {
			t.Fatalf("image missing %s pattern %s", name, strings.ToUpper(hexBytes(pattern)))
		}
	}

	image := compileHelloImage(t)
	transition := symbolBytes(t, image, "_wrela_method_platform_uefi_transition_DelegatedHardware_exit_to_owned_hardware")
	for name, pattern := range map[string][]byte{
		"GetMemoryMap table load":       {0x49, 0x8B, 0x43, 0x38},
		"ExitBootServices table load":  {0x49, 0x8B, 0x83, 0xE8, 0x00, 0x00, 0x00},
		"UEFI indirect call":           {0xFF, 0xD0},
		"cli":                          {0xFA},
		"mov cr3":                      {0x0F, 0x22, 0xD8},
		"retfq":                        {0x48, 0xCB},
		"mov ds, ax":                   {0x8E, 0xD8},
		"mov es, ax":                   {0x8E, 0xC0},
		"mov ss, ax":                   {0x8E, 0xD0},
		"mov fs, ax":                   {0x8E, 0xE0},
		"mov gs, ax":                   {0x8E, 0xE8},
	} {
		if !containsBytes(transition, pattern) {
			t.Fatalf("transition symbol missing %s pattern %s", name, strings.ToUpper(hexBytes(pattern)))
		}
	}

	executor := symbolBytes(t, image, "_wrela_method_examples_hello_program_HelloWorld_run")
	if !containsBytes(executor, []byte{0xEE}) {
		t.Fatal("executor symbol missing COM1 out instruction")
	}
	if !containsBytes(executor, []byte{0xF4, 0xE9}) {
		t.Fatal("executor symbol missing halt loop")
	}
}

func compileHelloImage(t *testing.T) *codegen.Image {
	t.Helper()
	repoRoot := resolveRepoRoot(".")
	graph, err := source.LoadGraph(source.Options{
		RootPath: filepath.Join(repoRoot, "examples/hello/main.wrela"),
		ImportRoots: []string{
			repoRoot,
			filepath.Join(repoRoot, "wrela"),
		},
	})
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	modules, ds := parse.ParseGraph(*graph)
	if len(ds) != 0 {
		t.Fatalf("ParseGraph diagnostics: %#v", ds)
	}
	index, ds := sem.BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("BuildIndex diagnostics: %#v", ds)
	}
	checked, ds := sem.Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("Check diagnostics: %#v", ds)
	}
	program, ds := ir.Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("Lower diagnostics: %#v", ds)
	}
	image, ds := codegen.Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	return image
}

func symbolBytes(t *testing.T, image *codegen.Image, symbol string) []byte {
	t.Helper()
	rva, ok := image.Symbols[symbol]
	if !ok {
		t.Fatalf("missing symbol %s", symbol)
	}
	text := image.Sections[0]
	start := int(rva - text.RVA)
	end := len(text.Data)
	var starts []int
	for _, other := range image.Symbols {
		if other > rva {
			starts = append(starts, int(other-text.RVA))
		}
	}
	if len(starts) != 0 {
		sort.Ints(starts)
		end = starts[0]
	}
	if start < 0 || start > len(text.Data) || end < start || end > len(text.Data) {
		t.Fatalf("invalid symbol span for %s: %d..%d in %d bytes", symbol, start, end, len(text.Data))
	}
	return text.Data[start:end]
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

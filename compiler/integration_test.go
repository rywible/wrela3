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

func TestBuildReturnsResolvedRelativeOutputPath(t *testing.T) {
	repoRoot := resolveRepoRoot(".")
	tmp := t.TempDir()
	out := filepath.Join(tmp, "hello.efi")
	relOut, err := filepath.Rel(repoRoot, out)
	if err != nil {
		t.Fatalf("relative output path: %v", err)
	}

	result, err := Build(BuildOptions{
		Mode:       ModeDev,
		RootPath:   "examples/hello/main.wrela",
		OutputPath: relOut,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("Build hello: %v", err)
	}
	want := filepath.Clean(filepath.Join(repoRoot, relOut))
	if result.OutputPath != want {
		t.Fatalf("OutputPath = %q, want %q", result.OutputPath, want)
	}
	if _, err := os.Stat(result.OutputPath); err != nil {
		t.Fatalf("stat returned output path: %v", err)
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
	getMap := symbolBytes(t, image, "_wrela_method_platform_uefi_boot_services_UefiBootServicesCalls_get_memory_map")
	exitBootServices := symbolBytes(t, image, "_wrela_method_platform_uefi_boot_services_UefiBootServicesCalls_exit_boot_services")
	activate := symbolBytes(t, image, "_wrela_method_platform_uefi_transition_DelegatedHardware_activate_owned_hardware")
	for name, pattern := range map[string][]byte{
		"GetMemoryMap UEFI indirect call":     {0xFF, 0xD0},
		"ExitBootServices UEFI indirect call": {0xFF, 0xD0},
	} {
		haystack := getMap
		if strings.HasPrefix(name, "ExitBootServices") {
			haystack = exitBootServices
		}
		if !containsBytes(haystack, pattern) {
			t.Fatalf("boot service bridge missing %s pattern %s", name, strings.ToUpper(hexBytes(pattern)))
		}
	}
	for name, pattern := range map[string][]byte{
		"cli":        {0xFA},
		"mov cr3":    {0x0F, 0x22, 0xDA},
		"retfq":      {0x48, 0xCB},
		"mov ds, ax": {0x8E, 0xD8},
		"mov es, ax": {0x8E, 0xC0},
		"mov ss, ax": {0x8E, 0xD0},
		"mov fs, ax": {0x8E, 0xE0},
		"mov gs, ax": {0x8E, 0xE8},
	} {
		if !containsBytes(activate, pattern) {
			t.Fatalf("activate_owned_hardware symbol missing %s pattern %s", name, strings.ToUpper(hexBytes(pattern)))
		}
	}

	serialWrite8 := symbolBytes(t, image, "_wrela_method_machine_x86_64_serial_SerialWriterRegisters_write8")
	if !containsBytes(serialWrite8, []byte{0xEE}) {
		t.Fatal("serial writer symbol missing COM1 out instruction")
	}
	haltForever := symbolBytes(t, image, "_wrela_method_machine_x86_64_executor_memory_ExecutorMemory_halt_forever")
	if !containsBytes(haltForever, []byte{0xF4, 0xE9}) {
		t.Fatal("executor memory halt symbol missing halt loop")
	}
}

func TestBuildHelloContainsInterruptBinding(t *testing.T) {
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
	if result.Image == nil {
		t.Fatalf("BuildResult.Image is nil")
	}
	if got := len(result.Image.InterruptBindings); got != 3 {
		t.Fatalf("interrupt bindings = %d, want 3", got)
	}
	gotVectors := map[uint8]bool{}
	for _, binding := range result.Image.InterruptBindings {
		gotVectors[binding.Vector] = true
	}
	for _, want := range []uint8{0x40, 0x41, 0x42} {
		if !gotVectors[want] {
			t.Fatalf("missing vector %#x in bindings %#v", want, result.Image.InterruptBindings)
		}
	}
}

func TestHelloTransitionFunctionsPreserveOwnedStackReturn(t *testing.T) {
	program := compileHelloProgram(t)
	for _, symbol := range []string{
		program.Entry.DelegatedPhaseSymbol,
		"_wrela_method_platform_uefi_transition_DelegatedHardware_exit_to_owned_hardware",
	} {
		fn := findIRFunction(program, symbol)
		if fn == nil {
			t.Fatalf("missing IR function %s", symbol)
		}
		if !fn.PreserveStackReturn {
			t.Fatalf("%s must return without restoring the old delegated stack", symbol)
		}
	}
}

func TestHelloOwnedPhaseCallsExecutorRun(t *testing.T) {
	program := compileHelloProgram(t)
	assertFunctionCalls(t, program, program.Entry.OwnedPhaseSymbol, "_wrela_method_examples_hello_program_HelloWorld_run")
}

func TestHelloIRCallGraphReachesSerialWrite(t *testing.T) {
	program := compileHelloProgram(t)
	assertFunctionCalls(t, program,
		"_wrela_method_examples_hello_program_HelloWorld_run",
		"_wrela_method_machine_x86_64_executor_memory_ExecutorMemory_static_bytes",
		"_wrela_method_machine_x86_64_serial_SerialConsolePath_write",
		"_wrela_method_machine_x86_64_executor_memory_ExecutorMemory_halt_forever",
	)
	assertFunctionCalls(t, program,
		"_wrela_method_machine_x86_64_serial_SerialConsolePath_write",
		"_wrela_method_machine_x86_64_serial_SerialConsolePath_wait_until_ready",
		"_wrela_method_machine_x86_64_serial_SerialConsolePath_write_byte",
	)
	assertFunctionCalls(t, program,
		"_wrela_method_machine_x86_64_serial_SerialConsolePath_write_byte",
		"_wrela_method_machine_x86_64_serial_SerialConsolePath_wait_until_ready",
		"_wrela_method_machine_x86_64_serial_SerialWriterRegisters_write8",
	)
	assertFunctionCalls(t, program,
		"_wrela_method_machine_x86_64_serial_SerialConsolePath_wait_until_ready",
		"_wrela_method_machine_x86_64_serial_SerialWriterRegisters_read8",
		"_wrela_method_machine_x86_64_serial_SerialConsolePath_pause",
	)
}

func TestHelloTransitionCompiledBytesUseSavedContinuation(t *testing.T) {
	image := compileHelloImage(t)
	for _, symbol := range []string{
		"_wrela_phase_examples_hello_main_HelloSerial_delegated_hardware",
		"_wrela_method_platform_uefi_transition_DelegatedHardware_exit_to_owned_hardware",
	} {
		code := symbolBytes(t, image, symbol)
		if containsBytes(code, []byte{0x48, 0x89, 0xEC, 0x5D, 0xC3}) {
			t.Fatalf("%s must not restore rsp to the delegated stack before returning", symbol)
		}
		if !containsBytes(code, []byte{0x4C, 0x8B, 0x55, 0xF8}) {
			t.Fatalf("%s missing saved continuation load", symbol)
		}
		if !containsBytes(code, []byte{0x48, 0x8B, 0x6D, 0x00, 0x41, 0x52, 0xC3}) {
			t.Fatalf("%s missing caller-rbp restore and owned-stack continuation return", symbol)
		}
	}
}

func compileHelloImage(t *testing.T) *codegen.Image {
	t.Helper()
	program := compileHelloProgram(t)
	image, ds := codegen.Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	return image
}

func findIRFunction(program *ir.Program, symbol string) *ir.Function {
	for i := range program.Functions {
		if program.Functions[i].Symbol == symbol {
			return &program.Functions[i]
		}
	}
	return nil
}

func assertFunctionCalls(t *testing.T, program *ir.Program, symbol string, want ...string) {
	t.Helper()
	fn := findIRFunction(program, symbol)
	if fn == nil {
		t.Fatalf("missing IR function %s", symbol)
	}
	calls := functionCalls(*fn)
	for _, target := range want {
		if !calls[target] {
			t.Fatalf("%s missing call to %s; calls: %#v", symbol, target, calls)
		}
	}
}

func functionCalls(fn ir.Function) map[string]bool {
	out := map[string]bool{}
	for _, block := range fn.Blocks {
		collectCalls(block.Ops, out)
	}
	return out
}

func collectCalls(ops []ir.Operation, out map[string]bool) {
	for _, op := range ops {
		switch v := op.(type) {
		case *ir.Call:
			out[v.Symbol] = true
		case *ir.If:
			collectCalls(v.ConditionOps, out)
			collectCalls(v.Then, out)
			collectCalls(v.Else, out)
		case *ir.While:
			collectCalls(v.ConditionOps, out)
			collectCalls(v.Body, out)
		case *ir.ForBytes:
			collectCalls(v.IterableOps, out)
			collectCalls(v.Body, out)
		}
	}
}

func compileHelloProgram(t *testing.T) *ir.Program {
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
	return program
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

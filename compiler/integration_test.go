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

func TestBuildMultiVcpuTopicsExample(t *testing.T) {
	source := readRepoFile(t, "examples/multi_vcpu_topics/main.wrela")
	for _, want := range []string{
		"hardware.vcpu1.start(executor = consumer)",
		"hardware.vcpu0.enter(executor = producer)",
		"panic: vcpu1 start failed\\n",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("multi-vCPU example missing %q", want)
		}
	}

	tmp := t.TempDir()
	out := filepath.Join(tmp, "multi-vcpu-topics.efi")
	result, err := Build(BuildOptions{
		Mode:       ModeDev,
		RootPath:   "examples/multi_vcpu_topics/main.wrela",
		OutputPath: out,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("Build multi-vCPU topics example: %v", err)
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
	if result.Image == nil {
		t.Fatal("BuildResult.Image is nil")
	}
	program := compileProgramAt(t, "examples/multi_vcpu_topics/main.wrela")
	if got := len(program.VcpuStarts); got != 2 {
		t.Fatalf("VcpuStarts = %d, want 2: %#v", got, program.VcpuStarts)
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

func TestHelloUsesHardwareDiscoverySource(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "examples", "hello", "main.wrela"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{
		"Q35" + "PciInterruptConfigurator",
		"Pci" + "ConfigPorts",
		"0xFEE00000",
		"0xFEC" + "00000",
		"MutableBytes(address = " + "0x200000",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("hello source still contains %q", forbidden)
		}
	}
	for _, required := range []string{"PlatformDiscoveryRoot", "require_usable_region", "require_device", "claim_msi", "discovery.report"} {
		if !strings.Contains(text, required) {
			t.Fatalf("hello source missing %q", required)
		}
	}
}

func TestHelloUsesProductionSubstrate(t *testing.T) {
	main := readRepoFile(t, "examples/hello/main.wrela")
	for _, want := range []string{
		"require_usable_region(",
		"create_arena(",
		"executor_memory(",
		"executor_memory_near(",
		"require_separate_physical_cores(",
		"require_periodic(period_us = 1000)",
		"timer.subscribe(subscriber = worker_slot)",
		"route_shared_irq(",
	} {
		if !strings.Contains(main, want) {
			t.Fatalf("hello main missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"MutableBytes(address = 0x",
		"arena_base = 0x",
		"two-vCPU",
		"q35",
	} {
		if strings.Contains(main, forbidden) {
			t.Fatalf("hello main contains forbidden shortcut %q", forbidden)
		}
	}
}

func TestHelloWakeStrategyDefaultsToStiHlt(t *testing.T) {
	checked := checkProgramAt(t, "examples/hello/main.wrela")
	reportImage := sem.BuildImageReport(checked)
	found := false
	for _, wake := range reportImage.Runtime.WakePaths {
		if wake.SlotLabel != "console" {
			continue
		}
		found = true
		if wake.Strategy != "sti_hlt" || wake.Fallback != "sti_hlt" {
			t.Fatalf("console wake path = %#v, want sti_hlt strategy and fallback", wake)
		}
	}
	if !found {
		t.Fatalf("console wake path missing: %#v", reportImage.Runtime.WakePaths)
	}
}

func TestNoLegacyQ35DiscoveryAssumptions(t *testing.T) {
	forbidden := []string{
		"Q35" + "PciInterruptConfigurator",
		"Pci" + "ConfigPorts",
		"0xFEC" + "00000",
		"MutableBytes(address = " + "0x200000",
		"LocalApic(base = " + "0xFEE00000",
		"lapicBase",
		"emitLapicWrite(e, lapic",
	}
	for _, root := range []string{"examples", "tests/e2e/fixtures", "wrela", "compiler/codegen"} {
		scanRepoSourceTree(t, root, forbidden)
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
		"PE MZ header":            []byte{'M', 'Z'},
		"hello newline data":      []byte("hello from wrela\n\x00"),
		"serial interrupt prefix": []byte("serial interrupt: \x00"),
		"msi interrupt data":      []byte("msi interrupt\n\x00"),
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
	if got := len(result.Image.InterruptBindings); got != 2 {
		t.Fatalf("interrupt bindings = %d, want 2", got)
	}
	gotVectors := map[uint8]bool{}
	for _, binding := range result.Image.InterruptBindings {
		gotVectors[binding.Vector] = true
		if binding.Vector == 0x40 && binding.TopicKind != "serial_rx" {
			t.Fatalf("serial interrupt binding TopicKind = %q, want serial_rx: %#v", binding.TopicKind, binding)
		}
	}
	for _, want := range []uint8{0x40, 0x41} {
		if !gotVectors[want] {
			t.Fatalf("missing vector %#x in bindings %#v", want, result.Image.InterruptBindings)
		}
	}
	if gotVectors[0x42] {
		t.Fatalf("normal hello image must not bind ivshmem vector 0x42: %#v", result.Image.InterruptBindings)
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

func TestHelloInterruptTopicContextStoreUsesPathFieldOffset(t *testing.T) {
	program := compileHelloProgram(t)
	fn := findIRFunction(program, program.Entry.OwnedPhaseSymbol)
	if fn == nil {
		t.Fatalf("missing IR function %s", program.Entry.OwnedPhaseSymbol)
	}
	store, ok := operationInList[*ir.InterruptContextStore](fn.Blocks[0].Ops)
	if !ok {
		t.Fatalf("%s missing interrupt context store: %#v", fn.Symbol, fn.Blocks)
	}
	var serialBinding *ir.InterruptBinding
	for i := range program.InterruptBindings {
		if program.InterruptBindings[i].TopicKind == "serial_rx" {
			serialBinding = &program.InterruptBindings[i]
			break
		}
	}
	if serialBinding == nil {
		t.Fatalf("hello missing serial_rx interrupt binding: %#v", program.InterruptBindings)
	}
	if store.ContextSymbol != serialBinding.ContextSymbol || store.ContextOffset != serialBinding.PathFieldOffset {
		t.Fatalf("context store = (%q, %d), want (%q, %d)", store.ContextSymbol, store.ContextOffset, serialBinding.ContextSymbol, serialBinding.PathFieldOffset)
	}
}

func TestHelloOwnedPhaseEntersExecutor(t *testing.T) {
	program := compileHelloProgram(t)
	fn := findIRFunction(program, program.Entry.OwnedPhaseSymbol)
	if fn == nil {
		t.Fatalf("missing IR function %s", program.Entry.OwnedPhaseSymbol)
	}
	enter, ok := functionOp[ir.VcpuEnter](*fn)
	if !ok || enter.VcpuID != 0 || enter.SlotLabel != "console" {
		t.Fatalf("%s missing VcpuEnter for console on vCPU0: %#v", program.Entry.OwnedPhaseSymbol, fn.Blocks)
	}
	start, ok := functionOp[ir.VcpuStart](*fn)
	if !ok || start.VcpuID != 1 || start.SlotLabel != "worker" {
		t.Fatalf("%s missing VcpuStart for worker on vCPU1: %#v", program.Entry.OwnedPhaseSymbol, fn.Blocks)
	}
}

func TestHelloIRCallGraphReachesSerialWrite(t *testing.T) {
	program := compileHelloProgram(t)
	assertFunctionCalls(t, program,
		"_wrela_method_examples_hello_program_HelloWorld_run",
		"_wrela_method_machine_x86_64_executor_memory_ExecutorMemory_bytes",
		"_wrela_method_machine_x86_64_interrupts_ApicInterruptController_initialize_for_com1_receive",
		"_wrela_method_machine_x86_64_pci_MsiCapability_route",
		"_wrela_method_machine_x86_64_interrupts_ApicInterruptController_enable_cpu_interrupts",
		"_wrela_method_machine_x86_64_edu_EduMsiPath_raise_test_interrupt",
		"_wrela_method_machine_x86_64_serial_SerialConsolePath_write",
	)
	assertFunctionCalls(t, program,
		program.Entry.OwnedPhaseSymbol,
		"_wrela_method_machine_x86_64_serial_SerialConsolePath_enable_receive_interrupts",
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

func TestHelloSourceUsesArenaFrames(t *testing.T) {
	source := readRepoFile(t, "examples/hello/program.wrela")
	for _, want := range []string{
		"with self.memory.frame(length =",
		".place(",
		".bytes(",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("hello program missing %q", want)
		}
	}
	if strings.Contains(source, "allocate_bytes") || strings.Contains(source, "static_bytes") {
		t.Fatalf("hello program must not use old memory vocabulary")
	}
	for _, forbidden := range []string{"owner = hardware.vcpu0", "hardware.vcpu0.memory", "hello.run()", "on serial_path.interrupt"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("hello program contains old executor wiring %q", forbidden)
		}
	}
	mainSource := readRepoFile(t, "examples/hello/main.wrela")
	for _, forbidden := range []string{"owner = hardware.vcpu0", "hardware.vcpu0.memory", "hello.run()", "on serial_path.interrupt"} {
		if strings.Contains(mainSource, forbidden) {
			t.Fatalf("hello main contains old executor wiring %q", forbidden)
		}
	}
}

func TestProductionSourcesUseExplicitExecutorContracts(t *testing.T) {
	forbidden := []string{
		"ExecutorPlacement",
		"owner = hardware.vcpu0",
		"hardware.vcpu0.memory",
		"hello.run()",
		"on serial_path.interrupt",
	}
	for _, root := range []string{"examples", filepath.Join("tests", "e2e", "fixtures"), "wrela"} {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		repoRoot := filepath.Clean(filepath.Join(wd, ".."))
		err = filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".wrela" {
				return nil
			}
			sourcePath, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			source := readRepoFile(t, sourcePath)
			for _, pattern := range forbidden {
				if strings.Contains(source, pattern) {
					t.Fatalf("%s contains old executor wiring %q", sourcePath, pattern)
				}
			}
			if (root == "examples" || root == filepath.Join("tests", "e2e", "fixtures")) &&
				(strings.Contains(source, "SerialConsolePath(") || strings.Contains(source, ".enable_receive_interrupts()")) {
				t.Fatalf("%s contains old serial console source wiring", sourcePath)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

func TestEduInterruptReceiverAcknowledgesBeforeTopicDelivery(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/edu.wrela")
	if !strings.Contains(source, "let status = self.mmio.read32(offset = 0x24)") ||
		!strings.Contains(source, "self.mmio.write32(offset = 0x64, value = status)") {
		t.Fatalf("EduMsiPath interrupt receiver must acknowledge device status before returning event")
	}
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

func functionOp[T ir.Operation](fn ir.Function) (T, bool) {
	var zero T
	for _, block := range fn.Blocks {
		if op, ok := operationInList[T](block.Ops); ok {
			return op, true
		}
	}
	return zero, false
}

func operationInList[T ir.Operation](ops []ir.Operation) (T, bool) {
	var zero T
	for _, op := range ops {
		if typed, ok := op.(T); ok {
			return typed, true
		}
		switch v := op.(type) {
		case *ir.If:
			if typed, ok := operationInList[T](v.ConditionOps); ok {
				return typed, true
			}
			if typed, ok := operationInList[T](v.Then); ok {
				return typed, true
			}
			if typed, ok := operationInList[T](v.Else); ok {
				return typed, true
			}
		case *ir.While:
			if typed, ok := operationInList[T](v.ConditionOps); ok {
				return typed, true
			}
			if typed, ok := operationInList[T](v.Body); ok {
				return typed, true
			}
		case *ir.ForBytes:
			if typed, ok := operationInList[T](v.IterableOps); ok {
				return typed, true
			}
			if typed, ok := operationInList[T](v.Body); ok {
				return typed, true
			}
		}
	}
	return zero, false
}

func compileHelloProgram(t *testing.T) *ir.Program {
	t.Helper()
	return compileProgramAt(t, "examples/hello/main.wrela")
}

func compileProgramAt(t *testing.T, rootPath string) *ir.Program {
	t.Helper()
	checked := checkProgramAt(t, rootPath)
	program, ds := ir.Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("Lower diagnostics: %#v", ds)
	}
	return program
}

func checkProgramAt(t *testing.T, rootPath string) *sem.CheckedProgram {
	t.Helper()
	repoRoot := resolveRepoRoot(".")
	graph, err := source.LoadGraph(source.Options{
		RootPath: filepath.Join(repoRoot, rootPath),
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
	return checked
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

func scanRepoSourceTree(t *testing.T, root string, forbidden []string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, ".."))
	if err := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".wrela":
		default:
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			rel = path
		}
		for _, needle := range forbidden {
			if strings.Contains(text, needle) {
				t.Fatalf("%s contains legacy hardware assumption %q", rel, needle)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, ".."))
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

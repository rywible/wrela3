package ir

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/sem"
)

func TestLowerReturnsCG0001ForNilProgram(t *testing.T) {
	_, diags := Lower(nil)
	if len(diags) != 1 || diags[0].Code != diag.CG0001 {
		t.Fatalf("diags = %#v, want one CG0001", diags)
	}
}

func TestLowerSynthesizesEntryAdapterFromImage(t *testing.T) {
	image := &sem.Type{Module: "m", Name: "Boot", Kind: sem.KindImage}
	checked := &sem.CheckedProgram{
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"m": {"Boot": image},
		}},
	}
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}
	if program.Entry.Symbol != "_wrela_efi_entry" {
		t.Fatalf("entry symbol = %q", program.Entry.Symbol)
	}
	if program.Entry.DelegatedPhaseSymbol == "" || program.Entry.OwnedPhaseSymbol == "" {
		t.Fatalf("entry phase symbols not set: %#v", program.Entry)
	}
}

func TestLowerStringLiteralAddressIsPhysicalAddress(t *testing.T) {
	ctx := newLowerContext(&sem.CheckedProgram{})
	info, ok := ctx.program.Types["StringLiteral"]
	if !ok {
		t.Fatalf("missing StringLiteral type info")
	}
	address, ok := info.Fields["address"]
	if !ok {
		t.Fatalf("missing StringLiteral.address field info")
	}
	if address.Type.Name != "PhysicalAddress" || address.Type.Kind != TypeKindPrimitive {
		t.Fatalf("StringLiteral.address type = %#v, want PhysicalAddress primitive", address.Type)
	}
}

func TestLowerUsesSourceVisiblePhaseAndExecutorPath(t *testing.T) {
	executorType := &sem.Type{
		Module: "examples.hello.program",
		Name:   "HelloWorld",
		Kind:   sem.KindExecutor,
		Methods: []sem.Method{{
			Name:   "run",
			Return: &sem.Type{Name: "never", Kind: sem.KindPrimitive},
		}, {
			Name:   "source_marker",
			Return: &sem.Type{Name: "void", Kind: sem.KindPrimitive},
		}},
	}
	checked := &sem.CheckedProgram{
		Modules: []*ast.Module{{
			Name: "examples.hello.program",
			Decls: []ast.Decl{&ast.ExecutorDecl{
				Name: "HelloWorld",
				Methods: []ast.MethodDecl{{
					Name:    "run",
					IsStart: true,
					Return:  "never",
					Body: []ast.Stmt{&ast.ExprStmt{Expr: &ast.CallExpr{
						Receiver: &ast.NameExpr{Name: "self"},
						Method:   "source_marker",
					}}},
				}},
			}},
		}},
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"examples.hello.program": {"HelloWorld": executorType},
		}},
	}
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}

	executor := findFunction(program, "_wrela_method_examples_hello_program_HelloWorld_run")
	if executor == nil {
		t.Fatal("missing lowered executor start function")
	}
	if !functionCalls(*executor, "_wrela_method_examples_hello_program_HelloWorld_source_marker") {
		t.Fatalf("executor did not lower source call: %#v", executor.Blocks)
	}
	if method := findAsmMethod(program, "_wrela_method_examples_hello_program_HelloWorld_run"); method != nil && strings.Contains(method.Body, "mov dx, 0x03f8") {
		t.Fatalf("executor start still lowered to fixed serial asm: %q", method.Body)
	}
}

func TestLowerKeepsAsmMethodsThatUseUEFIBridgeInstructions(t *testing.T) {
	bootServices := &sem.Type{
		Module: "platform.uefi.boot_services",
		Name:   "UefiBootServicesCalls",
		Kind:   sem.KindClass,
		Methods: []sem.Method{{
			Name:    "exit_boot_services",
			IsAsm:   true,
			AsmBody: &ast.AsmBody{Source: "sub rsp, 32\nmov rax, self.boot_services\ncall rax\nadd rsp, 32\nret"},
		}},
	}
	checked := &sem.CheckedProgram{
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"platform.uefi.boot_services": {"UefiBootServicesCalls": bootServices},
		}},
	}
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}
	method := findAsmMethod(program, "_wrela_method_platform_uefi_boot_services_UefiBootServicesCalls_exit_boot_services")
	if method == nil {
		t.Fatal("asm method using call rax was skipped")
	}
	if !strings.Contains(method.Body, "call rax") {
		t.Fatalf("asm body changed unexpectedly: %q", method.Body)
	}
}

func TestLowerStoresModuleQualifiedTypeInfoForSameNameTypes(t *testing.T) {
	dataResult := &sem.Type{Module: "data.mod", Name: "Result", Kind: sem.KindData}
	classResult := &sem.Type{Module: "class.mod", Name: "Result", Kind: sem.KindClass}
	checked := &sem.CheckedProgram{
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"data.mod":  {"Result": dataResult},
			"class.mod": {"Result": classResult},
		}},
	}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}
	if got := program.Types["data.mod.Result"].Kind; got != TypeKindData {
		t.Fatalf("data.mod.Result kind = %s, want %s", got, TypeKindData)
	}
	if got := program.Types["class.mod.Result"].Kind; got != TypeKindClass {
		t.Fatalf("class.mod.Result kind = %s, want %s", got, TypeKindClass)
	}
}

func TestLowerAsmMethodsAreDeterministicByModuleTypeMethod(t *testing.T) {
	types := map[string]map[string]*sem.Type{
		"z.module": {
			"ZType": asmTestType("z.module", "ZType", "beta", "alpha"),
		},
		"a.module": {
			"BType": asmTestType("a.module", "BType", "beta", "alpha"),
			"AType": asmTestType("a.module", "AType", "beta", "alpha"),
		},
	}
	checked := &sem.CheckedProgram{Index: &sem.Index{ByModule: types}}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}

	var got []string
	for _, method := range program.AsmMethods {
		got = append(got, method.Symbol)
	}
	want := []string{
		"_wrela_method_a_module_AType_alpha",
		"_wrela_method_a_module_AType_beta",
		"_wrela_method_a_module_BType_alpha",
		"_wrela_method_a_module_BType_beta",
		"_wrela_method_z_module_ZType_alpha",
		"_wrela_method_z_module_ZType_beta",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("asm method order = %#v, want %#v", got, want)
	}
}

func TestLowerFieldAssignmentEvaluatesTargetObjectBeforeValue(t *testing.T) {
	u64 := &sem.Type{Name: "U64", Kind: sem.KindPrimitive}
	holder := &sem.Type{
		Module: "test.assign",
		Name:   "Holder",
		Kind:   sem.KindClass,
		Fields: []sem.Field{{Name: "field", Type: u64}},
	}
	tester := &sem.Type{
		Module: "test.assign",
		Name:   "Tester",
		Kind:   sem.KindClass,
		Methods: []sem.Method{
			{Name: "value_marker", Return: u64},
			{Name: "target_marker", Return: holder},
			{Name: "run", Return: &sem.Type{Name: "void", Kind: sem.KindPrimitive}},
		},
	}
	checked := &sem.CheckedProgram{
		Modules: []*ast.Module{{
			Name: "test.assign",
			Decls: []ast.Decl{&ast.ClassDecl{
				Name: "Tester",
				Methods: []ast.MethodDecl{{
					Name:   "run",
					Return: "void",
					Body: []ast.Stmt{&ast.AssignStmt{
						Target: &ast.FieldExpr{
							Base:  &ast.CallExpr{Receiver: &ast.NameExpr{Name: "self"}, Method: "target_marker"},
							Field: "field",
						},
						Value: &ast.CallExpr{Receiver: &ast.NameExpr{Name: "self"}, Method: "value_marker"},
					}},
				}},
			}},
		}},
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"test.assign": {
				"Holder": holder,
				"Tester": tester,
			},
		}},
	}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}
	fn := findFunction(program, "_wrela_method_test_assign_Tester_run")
	if fn == nil {
		t.Fatal("missing lowered run method")
	}
	assertFunctionCallOrder(t, *fn,
		"_wrela_method_test_assign_Tester_target_marker",
		"_wrela_method_test_assign_Tester_value_marker",
	)
}

func TestLowerWithFrameReserveAndPlace(t *testing.T) {
	checked := checkedProgramForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data MutableBytes { address: PhysicalAddress; length: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 64) as tick {
            let msg = tick.place(Message(id = 1))
            let raw = tick.reserve(length = 32, align = 8)
        }
        while true {}
    }
}
`)
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("lower diagnostics: %#v", diags)
	}
	fn := findFunction(program, "_wrela_method_machine_x86_64_executor_memory_Worker_run")
	if fn == nil {
		t.Fatal("missing Worker.run")
	}
	if !containsOp[*FrameBegin](*fn) || !containsOp[*ArenaPlace](*fn) || !containsOp[*ArenaReserve](*fn) || !containsOp[*FrameEnd](*fn) {
		t.Fatalf("lowered function missing arena ops: %#v", fn.Blocks)
	}
}

func TestLowerBitwiseAndShiftOperatorsMapToIRShiftAndBitOr(t *testing.T) {
	bitOpType := &sem.Type{Name: "U64", Kind: sem.KindPrimitive}
	typeDecl := &sem.Type{
		Module: "test.shift",
		Name:   "OperatorSuite",
		Kind:   sem.KindClass,
		Methods: []sem.Method{{
			Name:   "run",
			Return: bitOpType,
		}},
	}

	checked := &sem.CheckedProgram{
		Modules: []*ast.Module{{
			Name: "test.shift",
			Decls: []ast.Decl{&ast.ClassDecl{
				Name: "OperatorSuite",
				Methods: []ast.MethodDecl{{
					Name:   "run",
					Return: "U64",
					Params: []ast.Param{
						{Name: "left", Type: "U64"},
						{Name: "right", Type: "U64"},
					},
					Body: []ast.Stmt{
						&ast.LetStmt{
							Name: "orValue",
							Expr: &ast.BinaryExpr{
								Op:    "|",
								Left:  &ast.NameExpr{Name: "left"},
								Right: &ast.NameExpr{Name: "right"},
							},
						},
						&ast.LetStmt{
							Name: "shiftedLeft",
							Expr: &ast.BinaryExpr{
								Op:    "<<",
								Left:  &ast.NameExpr{Name: "orValue"},
								Right: &ast.IntLiteral{Value: "11"},
							},
						},
						&ast.ReturnStmt{Value: &ast.BinaryExpr{
							Op:    ">>",
							Left:  &ast.NameExpr{Name: "shiftedLeft"},
							Right: &ast.IntLiteral{Value: "3"},
						}},
					},
				}},
			}},
		}},
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"test.shift": {
				"OperatorSuite": typeDecl,
				"U64":           bitOpType,
			},
		}},
	}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("Lower() diagnostics = %#v", diags)
	}

	fn := findFunction(program, symbolName("method", "test.shift", "OperatorSuite", "run"))
	if fn == nil {
		t.Fatal("missing lowered run method")
	}

	var got []string
	for _, op := range fn.Blocks[0].Ops {
		if b, ok := op.(*Binary); ok {
			got = append(got, b.Op)
		}
	}

	want := []string{"or", "shl", "shr"}
	if len(got) != len(want) {
		t.Fatalf("binary ops = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("binary ops[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLowerInterruptTopicBindingMetadata(t *testing.T) {
	checked := checkedProgramForTest(t, `
module machine.x86_64.serial

data SerialPathInterrupt { byte: U8 }

driver path SerialConsolePath {
    interrupt receiver -> SerialPathInterrupt {
        return SerialPathInterrupt(byte = 1)
    }
}
`)
	checked.ImageGraph.InterruptTopicRoutes = []sem.InterruptTopicRouteNode{{
		Vector:              0x40,
		PathLabel:           "console.com1",
		PathBinding:         "serial_path",
		ContextSymbol:       "_wrela_interrupt_context_console_com1",
		PathFieldOffset:     24,
		TopicLabel:          "console.com1.rx",
		TopicKind:           "serial_rx",
		EventType:           "machine.x86_64.serial.SerialPathInterrupt",
		EventFunctionSymbol: "_wrela_event_machine_x86_64_serial_SerialConsolePath_interrupt",
		SubscriberSlots:     []string{"console"},
	}}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("Lower() diagnostics = %#v", diags)
	}

	eventSymbol := symbolName("event_fn", "machine.x86_64.serial", "SerialConsolePath", "interrupt")
	eventFn := findFunction(program, eventSymbol)
	if eventFn == nil {
		t.Fatalf("missing lowered interrupt event function %q", eventSymbol)
	}
	if len(eventFn.Params) != 1 {
		t.Fatalf("event params = %d, want self", len(eventFn.Params))
	}

	if len(program.InterruptEvents) != 1 {
		t.Fatalf("interrupt events = %#v, want one", program.InterruptEvents)
	}
	if got := program.InterruptEvents[0].Symbol; got != "interrupt_event::machine.x86_64.serial::SerialConsolePath::interrupt" {
		t.Fatalf("event symbol = %q", got)
	}
	if program.InterruptEvents[0].FunctionSymbol != eventSymbol {
		t.Fatalf("event function symbol = %q, want %q", program.InterruptEvents[0].FunctionSymbol, eventSymbol)
	}

	if len(program.OnHandlers) != 0 {
		t.Fatalf("on handlers = %#v, want none", program.OnHandlers)
	}

	if len(program.InterruptBindings) != 1 {
		t.Fatalf("interrupt bindings = %#v, want one", program.InterruptBindings)
	}
	binding := program.InterruptBindings[0]
	if binding.Vector != 0x40 {
		t.Fatalf("binding vector = %#x, want 0x40", binding.Vector)
	}
	if binding.HandlerFunctionSymbol != "" {
		t.Fatalf("handler function symbol = %q, want empty", binding.HandlerFunctionSymbol)
	}
	if binding.PathFieldOffset != 24 {
		t.Fatalf("binding path offset = %d, want route offset 24", binding.PathFieldOffset)
	}
	wantEventStorageSize := program.Types["machine.x86_64.serial.SerialPathInterrupt"].StorageSize
	if binding.EventStorageSize != wantEventStorageSize {
		t.Fatalf("binding event storage size = %d, want %d", binding.EventStorageSize, wantEventStorageSize)
	}
	if binding.EventStorageSymbol != "_wrela_interrupt_event_40" {
		t.Fatalf("binding event storage symbol = %q", binding.EventStorageSymbol)
	}
	if binding.TopicLabel != "console.com1.rx" || binding.TopicKind != "serial_rx" {
		t.Fatalf("binding topic = %q/%q, want console.com1.rx/serial_rx", binding.TopicLabel, binding.TopicKind)
	}
	if binding.PublisherOwnerKind != "driver_path" || binding.PublisherOwnerLabel != "console.com1" {
		t.Fatalf("binding owner = %q/%q, want driver_path/console.com1", binding.PublisherOwnerKind, binding.PublisherOwnerLabel)
	}
	if len(binding.SubscriberSlots) != 1 || binding.SubscriberSlots[0] != "console" {
		t.Fatalf("binding subscribers = %#v, want [console]", binding.SubscriberSlots)
	}
	if binding.ContextSymbol != "_wrela_interrupt_context_console_com1" {
		t.Fatalf("binding context = %q", binding.ContextSymbol)
	}
}

func TestLowerInterruptTopicRouteMissingVectorDiagnostic(t *testing.T) {
	checked := checkedProgramForTest(t, `
module machine.x86_64.serial

data SerialPathInterrupt { byte: U8 }

driver path SerialConsolePath {
    interrupt receiver -> SerialPathInterrupt {
        return SerialPathInterrupt(byte = 0)
    }
}
`)
	checked.ImageGraph.InterruptTopicRoutes = []sem.InterruptTopicRouteNode{{
		TopicLabel:          "console.com1.rx",
		TopicKind:           "serial_rx",
		EventType:           "machine.x86_64.serial.SerialPathInterrupt",
		EventFunctionSymbol: "_wrela_event_machine_x86_64_serial_SerialConsolePath_interrupt",
	}}

	_, diags := Lower(checked)
	if len(diags) != 1 {
		t.Fatalf("Lower() diagnostics = %#v, want one", diags)
	}
	if diags[0].Code != diag.SEM0043 || diags[0].Message != "interrupt route missing vector" {
		t.Fatalf("diagnostic = %#v, want SEM0043 interrupt route missing vector", diags[0])
	}
}

func asmTestType(module, name string, methods ...string) *sem.Type {
	typ := &sem.Type{Module: module, Name: name, Kind: sem.KindClass}
	for _, method := range methods {
		typ.Methods = append(typ.Methods, sem.Method{
			Name:    method,
			IsAsm:   true,
			AsmBody: &ast.AsmBody{Source: "ret"},
		})
	}
	return typ
}

func findFunction(program *Program, symbol string) *Function {
	for i := range program.Functions {
		if program.Functions[i].Symbol == symbol {
			return &program.Functions[i]
		}
	}
	return nil
}

func containsOp[T any](fn Function) bool {
	for _, block := range fn.Blocks {
		for _, op := range block.Ops {
			if _, ok := any(op).(T); ok {
				return true
			}
		}
	}
	return false
}

func functionCalls(fn Function, symbol string) bool {
	for _, block := range fn.Blocks {
		for _, op := range block.Ops {
			if call, ok := op.(*Call); ok && call.Symbol == symbol {
				return true
			}
		}
	}
	return false
}

func findAsmMethod(program *Program, symbol string) *AsmMethod {
	for i := range program.AsmMethods {
		if program.AsmMethods[i].Symbol == symbol {
			return &program.AsmMethods[i]
		}
	}
	return nil
}

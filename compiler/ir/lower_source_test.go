package ir

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/sem"
)

func TestLowerImagePhaseBodiesFromSourceCallsInOrder(t *testing.T) {
	ownedRoot := &sem.Type{Module: "test.phase", Name: "OwnedHardware", Kind: sem.KindClass}
	imageType := &sem.Type{Module: "test.phase", Name: "BootImage", Kind: sem.KindImage}
	checked := &sem.CheckedProgram{
		Modules: []*ast.Module{{
			Name: "test.phase",
			Decls: []ast.Decl{&ast.ImageDecl{
				Name: "BootImage",
				Phases: []ast.PhaseDecl{
					{
						Name:   "delegated_hardware",
						Params: []ast.Param{{Name: "hardware", Type: "DelegatedHardware"}},
						Return: "OwnedHardware",
						Body: []ast.Stmt{
							&ast.LetStmt{
								Name: "owned",
								Expr: &ast.CallExpr{
									Receiver: &ast.NameExpr{Name: "hardware"},
									Method:   "claim_for_test",
								},
							},
							&ast.ReturnStmt{Value: &ast.NameExpr{Name: "owned"}},
						},
					},
					{
						Name:   "owned_hardware",
						Params: []ast.Param{{Name: "hardware", Type: "OwnedHardware"}},
						Return: "never",
						Body: []ast.Stmt{
							&ast.ExprStmt{Expr: &ast.CallExpr{
								Receiver: &ast.NameExpr{Name: "hardware"},
								Method:   "prepare_for_test",
							}},
							&ast.ExprStmt{Expr: &ast.CallExpr{
								Receiver: &ast.NameExpr{Name: "hardware"},
								Method:   "launch_for_test",
							}},
						},
					},
				},
			}},
		}},
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"test.phase": {"BootImage": imageType},
		}},
		OwnedRoot: ownedRoot,
	}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}

	delegated := findFunction(program, program.Entry.DelegatedPhaseSymbol)
	if delegated == nil {
		t.Fatalf("missing delegated phase function %q", program.Entry.DelegatedPhaseSymbol)
	}
	if !delegated.PreserveStackReturn {
		t.Fatalf("delegated phase must preserve the owned stack when returning across the transition")
	}
	assertFunctionCallOrder(t, *delegated,
		symbolName("method", "test.phase", "DelegatedHardware", "claim_for_test"),
	)
	if functionCalls(*delegated, symbolName("method", "platform.uefi.transition", "DelegatedHardware", "exit_to_owned_hardware")) {
		t.Fatalf("delegated phase used hardcoded ownership-transfer method: %#v", delegated.Blocks)
	}

	owned := findFunction(program, program.Entry.OwnedPhaseSymbol)
	if owned == nil {
		t.Fatalf("missing owned phase function %q", program.Entry.OwnedPhaseSymbol)
	}
	assertFunctionCallOrder(t, *owned,
		symbolName("method", "test.phase", "OwnedHardware", "prepare_for_test"),
		symbolName("method", "test.phase", "OwnedHardware", "launch_for_test"),
	)
}

func TestLowerExecutorStartMethodFromSourceBody(t *testing.T) {
	executorSymbol := symbolName("method", "test.executor", "BootExecutor", "run")
	checked := &sem.CheckedProgram{
		Modules: []*ast.Module{{
			Name: "test.executor",
			Decls: []ast.Decl{&ast.ExecutorDecl{
				Name: "BootExecutor",
				Methods: []ast.MethodDecl{{
					Name:    "run",
					IsStart: true,
					Return:  "never",
					Body: []ast.Stmt{&ast.ExprStmt{Expr: &ast.CallExpr{
						Receiver: &ast.NameExpr{Name: "self"},
						Method:   "source_start_marker",
					}}},
				}},
			}},
		}},
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{}},
	}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}
	if method := findAsmMethod(program, executorSymbol); method != nil {
		if strings.Contains(method.Body, "mov dx, 0x03f8") || strings.Contains(method.Body, "out dx, al") || strings.Contains(method.Body, "owned_halt:") {
			t.Fatalf("executor start lowered to fixed serial asm body:\n%s", method.Body)
		}
	}
	fn := findFunction(program, executorSymbol)
	if fn == nil {
		t.Fatalf("executor start method %q was not lowered as source IR function", executorSymbol)
	}
	assertFunctionCallOrder(t, *fn,
		symbolName("method", "test.executor", "BootExecutor", "source_start_marker"),
	)
}

func TestLowerNoReturnMethodCallUsesVoidType(t *testing.T) {
	consoleType := &sem.Type{
		Module: "test.void_call",
		Name:   "Console",
		Kind:   sem.KindClass,
		Methods: []sem.Method{{
			Name: "write",
		}},
	}
	ownedType := &sem.Type{
		Module: "test.void_call",
		Name:   "OwnedHardware",
		Kind:   sem.KindClass,
		Fields: []sem.Field{{Name: "console", Type: consoleType}},
		Methods: []sem.Method{{
			Name: "launch",
		}},
	}
	checked := &sem.CheckedProgram{
		Modules: []*ast.Module{{
			Name: "test.void_call",
			Decls: []ast.Decl{
				&ast.ClassDecl{
					Name: "Console",
					Methods: []ast.MethodDecl{{
						Name: "write",
					}},
				},
				&ast.ClassDecl{
					Name: "OwnedHardware",
					Fields: []ast.Field{{
						Name: "console",
						Type: "Console",
					}},
					Methods: []ast.MethodDecl{{
						Name: "launch",
						Body: []ast.Stmt{&ast.ExprStmt{Expr: &ast.CallExpr{
							Receiver: &ast.FieldExpr{
								Base:  &ast.NameExpr{Name: "self"},
								Field: "console",
							},
							Method: "write",
						}}},
					}},
				},
			},
		}},
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"test.void_call": {
				"Console":       consoleType,
				"OwnedHardware": ownedType,
			},
		}},
		OwnedRoot: ownedType,
	}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("Lower() diagnostics = %#v", diags)
	}

	fn := findFunction(program, symbolName("method", "test.void_call", "OwnedHardware", "launch"))
	if fn == nil {
		t.Fatal("missing OwnedHardware.launch")
	}
	call, ok := functionOp[*Call](*fn)
	if !ok {
		t.Fatalf("OwnedHardware.launch missing call: %#v", fn.Blocks)
	}
	if call.Type.Name != "void" {
		t.Fatalf("no-return call type = %#v, want void", call.Type)
	}
}

func TestLowerClaimExecutorArenaMaterializesInCaller(t *testing.T) {
	checked := checkedProgramFromSourcesForTest(t, `
module machine.x86_64.executor_memory

data MutableBytes {
    address: PhysicalAddress
    length: U64
}

class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
}

module machine.x86_64.cpu_state

use { ExecutorMemory, MutableBytes } from machine.x86_64.executor_memory

data ExecutorSlot {
    id: U64
}

class OwnedMemory {
    arena: MutableBytes

    asm fn claim_executor_arena(self, owner: ExecutorSlot, length: U64, align: U64) -> ExecutorMemory {
        ret
    }
}

class Boot {
    memory: OwnedMemory
    slot: ExecutorSlot

    fn run(self) {
        let first = self.memory.claim_executor_arena(owner = self.slot, length = 4096, align = 4096)
        let second = self.memory.claim_executor_arena(owner = self.slot, length = 8192, align = 4096)
    }
}
`)

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("Lower() diagnostics = %#v", diags)
	}
	fn := findFunction(program, symbolName("method", "machine.x86_64.cpu_state", "Boot", "run"))
	if fn == nil {
		t.Fatal("missing Boot.run")
	}
	if functionCalls(*fn, symbolName("method", "machine.x86_64.cpu_state", "OwnedMemory", "claim_executor_arena")) {
		t.Fatalf("claim_executor_arena should lower as caller-side intrinsic: %#v", fn.Blocks)
	}
	var constructs []*Construct
	var stores []*FieldStore
	var addCount, andCount, subCount int
	for _, block := range fn.Blocks {
		for _, op := range block.Ops {
			switch op := op.(type) {
			case *Construct:
				if op.Type.Name == "ExecutorMemory" {
					constructs = append(constructs, op)
				}
			case *FieldStore:
				if op.ObjectType == "MutableBytes" && op.Field == "address" {
					stores = append(stores, op)
				}
			case *Binary:
				switch op.Op {
				case "add":
					addCount++
				case "and":
					andCount++
				case "sub":
					subCount++
				}
			}
		}
	}
	if len(constructs) != 2 {
		t.Fatalf("Boot.run ExecutorMemory constructs = %d, want 2: %#v", len(constructs), fn.Blocks)
	}
	if len(stores) != 2 {
		t.Fatalf("Boot.run arena address stores = %d, want 2: %#v", len(stores), fn.Blocks)
	}
	for _, construct := range constructs {
		base := constructFieldValue(construct, "arena_base")
		if binary, ok := base.(*Binary); !ok || binary.Op != "and" {
			t.Fatalf("ExecutorMemory arena_base = %#v, want aligned arena address", base)
		}
	}
	if addCount < 4 || andCount < 2 || subCount < 4 {
		t.Fatalf("Boot.run missing alignment/allocation arithmetic: add=%d and=%d sub=%d ops=%#v", addCount, andCount, subCount, fn.Blocks)
	}
}

func TestLowerUsesSourceAsmForDelegatedHardwareExitToOwnedHardware(t *testing.T) {
	transitionSymbol := symbolName("method", "platform.uefi.transition", "DelegatedHardware", "exit_to_owned_hardware")
	sourceBody := "source_transfer_marker:\nret"
	delegatedHardware := &sem.Type{
		Module: "platform.uefi.transition",
		Name:   "DelegatedHardware",
		Kind:   sem.KindClass,
		Methods: []sem.Method{{
			Name:    "exit_to_owned_hardware",
			IsAsm:   true,
			AsmBody: &ast.AsmBody{Source: sourceBody},
			Return:  &sem.Type{Name: "OwnedHardware"},
		}},
	}
	checked := &sem.CheckedProgram{
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"platform.uefi.transition": {"DelegatedHardware": delegatedHardware},
		}},
	}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}

	var matches []AsmMethod
	for _, method := range program.AsmMethods {
		if method.Symbol == transitionSymbol {
			matches = append(matches, method)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("transition asm method count = %d, want exactly one source method: %#v", len(matches), matches)
	}
	if matches[0].Body != sourceBody {
		t.Fatalf("transition asm body = %q, want source body %q", matches[0].Body, sourceBody)
	}
	if strings.Contains(matches[0].Body, "fill_identity_pd:") || strings.Contains(matches[0].Body, "mov cr3, rax") {
		t.Fatalf("transition asm body contains synthesized transfer code:\n%s", matches[0].Body)
	}
}

func TestLowerMarksOwnershipTransferReturnIndependentOfMethodName(t *testing.T) {
	ownedRoot := &sem.Type{Module: "test.transfer", Name: "OwnedHardware", Kind: sem.KindClass}
	delegatedHardware := &sem.Type{
		Module:        "test.transfer",
		Name:          "DelegatedHardware",
		Kind:          sem.KindClass,
		Unique:        true,
		DelegatedOnly: true,
		Methods: []sem.Method{{
			Name:   "claim",
			Return: ownedRoot,
		}},
	}
	checked := &sem.CheckedProgram{
		Modules: []*ast.Module{{
			Name: "test.transfer",
			Decls: []ast.Decl{&ast.ClassDecl{
				Name:   "DelegatedHardware",
				Unique: true,
				Methods: []ast.MethodDecl{{
					Name:   "claim",
					Return: "OwnedHardware",
					Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.ConstructorExpr{
						Type: "OwnedHardware",
					}}},
				}},
			}},
		}},
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"test.transfer": {
				"DelegatedHardware": delegatedHardware,
				"OwnedHardware":     ownedRoot,
			},
		}},
		OwnedRoot: ownedRoot,
	}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("Lower() diagnostics = %#v", diags)
	}
	fn := findFunction(program, symbolName("method", "test.transfer", "DelegatedHardware", "claim"))
	if fn == nil {
		t.Fatal("missing lowered claim method")
	}
	if !fn.PreserveStackReturn {
		t.Fatalf("ownership-transfer method with non-special name must preserve stack return: %#v", fn)
	}
}

func TestLowerDoesNotPreserveStackForNonAuthorityNamedExitMethod(t *testing.T) {
	notAuthority := &sem.Type{
		Module: "test.transfer",
		Name:   "DelegatedHardware",
		Kind:   sem.KindClass,
		Methods: []sem.Method{{
			Name:   "exit_to_owned_hardware",
			Return: &sem.Type{Module: "test.transfer", Name: "OtherHardware", Kind: sem.KindClass},
		}},
	}
	checked := &sem.CheckedProgram{
		Modules: []*ast.Module{{
			Name: "test.transfer",
			Decls: []ast.Decl{&ast.ClassDecl{
				Name: "DelegatedHardware",
				Methods: []ast.MethodDecl{{
					Name:   "exit_to_owned_hardware",
					Return: "OtherHardware",
					Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.ConstructorExpr{
						Type: "OtherHardware",
					}}},
				}},
			}},
		}},
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"test.transfer": {
				"DelegatedHardware": notAuthority,
				"OtherHardware":     {Module: "test.transfer", Name: "OtherHardware", Kind: sem.KindClass},
				"OwnedHardware":     {Module: "test.transfer", Name: "OwnedHardware", Kind: sem.KindClass},
			},
		}},
		OwnedRoot: &sem.Type{Module: "test.transfer", Name: "OwnedHardware", Kind: sem.KindClass},
	}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("Lower() diagnostics = %#v", diags)
	}
	fn := findFunction(program, symbolName("method", "test.transfer", "DelegatedHardware", "exit_to_owned_hardware"))
	if fn == nil {
		t.Fatal("missing lowered exit_to_owned_hardware method")
	}
	if fn.PreserveStackReturn {
		t.Fatalf("method name alone must not trigger preserve-stack return: %#v", fn)
	}
}

func TestLowerUnsupportedImagePhaseStatementReturnsCG0001(t *testing.T) {
	imageType := &sem.Type{Module: "test.unsupported", Name: "BootImage", Kind: sem.KindImage}
	checked := &sem.CheckedProgram{
		Modules: []*ast.Module{{
			Name: "test.unsupported",
			Decls: []ast.Decl{&ast.ImageDecl{
				Name: "BootImage",
				Phases: []ast.PhaseDecl{{
					Name:   "owned_hardware",
					Params: []ast.Param{{Name: "hardware", Type: "OwnedHardware"}},
					Return: "never",
					Body: []ast.Stmt{&ast.WhileStmt{
						Cond: &ast.BoolLiteral{Value: true},
						Body: []ast.Stmt{&ast.ExprStmt{Expr: &ast.NameExpr{Name: "hardware"}}},
					}},
				}},
			}},
		}},
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"test.unsupported": {"BootImage": imageType},
		}},
	}

	_, diags := Lower(checked)
	if !hasDiagCode(diags, diag.CG0001) {
		t.Fatalf("diags = %#v, want CG0001 for unsupported source phase statement", diags)
	}
}

func TestReceiverLayoutUsesLoweredCompositeOffsets(t *testing.T) {
	info := TypeInfo{
		Name: "AsmReceiver",
		Fields: map[string]FieldInfo{
			"nested": {Name: "nested", Offset: 0, Size: 8},
			"value":  {Name: "value", Offset: 8, Size: 8},
		},
		FieldOrder: []string{"nested", "value"},
	}
	offsets, widths := receiverLayout(info)
	if offsets["value"] != 8 {
		t.Fatalf("value offset = %d, want Appendix E handle offset 8", offsets["value"])
	}
	if widths["nested"] != 64 {
		t.Fatalf("nested width = %d, want pointer-sized handle width 64", widths["nested"])
	}
}

func TestLowerAppendixECompositeFieldsAreHandles(t *testing.T) {
	u64 := &sem.Type{Name: "U64", Kind: sem.KindPrimitive}
	u32 := &sem.Type{Name: "U32", Kind: sem.KindPrimitive}
	nested := &sem.Type{
		Module: "test.layout",
		Name:   "Nested",
		Kind:   sem.KindData,
		Fields: []sem.Field{
			{Name: "lo", Type: u64},
			{Name: "hi", Type: u64},
		},
	}
	container := &sem.Type{
		Module: "test.layout",
		Name:   "Container",
		Kind:   sem.KindData,
		Fields: []sem.Field{
			{Name: "nested", Type: nested},
			{Name: "value", Type: u64},
		},
	}
	status := &sem.Type{
		Module: "test.layout",
		Name:   "Status",
		Kind:   sem.KindData,
		Fields: []sem.Field{{Name: "value", Type: u64}},
	}
	bytes := &sem.Type{
		Module: "test.layout",
		Name:   "Bytes",
		Kind:   sem.KindData,
		Fields: []sem.Field{
			{Name: "address", Type: u64},
			{Name: "length", Type: u64},
		},
	}
	memoryMap := &sem.Type{
		Module: "test.layout",
		Name:   "MemoryMap",
		Kind:   sem.KindData,
		Fields: []sem.Field{
			{Name: "descriptors", Type: bytes},
			{Name: "descriptor_size", Type: u64},
			{Name: "descriptor_version", Type: u32},
			{Name: "key", Type: u64},
		},
	}
	mapResult := &sem.Type{
		Module: "test.layout",
		Name:   "MemoryMapResult",
		Kind:   sem.KindData,
		Fields: []sem.Field{
			{Name: "status", Type: status},
			{Name: "memory_map", Type: memoryMap},
			{Name: "required_size", Type: u64},
		},
	}
	checked := &sem.CheckedProgram{Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
		"test.layout": {
			"Nested":          nested,
			"Container":       container,
			"Status":          status,
			"Bytes":           bytes,
			"MemoryMap":       memoryMap,
			"MemoryMapResult": mapResult,
		},
	}}}

	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("Lower() diagnostics = %#v", diags)
	}
	info := program.Types["Container"]
	if info.Fields["nested"].Size != 8 || info.Fields["nested"].Offset != 0 {
		t.Fatalf("nested field layout = %#v, want pointer-sized handle at +0", info.Fields["nested"])
	}
	if info.Fields["value"].Offset != 8 || info.Size != 16 {
		t.Fatalf("container layout = %#v, want value at +8 and size 16", info)
	}
	if program.Types["Nested"].Size != 16 {
		t.Fatalf("Nested object size = %d, want 16", program.Types["Nested"].Size)
	}
	mapInfo := program.Types["MemoryMap"]
	if mapInfo.Fields["descriptors"].Size != 8 ||
		mapInfo.Fields["descriptor_size"].Offset != 8 ||
		mapInfo.Fields["descriptor_version"].Offset != 16 ||
		mapInfo.Fields["key"].Offset != 24 ||
		mapInfo.Size != 32 ||
		mapInfo.StorageSize != 48 {
		t.Fatalf("MemoryMap layout = %#v, want handle descriptors and 48-byte backing storage", mapInfo)
	}
	resultInfo := program.Types["MemoryMapResult"]
	if resultInfo.Fields["status"].Offset != 0 ||
		resultInfo.Fields["memory_map"].Offset != 8 ||
		resultInfo.Fields["required_size"].Offset != 16 ||
		resultInfo.Size != 24 ||
		resultInfo.StorageSize != 80 {
		t.Fatalf("MemoryMapResult layout = %#v, want handle fields and 80-byte backing storage", resultInfo)
	}
}

func assertFunctionCallOrder(t *testing.T, fn Function, want ...string) {
	t.Helper()
	got := functionCallSymbols(fn)
	if len(got) != len(want) {
		t.Fatalf("%s calls = %#v, want %#v", fn.Symbol, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s calls = %#v, want %#v", fn.Symbol, got, want)
		}
	}
}

func functionCallSymbols(fn Function) []string {
	var out []string
	for _, block := range fn.Blocks {
		for _, op := range block.Ops {
			if call, ok := op.(*Call); ok {
				out = append(out, call.Symbol)
			}
		}
	}
	return out
}

func constructFieldValue(construct *Construct, name string) Value {
	for _, field := range construct.Fields {
		if field.Name == name {
			return field.Value
		}
	}
	return nil
}

func hasDiagCode(diags []diag.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

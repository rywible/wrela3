package codegen

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestCompileWhileLoops(t *testing.T) {
	cond := &ir.Param{Symbol: "cond", Type: ir.Type{Name: "U64"}}
	done := &ir.ConstInt{
		Symbol: "done",
		Value:  0,
		Type:   ir.Type{Name: "U64"},
	}
	fn := ir.Function{
		Symbol: "while_fn",
		Params: []ir.Value{
			cond,
		},
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					&ir.While{
						Condition: cond,
						Body:      []ir.Operation{},
					},
					done,
					&ir.Return{Value: done},
				},
			},
		},
	}

	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := image.Sections[0].Data
	if !bytes.Contains(code, []byte{0x0F, 0x84}) {
		t.Fatalf("expected conditional jump for while condition, got %#x", code)
	}
	if !bytes.Contains(code, []byte{0xE9}) {
		t.Fatalf("expected loop jump in while body, got %#x", code)
	}
}

func TestCompileForBytes(t *testing.T) {
	input := &ir.Param{Symbol: "input", Type: ir.Type{Name: "Bytes"}}
	done := &ir.ConstInt{
		Symbol: "done",
		Value:  0,
		Type:   ir.Type{Name: "U64"},
	}
	fn := ir.Function{
		Symbol: "bytes_fn",
		Params: []ir.Value{
			input,
		},
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					&ir.ForBytes{
						Iterable:  input,
						Index:     &ir.Param{Symbol: "index", Type: ir.Type{Name: "U64"}},
						ByteValue: &ir.Param{Symbol: "byte", Type: ir.Type{Name: "U8"}},
						Body:      []ir.Operation{},
					},
					done,
					&ir.Return{Value: done},
				},
			},
		},
	}

	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := image.Sections[0].Data
	if !bytes.Contains(code, []byte{0x4D, 0x8B, 0x53, 0x08}) {
		t.Fatalf("ForBytes over aggregate param must load length through param pointer, got %#x", code)
	}
	if !bytes.Contains(code, []byte{0x48, 0x0F, 0xB6, 0x04, 0x0F}) {
		t.Fatalf("expected u8 load for ForBytes, got %#x", code)
	}
	load := bytes.Index(code, []byte{0x48, 0x0F, 0xB6, 0x04, 0x0F})
	if load < 4 || !bytes.Equal(code[load-4:load-1], []byte{0x48, 0x8B, 0x4D}) {
		t.Fatalf("ForBytes byte load must index with the current loop index in rcx, got %#x", code)
	}
	if !bytes.Contains(code, []byte{0x48, 0x83, 0xC1, 0x01}) {
		t.Fatalf("expected index increment in ForBytes loop, got %#x", code)
	}
}

func TestCompileCallRelocation(t *testing.T) {
	calleeRet := &ir.ConstInt{
		Symbol: "ret",
		Value:  7,
		Type:   ir.Type{Name: "U64"},
	}
	callee := ir.Function{
		Symbol: "callee",
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					calleeRet,
					&ir.Return{Value: calleeRet},
				},
			},
		},
	}

	call := &ir.Call{
		Symbol: "callee",
		Type:   ir.Type{Name: "U64"},
	}
	caller := ir.Function{
		Symbol: "caller",
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					call,
					&ir.Return{
						Value: call,
					},
				},
			},
		},
	}

	image, diags := Compile(&ir.Program{Functions: []ir.Function{callee, caller}})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	callerStart := int(image.Symbols["caller"] - 0x1000)
	callerCode := image.Sections[0].Data[callerStart:]
	callOffset := bytes.IndexByte(callerCode, 0xE8)
	if callOffset < 0 {
		t.Fatalf("missing call opcode in caller")
	}

	callRel := int32(binary.LittleEndian.Uint32(callerCode[callOffset+1 : callOffset+5]))
	got := int64(callRel)
	expect := int64(int64(image.Symbols["callee"]) - int64(image.Symbols["caller"]+uint64(callOffset+5)))
	if got != expect {
		t.Fatalf("call rel32 = %d, want %d", got, expect)
	}
}

func TestCompileDataRelocationOffsetIsRelativeToOwningSymbol(t *testing.T) {
	fillerRet := &ir.ConstInt{Symbol: "ret", Value: 0, Type: ir.Type{Name: "U64"}}
	filler := ir.Function{
		Symbol: "filler",
		Blocks: []ir.Block{{
			Label: "entry",
			Ops:   []ir.Operation{fillerRet, &ir.Return{Value: fillerRet}},
		}},
	}
	literal := &ir.StringLiteral{
		Symbol:     "literal",
		Value:      "hello",
		DataSymbol: "hello_data",
		Type:       ir.Type{Name: "StringLiteral"},
	}
	usesData := ir.Function{
		Symbol: "uses_data",
		Blocks: []ir.Block{{
			Label: "entry",
			Ops:   []ir.Operation{literal, &ir.Return{}},
		}},
	}
	image, diags := Compile(&ir.Program{
		Functions: []ir.Function{filler, usesData},
		Data:      []ir.DataObject{{Symbol: "hello_data", Bytes: []byte("hello\x00")}},
	})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	if len(image.Relocs) != 1 {
		t.Fatalf("relocs = %#v, want one relocation", image.Relocs)
	}
	reloc := image.Relocs[0]
	if reloc.Symbol != "uses_data" {
		t.Fatalf("reloc symbol = %q, want uses_data", reloc.Symbol)
	}
	locationRVA := image.Symbols[reloc.Symbol] + reloc.Offset
	start := int(locationRVA - image.Sections[0].RVA)
	if start < 0 || start+8 > len(image.Sections[0].Data) {
		t.Fatalf("reloc location RVA %#x outside .text", locationRVA)
	}
	got := binary.LittleEndian.Uint64(image.Sections[0].Data[start : start+8])
	want := uint64(runtimeImageBase + image.Symbols["hello_data"])
	if got != want {
		t.Fatalf("reloc points at %#x containing %#x, want data address %#x", locationRVA, got, want)
	}
}

func TestCompileRejectsTooManyCallArguments(t *testing.T) {
	args := make([]ir.Value, 6)
	for i := range args {
		args[i] = &ir.ConstInt{
			Symbol: fmt.Sprintf("arg%d", i),
			Value:  uint64(i),
			Type:   ir.Type{Name: "U64"},
		}
	}

	fn := ir.Function{
		Symbol: "bad_call",
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					&ir.Call{
						Symbol: "callee",
						Args:   args,
						Type:   ir.Type{Name: "U64"},
					},
				},
			},
		},
	}

	_, diags := Compile(&ir.Program{Functions: []ir.Function{fn}})
	if len(diags) != 1 || diags[0].Code != diag.SEM0013 {
		t.Fatalf("diags = %#v, want one SEM0013", diags)
	}
}

func TestCompileBranchTargetsBlockLabelsAfterPrologue(t *testing.T) {
	cond := &ir.Param{Symbol: "cond", Type: ir.Type{Name: "Bool"}}
	one := &ir.ConstInt{Symbol: "one", Value: 1, Type: ir.Type{Name: "U64"}}
	zero := &ir.ConstInt{Symbol: "zero", Value: 0, Type: ir.Type{Name: "U64"}}
	fn := ir.Function{
		Symbol: "branch_blocks",
		Params: []ir.Value{cond},
		Blocks: []ir.Block{
			{Label: "entry", Ops: []ir.Operation{
				&ir.Branch{Condition: cond, True: "then", False: "else"},
			}},
			{Label: "then", Ops: []ir.Operation{one, &ir.Return{Value: one}}},
			{Label: "else", Ops: []ir.Operation{zero, &ir.Return{Value: zero}}},
		},
	}
	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := image.Sections[0].Data
	jcc := bytes.Index(code, []byte{0x0F, 0x84})
	if jcc < 0 {
		t.Fatalf("missing conditional branch in %#x", code)
	}
	falseTarget := jcc + 6 + int(int32(binary.LittleEndian.Uint32(code[jcc+2:jcc+6])))
	if falseTarget <= 0 {
		t.Fatalf("false branch target = %d, want after prologue", falseTarget)
	}
	jmp := bytes.IndexByte(code[jcc+6:], 0xE9)
	if jmp < 0 {
		t.Fatalf("missing unconditional branch in %#x", code)
	}
	jmp += jcc + 6
	trueTarget := jmp + 5 + int(int32(binary.LittleEndian.Uint32(code[jmp+1:jmp+5])))
	if trueTarget <= 0 {
		t.Fatalf("true branch target = %d, want after prologue", trueTarget)
	}
}

func TestCompileBinaryAddWithHighScratchRegister(t *testing.T) {
	left := &ir.Param{Symbol: "left", Type: ir.Type{Name: "U64"}}
	right := &ir.Param{Symbol: "right", Type: ir.Type{Name: "U64"}}
	sum := &ir.Binary{Op: "add", Left: left, Right: right, Type: ir.Type{Name: "U64"}}
	fn := ir.Function{
		Symbol: "add_high_scratch",
		Params: []ir.Value{left, right},
		Blocks: []ir.Block{{
			Label: "entry",
			Ops:   []ir.Operation{sum, &ir.Return{Value: sum}},
		}},
	}
	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := image.Sections[0].Data
	if !bytes.Contains(code, []byte{0x4C, 0x01, 0xD0}) {
		t.Fatalf("expected add rax, r10 encoding, got %#x", code)
	}
	if bytes.Contains(code, []byte{0x49, 0x01, 0xD0}) {
		t.Fatalf("encoded add targets r8 instead of rax: %#x", code)
	}
}

func TestCompilePreserveStackReturnUsesSavedContinuation(t *testing.T) {
	result := &ir.ConstInt{Symbol: "result", Value: 7, Type: ir.Type{Name: "U64"}}
	fn := ir.Function{
		Symbol:              "preserve_return",
		PreserveStackReturn: true,
		Blocks: []ir.Block{{
			Label: "entry",
			Ops:   []ir.Operation{result, &ir.Return{Value: result}},
		}},
	}
	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := image.Sections[0].Data
	if bytes.Contains(code, []byte{0x48, 0x89, 0xEC, 0x5D, 0xC3}) {
		t.Fatalf("preserve-stack return restored old frame: %#x", code)
	}
	if !bytes.Contains(code, []byte{0x4C, 0x8B, 0x55, 0xF8}) {
		t.Fatalf("preserve-stack return missing saved continuation load: %#x", code)
	}
	if !bytes.Contains(code, []byte{0x48, 0x8B, 0x6D, 0x00, 0x41, 0x52, 0xC3}) {
		t.Fatalf("preserve-stack return must restore caller rbp before pushing continuation: %#x", code)
	}
}

func TestBuildFrameSeparatesContinuationAndRecordReturnSlots(t *testing.T) {
	result := &ir.Construct{Symbol: "result", Type: ir.Type{Name: "WideRecord"}}
	fn := ir.Function{
		Symbol:              "preserve_wide_record",
		Return:              ir.Type{Name: "WideRecord", Kind: ir.TypeKindData},
		PreserveStackReturn: true,
		Blocks: []ir.Block{{
			Label: "entry",
			Ops:   []ir.Operation{result, &ir.Return{Value: result}},
		}},
	}
	frame := buildFrame(fn, compileContext{types: map[string]ir.TypeInfo{
		"WideRecord": {
			Name:  "WideRecord",
			Kind:  ir.TypeKindData,
			Size:  16,
			Align: 8,
		},
	}})
	if frame.ContinuationSlot == 0 {
		t.Fatal("missing saved continuation slot")
	}
	if frame.RecordReturnSlot == 0 {
		t.Fatal("missing hidden record-return slot")
	}
	if frame.ContinuationSlot == frame.RecordReturnSlot {
		t.Fatalf("continuation and record-return slots alias at %d", frame.ContinuationSlot)
	}
}

func TestBuildFrameUsesDeclaredReturnForNestedDataReturn(t *testing.T) {
	result := &ir.Construct{Symbol: "result", Type: ir.Type{Name: "WideRecord"}}
	cond := &ir.ConstInt{Symbol: "cond", Value: 1, Type: ir.Type{Name: "Bool"}}
	fn := ir.Function{
		Symbol: "nested_data_return",
		Return: ir.Type{Name: "WideRecord", Kind: ir.TypeKindData},
		Blocks: []ir.Block{{
			Label: "entry",
			Ops: []ir.Operation{
				cond,
				&ir.If{
					Condition: cond,
					Then:      []ir.Operation{result, &ir.Return{Value: result}},
				},
			},
		}},
	}

	frame := buildFrame(fn, compileContext{types: wideRecordTypes()})
	if !frame.HasRecordReturn || frame.RecordReturnSlot == 0 {
		t.Fatalf("frame = %#v, want hidden record-return slot from declared return type", frame)
	}
}

func TestCompileNestedDataReturnRewritesNestedHandleIntoHiddenSlot(t *testing.T) {
	inner := &ir.Construct{Symbol: "inner", Type: ir.Type{Name: "Inner", Kind: ir.TypeKindData}}
	outer := &ir.Construct{
		Symbol: "outer",
		Type:   ir.Type{Name: "Outer", Kind: ir.TypeKindData},
		Fields: []ir.FieldValue{{Name: "inner", Value: inner}},
	}
	fn := ir.Function{
		Symbol: "return_outer",
		Return: ir.Type{Name: "Outer", Kind: ir.TypeKindData},
		Blocks: []ir.Block{{
			Label: "entry",
			Ops:   []ir.Operation{inner, outer, &ir.Return{Value: outer}},
		}},
	}

	image, diags := Compile(&ir.Program{
		Functions: []ir.Function{fn},
		Types:     wideRecordTypes(),
	})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := image.Sections[0].Data
	if !bytes.Contains(code, []byte{0x49, 0x8B, 0xC2, 0x48, 0x83, 0xC0, 0x08, 0x49, 0x89, 0x02}) {
		t.Fatalf("return of nested data must rewrite child handle into caller storage: %#x", code)
	}
}

func TestCompileReturnOfDataCallInsideIfUsesDeclaredReturnSlot(t *testing.T) {
	call := &ir.Call{Symbol: "callee", Type: ir.Type{Name: "WideRecord", Kind: ir.TypeKindData}}
	cond := &ir.ConstInt{Symbol: "cond", Value: 1, Type: ir.Type{Name: "Bool"}}
	caller := ir.Function{
		Symbol: "caller",
		Return: ir.Type{Name: "WideRecord", Kind: ir.TypeKindData},
		Blocks: []ir.Block{{
			Label: "entry",
			Ops: []ir.Operation{
				cond,
				&ir.If{
					Condition: cond,
					Then:      []ir.Operation{call, &ir.Return{Value: call}},
				},
			},
		}},
	}

	image, diags := Compile(&ir.Program{
		Functions: []ir.Function{caller},
		AsmMethods: []ir.AsmMethod{{
			Symbol: "callee",
			Return: ir.Type{Name: "WideRecord", Kind: ir.TypeKindData},
			Body:   "mov rax, r10\nret",
		}},
		Types: wideRecordTypes(),
	})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := image.Sections[0].Data
	if !bytes.Contains(code, []byte{0x4C, 0x89, 0x55}) {
		t.Fatalf("caller prologue must spill hidden r10 return slot: %#x", code)
	}
	if !bytes.Contains(code, []byte{0x4C, 0x8B, 0xD5}) {
		t.Fatalf("caller should set up an object slot for the callee hidden return: %#x", code)
	}
	if !bytes.Contains(code, []byte{0x4C, 0x8B, 0x55, 0xF8}) {
		t.Fatalf("caller should copy callee result into caller hidden return storage: %#x", code)
	}
}

func wideRecordTypes() map[string]ir.TypeInfo {
	return map[string]ir.TypeInfo{
		"WideRecord": {
			Name:        "WideRecord",
			Kind:        ir.TypeKindData,
			Size:        16,
			Align:       8,
			StorageSize: 16,
		},
		"Inner": {
			Name:        "Inner",
			Kind:        ir.TypeKindData,
			Size:        16,
			Align:       8,
			StorageSize: 16,
		},
		"Outer": {
			Name:        "Outer",
			Kind:        ir.TypeKindData,
			Size:        8,
			Align:       8,
			StorageSize: 24,
			Fields: map[string]ir.FieldInfo{
				"inner": {
					Name:          "inner",
					Type:          ir.Type{Name: "Inner", Kind: ir.TypeKindData},
					Offset:        0,
					Size:          8,
					Align:         8,
					StorageOffset: 8,
					StorageSize:   16,
				},
			},
			FieldOrder: []string{"inner"},
		},
	}
}

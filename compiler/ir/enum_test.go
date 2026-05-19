package ir

import "testing"

func TestLowerEnumMatch(t *testing.T) {
	src := `
module ir.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
executor Worker {
    start fn run(self, next: Option<Event>) -> never {
        match next {
            Option.Some(value = next_event) => { let k = next_event.kind }
            Option.None => { let z = 0 }
        }
        while true {}
    }
}
`
	program := lowerSourceForTest(t, src)
	fn := findFunction(program, "_wrela_method_ir_enums_Worker_run")
	if fn == nil {
		t.Fatal("missing Worker.run")
	}
	if !containsNestedOp[*EnumVariantTest](*fn) || !containsNestedOp[*EnumPayloadExtract](*fn) {
		t.Fatalf("lowered ops missing enum match operations: %#v", fn.Blocks[0].Ops)
	}
}

func TestLowerMatchBindsPayloadBeforeArmBody(t *testing.T) {
	program := lowerSourceForTest(t, `
module ir.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
class Worker {
    fn consume(self, event: Event) {}
    start fn run(self, next: Option<Event>) -> never {
        match next {
            Option.Some(value = next_event) => { self.consume(event = next_event) }
            Option.None => {}
        }
        while true {}
    }
}
`)
	fn := findFunction(program, "_wrela_method_ir_enums_Worker_run")
	if fn == nil {
		t.Fatal("missing Worker.run")
	}
	extractAt := enumPayloadExtractIndex(*fn)
	callAt := callIndex(*fn, "_wrela_method_ir_enums_Worker_consume")
	if extractAt < 0 || callAt < 0 || extractAt > callAt {
		t.Fatalf("payload extract must happen before consume call: extract=%d call=%d blocks=%#v", extractAt, callAt, fn.Blocks)
	}
}

func TestLowerEnumConstructorInfersThroughGenericPayload(t *testing.T) {
	program := lowerSourceForTest(t, `
module ir.enums
data Event { kind: U64 }
data Wrapper<T> { value: T }
enum MaybeWrapped<T> { None Some(value: Wrapper<T>) }
class Worker {
    fn some(self) -> MaybeWrapped<Event> {
        return MaybeWrapped.Some(value = Wrapper<Event>(value = Event(kind = 1)))
    }
}
`)
	fn := findFunction(program, "_wrela_method_ir_enums_Worker_some")
	if fn == nil {
		t.Fatal("missing Worker.some")
	}
	construct, ok := functionOp[*EnumConstruct](*fn)
	if !ok {
		t.Fatalf("missing enum construct: %#v", fn.Blocks)
	}
	if construct.Type.Name != "ir.enums.MaybeWrapped[ir.enums.Event]" {
		t.Fatalf("enum construct type = %#v, want MaybeWrapped<Event>", construct.Type)
	}
}

func TestLowerSizeofUsesStorageLayout(t *testing.T) {
	program := lowerSourceForTest(t, `
	module ir.sizes
data Inner { payload: U64 }
data Middle { inner: Inner }
data Outer { middle: Middle; marker: U8 }
class Worker {
    fn size(self) -> U64 {
        return sizeof(Outer)
    }
}
`)
	fn := findFunction(program, "_wrela_method_ir_sizes_Worker_size")
	if fn == nil {
		t.Fatal("missing Worker.size")
	}
	var sizeof *ConstInt
	for _, block := range fn.Blocks {
		for _, op := range block.Ops {
			if c, ok := op.(*ConstInt); ok && c.Symbol == "sizeof" {
				sizeof = c
				break
			}
		}
	}
	if sizeof == nil {
		t.Fatalf("missing sizeof const in lowered function: %#v", fn.Blocks)
	}
	if sizeof.Value != 32 {
		t.Fatalf("sizeof(Outer) = %d, want storage size 32", sizeof.Value)
	}
}

func containsNestedOp[T any](fn Function) bool {
	for _, block := range fn.Blocks {
		if containsNestedOpInOps[T](block.Ops) {
			return true
		}
	}
	return false
}

func containsNestedOpInOps[T any](ops []Operation) bool {
	for _, op := range ops {
		if _, ok := any(op).(T); ok {
			return true
		}
		switch branch := op.(type) {
		case *If:
			if containsNestedOpInOps[T](branch.ConditionOps) || containsNestedOpInOps[T](branch.Then) || containsNestedOpInOps[T](branch.Else) {
				return true
			}
		case *While:
			if containsNestedOpInOps[T](branch.ConditionOps) || containsNestedOpInOps[T](branch.Body) {
				return true
			}
		case *ForBytes:
			if containsNestedOpInOps[T](branch.IterableOps) || containsNestedOpInOps[T](branch.Body) {
				return true
			}
		}
	}
	return false
}

func enumPayloadExtractIndex(fn Function) int {
	ordinal := 0
	for bi := range fn.Blocks {
		if at := enumPayloadExtractIndexOps(fn.Blocks[bi].Ops, &ordinal); at >= 0 {
			return at
		}
	}
	return -1
}

func enumPayloadExtractIndexOps(ops []Operation, ordinal *int) int {
	for _, op := range ops {
		current := *ordinal
		*ordinal = *ordinal + 1
		if _, ok := op.(*EnumPayloadExtract); ok {
			return current
		}
		if branch, ok := op.(*If); ok {
			if at := enumPayloadExtractIndexOps(branch.ConditionOps, ordinal); at >= 0 {
				return at
			}
			if at := enumPayloadExtractIndexOps(branch.Then, ordinal); at >= 0 {
				return at
			}
			if at := enumPayloadExtractIndexOps(branch.Else, ordinal); at >= 0 {
				return at
			}
		}
	}
	return -1
}

func callIndex(fn Function, symbol string) int {
	ordinal := 0
	for bi := range fn.Blocks {
		if at := callIndexOps(fn.Blocks[bi].Ops, symbol, &ordinal); at >= 0 {
			return at
		}
	}
	return -1
}

func callIndexOps(ops []Operation, symbol string, ordinal *int) int {
	for _, op := range ops {
		current := *ordinal
		*ordinal = *ordinal + 1
		call, ok := op.(*Call)
		if ok && call.Symbol == symbol {
			return current
		}
		if branch, ok := op.(*If); ok {
			if at := callIndexOps(branch.ConditionOps, symbol, ordinal); at >= 0 {
				return at
			}
			if at := callIndexOps(branch.Then, symbol, ordinal); at >= 0 {
				return at
			}
			if at := callIndexOps(branch.Else, symbol, ordinal); at >= 0 {
				return at
			}
		}
	}
	return -1
}

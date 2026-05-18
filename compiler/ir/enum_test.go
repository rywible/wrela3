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
            Option.Some(value = event) => { let k = event.kind }
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
            Option.Some(value = event) => { self.consume(event = event) }
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

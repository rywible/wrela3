package ir

import "testing"

func TestMemoryOpsDefineExpectedValues(t *testing.T) {
	frame := &FrameBegin{Symbol: "tick", Parent: Local{Symbol: "memory"}, Length: ConstInt{Value: 64}}
	reserve := &ArenaReserve{
		Arena:  frame,
		Length: ConstInt{Value: 32},
		Align:  ConstInt{Value: 8},
	}
	place := &ArenaPlace{Arena: frame, Type: Type{Name: "Message"}}
	fn := Function{
		Blocks: []Block{{
			Label: "entry",
			Ops:   []Operation{frame, reserve, place, &FrameEnd{Frame: frame}},
		}},
	}
	values := fn.ValuesInDeterministicOrder()
	if len(values) != 3 {
		t.Fatalf("values = %#v, want frame reserve place", values)
	}
}

func TestLowerReserveArrayAndSlotWrite(t *testing.T) {
	src := `
module machine.x86_64.executor_memory
data Event { kind: U64 }
data Slots<T> {
    address: PhysicalAddress
    capacity: U64
    fn write(self, index: U64, value: T) {}
}
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
}
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 4096) as tick {
            let slots = tick.reserve_array(Event, count = 4)
            slots.write(index = 0, value = Event(kind = 7))
        }
        while true {}
    }
}
`
	program := lowerSourceForTest(t, src)
	fn := findFunction(program, "_wrela_method_machine_x86_64_executor_memory_Worker_run")
	if fn == nil {
		t.Fatal("missing Worker.run")
	}
	if !containsOp[*ArenaReserveArray](*fn) || !containsOp[*SlotWrite](*fn) {
		t.Fatalf("lowered ops missing reserve array or slot write: %#v", fn.Blocks[0].Ops)
	}
	if functionCalls(*fn, "_wrela_method_machine_x86_64_executor_memory_Slots_Event_write") {
		t.Fatalf("Slots<Event>.write lowered as a normal method call instead of SlotWrite: %#v", fn.Blocks)
	}
}

func TestLowerSlotFillAndSliceIntrinsics(t *testing.T) {
	src := `
module machine.x86_64.executor_memory
data Event { kind: U64 }
data Slice<T> {
    address: PhysicalAddress
    length: U64
    fn get(self, index: U64) -> T {}
}
data MutableSlice<T> {
    address: PhysicalAddress
    length: U64
    fn get(self, index: U64) -> T {}
    fn set(self, index: U64, value: T) {}
}
data Slots<T> {
    address: PhysicalAddress
    capacity: U64
    fn fill(self, value: T) -> MutableSlice<T> {
        return MutableSlice<T>(address = self.address, length = self.capacity)
    }
}
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
}
class Reader {
    fn read(self, slice: Slice<Event>) {
        let event = slice.get(index = 0)
    }
}
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 4096) as tick {
            let slots = tick.reserve_array(Event, count = 4)
            let mutable = slots.fill(value = Event(kind = 1))
            mutable.set(index = 0, value = Event(kind = 2))
            let event = mutable.get(index = 0)
        }
        while true {}
    }
}
`
	program := lowerSourceForTest(t, src)
	fn := findFunction(program, "_wrela_method_machine_x86_64_executor_memory_Worker_run")
	if fn == nil {
		t.Fatal("missing Worker.run")
	}
	if !containsOp[*SlotFill](*fn) || !containsOp[*SliceSet](*fn) || !containsOp[*SliceGet](*fn) {
		t.Fatalf("lowered ops missing slot fill or mutable slice operations: %#v", fn.Blocks[0].Ops)
	}
	if functionCalls(*fn, "_wrela_method_machine_x86_64_executor_memory_Slots_Event_fill") ||
		functionCalls(*fn, "_wrela_method_machine_x86_64_executor_memory_MutableSlice_Event_set") ||
		functionCalls(*fn, "_wrela_method_machine_x86_64_executor_memory_MutableSlice_Event_get") {
		t.Fatalf("typed memory intrinsic lowered as normal method call: %#v", fn.Blocks)
	}
	reader := findFunction(program, "_wrela_method_machine_x86_64_executor_memory_Reader_read")
	if reader == nil {
		t.Fatal("missing Reader.read")
	}
	if !containsOp[*SliceGet](*reader) {
		t.Fatalf("Slice<Event>.get did not lower to SliceGet: %#v", reader.Blocks)
	}
}

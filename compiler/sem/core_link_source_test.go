package sem

import (
	"strings"
	"testing"
)

func TestCoreLinkSourceCompiles(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	producer := moduleType(t, checked.Index, "machine.x86_64.core_link", "CoreSpscProducer")
	assertMethodExists(t, producer, "try_send")
	assertTypeFields(t, producer, map[string]string{
		"owner":         "ExecutorSlot",
		"peer":          "ExecutorSlot",
		"slots":         "MutableSlice<T>",
		"control":       "MutableSlice<U64>",
		"capacity":      "U64",
		"head":          "U64",
		"tail":          "U64",
		"credits":       "U64",
		"wake_strategy": "WakeStrategy",
	})

	consumer := moduleType(t, checked.Index, "machine.x86_64.core_link", "CoreSpscConsumer")
	assertMethodExists(t, consumer, "try_next")
	assertMethodExists(t, consumer, "arm_wait")
	assertTypeFields(t, consumer, map[string]string{
		"owner":         "ExecutorSlot",
		"peer":          "ExecutorSlot",
		"slots":         "MutableSlice<T>",
		"control":       "MutableSlice<U64>",
		"capacity":      "U64",
		"head":          "U64",
		"tail":          "U64",
		"wait_armed":    "Bool",
		"wake_strategy": "WakeStrategy",
	})

	moduleType(t, checked.Index, "machine.x86_64.core_link", "CoreLinkFull")
	moduleType(t, checked.Index, "machine.x86_64.core_link", "CoreLinkEmpty")

	source := readRepoFile(t, "wrela/machine/x86_64/core_link.wrela")
	trySend := sourceBetween(t, source, "fn try_send(self, value: T) -> Result<Unit, CoreLinkFull> {", "\n    }\n}")
	for _, want := range []string{
		"let observed_tail = self.control.get(index = 1)",
		"let occupied = self.control.get(index = 2)",
		"self.slots.set(index = observed_tail, value = value)",
		"self.control.set(index = 1, value = next_tail)",
		"self.control.set(index = 2, value = occupied + 1)",
		"Result.Ok(value = Unit())",
		"Result.Err(error = CoreLinkFull())",
	} {
		if !strings.Contains(trySend, want) {
			t.Fatalf("CoreSpscProducer.try_send missing %q", want)
		}
	}
	tryNext := sourceBetween(t, source, "fn try_next(self) -> Option<T> {", "\n    }\n\n    fn arm_wait")
	for _, want := range []string{
		"let occupied = self.control.get(index = 2)",
		"return Option.None()",
		"let value = self.slots.get(index = observed_head)",
		"self.control.set(index = 0, value = next_head)",
		"self.control.set(index = 2, value = occupied - 1)",
		"return Option.Some(value = value)",
	} {
		if !strings.Contains(tryNext, want) {
			t.Fatalf("CoreSpscConsumer.try_next missing %q", want)
		}
	}
}

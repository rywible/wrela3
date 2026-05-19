package sem

import "testing"

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
		"slots":         "Slots<T>",
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
		"slots":         "Slots<T>",
		"capacity":      "U64",
		"head":          "U64",
		"tail":          "U64",
		"wait_armed":    "Bool",
		"wake_strategy": "WakeStrategy",
	})

	moduleType(t, checked.Index, "machine.x86_64.core_link", "CoreLinkFull")
	moduleType(t, checked.Index, "machine.x86_64.core_link", "CoreLinkEmpty")
}

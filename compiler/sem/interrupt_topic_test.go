package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestExecutorOnInterruptRejected(t *testing.T) {
	_, diags := checkModuleForTest(t, `
module sem.executor_on_interrupt_rejected

data Event { vector: U8 }

driver path P {
    interrupt receiver -> Event {
        return Event(vector = 1)
    }
}

executor E {
    p: P

    on p.interrupt(interrupt_payload: Event) {}
}
`)
	if !hasMessage(diags, diag.SEM0042, "executor on interrupt handlers are no longer supported; use path-owned interrupt topics") {
		t.Fatalf("expected SEM0042, got %#v", diags)
	}
}

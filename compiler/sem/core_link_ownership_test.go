package sem

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const coreLinkWrongOwnerSource = `
module wrela.lang.core
data Unit {}

---

module machine.x86_64.executor_slot
data ExecutorSlot { id: U64 }

---

module machine.x86_64.executor_memory
data Slots<T> {
    address: U64
    capacity: U64
}

---

module machine.x86_64.executor_loop
data WakeStrategy {
    monitor_mwait: Bool
    fallback_hlt: Bool
}
class HotPollPolicy {}

---

module machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
data SlotIdentity { label: StringLiteral }
class ExecutorRegistry {
    fn claim(self, identity: SlotIdentity) -> ExecutorSlot {
        return ExecutorSlot(id = 0)
    }
}

---

module machine.x86_64.core_link
use { ExecutorSlot } from machine.x86_64.executor_slot
use { Slots } from machine.x86_64.executor_memory
use { WakeStrategy } from machine.x86_64.executor_loop
use { Unit } from wrela.lang.core
data CoreLinkFull {}
class CoreSpscProducer<T> {
    owner: ExecutorSlot
    peer: ExecutorSlot
    slots: Slots<T>
    capacity: U64
    head: U64
    tail: U64
    credits: U64
    wake_strategy: WakeStrategy

    fn try_send(self, value: T) -> Unit {
        return Unit()
    }
}
class CoreSpscConsumer<T> {
    owner: ExecutorSlot
    peer: ExecutorSlot
    slots: Slots<T>
    capacity: U64
    head: U64
    tail: U64
    wait_armed: Bool
    wake_strategy: WakeStrategy

    fn try_next(self) -> Unit {
        return Unit()
    }
}

---

module sem.core_link_wrong_owner

use { CoreSpscConsumer, CoreSpscProducer } from machine.x86_64.core_link
use { ExecutorSlot } from machine.x86_64.executor_slot
use { Slots } from machine.x86_64.executor_memory
use { HotPollPolicy, WakeStrategy } from machine.x86_64.executor_loop
use { ExecutorRegistry, SlotIdentity } from machine.x86_64.cpu_state

unique class OwnedHardware {
    executors: ExecutorRegistry
}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware(executors = ExecutorRegistry())
    }
}

image CoreLinkWrongOwnerImage {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let producer_owner = ExecutorSlot(id = 0)
        let consumer_owner = hardware.executors.claim(identity = SlotIdentity(label = "executor_slot.1"))
        let slots = Slots<U64>(address = 0, capacity = 8)
        let wake = WakeStrategy(monitor_mwait = false, fallback_hlt = true)
        let producer = CoreSpscProducer<U64>(owner = producer_owner, peer = consumer_owner, slots = slots, capacity = 8, head = 0, tail = 0, credits = 8, wake_strategy = wake)
        let consumer = CoreSpscConsumer<U64>(owner = consumer_owner, peer = producer_owner, slots = slots, capacity = 8, head = 0, tail = 0, wait_armed = false, wake_strategy = wake)
        let worker = Worker(slot = consumer_owner, loop = HotPollPolicy(), producer = producer, consumer = consumer)
        while true {}
    }
}

executor Worker {
    slot: ExecutorSlot
    loop: HotPollPolicy
    producer: CoreSpscProducer<U64>
    consumer: CoreSpscConsumer<U64>

    start fn run(self) -> never {
        let sent = self.producer.try_send(value = 1)
        while true {}
    }
}
`

func TestCoreLinkWrongOwnerFails(t *testing.T) {
	modules := parseModulesForTest(t, strings.Split(coreLinkWrongOwnerSource, "\n---\n")...)
	index := mustBuildIndex(t, modules)
	checked, ds := Check(index, modules)
	if !hasCode(ds, diag.SEM0112) {
		t.Fatalf("diagnostics = %#v, want SEM0112", ds)
	}
	if len(checked.ImageGraph.CoreLinkEndpoints) != 2 {
		t.Fatalf("core link endpoints = %#v, want producer and consumer", checked.ImageGraph.CoreLinkEndpoints)
	}
	want := map[string]CoreLinkEndpointNode{
		"producer": {Direction: "tx", Role: "producer", Owner: "executor_slot.0", Peer: "executor_slot.1", Depth: 8},
		"consumer": {Direction: "rx", Role: "consumer", Owner: "executor_slot.1", Peer: "executor_slot.0", Depth: 8},
	}
	for _, endpoint := range checked.ImageGraph.CoreLinkEndpoints {
		expected, ok := want[endpoint.Role]
		if !ok {
			t.Fatalf("unexpected core link endpoint %#v", endpoint)
		}
		if endpoint.Label == "" || endpoint.Direction != expected.Direction || endpoint.Owner != expected.Owner || endpoint.Peer != expected.Peer || endpoint.Depth != expected.Depth {
			t.Fatalf("core link endpoint %#v, want %#v", endpoint, expected)
		}
		delete(want, endpoint.Role)
	}
	if len(want) != 0 {
		t.Fatalf("missing core link endpoints %#v", want)
	}
}

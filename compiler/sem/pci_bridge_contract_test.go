package sem

import (
	"reflect"
	"testing"
)

func TestPciBridgeWalkSyntheticTopology(t *testing.T) {
	cfg := syntheticPciConfig{
		functions: map[pciBDF]syntheticFunction{
			{bus: 0, device: 1, function: 0}: {vendor: 0x1234, deviceID: 0x0001, class: 0x06, subclass: 0x04, secondary: 2, subordinate: 3},
			{bus: 2, device: 0, function: 0}: {vendor: 0x1234, deviceID: 0x0002, class: 0x02, subclass: 0x00},
			{bus: 3, device: 0, function: 0}: {vendor: 0x1234, deviceID: 0x0003, class: 0x01, subclass: 0x06},
		},
	}

	got := walkSyntheticPCI(cfg, 0, 0)
	want := []pciBDF{{bus: 0, device: 1, function: 0}, {bus: 2, device: 0, function: 0}, {bus: 3, device: 0, function: 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walkSyntheticPCI() = %#v, want %#v", got, want)
	}
}

func TestPciBridgeWalkSyntheticWindowDoesNotDuplicateBridgeBuses(t *testing.T) {
	cfg := syntheticPciConfig{
		functions: map[pciBDF]syntheticFunction{
			{bus: 0, device: 1, function: 0}: {vendor: 0x1234, deviceID: 0x0001, class: 0x06, subclass: 0x04, secondary: 2, subordinate: 2},
			{bus: 2, device: 0, function: 0}: {vendor: 0x1234, deviceID: 0x0002, class: 0x02, subclass: 0x00},
		},
	}

	got := walkSyntheticPCIWindow(cfg, 0, 2)
	want := []pciBDF{{bus: 0, device: 1, function: 0}, {bus: 2, device: 0, function: 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walkSyntheticPCIWindow() = %#v, want %#v", got, want)
	}
}

type pciBDF struct {
	bus      uint8
	device   uint8
	function uint8
}

type syntheticFunction struct {
	vendor      uint16
	deviceID    uint16
	class       uint8
	subclass    uint8
	secondary   uint8
	subordinate uint8
}

type syntheticPciConfig struct {
	functions map[pciBDF]syntheticFunction
}

func walkSyntheticPCI(cfg syntheticPciConfig, startBus uint8, depth int) []pciBDF {
	return walkSyntheticPCIBus(cfg, startBus, depth, map[pciBDF]bool{})
}

func walkSyntheticPCIWindow(cfg syntheticPciConfig, startBus, endBus uint8) []pciBDF {
	seen := map[pciBDF]bool{}
	var out []pciBDF
	for bus := startBus; bus <= endBus; bus++ {
		out = append(out, walkSyntheticPCIBus(cfg, bus, 0, seen)...)
		if bus == endBus {
			break
		}
	}
	return out
}

func walkSyntheticPCIBus(cfg syntheticPciConfig, startBus uint8, depth int, seen map[pciBDF]bool) []pciBDF {
	if depth > 8 {
		panic("bridge depth exceeded")
	}

	var out []pciBDF
	var device uint8
	for device = 0; device < 32; device++ {
		bdf := pciBDF{bus: startBus, device: device, function: 0}
		fn, ok := cfg.functions[bdf]
		if !ok || fn.vendor == 0xffff {
			continue
		}
		if !seen[bdf] {
			seen[bdf] = true
			out = append(out, bdf)
		}
		if fn.class == 0x06 && fn.subclass == 0x04 {
			for bus := fn.secondary; bus <= fn.subordinate; bus++ {
				out = append(out, walkSyntheticPCIBus(cfg, bus, depth+1, seen)...)
				if bus == fn.subordinate {
					break
				}
			}
		}
	}
	return out
}

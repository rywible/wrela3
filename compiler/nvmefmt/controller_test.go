package nvmefmt

import (
	"reflect"
	"testing"
)

func TestControllerInitSequence(t *testing.T) {
	got := PlannedControllerInitOps()
	want := []string{"read CAP", "write CC.EN=0", "wait RDY=0", "write AQA", "write ASQ", "write ACQ", "write CC.EN=1", "wait RDY=1", "identify controller", "identify namespace"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("init sequence = %#v, want %#v", got, want)
	}
}

package nvmefmt

import (
	"errors"
	"testing"
)

func identifyNamespaceWithFormat(format byte, lbads byte) []byte {
	data := make([]byte, 128+16*4)
	data[24] = format
	data[128+int(format)*4+2] = lbads
	return data
}

func TestParseIdentifyNamespaceLogicalBlockSize(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint64
	}{
		{
			name: "512 byte blocks",
			data: identifyNamespaceWithFormat(0, 9),
			want: 512,
		},
		{
			name: "4096 byte blocks",
			data: identifyNamespaceWithFormat(1, 12),
			want: 4096,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIdentifyNamespace(tt.data)
			if err != nil {
				t.Fatalf("ParseIdentifyNamespace() error = %v", err)
			}
			if got.LogicalBlockSize != tt.want {
				t.Fatalf("LogicalBlockSize = %d, want %d", got.LogicalBlockSize, tt.want)
			}
		})
	}
}

func TestParseIdentifyNamespaceRejectsUnsupportedLBA(t *testing.T) {
	_, err := ParseIdentifyNamespace(identifyNamespaceWithFormat(0, 10))
	if !errors.Is(err, ErrUnsupportedLBA) {
		t.Fatalf("ParseIdentifyNamespace() error = %v, want %v", err, ErrUnsupportedLBA)
	}
}

func TestSelectDurabilityModes(t *testing.T) {
	tests := []struct {
		name string
		ns   NamespaceFacts
		want DurabilityMode
	}{
		{
			name: "FUA for supported 512 byte namespace",
			ns:   NamespaceFacts{LogicalBlockSize: 512, SupportsFUA: true},
			want: DurabilityMode{Mode: DurabilityFUA, UseFUA: true},
		},
		{
			name: "write plus flush for 4096 byte namespace without FUA",
			ns:   NamespaceFacts{LogicalBlockSize: 4096},
			want: DurabilityMode{Mode: DurabilityWritePlusFlush, RequiresFlush: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectDurability(tt.ns)
			if err != nil {
				t.Fatalf("SelectDurability() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("SelectDurability() = %+v, want %+v", got, tt.want)
			}
		})
	}

	_, err := SelectDurability(NamespaceFacts{LogicalBlockSize: 1024, SupportsFUA: true})
	if !errors.Is(err, ErrUnsupportedLBA) {
		t.Fatalf("SelectDurability(unsupported LBA) error = %v, want %v", err, ErrUnsupportedLBA)
	}
}

func TestWriteCommandDword12(t *testing.T) {
	if got, want := WriteCommandDword12(8, false), uint32(7); got != want {
		t.Fatalf("WriteCommandDword12(8, false) = %#x, want %#x", got, want)
	}

	if got, want := WriteCommandDword12(8, true), uint32(7)|(1<<30); got != want {
		t.Fatalf("WriteCommandDword12(8, true) = %#x, want %#x", got, want)
	}
}

func TestCompletionQueueAdvanceWrapsHeadAndTogglesPhase(t *testing.T) {
	q := CompletionQueue{Depth: 4, Phase: true}

	q.Advance(3)
	if q.Head != 3 || !q.Phase {
		t.Fatalf("after 3 advances queue = %+v, want head 3 phase true", q)
	}

	q.Advance(1)
	if q.Head != 0 || q.Phase {
		t.Fatalf("after wrap queue = %+v, want head 0 phase false", q)
	}

	q.Advance(4)
	if q.Head != 0 || !q.Phase {
		t.Fatalf("after second wrap queue = %+v, want head 0 phase true", q)
	}
}

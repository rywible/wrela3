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

func TestWritePlusFlushAcknowledgesAfterFlushOnly(t *testing.T) {
	sm := DurabilityState{Mode: DurabilityWritePlusFlush, PendingWrites: 2}
	sm.CompleteWrite(1, true)
	sm.CompleteWrite(2, true)
	if sm.Acknowledged() {
		t.Fatal("must not ack before flush")
	}
	sm.CompleteFlush(true)
	if !sm.Acknowledged() {
		t.Fatal("must ack after flush")
	}
}

func TestFUAAcknowledgesAfterWritesComplete(t *testing.T) {
	sm := DurabilityState{Mode: DurabilityFUA, PendingWrites: 2}
	sm.CompleteWrite(1, true)
	if sm.Acknowledged() {
		t.Fatal("must not ack before all writes complete")
	}
	sm.CompleteWrite(2, true)
	if !sm.Acknowledged() {
		t.Fatal("must ack after all FUA writes complete")
	}
}

func TestDurabilityFailureRejectsBatch(t *testing.T) {
	sm := DurabilityState{Mode: DurabilityWritePlusFlush, PendingWrites: 1}
	sm.CompleteWrite(1, true)
	sm.CompleteFlush(false)
	if !sm.Failed() || sm.Acknowledged() {
		t.Fatalf("state = %+v, want failed without ack", sm)
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

func TestWriteCommandSetsFUA(t *testing.T) {
	cmd, err := BuildWriteCommand(1, 99, 8, 0x200000, 512, true)
	if err != nil {
		t.Fatalf("BuildWriteCommand() error = %v", err)
	}
	if cmd.Opcode != NVME_OPCODE_WRITE {
		t.Fatalf("Opcode = %#x, want %#x", cmd.Opcode, NVME_OPCODE_WRITE)
	}
	if cmd.NamespaceID != 1 || cmd.StartLBA != 99 || cmd.BlockCountMinusOne != 7 || cmd.PRP1 != 0x200000 {
		t.Fatalf("write command fields = %#v", cmd)
	}
	if cmd.CDW12&(1<<30) == 0 {
		t.Fatalf("CDW12 = %#x, want FUA bit set", cmd.CDW12)
	}
}

func TestReadFlushAndZoneAppendCommands(t *testing.T) {
	read, err := BuildReadCommand(7, 0x123456789, 16, 0x300000, 4096)
	if err != nil {
		t.Fatalf("BuildReadCommand() error = %v", err)
	}
	if read.Opcode != NVME_OPCODE_READ || read.NamespaceID != 7 || read.StartLBA != 0x123456789 || read.BlockCountMinusOne != 15 || read.PRP1 != 0x300000 {
		t.Fatalf("read command = %#v", read)
	}

	flush, err := BuildFlushCommand(11)
	if err != nil {
		t.Fatalf("BuildFlushCommand() error = %v", err)
	}
	if flush.Opcode != NVME_OPCODE_FLUSH || flush.NamespaceID != 11 {
		t.Fatalf("flush command = %#v", flush)
	}

	appendCmd, err := BuildZoneAppendCommand(3, 0x8000, 4, 0x400000, 512, true)
	if err != nil {
		t.Fatalf("BuildZoneAppendCommand() error = %v", err)
	}
	if appendCmd.Opcode != NVME_OPCODE_ZONE_APPEND || appendCmd.NamespaceID != 3 || appendCmd.StartLBA != 0x8000 || appendCmd.BlockCountMinusOne != 3 || appendCmd.PRP1 != 0x400000 {
		t.Fatalf("zone append command = %#v", appendCmd)
	}
	if appendCmd.CDW12&(1<<30) == 0 {
		t.Fatalf("zone append CDW12 = %#x, want FUA bit set", appendCmd.CDW12)
	}
}

func TestCommandRejectsTransferPastPRPLimit(t *testing.T) {
	for name, build := range map[string]func() (Command, error){
		"read": func() (Command, error) {
			return BuildReadCommand(1, 0, 257, 0x200000, 512)
		},
		"write": func() (Command, error) {
			return BuildWriteCommand(1, 0, 33, 0x200000, 4096, false)
		},
		"zone append": func() (Command, error) {
			return BuildZoneAppendCommand(1, 0, 257, 0x200000, 512, false)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if cmd, err := build(); !errors.Is(err, ErrTransferTooLarge) {
				t.Fatalf("build command = %#v, error = %v, want %v", cmd, err, ErrTransferTooLarge)
			}
		})
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

func TestCompletionDrainStopsAtPhaseMismatch(t *testing.T) {
	q := CompletionQueue{QueueID: 7, Depth: 4, Phase: true}
	entries := []CompletionEntry{
		{Phase: true},
		{Phase: true},
		{Phase: false},
		{Phase: true},
	}

	interrupt := DrainCompletions(&q, entries, nil)

	if interrupt.QueueID != 7 || interrupt.CompletedCount != 2 {
		t.Fatalf("interrupt = %+v, want queue 7 completed count 2", interrupt)
	}
	if q.Head != 2 || !q.Phase {
		t.Fatalf("queue = %+v, want head 2 phase true", q)
	}
}

func TestCompletionHeadWrapTogglesPhase(t *testing.T) {
	q := CompletionQueue{Depth: 2, Head: 1, Phase: true}

	q.Advance(1)

	if q.Head != 0 || q.Phase {
		t.Fatalf("queue = %+v, want head 0 phase false", q)
	}
}

func TestCompletionDrainRingsDoorbellAndReturnsInterrupt(t *testing.T) {
	q := CompletionQueue{QueueID: 3, Depth: 3, Head: 2, Phase: true}
	entries := []CompletionEntry{
		{Phase: false},
		{Phase: true},
		{Phase: true},
	}
	var doorbells []struct {
		queueID uint16
		head    uint16
	}

	interrupt := DrainCompletions(&q, entries, func(queueID uint16, head uint16) {
		doorbells = append(doorbells, struct {
			queueID uint16
			head    uint16
		}{queueID: queueID, head: head})
	})

	if interrupt.QueueID != 3 || interrupt.CompletedCount != 2 {
		t.Fatalf("interrupt = %+v, want queue 3 completed count 2", interrupt)
	}
	if q.Head != 1 || q.Phase {
		t.Fatalf("queue = %+v, want head 1 phase false", q)
	}
	if len(doorbells) != 1 || doorbells[0].queueID != 3 || doorbells[0].head != 1 {
		t.Fatalf("doorbells = %+v, want one ring for queue 3 head 1", doorbells)
	}
}

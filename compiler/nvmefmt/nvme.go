package nvmefmt

import (
	"encoding/binary"
	"errors"
)

type NamespaceFacts struct {
	LogicalBlockSize               uint64
	SupportsFUA                    bool
	VolatileWriteCache             bool
	PowerFailAtomicWriteUnitBlocks uint32
	AtomicWriteUnitBlocks          uint32
}

var ErrUnsupportedLBA = errors.New("unsupported nvme lba size")
var ErrTransferTooLarge = errors.New("nvme transfer exceeds max prp bytes")
var ErrInvalidBlockCount = errors.New("nvme transfer block count must be nonzero")
var ErrInvalidIdentifyNamespace = errors.New("invalid nvme identify namespace data")

const (
	NVME_OPCODE_FLUSH       uint64 = 0x00
	NVME_OPCODE_WRITE       uint64 = 0x01
	NVME_OPCODE_READ        uint64 = 0x02
	NVME_OPCODE_ZONE_APPEND uint64 = 0x7D
	NVME_COMMAND_FUA_BIT    uint64 = 30
	MaxPRPTransferBytes     uint64 = 131072
)

func PlannedControllerInitOps() []string {
	return []string{
		"read CAP",
		"write CC.EN=0",
		"wait RDY=0",
		"write AQA",
		"write ASQ",
		"write ACQ",
		"write CC.EN=1",
		"wait RDY=1",
		"identify controller",
		"identify namespace",
		"create foreground IO CQ",
		"create foreground IO SQ",
		"create background IO CQ",
		"create background IO SQ",
		"route MSI-X or MSI",
	}
}

func ParseIdentifyNamespace(data []byte) (NamespaceFacts, error) {
	if len(data) <= 26 {
		return NamespaceFacts{}, ErrInvalidIdentifyNamespace
	}
	format := int(data[26] & 0x0f)
	if len(data) <= 128+format*4+2 {
		return NamespaceFacts{}, ErrInvalidIdentifyNamespace
	}
	lbads := data[128+format*4+2]
	logicalBlockSize := uint64(1) << lbads
	if logicalBlockSize != 512 && logicalBlockSize != 4096 {
		return NamespaceFacts{}, ErrUnsupportedLBA
	}

	return NamespaceFacts{LogicalBlockSize: logicalBlockSize}, nil
}

func ParseIdentifyController(data []byte) NamespaceFacts {
	facts := NamespaceFacts{
		SupportsFUA:                    true,
		VolatileWriteCache:             len(data) > 256 && data[256]&1 != 0,
		AtomicWriteUnitBlocks:          1,
		PowerFailAtomicWriteUnitBlocks: 1,
	}
	if len(data) >= 514 {
		facts.AtomicWriteUnitBlocks = uint32(binary.LittleEndian.Uint16(data[512:514])) + 1
	}
	if len(data) >= 516 {
		facts.PowerFailAtomicWriteUnitBlocks = uint32(binary.LittleEndian.Uint16(data[514:516])) + 1
	}
	return facts
}

const (
	DurabilityFUA            = "FUA"
	DurabilityWritePlusFlush = "WRITE_PLUS_FLUSH"
)

type DurabilityMode struct {
	Mode          string
	RequiresFlush bool
	UseFUA        bool
}

type DurabilityState struct {
	Mode                string
	PendingWrites       uint32
	CompletedWrites     uint32
	FlushCompleted      bool
	completedCommandIDs map[uint16]struct{}
	failed              bool
}

func SelectDurability(ns NamespaceFacts) (DurabilityMode, error) {
	if ns.LogicalBlockSize != 512 && ns.LogicalBlockSize != 4096 {
		return DurabilityMode{}, ErrUnsupportedLBA
	}
	if ns.SupportsFUA {
		return DurabilityMode{Mode: DurabilityFUA, UseFUA: true}, nil
	}

	return DurabilityMode{Mode: DurabilityWritePlusFlush, RequiresFlush: true}, nil
}

func (s *DurabilityState) CompleteWrite(commandID uint16, ok bool) {
	if !ok {
		s.failed = true
		return
	}
	if _, ok := s.completedCommandIDs[commandID]; ok {
		return
	}
	if s.CompletedWrites < s.PendingWrites {
		if s.completedCommandIDs == nil {
			s.completedCommandIDs = map[uint16]struct{}{}
		}
		s.completedCommandIDs[commandID] = struct{}{}
		s.CompletedWrites++
	}
}

func (s *DurabilityState) CompleteFlush(ok bool) {
	if !ok {
		s.failed = true
		return
	}
	s.FlushCompleted = true
}

func (s DurabilityState) Acknowledged() bool {
	if s.failed || s.CompletedWrites < s.PendingWrites {
		return false
	}
	if s.Mode == DurabilityWritePlusFlush {
		return s.FlushCompleted
	}
	return true
}

func (s DurabilityState) Failed() bool {
	return s.failed
}

func WriteCommandDword12(blockCount uint32, fua bool) uint32 {
	dword := blockCount - 1
	if fua {
		dword |= 1 << 30
	}
	return dword
}

type Command struct {
	Opcode             uint64
	NamespaceID        uint32
	StartLBA           uint64
	BlockCountMinusOne uint32
	PRP1               uint64
	CDW10              uint32
	CDW11              uint32
	CDW12              uint32
}

func BuildReadCommand(namespaceID uint32, startLBA uint64, blockCount uint32, prp1 uint64, logicalBlockSize uint64) (Command, error) {
	return buildDataCommand(NVME_OPCODE_READ, namespaceID, startLBA, blockCount, prp1, logicalBlockSize, false)
}

func BuildWriteCommand(namespaceID uint32, startLBA uint64, blockCount uint32, prp1 uint64, logicalBlockSize uint64, fua bool) (Command, error) {
	return buildDataCommand(NVME_OPCODE_WRITE, namespaceID, startLBA, blockCount, prp1, logicalBlockSize, fua)
}

func BuildFlushCommand(namespaceID uint32) (Command, error) {
	return Command{Opcode: NVME_OPCODE_FLUSH, NamespaceID: namespaceID}, nil
}

func BuildZoneAppendCommand(namespaceID uint32, startLBA uint64, blockCount uint32, prp1 uint64, logicalBlockSize uint64, fua bool) (Command, error) {
	return buildDataCommand(NVME_OPCODE_ZONE_APPEND, namespaceID, startLBA, blockCount, prp1, logicalBlockSize, fua)
}

func buildDataCommand(opcode uint64, namespaceID uint32, startLBA uint64, blockCount uint32, prp1 uint64, logicalBlockSize uint64, fua bool) (Command, error) {
	if blockCount == 0 {
		return Command{}, ErrInvalidBlockCount
	}
	if uint64(blockCount) > MaxPRPTransferBytes/logicalBlockSize {
		return Command{}, ErrTransferTooLarge
	}
	cdw12 := WriteCommandDword12(blockCount, fua)
	return Command{
		Opcode:             opcode,
		NamespaceID:        namespaceID,
		StartLBA:           startLBA,
		BlockCountMinusOne: blockCount - 1,
		PRP1:               prp1,
		CDW10:              uint32(startLBA),
		CDW11:              uint32(startLBA >> 32),
		CDW12:              cdw12,
	}, nil
}

func PutLE64(dst []byte, off int, value uint64) {
	binary.LittleEndian.PutUint64(dst[off:], value)
}

type CompletionQueue struct {
	QueueID uint16
	Depth   uint16
	Head    uint16
	Phase   bool
}

func (q *CompletionQueue) Advance(count uint16) {
	for range count {
		q.Head++
		if q.Head == q.Depth {
			q.Head = 0
			q.Phase = !q.Phase
		}
	}
}

type CompletionEntry struct {
	Phase bool
}

type CompletionInterrupt struct {
	QueueID        uint16
	CompletedCount uint32
}

func DrainCompletions(q *CompletionQueue, entries []CompletionEntry, ringDoorbell func(queueID uint16, head uint16)) CompletionInterrupt {
	if q.Depth == 0 || q.Head >= q.Depth || len(entries) < int(q.Depth) {
		return CompletionInterrupt{QueueID: q.QueueID}
	}
	var completed uint16
	for completed < q.Depth {
		entry := entries[q.Head]
		if entry.Phase != q.Phase {
			break
		}
		completed++
		q.Advance(1)
	}
	if completed > 0 && ringDoorbell != nil {
		ringDoorbell(q.QueueID, q.Head)
	}
	return CompletionInterrupt{QueueID: q.QueueID, CompletedCount: uint32(completed)}
}

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
}

var ErrUnsupportedLBA = errors.New("unsupported nvme lba size")

func ParseIdentifyNamespace(data []byte) (NamespaceFacts, error) {
	format := int(data[24] & 0x0f)
	lbads := data[128+format*4+2]
	logicalBlockSize := uint64(1) << lbads
	if logicalBlockSize != 512 && logicalBlockSize != 4096 {
		return NamespaceFacts{}, ErrUnsupportedLBA
	}

	return NamespaceFacts{LogicalBlockSize: logicalBlockSize}, nil
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

func SelectDurability(ns NamespaceFacts) (DurabilityMode, error) {
	if ns.LogicalBlockSize != 512 && ns.LogicalBlockSize != 4096 {
		return DurabilityMode{}, ErrUnsupportedLBA
	}
	if ns.SupportsFUA {
		return DurabilityMode{Mode: DurabilityFUA, UseFUA: true}, nil
	}

	return DurabilityMode{Mode: DurabilityWritePlusFlush, RequiresFlush: true}, nil
}

func WriteCommandDword12(blockCount uint32, fua bool) uint32 {
	dword := blockCount - 1
	if fua {
		dword |= 1 << 30
	}
	return dword
}

func PutLE64(dst []byte, off int, value uint64) {
	binary.LittleEndian.PutUint64(dst[off:], value)
}

type CompletionQueue struct {
	Depth uint16
	Head  uint16
	Phase bool
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

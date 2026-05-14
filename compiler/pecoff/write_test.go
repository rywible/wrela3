package pecoff

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ryanwible/wrela3/compiler/codegen"
)

func TestWriteEFIHeaders(t *testing.T) {
	img := &codegen.Image{
		EntrySymbol: "_wrela_efi_entry",
		Symbols: map[string]uint64{
			"_wrela_efi_entry": 0x1000,
		},
		Sections: []codegen.Section{
			{Name: ".text", Data: []byte{0xC3}},
		},
	}

	got, err := WriteEFI(img)
	if err != nil {
		t.Fatalf("WriteEFI() returned error: %v", err)
	}

	if got[0] != 'M' || got[1] != 'Z' {
		t.Fatalf("DOS header magic = %x%x, want MZ", got[0], got[1])
	}
	if binary.LittleEndian.Uint32(got[0x3c:0x40]) != peSignatureOffset {
		t.Fatalf("e_lfanew = %#x, want %#x", binary.LittleEndian.Uint32(got[0x3c:0x40]), peSignatureOffset)
	}
	if !bytes.Equal(got[peSignatureOffset:peSignatureOffset+4], []byte{'P', 'E', 0, 0}) {
		t.Fatalf("PE signature = %#v, want PE\\x00\\x00", got[peSignatureOffset:peSignatureOffset+4])
	}

	coffOffset := peSignatureOffset + 4
	if gotMachine := binary.LittleEndian.Uint16(got[coffOffset:]); gotMachine != 0x8664 {
		t.Fatalf("Machine = %#x, want 0x8664", gotMachine)
	}
	if gotOpt := binary.LittleEndian.Uint16(got[coffOffset+16:]); gotOpt != optionalHeaderSize {
		t.Fatalf("SizeOfOptionalHeader = %#x, want %#x", gotOpt, optionalHeaderSize)
	}
	if gotChar := binary.LittleEndian.Uint16(got[coffOffset+18:]); gotChar != 0x2022 {
		t.Fatalf("Characteristics = %#x, want 0x2022", gotChar)
	}
}

func TestWriteEFISectionLayout(t *testing.T) {
	img := &codegen.Image{
		EntrySymbol: "entry",
		Symbols: map[string]uint64{
			"entry": 0x1000,
		},
		Sections: []codegen.Section{
			{Name: ".text", Data: []byte{0xC3}},
			{Name: ".rdata", Data: []byte{1, 2}},
			{Name: ".data", Data: []byte{3, 4}},
		},
	}

	got, err := WriteEFI(img)
	if err != nil {
		t.Fatalf("WriteEFI() returned error: %v", err)
	}

	optOffset := peSignatureOffset + 4 + coffHeaderSize
	if gotMagic := binary.LittleEndian.Uint16(got[optOffset:]); gotMagic != 0x20B {
		t.Fatalf("OptionalHeader.Magic = %#x, want 0x20B", gotMagic)
	}
	if gotSub := binary.LittleEndian.Uint16(got[optOffset+48:]); gotSub != majorSubsystemVersion {
		t.Fatalf("OptionalHeader.MajorSubsystemVersion = %d, want %d", gotSub, majorSubsystemVersion)
	}
	if gotBase := binary.LittleEndian.Uint32(got[optOffset+20:]); gotBase != 0x1000 {
		t.Fatalf("BaseOfCode = %#x, want 0x1000", gotBase)
	}

	coffOffset := peSignatureOffset + 4
	sectionCount := int(binary.LittleEndian.Uint16(got[coffOffset+2:]))
	sectionTableOffset := peSignatureOffset + 4 + coffHeaderSize + optionalHeaderSize
	var textRVA uint32
	for i := 0; i < sectionCount; i++ {
		off := sectionTableOffset + i*sectionHeaderSize
		name := bytes.TrimRight(got[off:off+8], "\x00")
		if string(name) != ".text" {
			continue
		}
		textRVA = binary.LittleEndian.Uint32(got[off+12:])
	}
	if textRVA != 0x1000 {
		t.Fatalf("text section RVA = %#x, want 0x1000", textRVA)
	}
}

func TestWriteEFIDir64Relocation(t *testing.T) {
	img := &codegen.Image{
		EntrySymbol: "entry",
		Symbols: map[string]uint64{
			"entry":  0x1000,
			"target": 0x2000,
		},
		Sections: []codegen.Section{
			{Name: ".text", Data: make([]byte, 0x200)},
		},
		Relocs: []codegen.Reloc{
			{
				Kind:   codegen.RelocKindDIR64,
				Offset: 0x20,
				Symbol: "target",
			},
		},
	}

	got, err := WriteEFI(img)
	if err != nil {
		t.Fatalf("WriteEFI() returned error: %v", err)
	}

	coffOffset := peSignatureOffset + 4
	sectionCount := int(binary.LittleEndian.Uint16(got[coffOffset+2:]))
	sectionTableOffset := peSignatureOffset + 4 + coffHeaderSize + optionalHeaderSize
	var relocVA, relocSize uint32
	var relocDataPtr uint32
	var foundReloc bool
	for i := 0; i < sectionCount; i++ {
		off := sectionTableOffset + i*sectionHeaderSize
		name := bytes.TrimRight(got[off:off+8], "\x00")
		if string(name) == ".reloc" {
			foundReloc = true
			relocVA = binary.LittleEndian.Uint32(got[off+12:])
			relocSize = binary.LittleEndian.Uint32(got[off+8:])
			relocDataPtr = binary.LittleEndian.Uint32(got[off+20:])
		}
	}
	if !foundReloc {
		t.Fatal("missing .reloc section")
	}
	optOffset := peSignatureOffset + 4 + coffHeaderSize
	dirOffset := optOffset + 0x70 + relocDirectoryIndex*8
	if binary.LittleEndian.Uint32(got[dirOffset:]) != relocVA {
		t.Fatalf("data directory RVA = %#x, want %#x", binary.LittleEndian.Uint32(got[dirOffset:]), relocVA)
	}
	if binary.LittleEndian.Uint32(got[dirOffset+4:]) != relocSize {
		t.Fatalf("data directory size = %#x, want %#x", binary.LittleEndian.Uint32(got[dirOffset+4:]), relocSize)
	}
	if relocSize == 0 {
		t.Fatal("relocation data size is zero")
	}

	blockRVA := binary.LittleEndian.Uint32(got[relocDataPtr : relocDataPtr+4])
	blockSize := binary.LittleEndian.Uint32(got[relocDataPtr+4 : relocDataPtr+8])
	if blockRVA != 0x2000 {
		t.Fatalf("block page RVA = %#x, want 0x2000", blockRVA)
	}
	if blockSize < 10 {
		t.Fatalf("reloc block size = %d, want >= 10", blockSize)
	}
	if gotKind := binary.LittleEndian.Uint16(got[relocDataPtr+8:]); gotKind != uint16(codegen.RelocKindDIR64<<12)|0x20 {
		t.Fatalf("reloc entry = %#x, want %#x", gotKind, uint16(codegen.RelocKindDIR64<<12)|0x20)
	}
}

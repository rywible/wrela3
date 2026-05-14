package pecoff

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/ryanwible/wrela3/compiler/codegen"
)

const (
	dosHeaderSize     = 0x80
	dosLfanewOffset   = 0x3c
	peSignatureOffset = 0x80

	coffHeaderSize     = 0x14
	optionalHeaderSize = 0xF0
	sectionHeaderSize  = 0x28

	fileAlignment    = 0x200
	sectionAlignment = 0x1000

	machineAMD64 = 0x8664
	imageBase    = 0x100000

	coffCharacteristics = 0x2022
	dataDirectoryCount  = 16
	relocDirectoryIndex = 5
)

const (
	majorSubsystemVersion = 2
	subsystemEFI          = 10
)

var sectionCharacteristics = map[string]uint32{
	".text":  0x60000020,
	".rdata": 0x40000040,
	".data":  0xC0000040,
	".reloc": 0x42000040,
}

func WriteEFI(img *codegen.Image) ([]byte, error) {
	if img == nil {
		return nil, fmt.Errorf("nil image")
	}
	if img.Symbols == nil {
		return nil, fmt.Errorf("nil symbols map")
	}

	entryPoint, ok := img.Symbols[img.EntrySymbol]
	if !ok {
		return nil, fmt.Errorf("entry symbol %q not found", img.EntrySymbol)
	}

	sections := append([]codegen.Section(nil), img.Sections...)
	if len(img.Relocs) > 0 {
		relocData, err := buildRelocData(img)
		if err != nil {
			return nil, err
		}

		found := false
		for i := range sections {
			if sections[i].Name == ".reloc" {
				sections[i].Data = relocData
				found = true
				break
			}
		}
		if !found {
			sections = append(sections, codegen.Section{
				Name:            ".reloc",
				Data:            relocData,
				Characteristics: sectionCharacteristics[".reloc"],
			})
		}
	}

	ordered := normalizeAndOrderSections(sections)
	assignSectionCharacteristics(ordered)
	assignSectionRVAs(ordered)

	sectionTableOffset := peSignatureOffset + 4 + coffHeaderSize + optionalHeaderSize
	sizeOfHeaders := alignUp(uint64(sectionTableOffset+len(ordered)*sectionHeaderSize), fileAlignment)

	sectionRawPointers := make([]uint64, len(ordered))
	sectionRawSizes := make([]uint64, len(ordered))
	nextRaw := sizeOfHeaders
	for i, section := range ordered {
		rawSize := alignUp(uint64(len(section.Data)), fileAlignment)
		sectionRawPointers[i] = nextRaw
		sectionRawSizes[i] = rawSize
		nextRaw += rawSize
	}

	sizeOfCode, sizeOfData := computeDataSizes(ordered)
	imageSize := computeImageSize(ordered)
	relocRVA, relocSize := locateRelocDirectory(ordered)

	out := make([]byte, nextRaw)

	writeDosHeaders(out)
	writeCoffHeader(out, len(ordered))
	writeOptionalHeader(out, ordered, entryPoint, sizeOfCode, sizeOfData, sizeOfHeaders, imageSize, relocRVA, relocSize)
	writeSectionHeaders(out, sectionTableOffset, ordered, sectionRawPointers, sectionRawSizes)

	for i, section := range ordered {
		copy(out[sectionRawPointers[i]:sectionRawPointers[i]+uint64(len(section.Data))], section.Data)
	}

	return out, nil
}

func writeDosHeaders(headers []byte) {
	binary.LittleEndian.PutUint16(headers[0:2], 0x5a4d)
	binary.LittleEndian.PutUint32(headers[dosLfanewOffset:dosLfanewOffset+4], peSignatureOffset)
	copy(headers[peSignatureOffset:peSignatureOffset+4], []byte{'P', 'E', 0, 0})
}

func writeCoffHeader(headers []byte, numberOfSections int) {
	offset := peSignatureOffset + 4
	binary.LittleEndian.PutUint16(headers[offset+0:], machineAMD64)
	binary.LittleEndian.PutUint16(headers[offset+2:], uint16(numberOfSections))
	// TimeDateStamp, PointerToSymbolTable, NumberOfSymbols remain zero.
	binary.LittleEndian.PutUint16(headers[offset+16:], optionalHeaderSize)
	binary.LittleEndian.PutUint16(headers[offset+18:], coffCharacteristics)
}

func writeOptionalHeader(headers []byte, sections []codegen.Section, entryPoint, sizeOfCode, sizeOfData, sizeOfHeaders, imageSize, relocVA, relocSize uint64) {
	offset := peSignatureOffset + 4 + coffHeaderSize

	binary.LittleEndian.PutUint16(headers[offset+0:], 0x20B)
	headers[offset+2] = 0 // MajorLinkerVersion
	headers[offset+3] = 0 // MinorLinkerVersion
	binary.LittleEndian.PutUint32(headers[offset+4:], uint32(sizeOfCode))
	binary.LittleEndian.PutUint32(headers[offset+8:], uint32(sizeOfData))
	// SizeOfUninitializedData = 0
	binary.LittleEndian.PutUint32(headers[offset+16:], uint32(entryPoint))
	binary.LittleEndian.PutUint32(headers[offset+20:], uint32(baseCodeRVA(sections)))
	binary.LittleEndian.PutUint64(headers[offset+24:], imageBase)
	binary.LittleEndian.PutUint32(headers[offset+32:], sectionAlignment)
	binary.LittleEndian.PutUint32(headers[offset+36:], fileAlignment)
	binary.LittleEndian.PutUint16(headers[offset+40:], 0) // MajorOperatingSystemVersion
	binary.LittleEndian.PutUint16(headers[offset+42:], 0) // MinorOperatingSystemVersion
	binary.LittleEndian.PutUint16(headers[offset+44:], 0) // MajorImageVersion
	binary.LittleEndian.PutUint16(headers[offset+46:], 0) // MinorImageVersion
	binary.LittleEndian.PutUint16(headers[offset+48:], majorSubsystemVersion)
	binary.LittleEndian.PutUint16(headers[offset+50:], 0) // MinorSubsystemVersion
	binary.LittleEndian.PutUint32(headers[offset+52:], 0) // Win32VersionValue
	binary.LittleEndian.PutUint32(headers[offset+56:], uint32(imageSize))
	binary.LittleEndian.PutUint32(headers[offset+60:], uint32(sizeOfHeaders))
	// CheckSum = 0
	binary.LittleEndian.PutUint16(headers[offset+68:], subsystemEFI)
	// DllCharacteristics = 0
	binary.LittleEndian.PutUint64(headers[offset+72:], 0x100000) // StackReserve
	binary.LittleEndian.PutUint64(headers[offset+80:], 0x1000)   // StackCommit
	binary.LittleEndian.PutUint64(headers[offset+88:], 0)        // HeapReserve
	binary.LittleEndian.PutUint64(headers[offset+96:], 0)        // HeapCommit
	binary.LittleEndian.PutUint32(headers[offset+104:], 0)       // LoaderFlags
	binary.LittleEndian.PutUint32(headers[offset+108:], dataDirectoryCount)

	for i := 0; i < dataDirectoryCount; i++ {
		dirOffset := offset + 0x70 + i*8
		binary.LittleEndian.PutUint32(headers[dirOffset:], 0)
		binary.LittleEndian.PutUint32(headers[dirOffset+4:], 0)
	}
	if relocVA != 0 || relocSize != 0 {
		dirOffset := offset + 0x70 + relocDirectoryIndex*8
		binary.LittleEndian.PutUint32(headers[dirOffset:], uint32(relocVA))
		binary.LittleEndian.PutUint32(headers[dirOffset+4:], uint32(relocSize))
	}
}

func baseCodeRVA(sections []codegen.Section) uint64 {
	for _, section := range sections {
		if section.Name == ".text" {
			return section.RVA
		}
	}
	return 0x1000
}

func writeSectionHeaders(headers []byte, sectionTableOffset int, sections []codegen.Section, rawPtrs []uint64, rawSizes []uint64) {
	for i, section := range sections {
		offset := sectionTableOffset + i*sectionHeaderSize
		copy(headers[offset:offset+8], encodeSectionName(section.Name))
		binary.LittleEndian.PutUint32(headers[offset+8:], uint32(len(section.Data)))
		binary.LittleEndian.PutUint32(headers[offset+12:], uint32(section.RVA))
		binary.LittleEndian.PutUint32(headers[offset+16:], uint32(rawSizes[i]))
		binary.LittleEndian.PutUint32(headers[offset+20:], uint32(rawPtrs[i]))
		// relocation and linenumber metadata stays zero
		binary.LittleEndian.PutUint16(headers[offset+32:], 0)
		binary.LittleEndian.PutUint16(headers[offset+34:], 0)
		binary.LittleEndian.PutUint32(headers[offset+36:], section.Characteristics)
	}
}

func normalizeAndOrderSections(sections []codegen.Section) []codegen.Section {
	out := append([]codegen.Section(nil), sections...)
	type item struct {
		section codegen.Section
		index   int
	}
	items := make([]item, len(out))
	for i, section := range out {
		items[i] = item{section: section, index: i}
	}
	priority := func(name string) int {
		switch name {
		case ".text":
			return 0
		case ".rdata":
			return 1
		case ".data":
			return 2
		case ".reloc":
			return 3
		default:
			return 4
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		pi, pj := priority(items[i].section.Name), priority(items[j].section.Name)
		if pi != pj {
			return pi < pj
		}
		return items[i].index < items[j].index
	})
	for i := range items {
		out[i] = items[i].section
	}
	return out
}

func assignSectionCharacteristics(sections []codegen.Section) {
	for i := range sections {
		if sections[i].Characteristics == 0 {
			if characteristics, ok := sectionCharacteristics[sections[i].Name]; ok {
				sections[i].Characteristics = characteristics
			}
		}
	}
}

func assignSectionRVAs(sections []codegen.Section) {
	next := uint64(0x1000)
	for i := range sections {
		if sections[i].RVA == 0 {
			sections[i].RVA = next
		}
		sectionSize := uint64(len(sections[i].Data))
		if sectionSize == 0 {
			sectionSize = 1
		}
		next = alignUp(sections[i].RVA+sectionSize, sectionAlignment)
	}
}

func computeDataSizes(sections []codegen.Section) (uint64, uint64) {
	var sizeOfCode, sizeOfData uint64
	for _, section := range sections {
		size := alignUp(uint64(len(section.Data)), sectionAlignment)
		if section.Name == ".text" {
			sizeOfCode = size
			continue
		}
		sizeOfData += size
	}
	return sizeOfCode, sizeOfData
}

func computeImageSize(sections []codegen.Section) uint64 {
	var end uint64
	for _, section := range sections {
		limit := alignUp(section.RVA+uint64(len(section.Data)), sectionAlignment)
		if limit > end {
			end = limit
		}
	}
	return alignUp(end, sectionAlignment)
}

func locateRelocDirectory(sections []codegen.Section) (uint64, uint64) {
	for _, section := range sections {
		if section.Name == ".reloc" {
			return section.RVA, uint64(len(section.Data))
		}
	}
	return 0, 0
}

func buildRelocData(img *codegen.Image) ([]byte, error) {
	entriesByPage := map[uint32][]uint16{}
	for _, reloc := range img.Relocs {
		symbolRVA, ok := img.Symbols[reloc.Symbol]
		if !ok {
			return nil, fmt.Errorf("relocation symbol %q not found", reloc.Symbol)
		}
		locationRVA := symbolRVA + reloc.Offset
		page := uint32(locationRVA &^ 0xFFF)
		entry := (uint16(reloc.Kind) << 12) | uint16(locationRVA&0xFFF)
		entriesByPage[page] = append(entriesByPage[page], entry)
	}

	var pages []uint32
	for page := range entriesByPage {
		pages = append(pages, page)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i] < pages[j] })

	var out bytes.Buffer
	for _, page := range pages {
		entries := entriesByPage[page]
		sort.Slice(entries, func(i, j int) bool { return entries[i] < entries[j] })

		block := &bytes.Buffer{}
		block.Write(make([]byte, 8))
		for _, entry := range entries {
			var raw [2]byte
			binary.LittleEndian.PutUint16(raw[:], entry)
			block.Write(raw[:])
		}
		for block.Len()%4 != 0 {
			block.WriteByte(0)
		}
		data := block.Bytes()
		binary.LittleEndian.PutUint32(data[0:4], page)
		binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)))
		out.Write(data)
	}

	return out.Bytes(), nil
}

func encodeSectionName(name string) []byte {
	out := make([]byte, 8)
	copy(out, []byte(name))
	return out
}

func alignUp(value, alignment uint64) uint64 {
	if alignment == 0 {
		return value
	}
	return (value + alignment - 1) &^ (alignment - 1)
}

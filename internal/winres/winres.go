// Package winres turns a .ico file into a Windows resource object
// (.syso) holding the program icon. The Go linker links any
// *_windows_amd64.syso in a main package into the .exe, so Explorer, the
// taskbar and shortcuts show Ebur's icon.
//
// It writes a COFF object with one .rsrc section: a resource directory
// (type, then ID, then language) with RT_ICON entries for each image and
// one RT_GROUP_ICON that lists them, as the resource compiler would. Data
// entries hold section-relative addresses, fixed up by ADDR32NB
// relocations against the section symbol.
package winres

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	langEnUS    = 0x0409

	machineAMD64     = 0x8664
	relAMD64Addr32NB = 0x0003
	scnInitData      = 0x00000040
	scnMemRead       = 0x40000000
	symClassStatic   = 3
	subdirFlag       = 0x80000000
)

// iconImage is one image from a .ico file.
type iconImage struct {
	width, height, colors, reserved byte
	planes, bitCount                uint16
	data                            []byte
}

// parseICO reads the images of a .ico file.
func parseICO(ico []byte) ([]iconImage, error) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[0:]) != 0 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return nil, errors.New("not a .ico file")
	}
	n := int(binary.LittleEndian.Uint16(ico[4:]))
	if n == 0 || len(ico) < 6+16*n {
		return nil, errors.New(".ico file has no images or is truncated")
	}
	imgs := make([]iconImage, n)
	for i := range imgs {
		e := ico[6+16*i:]
		size := binary.LittleEndian.Uint32(e[8:])
		off := binary.LittleEndian.Uint32(e[12:])
		if uint64(off)+uint64(size) > uint64(len(ico)) || size == 0 {
			return nil, fmt.Errorf(".ico image %d lies outside the file", i+1)
		}
		imgs[i] = iconImage{
			width: e[0], height: e[1], colors: e[2], reserved: e[3],
			planes: binary.LittleEndian.Uint16(e[4:]), bitCount: binary.LittleEndian.Uint16(e[6:]),
			data: ico[off : off+size],
		}
	}
	return imgs, nil
}

// leaf is one resource: its type, ID and bytes.
type leaf struct {
	typ, id uint16
	data    []byte
}

// SysoFromICO builds an amd64 .syso containing the icon. The output is
// deterministic, so a committed .syso can be checked against its .ico.
func SysoFromICO(ico []byte) ([]byte, error) {
	imgs, err := parseICO(ico)
	if err != nil {
		return nil, err
	}
	var leaves []leaf
	group := new(bytes.Buffer)
	binary.Write(group, binary.LittleEndian, [3]uint16{0, 1, uint16(len(imgs))}) // GRPICONDIR
	for i, img := range imgs {
		id := uint16(i + 1)
		leaves = append(leaves, leaf{rtIcon, id, img.data})
		group.Write([]byte{img.width, img.height, img.colors, img.reserved})
		binary.Write(group, binary.LittleEndian, img.planes)
		binary.Write(group, binary.LittleEndian, img.bitCount)
		binary.Write(group, binary.LittleEndian, uint32(len(img.data)))
		binary.Write(group, binary.LittleEndian, id)
	}
	leaves = append(leaves, leaf{rtGroupIcon, 1, group.Bytes()})
	section, relocs := resourceSection(leaves)
	return coff(section, relocs), nil
}

// resourceSection lays out the three-level directory, data entries and
// data. It returns the section bytes and the offsets of the data-entry
// address fields that need relocating.
func resourceSection(leaves []leaf) ([]byte, []uint32) {
	// Group leaves by type, in ascending type order (they are added that way).
	var types []uint16
	byType := map[uint16][]leaf{}
	for _, l := range leaves {
		if _, ok := byType[l.typ]; !ok {
			types = append(types, l.typ)
		}
		byType[l.typ] = append(byType[l.typ], l)
	}
	const dirSize, entrySize, dataEntrySize = 16, 8, 16

	// Sizes of each part, to compute offsets before writing.
	rootSize := dirSize + entrySize*len(types)
	typeDirsSize := 0
	for _, t := range types {
		typeDirsSize += dirSize + entrySize*len(byType[t])
	}
	langDirsSize := len(leaves) * (dirSize + entrySize)
	dataEntriesStart := rootSize + typeDirsSize + langDirsSize
	dataStart := dataEntriesStart + dataEntrySize*len(leaves)

	buf := new(bytes.Buffer)
	le := func(v any) { binary.Write(buf, binary.LittleEndian, v) }
	dir := func(ids int) { le([6]uint16{}); le(uint16(0)); le(uint16(ids)) } // characteristics, time, version: 0

	// Root: one entry per type, pointing at that type's directory.
	dir(len(types))
	off := rootSize
	for _, t := range types {
		le(uint32(t))
		le(uint32(off) | subdirFlag)
		off += dirSize + entrySize*len(byType[t])
	}
	// Type directories: one entry per ID, pointing at its language directory.
	langOff := rootSize + typeDirsSize
	for _, t := range types {
		dir(len(byType[t]))
		for _, l := range byType[t] {
			le(uint32(l.id))
			le(uint32(langOff) | subdirFlag)
			langOff += dirSize + entrySize
		}
	}
	// Language directories: one entry each, pointing at a data entry.
	for i := range leaves {
		dir(1)
		le(uint32(langEnUS))
		le(uint32(dataEntriesStart + dataEntrySize*i))
	}
	// Data entries, in the same order as the language directories.
	var relocs []uint32
	dataOff := dataStart
	order := make([]leaf, 0, len(leaves))
	for _, t := range types {
		order = append(order, byType[t]...)
	}
	for _, l := range order {
		relocs = append(relocs, uint32(buf.Len()))
		le(uint32(dataOff)) // section-relative; the linker adds the section's address
		le(uint32(len(l.data)))
		le(uint32(0)) // code page
		le(uint32(0)) // reserved
		dataOff += align8(len(l.data))
	}
	for _, l := range order {
		buf.Write(l.data)
		buf.Write(make([]byte, align8(len(l.data))-len(l.data)))
	}
	return buf.Bytes(), relocs
}

func align8(n int) int { return (n + 7) &^ 7 }

// coff wraps a .rsrc section in an object file: header, section header,
// data, relocations, one section symbol, and an empty string table.
func coff(section []byte, relocs []uint32) []byte {
	const fileHeaderSize, sectionHeaderSize, relocSize = 20, 40, 10
	dataPtr := uint32(fileHeaderSize + sectionHeaderSize)
	relocPtr := dataPtr + uint32(len(section))
	symPtr := relocPtr + uint32(relocSize*len(relocs))

	buf := new(bytes.Buffer)
	le := func(v any) { binary.Write(buf, binary.LittleEndian, v) }
	name := [8]byte{'.', 'r', 's', 'r', 'c'}
	// File header.
	le(uint16(machineAMD64))
	le(uint16(1)) // sections
	le(uint32(0)) // timestamp: 0 keeps the output reproducible
	le(symPtr)
	le(uint32(1)) // symbols
	le(uint16(0)) // optional header size
	le(uint16(0)) // characteristics
	// Section header.
	le(name)
	le(uint32(0)) // virtual size
	le(uint32(0)) // virtual address
	le(uint32(len(section)))
	le(dataPtr)
	le(relocPtr)
	le(uint32(0)) // line numbers
	le(uint16(len(relocs)))
	le(uint16(0))
	le(uint32(scnInitData | scnMemRead))
	buf.Write(section)
	for _, r := range relocs {
		le(r)         // offset of the field within the section
		le(uint32(0)) // symbol 0: the section
		le(uint16(relAMD64Addr32NB))
	}
	// Symbol: the section itself.
	le(name)
	le(uint32(0)) // value
	le(int16(1))  // section number
	le(uint16(0)) // type
	le(uint8(symClassStatic))
	le(uint8(0))  // aux symbols
	le(uint32(4)) // string table: just its own size
	return buf.Bytes()
}

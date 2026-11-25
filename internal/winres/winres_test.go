package winres

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// testICO builds a .ico with the given image payloads (any bytes will do).
func testICO(images ...[]byte) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, [3]uint16{0, 1, uint16(len(images))})
	off := 6 + 16*len(images)
	for i, img := range images {
		b.Write([]byte{byte(16 * (i + 1)), byte(16 * (i + 1)), 0, 0})
		binary.Write(&b, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&b, binary.LittleEndian, [2]uint32{uint32(len(img)), uint32(off)})
		off += len(img)
	}
	for _, img := range images {
		b.Write(img)
	}
	return b.Bytes()
}

// lookup follows a resource directory path (type, id, language) and
// returns the data the leaf points at, applying the section-relative
// address the way the linker's relocation would with a base of 0.
func lookup(t *testing.T, sec []byte, path ...uint32) []byte {
	t.Helper()
	off := uint32(0)
	for depth, want := range path {
		named := binary.LittleEndian.Uint16(sec[off+12:])
		ids := binary.LittleEndian.Uint16(sec[off+14:])
		found := false
		for i := 0; i < int(named+ids); i++ {
			e := sec[off+16+uint32(8*i):]
			if binary.LittleEndian.Uint32(e) != want {
				continue
			}
			next := binary.LittleEndian.Uint32(e[4:])
			isDir := next&subdirFlag != 0
			if isDir != (depth < len(path)-1) {
				t.Fatalf("level %d entry %d: subdirectory flag %v", depth, want, isDir)
			}
			off, found = next&^subdirFlag, true
			break
		}
		if !found {
			t.Fatalf("no entry %d at level %d", want, depth)
		}
	}
	rva := binary.LittleEndian.Uint32(sec[off:])
	size := binary.LittleEndian.Uint32(sec[off+4:])
	return sec[rva : rva+size]
}

func TestSysoFromICO(t *testing.T) {
	small, big := bytes.Repeat([]byte{0xA1}, 13), bytes.Repeat([]byte{0xB2}, 300)
	syso, err := SysoFromICO(testICO(small, big))
	if err != nil {
		t.Fatal(err)
	}
	f, err := pe.NewFile(bytes.NewReader(syso))
	if err != nil {
		t.Fatalf("not a valid COFF object: %v", err)
	}
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || len(f.Sections) != 1 || f.Sections[0].Name != ".rsrc" {
		t.Fatalf("header: machine %#x, sections %v", f.Machine, f.Sections)
	}
	s := f.Sections[0]
	if len(s.Relocs) != 3 {
		t.Fatalf("%d relocations, want one per resource (3)", len(s.Relocs))
	}
	for _, r := range s.Relocs {
		if r.Type != relAMD64Addr32NB || r.SymbolTableIndex != 0 {
			t.Errorf("relocation %+v", r)
		}
	}
	if len(f.Symbols) != 1 || f.Symbols[0].Name != ".rsrc" || f.Symbols[0].SectionNumber != 1 {
		t.Fatalf("symbols %+v", f.Symbols)
	}
	sec, _ := s.Data()
	if got := lookup(t, sec, rtIcon, 1, langEnUS); !bytes.Equal(got, small) {
		t.Error("icon 1 data differs")
	}
	if got := lookup(t, sec, rtIcon, 2, langEnUS); !bytes.Equal(got, big) {
		t.Error("icon 2 data differs")
	}
	grp := lookup(t, sec, rtGroupIcon, 1, langEnUS)
	if len(grp) != 6+14*2 || binary.LittleEndian.Uint16(grp[4:]) != 2 {
		t.Fatalf("group header %x", grp[:6])
	}
	second := grp[6+14:]
	if second[0] != 32 || binary.LittleEndian.Uint16(second[6:]) != 32 ||
		binary.LittleEndian.Uint32(second[8:]) != 300 || binary.LittleEndian.Uint16(second[12:]) != 2 {
		t.Errorf("group entry 2: %x", second)
	}
	again, _ := SysoFromICO(testICO(small, big))
	if !bytes.Equal(again, syso) {
		t.Error("output is not deterministic")
	}
}

func TestRejectsBadICO(t *testing.T) {
	for name, b := range map[string][]byte{
		"empty":     nil,
		"not icon":  []byte("PNG not an icon file"),
		"no images": {0, 0, 1, 0, 0, 0},
		"truncated": testICO([]byte("abc"))[:20],
	} {
		if _, err := SysoFromICO(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The committed .syso files must match build\ebur.ico.
func TestCommittedSysoUpToDate(t *testing.T) {
	ico, err := os.ReadFile(filepath.Join("..", "..", "build", "ebur.ico"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := SysoFromICO(ico)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"cli", "app", "helper"} {
		got, err := os.ReadFile(filepath.Join("..", "..", dir, "rsrc_windows_amd64.syso"))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s\\rsrc_windows_amd64.syso is missing or stale; run `go run ./build/winres`", dir)
		}
	}
}

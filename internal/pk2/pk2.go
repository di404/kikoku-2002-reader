// Package pk2 reads the ARCV archives (.PK2, script.chs) of Nitroplus's
// 2002 engine.
//
// Layout, all integers little-endian:
//
//	"ARCV" u32 n | names: packed entry (n bytes)    file names, '\r'-separated
//	"INDX" u32 n | n/12 × {u32 offset, u32 size, u32 dos time}
//	"DATA" u32 0 | entry data
//
// The first name and index entry describe the archive itself; the others
// are files, at absolute offsets in the archive. An entry is stored either
// as is (PNG, JPEG, WAV) or packed as u24 length, 'Z', zlib stream.
package pk2

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"golang.org/x/text/encoding/japanese"
)

type Entry struct {
	Name   string // as stored, decoded from Shift-JIS
	Offset int64
	Size   int64
}

type Archive struct {
	f       *os.File
	Name    string
	Entries []Entry
	byKey   map[string]int
}

func Open(p string) (*Archive, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	a, err := parse(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path.Base(p), err)
	}
	return a, nil
}

func parse(f *os.File) (*Archive, error) {
	var hdr [8]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil {
		return nil, err
	}
	if string(hdr[:4]) != "ARCV" {
		return nil, errors.New("not an ARCV archive")
	}
	n := int64(binary.LittleEndian.Uint32(hdr[4:]))
	// The packed name list is followed directly by "INDX"; the stored
	// length is not always the exact size, so look for the tag.
	head := make([]byte, n+64)
	m, _ := f.ReadAt(head, 8)
	head = head[:m]
	i := bytes.Index(head, []byte("INDX"))
	if i < 0 {
		return nil, errors.New("no INDX")
	}
	names, err := unpack(head[:i])
	if err != nil {
		return nil, fmt.Errorf("names: %w", err)
	}
	indx := int64(8 + i)
	var sz [4]byte
	if _, err := f.ReadAt(sz[:], indx+4); err != nil {
		return nil, err
	}
	idx := make([]byte, binary.LittleEndian.Uint32(sz[:]))
	if _, err := f.ReadAt(idx, indx+8); err != nil {
		return nil, err
	}
	list := strings.Split(decodeSJIS(names), "\r")
	count := len(idx) / 12
	if len(list) < count {
		return nil, fmt.Errorf("%d names for %d entries", len(list), count)
	}
	a := &Archive{f: f, Name: list[0], byKey: map[string]int{}}
	for k := 1; k < count; k++ {
		e := Entry{
			Name:   list[k],
			Offset: int64(binary.LittleEndian.Uint32(idx[k*12:])),
			Size:   int64(binary.LittleEndian.Uint32(idx[k*12+4:])),
		}
		a.byKey[Key(e.Name)] = len(a.Entries)
		a.Entries = append(a.Entries, e)
	}
	return a, nil
}

// Key is the lookup form of a name: lower case, no extension. Scripts name
// resources without extension and in any case ("B_..." for "b_....jpg").
func Key(name string) string {
	name = strings.ToLower(name)
	return strings.TrimSuffix(name, path.Ext(name))
}

func (a *Archive) Close() error { return a.f.Close() }

// Find returns the entry for a name with or without extension.
func (a *Archive) Find(name string) (Entry, bool) {
	i, ok := a.byKey[Key(name)]
	if !ok {
		return Entry{}, false
	}
	return a.Entries[i], true
}

func (a *Archive) Has(name string) bool {
	_, ok := a.byKey[Key(name)]
	return ok
}

func (a *Archive) Read(e Entry) ([]byte, error) {
	b := make([]byte, e.Size)
	if _, err := a.f.ReadAt(b, e.Offset); err != nil {
		return nil, err
	}
	return unpack(b)
}

func (a *Archive) ReadFile(name string) ([]byte, error) {
	e, ok := a.Find(name)
	if !ok {
		return nil, fmt.Errorf("%s: not found", name)
	}
	return a.Read(e)
}

// unpack inflates a packed entry and returns stored entries unchanged.
func unpack(b []byte) ([]byte, error) {
	if len(b) < 6 || b[3] != 'Z' || b[4] != 0x78 {
		return b, nil
	}
	n := int(b[0]) | int(b[1])<<8 | int(b[2])<<16
	r, err := zlib.NewReader(bytes.NewReader(b[4:]))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := make([]byte, n)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeSJIS(b []byte) string {
	s, err := japanese.ShiftJIS.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(s)
}

// Package altium writes a CAD library as Altium Designer libraries: a
// schematic library (.SchLib) and a PCB footprint library (.PcbLib), both
// compound files of binary records. The record layouts follow what Altium
// itself writes, as KiCad's Altium importer and AltiumSharp read them.
package altium

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"sort"
	"strings"
	"unicode/utf16"
)

// A compound file (Microsoft's [MS-CFB], version 3: 512-byte sectors),
// the container Altium's libraries are: storages (folders) of streams
// (files). Streams under 4096 bytes live in the mini stream, in 64-byte
// mini sectors, as the format requires.

// Special sector numbers.
const (
	freeSect   = 0xFFFFFFFF
	endOfChain = 0xFFFFFFFE
	fatSect    = 0xFFFFFFFD
	noStream   = 0xFFFFFFFF
)

// Sizes.
const (
	sectorSize     = 512
	miniSectorSize = 64
	miniCutoff     = 4096
)

// Storage is a folder in a compound file.
type Storage struct {
	name     string
	storages []*Storage
	streams  []*stream
}

// stream is a file in a compound file.
type stream struct {
	name string
	data []byte
}

// NewStorage is an empty root storage.
func NewStorage() *Storage { return &Storage{name: "Root Entry"} }

// Storage adds a storage.
func (s *Storage) Storage(name string) *Storage {
	c := &Storage{name: name}
	s.storages = append(s.storages, c)
	return c
}

// Stream adds a stream.
func (s *Storage) Stream(name string, data []byte) {
	s.streams = append(s.streams, &stream{name: name, data: data})
}

// entry is a directory entry being laid out.
type entry struct {
	name               string
	kind               byte // 1 storage, 2 stream, 5 root
	left, right, child uint32
	red                bool
	start              uint32
	size               uint32
	data               []byte
	children           []int
	storage            *Storage
}

// less orders sibling names as the format does: shorter first, then by
// their upper-cased UTF-16 code units.
func less(a, b string) bool {
	ua, ub := utf16.Encode([]rune(strings.ToUpper(a))), utf16.Encode([]rune(strings.ToUpper(b)))
	if len(ua) != len(ub) {
		return len(ua) < len(ub)
	}
	for i := range ua {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return false
}

// Bytes lays the compound file out.
func (s *Storage) Bytes() ([]byte, error) {
	// Directory entries, the root first.
	var entries []*entry
	var add func(st *Storage, kind byte) int
	add = func(st *Storage, kind byte) int {
		id := len(entries)
		e := &entry{name: st.name, kind: kind, left: noStream, right: noStream, child: noStream, storage: st}
		entries = append(entries, e)
		for _, c := range st.storages {
			e.children = append(e.children, add(c, 1))
		}
		for _, sm := range st.streams {
			cid := len(entries)
			entries = append(entries, &entry{name: sm.name, kind: 2, left: noStream, right: noStream, child: noStream, data: sm.data, size: u32(len(sm.data))})
			e.children = append(e.children, cid)
		}
		return id
	}
	add(s, 5)
	for _, e := range entries {
		if len(utf16.Encode([]rune(e.name))) > 31 {
			return nil, errors.New("compound file name over 31 characters: " + e.name)
		}
		if len(e.children) == 0 {
			continue
		}
		kids := append([]int{}, e.children...)
		sort.Slice(kids, func(i, j int) bool { return less(entries[kids[i]].name, entries[kids[j]].name) })
		e.child = u32(tree(entries, kids, 0, maxDepth(len(kids))))
	}

	// The mini stream: small streams in 64-byte mini sectors.
	var mini bytes.Buffer
	var miniFAT []uint32
	for _, e := range entries {
		if e.kind != 2 || e.size == 0 || e.size >= miniCutoff {
			if e.kind == 2 && e.size == 0 {
				e.start = endOfChain
			}
			continue
		}
		e.start = u32(len(miniFAT))
		n := (len(e.data) + miniSectorSize - 1) / miniSectorSize
		for i := 0; i < n; i++ {
			if i == n-1 {
				miniFAT = append(miniFAT, endOfChain)
			} else {
				miniFAT = append(miniFAT, u32(len(miniFAT)+1))
			}
		}
		mini.Write(e.data)
		mini.Write(make([]byte, n*miniSectorSize-len(e.data)))
	}

	// Sectors: the big streams, then the mini stream, the mini FAT and
	// the directory; the FAT goes last, sized to cover them and itself.
	var fat []uint32
	var body bytes.Buffer
	chain := func(data []byte) uint32 {
		if len(data) == 0 {
			return endOfChain
		}
		start := u32(len(fat))
		n := (len(data) + sectorSize - 1) / sectorSize
		for i := 0; i < n; i++ {
			if i == n-1 {
				fat = append(fat, endOfChain)
			} else {
				fat = append(fat, u32(len(fat)+1))
			}
		}
		body.Write(data)
		body.Write(make([]byte, n*sectorSize-len(data)))
		return start
	}
	for _, e := range entries {
		if e.kind == 2 && e.size >= miniCutoff {
			e.start = chain(e.data)
		}
	}
	root := entries[0]
	root.start = chain(mini.Bytes())
	root.size = u32(mini.Len())
	if mini.Len() == 0 {
		root.start = endOfChain
	}
	miniFATStart, miniFATSectors := uint32(endOfChain), 0
	if len(miniFAT) > 0 {
		b := make([]byte, 0, len(miniFAT)*4)
		for _, v := range miniFAT {
			b = binary.LittleEndian.AppendUint32(b, v)
		}
		for len(b)%sectorSize != 0 {
			b = binary.LittleEndian.AppendUint32(b, freeSect)
		}
		miniFATStart, miniFATSectors = chain(b), len(b)/sectorSize
	}
	var dir []byte
	for _, e := range entries {
		dir = append(dir, e.encode()...)
	}
	for len(dir)%sectorSize != 0 {
		dir = append(dir, emptyEntry()...)
	}
	dirStart := chain(dir)

	// The FAT covers every sector, its own included.
	nFAT := 1
	for (len(fat)+nFAT)*4 > nFAT*sectorSize {
		nFAT++
	}
	if nFAT > 109 {
		return nil, errors.New("compound file too big for one header's FAT list")
	}
	fatStart := u32(len(fat))
	for i := 0; i < nFAT; i++ {
		fat = append(fat, fatSect)
	}
	for len(fat) < nFAT*sectorSize/4 {
		fat = append(fat, freeSect)
	}

	var out bytes.Buffer
	h := make([]byte, sectorSize)
	copy(h, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	le := binary.LittleEndian
	le.PutUint16(h[24:], 0x003E) // minor version
	le.PutUint16(h[26:], 3)      // major version
	le.PutUint16(h[28:], 0xFFFE) // byte order
	le.PutUint16(h[30:], 9)      // sector shift
	le.PutUint16(h[32:], 6)      // mini sector shift
	le.PutUint32(h[44:], u32(nFAT))
	le.PutUint32(h[48:], dirStart)     // first directory sector
	le.PutUint32(h[56:], miniCutoff)   // mini stream cutoff
	le.PutUint32(h[60:], miniFATStart) // first mini FAT sector
	le.PutUint32(h[64:], u32(miniFATSectors))
	le.PutUint32(h[68:], endOfChain) // first DIFAT sector
	for i := 0; i < 109; i++ {
		v := uint32(freeSect)
		if i < nFAT {
			v = fatStart + u32(i)
		}
		le.PutUint32(h[76+4*i:], v)
	}
	out.Write(h)
	out.Write(body.Bytes())
	for _, v := range fat {
		out.Write(le.AppendUint32(nil, v))
	}
	return out.Bytes(), nil
}

// u32 is a count or index as the format's 32-bit field; past that the
// file can't be written.
func u32(n int) uint32 {
	if n < 0 || n > math.MaxUint32 {
		panic("compound file: a count out of range")
	}
	return uint32(n) //nolint:gosec // checked above
}

// maxDepth is the depth of the deepest node of a balanced tree of n.
func maxDepth(n int) int {
	d := 0
	for (1<<(d+1))-1 < n {
		d++
	}
	return d
}

// tree builds a balanced binary search tree of sorted siblings and
// returns its root: nodes on the deepest, incomplete level are red, the
// rest black, which keeps every path's black count the same.
func tree(entries []*entry, ids []int, depth, deepest int) int {
	if len(ids) == 0 {
		return -1
	}
	mid := len(ids) / 2
	e := entries[ids[mid]]
	e.red = depth == deepest && depth > 0
	l := tree(entries, ids[:mid], depth+1, deepest)
	r := tree(entries, ids[mid+1:], depth+1, deepest)
	e.left, e.right = noStream, noStream
	if l >= 0 {
		e.left = u32(l)
	}
	if r >= 0 {
		e.right = u32(r)
	}
	return ids[mid]
}

// encode writes a directory entry.
func (e *entry) encode() []byte {
	b := make([]byte, 128)
	u := utf16.Encode([]rune(e.name))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	binary.LittleEndian.PutUint16(b[64:], uint16(u32(2*(len(u)+1)))) //nolint:gosec // names are at most 31 characters (checked in Bytes)
	b[66] = e.kind
	b[67] = 1 // black
	if e.red {
		b[67] = 0
	}
	binary.LittleEndian.PutUint32(b[68:], e.left)
	binary.LittleEndian.PutUint32(b[72:], e.right)
	binary.LittleEndian.PutUint32(b[76:], e.child)
	binary.LittleEndian.PutUint32(b[116:], e.start)
	binary.LittleEndian.PutUint32(b[120:], e.size)
	return b
}

// emptyEntry is an unused directory entry.
func emptyEntry() []byte {
	b := make([]byte, 128)
	binary.LittleEndian.PutUint32(b[68:], noStream)
	binary.LittleEndian.PutUint32(b[72:], noStream)
	binary.LittleEndian.PutUint32(b[76:], noStream)
	return b
}

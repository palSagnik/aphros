package log

import (
	"io"
	"os"

	"github.com/tysonmote/gommap"
)

// INDEX ENTRY:
//  offset | position |
// offsets stored as uint32 -> 4 bytes
// positions stored as uint64 -> 8 bytes

var (
	offsetWidth   uint64 = 4
	positionWidth uint64 = 8
	entryWidth    uint64 = offsetWidth + positionWidth
)

type index struct {
	file *os.File
	mmap gommap.MMap
	size uint64
}

// newIndex creates a new index instance with memory-mapped file access.
// It takes a file pointer and configuration, then:
// - Gets file size information
// - Truncates the file to the maximum index size specified in config
// - Memory-maps the file for efficient read/write operations
// Returns the initialized index or an error if any operation fails.
func newIndex(f *os.File, c Config) (*index, error) {
	fi, err := os.Stat(f.Name())
	if err != nil {
		return nil, err
	}

	idx := &index{
		file: f,
		size: uint64(fi.Size()),
	}
	if err = os.Truncate(f.Name(), int64(c.Segment.MaxIndexBytes)); err != nil {
		return nil, err
	}

	if idx.mmap, err = gommap.Map(
		idx.file.Fd(), 
		gommap.PROT_READ|gommap.PROT_WRITE, 
		gommap.MAP_SHARED,
	); err != nil {
		return nil, err
	}

	return idx, nil
}

func (i *index) Close() error {
	if err := i.mmap.Sync(gommap.MS_SYNC); err != nil {
		return err
	}

	if err := i.file.Sync(); err != nil {
		return err
	}

	if err := i.file.Truncate(int64(i.size)); err != nil {
		return err
	} 

	return i.file.Close()
}

// Read returns the associated record's position in the store for the given offset.
// If in is -1, it reads the last entry in the index. Returns the record's offset, position in the store, and any error encountered. 
// Returns io.EOF if the index is empty or if the requested offset is beyond the end of the index.
func (i *index) Read(in int64) (out uint32, pos uint64, err error) {
	if i.size == 0 {
		return 0, 0, io.EOF
	}

	// in == -1 sets the last offset as out 
	// else the current offset
	if in == -1 {
		out = uint32((i.size/entryWidth) - 1)
	} else {
		out = uint32(in)
	}

	// if position + entrySize exceeds filesize -> End Of File
	pos = uint64(out) * entryWidth
	if i.size < pos + entryWidth {
		return 0, 0, io.EOF
	}
	out = encoding.Uint32(i.mmap[pos : pos+offsetWidth])
	pos = encoding.Uint64(i.mmap[pos+offsetWidth : pos+entryWidth])
	return out, pos, nil
}

func (i *index) Write(off uint32, pos uint64) error {
	
	// Check if index file is not full
	if i.IsMaxed() {
		return io.EOF
	}
	encoding.PutUint32(i.mmap[i.size : i.size + offsetWidth], off)
	encoding.PutUint64(i.mmap[i.size + offsetWidth : i.size + entryWidth], pos)
	i.size += entryWidth
	return nil
}

func (i *index) Name() string {
	return i.file.Name()
}

func (i *index) IsMaxed() bool {
	return uint64(len(i.mmap)) < i.size+entryWidth
}

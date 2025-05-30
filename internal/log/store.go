package log

import (
	"bufio"
	"encoding/binary"
	"os"
	"sync"
)

var (
	encoding = binary.BigEndian
)

const (
	lenWidth = 8
)

type store struct {
	File *os.File
	mu sync.Mutex
	buf *bufio.Writer
	size uint64
}

func newStore(f *os.File) (*store, error) {
	fi, err := os.Stat(f.Name())
	if err != nil {
		return nil, err
	}

	size := uint64(fi.Size())
	return &store{
		File: f,
		size: size,
		buf: bufio.NewWriter(f),
	}, err
}

// Append writes data to the store and returns the number of bytes written,
// the position where the data was written, and any error encountered.
// It first writes the length of the data as a uint64, followed by the data itself.
// Returns the total bytes written (including the length prefix), the starting
// position of the write operation, and nil on success.
func (s *store) Append(p []byte) (n uint64, position uint64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	position = s.size

	// Apending the size of the record to be written
	if err := binary.Write(s.buf, encoding, uint64(len(p))); err != nil {
		return 0, 0, err
	}
	w, err := s.buf.Write(p)
	if err != nil {
		return 0, 0, err
	}

	w += lenWidth
	s.size += uint64(w)
	return uint64(w), position, nil
}

// Read reads the record stored at the given position in the store.
// It first flushes any buffered writes to ensure data consistency,
// then reads the record length from the position and uses it to
// read the complete record data.
// Returns the record data as a byte slice or an error if the read fails.
func (s *store) Read(position uint64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Flushing the buffer
	if err := s.buf.Flush(); err != nil {
		return nil, err
	}

	// Get the number of bytes to read in size buffer
	size := make([]byte, lenWidth)
	if _, err := s.File.ReadAt(size, int64(position)); err != nil {
		return nil, err
	}

	b := make([]byte, encoding.Uint64(size))
	if _, err := s.File.ReadAt(b, int64(position + lenWidth)); err != nil {
		return nil, err
	}
	return b, nil
}

// ReadAt reads len(p) bytes from the store starting at byte offset off.
// It returns the number of bytes read and any error encountered.
// ReadAt flushes any buffered data before reading to ensure consistency.
// This method is safe for concurrent use as it acquires a mutex lock.
func (s *store) ReadAt(p []byte, off int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.buf.Flush(); err != nil {
		return 0, err
	}
	return s.File.ReadAt(p, off)
}

func (s *store) Name() string {
	return s.File.Name()
}

func (s *store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.buf.Flush(); err != nil {
		return err
	}

	return s.File.Close()
}
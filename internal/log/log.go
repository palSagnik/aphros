package log

import (
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"

	api "github.com/palSagnik/aphros/api/v1"
)

// This is the implementation of an Append only Log
// A log contains many segments where each segment has a store and index
// A store contains the messages, while index stores the offsets to the entries of store


// Log represents a write-ahead log that manages multiple segments of log data.
// It provides thread-safe operations for reading and writing log entries across
// multiple segment files stored in a directory. The Log maintains an active
// segment for writes and a collection of segments for reads, automatically
// managing segment rotation based on the provided configuration.
type Log struct {
	mu            sync.RWMutex

	Dir           string
	Config        Config

	activeSegment *segment
	segments      []*segment
}

func NewLog(dir string, c Config) (*Log, error) {
	if c.Segment.MaxStoreBytes == 0 {
		c.Segment.MaxStoreBytes = 1024
	}

	if c.Segment.MaxIndexBytes == 0 {
		c.Segment.MaxIndexBytes = 1024
	}

	l := &Log{
		Dir: dir,
		Config: c,
	}

	return l, l.setup()
}

func (l *Log) newSegment(off uint64) error {
	s, err := newSegment(l.Dir, off, l.Config)
	if err != nil {
		return err
	}

	l.segments = append(l.segments, s)
	l.activeSegment = s

	return nil
}

// setup initializes the Log by reading existing segment files from the directory,
// parsing their base offsets from filenames, sorting them in ascending order,
// and creating segment objects for each. If no segments exist, it creates an
// initial segment starting from the configured initial offset.
func (l *Log) setup() error {
	files, err := os.ReadDir(l.Dir)
	if err != nil {
		return err
	}

	var baseOffsets []uint64
	for _, file := range files {
		offStr := strings.TrimSuffix(
			file.Name(),
			path.Ext(file.Name()),
		)
		off, _ := strconv.ParseUint(offStr, 10, 0)
		baseOffsets = append(baseOffsets, off)
	}
	sort.Slice(baseOffsets, func (i, j int) bool {
		return baseOffsets[i] < baseOffsets[j]
	})

	for _, baseOffset := range baseOffsets {
		if err := l.newSegment(baseOffset); err != nil {
			return err
		}
	}
	
	// if segments empty, start from initial offset
	if l.segments == nil {
		if err := l.newSegment(l.Config.Segment.InitialOffset); err != nil {
			return err
		}
	}

	return nil
}

// Append adds a record to the log and returns the offset where the record was stored.
// It appends the record to the active segment and creates a new segment if the current
// segment has reached its maximum capacity. The method is thread-safe and returns
// the offset of the appended record or an error if the operation fails.
func (l *Log) Append(record *api.Record) (uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	off, err := l.activeSegment.Append(record)
	if err != nil {
		return 0, err
	}

	// if segment is maxed out
	if l.activeSegment.IsMaxed() {
		err = l.newSegment(off + 1)
	}
	return off, err
}

// Read retrieves a record from the log at the specified offset.
// It searches through the log segments to find the one containing the given offset,
// then reads the record from that segment.
//
// Returns: 
// The record at the specified offset 
// An error if the offset is out of range or the record cannot be read
//
// The method is thread-safe and uses a read lock to prevent concurrent modifications
// while searching for and reading the record.
func (l *Log) Read (off uint64) (*api.Record, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var s *segment
	for _, segment := range l.segments {
		if segment.baseOffset <= off && off <= segment.nextOffset {
			s = segment
			break
		}
	}

	if s == nil || s.nextOffset <= off {
		return nil, api.ErrOffsetOutOfRange{Offset: off}
	}

	return s.Read(off)
}	

// Close closes the log and all its segments. It acquires a lock to ensure
// thread-safe operation and iterates through all segments, closing each one.
// If any segment fails to close, it returns the first error encountered.
// Returns nil if all segments are successfully closed.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, segment := range l.segments {
		if err := segment.Close(); err != nil {
			return err
		}
	}

	return nil
}

// Remove closes the log and removes all files and directories associated with it.
// It first attempts to close the log gracefully, and if successful, removes the
// entire directory structure. Returns an error if either the close operation or
// the removal fails.
func (l *Log) Remove() error {
	if err := l.Close(); err != nil {
		return err
	}

	return os.RemoveAll(l.Dir)
}

func (l *Log) Reset() error {
	if err := l.Remove(); err != nil {
		return err
	}

	return l.setup()
}

func (l *Log) LowestOffset() (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.segments[0].baseOffset, nil
}

func (l *Log) HighestOffset() (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	off := l.segments[len(l.segments) - 1].nextOffset
	if off == 0 {
		return 0, nil
	}

	return off - 1, nil
}

func (l *Log) Truncate(lowest uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var segments []*segment
	for _, s := range l.segments {
		if s.nextOffset <= lowest + 1 {
			if err := s.Remove(); err != nil {
				return err
			}
			continue
		}
		segments = append(segments, s)
	}
	l.segments = segments
	return nil
} 

// Methods required for consensus implementation
type originReader struct {
	*store
	off int64
}

func (l *Log) Reader() io.Reader {
	l.mu.RLock()
	defer l.mu.RUnlock()

	readers := make([]io.Reader, len(l.segments)) 
	for i, segment := range l.segments {
		readers[i] = &originReader{segment.store, 0}
	}

	return io.MultiReader(readers...)
}

func (o *originReader) Read(p []byte) (int, error) { 
	n, err := o.ReadAt(p, o.off)
	o.off += int64(n) 
	return n, err
}
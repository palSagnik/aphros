package log

import (
	"fmt"
	"os"
	"path"

	"google.golang.org/protobuf/proto"
	api "github.com/palSagnik/proglog/api/v1"
)

// Segment wraps index and store to coordinate actions across the two.
// For example, when the log appends a record to the active segment,
// the segment needs to write the data to its store and add a new entry in the index.
// Similarly for reads, the segment needs to look up the entry from the index and
// then fetch the data from the store.

type segment struct {
	store                  *store
	index                  *index
	baseOffset, nextOffset uint64
	config                 Config
}

func newSegment(dir string, baseOffset uint64, c Config) (*segment, error) {
	s := &segment{
		baseOffset: baseOffset,
		config: c,
	}

	var err error
	
	// Creating the storeFile
	// each store name has baseOffset[.]store
	storeFile, err := os.OpenFile(
		path.Join(dir, fmt.Sprintf("%d%s", baseOffset, ".store")),
		os.O_RDWR|os.O_CREATE|os.O_APPEND,
		0644,
	)
	if err != nil {
		return nil, err
	}
	if s.store, err = newStore(storeFile); err != nil {
		return nil, err
	}

	// Creating the indexFile
	// each index name has baseOffset[.]index
	// Segment: {Store, Index}
	indexFile, err := os.OpenFile(
		path.Join(dir, fmt.Sprintf("%d%s", baseOffset, ".index")),
		os.O_RDWR|os.O_CREATE,
		0644,
	)
	if err != nil {
		return nil, err
	}
	if s.index, err = newIndex(indexFile, c); err != nil {
		return nil, err
	}

	if off, _, err := s.index.Read(-1); err != nil {
		s.nextOffset = baseOffset 
	} else {
		s.nextOffset = baseOffset + uint64(off) + 1 
	}

	return s, nil
}

func (s *segment) Append(record *api.Record) (offset uint64, err error) {
	curr := s.nextOffset
	record.Offset = curr

	p, err := proto.Marshal(record)
	if err != nil {
		return 0, err
	}

	_, pos, err := s.store.Append(p)
	if err != nil {
		return 0, err
	}

	if err := s.index.Write(
		// index offsets are relative to base offsets
		uint32(s.nextOffset - uint64(s.baseOffset)),
		pos,
	); err != nil {
		return 0, err
	}
	s.nextOffset++
	return curr, nil
}

func (s *segment) Read(off uint64) (*api.Record, error) {
	_, pos, err := s.index.Read(int64(off - s.baseOffset))
	if err != nil {
		return nil, err
	}

	p, err := s.store.Read(pos)
	if err != nil {
		return nil, err
	}

	record := &api.Record{}
	err = proto.Unmarshal(p, record)
	return record, err
}

// checking when to create the next segment
// small number of long logs -> store limit
// large number of small logs -> index limit
func (s *segment) IsMaxed() bool {
	return s.store.size >= s.config.Segment.MaxStoreBytes ||
			s.index.size >= s.config.Segment.MaxIndexBytes
}

func (s *segment) Remove() error {
	if err := s.Close(); err != nil {
		return err
	}
	if err := os.Remove(s.index.Name()); err != nil {
		return err
	}
	if err := os.Remove(s.store.Name()); err != nil {
		return err
	}
	
	return nil
}

func (s *segment) Close() error {
	if err := s.index.Close(); err != nil {
		return err
	}
	
	if err := s.store.Close(); err != nil {
		return err
	}

	return nil
}

func nearestMultiple(j, k uint64) uint64 {
	if j > 0 {
		return (j / k) * k
	}
	return ((j - k + 1) / k) * k
}

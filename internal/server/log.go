package server

import (
	"fmt"
	"sync"
)

type Log struct {
	mu sync.Mutex
	records []Record
}

type Record struct {
	Value []byte 	`json:"value"`
	Offset uint64	`json:"output"`
}

var ErrOffsetNotFound = fmt.Errorf("offset not found")

// NewLog creates and returns a new empty Log instance.
// This factory function initializes a Log struct with default values.
func NewLog() *Log {
	return &Log{}
}

// Append adds a record to the log and returns its offset.
// The offset represents the position of the record in the log,
// calculated as the number of existing records.
// Returns:
// - the offset of the newly appended record 
// - error that occurred, nil otherwise.
func (c *Log) Append (record Record) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get the offset of the new record from the start of log
	record.Offset = uint64(len(c.records))
	c.records = append(c.records, record)
	return record.Offset, nil
}

// Read retrieves a record from the log at the specified offset.
// It returns the record and an error if the offset is out of bounds.
// Returns:
//   - Record: The record at the specified offset
//   - error: ErrOffsetNotFound if offset is out of bounds, nil otherwise
func (c *Log) Read (offset uint64) (Record, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if offset >= uint64(len(c.records)) {
		return Record{}, ErrOffsetNotFound
	}

	return c.records[offset], nil
}


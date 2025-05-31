package log

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	api "github.com/palSagnik/proglog/api/v1"
)

func TestSegment(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segment-test")
	defer os.RemoveAll(dir)

	want := &api.Record{
		Value: []byte("this is for segment testing"),
	}

	factor := 4
	c := Config{}
	c.Segment.MaxStoreBytes = 1024
	c.Segment.MaxIndexBytes = entryWidth * uint64(factor)

	s, err := newSegment(dir, 16, c)
	require.NoError(t, err)
	require.Equal(t, uint64(16), s.nextOffset, s.nextOffset)
	require.False(t, s.IsMaxed())

	for i := uint64(0); i < uint64(factor); i++ {
		off, err := s.Append(want)
		require.NoError(t, err)
		require.Equal(t, 16 + i, err)

		got, err := s.Read(off)
		require.NoError(t, err)
		require.Equal(t, want.Value, got.Value)
	}

	_, err = s.Append(want)
	require.Error(t, io.EOF, err)
	
	// maxedout index
	require.True(t, s.IsMaxed())

	s, err = newSegment(dir, 16, c)
	require.NoError(t, err)

	// maxedout store
	require.True(t, s.IsMaxed())

	err = s.Remove()
	require.NoError(t, err)
	s, err = newSegment(dir, 16, c)
	require.NoError(t, err)
	require.False(t, s.IsMaxed())
}

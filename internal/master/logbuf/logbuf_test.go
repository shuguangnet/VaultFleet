package logbuf

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRingBuffer_WriteAndRead(t *testing.T) {
	buf := New(64)
	n, err := buf.Write([]byte("hello world\n"))
	require.NoError(t, err)
	assert.Equal(t, 12, n)

	data := buf.Bytes()
	assert.Equal(t, "hello world\n", string(data))
}

func TestRingBuffer_Overflow(t *testing.T) {
	buf := New(16)
	buf.Write([]byte("AAAAAAAABBBBBBBB"))
	buf.Write([]byte("CCCC"))

	data := string(buf.Bytes())
	assert.Equal(t, 16, len(data))
	assert.Equal(t, "AAAABBBBBBBBCCCC", data)
}

func TestRingBuffer_ExactCapacity(t *testing.T) {
	buf := New(8)
	buf.Write([]byte("12345678"))
	assert.Equal(t, "12345678", string(buf.Bytes()))
}

func TestRingBuffer_Empty(t *testing.T) {
	buf := New(64)
	assert.Equal(t, 0, len(buf.Bytes()))
}

func TestRingBuffer_MultipleSmallWrites(t *testing.T) {
	buf := New(10)
	buf.Write([]byte("aa"))
	buf.Write([]byte("bb"))
	buf.Write([]byte("cc"))
	assert.Equal(t, "aabbcc", string(buf.Bytes()))
}

func TestRingBuffer_ConcurrentSafety(t *testing.T) {
	buf := New(1024)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			buf.Write([]byte("write\n"))
		}
		close(done)
	}()
	for i := 0; i < 100; i++ {
		_ = buf.Bytes()
	}
	<-done
}

func TestRingBuffer_MultiWriterCapturesBeforePassthroughError(t *testing.T) {
	buf := New(64)
	writer := buf.MultiWriter(failingWriter{})

	_, err := writer.Write([]byte("captured log line\n"))

	require.Error(t, err)
	assert.Equal(t, "captured log line\n", string(buf.Bytes()))
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("passthrough failed")
}

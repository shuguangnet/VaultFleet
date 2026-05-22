package logbuf

import (
	"io"
	"os"
	"sync"
)

type RingBuffer struct {
	mu       sync.Mutex
	buf      []byte
	pos      int
	full     bool
	capacity int
}

func New(capacity int) *RingBuffer {
	return &RingBuffer{
		buf:      make([]byte, capacity),
		capacity: capacity,
	}
}

func (r *RingBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(p)
	if n >= r.capacity {
		copy(r.buf, p[n-r.capacity:])
		r.pos = 0
		r.full = true
		return n, nil
	}

	spaceToEnd := r.capacity - r.pos
	if n <= spaceToEnd {
		copy(r.buf[r.pos:], p)
	} else {
		copy(r.buf[r.pos:], p[:spaceToEnd])
		copy(r.buf, p[spaceToEnd:])
	}

	if !r.full && r.pos+n >= r.capacity {
		r.full = true
	}
	r.pos = (r.pos + n) % r.capacity
	return n, nil
}

func (r *RingBuffer) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.full {
		out := make([]byte, r.pos)
		copy(out, r.buf[:r.pos])
		return out
	}

	out := make([]byte, r.capacity)
	copy(out, r.buf[r.pos:])
	copy(out[r.capacity-r.pos:], r.buf[:r.pos])
	return out
}

func (r *RingBuffer) MultiWriter(dst ...io.Writer) io.Writer {
	passthrough := io.Writer(os.Stderr)
	if len(dst) > 0 && dst[0] != nil {
		passthrough = dst[0]
	}
	return io.MultiWriter(r, passthrough)
}

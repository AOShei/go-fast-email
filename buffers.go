package gofastemail

import (
	"bufio"
	"io"
	"sync"

	"github.com/AOShei/go-fast-core/rfc822"
)

// Memory used while one message is read, and never after, is reused from
// one message to the next: the message's bytes, the scratch of its header,
// and the reader that finds the messages of a mailbox. Go empties the
// pools as it collects garbage, so what they hold is bounded by what the
// workers use at once.
//
// Nothing in a Document may refer to this memory. Text is made into
// strings, which copy; the bytes of an image are copied (see image); an
// embedded document's extractor is handed a reader, which copies.

// Message buffers come in classes of powers of two from 256 bytes to
// 8 MiB; a larger message has a buffer of its own. The classes start
// small so that a buffer the pool cannot give (it is emptied as garbage
// is collected) costs about what is read: a mailbox of a great many tiny
// messages looks at 256 bytes of each one's header.
const (
	minBufferClass = 8  // 256 bytes
	maxBufferClass = 23 // 8 MiB
)

var bufferPools [maxBufferClass + 1]sync.Pool

// poisonReleased, set by tests, overwrites memory as it is given back, so
// that a document that still refers to it shows it at once.
var poisonReleased bool

func poison(b []byte) {
	if poisonReleased {
		for i := range b {
			b[i] = 0xFF
		}
	}
}

func bufferClass(size int64) int {
	c := minBufferClass
	for c <= maxBufferClass && int64(1)<<c < size {
		c++
	}
	return c
}

// getBuffer returns a buffer of size bytes, from a pool when there is one
// of its class. What it holds is not cleared.
func getBuffer(size int64) []byte {
	c := bufferClass(size)
	if c > maxBufferClass {
		return make([]byte, size)
	}
	if p, ok := bufferPools[c].Get().(*[]byte); ok {
		return (*p)[:size]
	}
	return make([]byte, size, int64(1)<<c)
}

// putBuffer gives back a buffer of getBuffer: nothing may use it after.
func putBuffer(b []byte) {
	c := bufferClass(int64(cap(b)))
	if c > maxBufferClass || int64(cap(b)) != int64(1)<<c {
		return
	}
	b = b[:cap(b)]
	poison(b)
	bufferPools[c].Put(&b)
}

var scratchPool = sync.Pool{New: func() any { return new(rfc822.Scratch) }}

// getScratch returns a scratch for one header, to be given back with
// putScratch once nothing refers to the header.
func getScratch() *rfc822.Scratch { return scratchPool.Get().(*rfc822.Scratch) }

func putScratch(s *rfc822.Scratch) {
	if poisonReleased {
		s.Poison()
	}
	if s.Small() {
		scratchPool.Put(s)
	}
}

// Readers of a mailbox have buffers of 4 KiB to 64 KiB, in powers of two.
const (
	minReaderClass = 12 // 4 KiB
	maxReaderClass = 16 // 64 KiB
)

var readerPools [maxReaderClass + 1]sync.Pool

// getReader returns a reader of r with a buffer of at least size bytes, at
// most 64 KiB.
func getReader(r io.Reader, size int64) *bufio.Reader {
	c := minReaderClass
	for c < maxReaderClass && int64(1)<<c < size {
		c++
	}
	if br, ok := readerPools[c].Get().(*bufio.Reader); ok {
		br.Reset(r)
		return br
	}
	return bufio.NewReaderSize(r, 1<<c)
}

// putReader gives back a reader of getReader: nothing may use it after.
func putReader(br *bufio.Reader) {
	br.Reset(nil)
	for c := minReaderClass; c <= maxReaderClass; c++ {
		if br.Size() == 1<<c {
			readerPools[c].Put(br)
			return
		}
	}
}

package rtfcp

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestPrebuf(t *testing.T) {
	if len(prebuf) != 207 {
		t.Fatalf("the dictionary's initial text is %d bytes, the specification's 207", len(prebuf))
	}
}

// TestSpecExample decompresses the example of [MS-OXRTFCP] 4.1, as
// remembered from the specification (to be checked against it).
func TestSpecExample(t *testing.T) {
	data := []byte{
		0x2d, 0x00, 0x00, 0x00, 0x2b, 0x00, 0x00, 0x00, 0x4c, 0x5a, 0x46, 0x75, 0xf1, 0xc5, 0xc7, 0xa7,
		0x03, 0x00, 0x0a, 0x00, 0x72, 0x63, 0x70, 0x67, 0x31, 0x32, 0x35, 0x42, 0x32, 0x0a, 0xf3, 0x20,
		0x68, 0x65, 0x6c, 0x09, 0x00, 0x20, 0x62, 0x77, 0x05, 0xb0, 0x6c, 0x64, 0x7d, 0x0a, 0x80, 0x0f, 0xa0,
	}
	got, err := Decompress(data, 1<<20)
	if want := "{\\rtf1\\ansi\\ansicpg1252\\pard hello world}\r\n"; err != nil || string(got) != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

// literal builds compressed data of literal bytes only, ended by a
// reference to the write position.
func literal(text string) []byte {
	var body []byte
	write := len(prebuf)
	for i := 0; i < len(text); i += 8 {
		end := min(i+8, len(text))
		body = append(body, 0)
		body = append(body, text[i:end]...)
		write += end - i
	}
	if len(text)%8 == 0 {
		body = append(body, 0x01)
	} else {
		body[len(body)-(len(text)%8)-1] |= 1 << (len(text) % 8)
	}
	ref := (write & 4095) << 4
	body = append(body, byte(ref>>8), byte(ref))
	head := make([]byte, 16)
	binary.LittleEndian.PutUint32(head[0:], uint32(len(body)+12))
	binary.LittleEndian.PutUint32(head[4:], uint32(len(text)))
	binary.LittleEndian.PutUint32(head[8:], typeCompressed)
	return append(head, body...)
}

func TestLiteralAndLimit(t *testing.T) {
	text := "{\\rtf1 plain text in a literal stream}"
	got, err := Decompress(literal(text), 1<<20)
	if err != nil || string(got) != text {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := Decompress(literal(text), 5); !errors.Is(err, ErrLimit) || len(got) != 5 {
		t.Fatalf("limit: %q %v", got, err)
	}
	if _, err := Decompress([]byte("short"), 10); !errors.Is(err, ErrFormat) {
		t.Fatalf("short: %v", err)
	}
}

// FuzzDecompress checks that no input makes the decompressor panic or
// exceed its limit.
func FuzzDecompress(f *testing.F) {
	f.Add(literal("{\\rtf1 x}"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, _ := Decompress(data, 1<<16)
		if len(got) > 1<<16 {
			t.Fatalf("%d bytes", len(got))
		}
	})
}

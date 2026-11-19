// Package rtfcp decompresses the compressed RTF that Outlook stores in a
// message (PR_RTF_COMPRESSED), by the algorithm of [MS-OXRTFCP].
//
// The stream has a header of 16 bytes: the compressed size (counting the
// header from its fifth byte), the raw size, the type ("LZFu" compressed or
// "MELA" stored as it is) and a CRC. Compressed data is groups of a control
// byte and eight items, each a literal byte (bit clear) or a reference of
// two bytes (bit set) to a run of 2 to 17 bytes in a dictionary of 4096
// bytes, which begins with a fixed text of RTF. A reference to the
// dictionary's write position ends the data.
package rtfcp

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Errors.
var (
	// ErrFormat: the data is not compressed RTF, or is cut short before
	// its header ends.
	ErrFormat = errors.New("rtfcp: not compressed RTF")
	// ErrLimit: the data decompresses to more than the limit allows.
	ErrLimit = errors.New("rtfcp: limit reached")
)

const (
	typeCompressed   = 0x75465A4C // "LZFu"
	typeUncompressed = 0x414C454D // "MELA"
)

// prebuf is the text the dictionary begins with ([MS-OXRTFCP] 3.1.3.1).
const prebuf = `{\rtf1\ansi\mac\deff0\deftab720{\fonttbl;}{\f0\fnil \froman \fswiss \fmodern \fscript \fdecor MS Sans SerifSymbolArialTimes New RomanCourier{\colortbl\red0\green0\blue0` +
	"\r\n" + `\par \pard\plain\f0\fs20\b\i\u\tab\tx`

// Decompress returns the RTF the data holds, at most limit bytes of it. It
// stops at the end the data marks, at the end of the data, or at the raw
// size the header states, whichever comes first; a stream that is cut
// short gives what it holds. Data that would decompress to more than
// limit is ErrLimit, with what came out before it.
func Decompress(data []byte, limit int64) ([]byte, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("%w: %d bytes", ErrFormat, len(data))
	}
	le := binary.LittleEndian
	compSize := int64(le.Uint32(data[0:]))
	rawSize := int64(le.Uint32(data[4:]))
	kind := le.Uint32(data[8:])
	body := data[16:]
	// The compressed size counts from the raw-size field.
	if n := compSize - 12; n >= 0 && n < int64(len(body)) {
		body = body[:n]
	}
	switch kind {
	case typeUncompressed:
		if int64(len(body)) > rawSize {
			body = body[:rawSize]
		}
		if int64(len(body)) > limit {
			return append([]byte(nil), body[:limit]...), ErrLimit
		}
		return append([]byte(nil), body...), nil
	case typeCompressed:
	default:
		return nil, fmt.Errorf("%w: type %#x", ErrFormat, kind)
	}
	var dict [4096]byte
	copy(dict[:], prebuf)
	write := len(prebuf)
	want := min(rawSize, limit+1)
	out := make([]byte, 0, min(want, int64(len(body))*8+16))
	for i := 0; i < len(body); {
		control := body[i]
		i++
		for bit := 0; bit < 8 && i < len(body); bit++ {
			if control&(1<<bit) == 0 {
				c := body[i]
				i++
				dict[write] = c
				write = (write + 1) & 4095
				out = append(out, c)
			} else {
				if i+1 >= len(body) {
					return out, nil
				}
				ref := int(body[i])<<8 | int(body[i+1])
				i += 2
				offset, length := ref>>4, ref&15+2
				if offset == write {
					return trim(out, rawSize, limit)
				}
				for k := 0; k < length; k++ {
					c := dict[(offset+k)&4095]
					dict[write] = c
					write = (write + 1) & 4095
					out = append(out, c)
				}
			}
			if int64(len(out)) >= want {
				return trim(out, rawSize, limit)
			}
		}
	}
	return trim(out, rawSize, limit)
}

func trim(out []byte, rawSize, limit int64) ([]byte, error) {
	if int64(len(out)) > rawSize {
		out = out[:rawSize]
	}
	if int64(len(out)) > limit {
		return out[:limit], ErrLimit
	}
	return out, nil
}

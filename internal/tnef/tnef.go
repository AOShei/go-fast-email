// Package tnef reads the Transport Neutral Encapsulation Format
// ([MS-OXTNEF]) that Outlook and Exchange attach to Internet mail as
// winmail.dat (application/ms-tnef): the message's properties, among them
// its bodies, and its attachments.
//
// A TNEF stream is a signature, a key, and a sequence of attributes, each a
// level (message or attachment), an id, a length, the data, and a
// checksum. Two attributes hold MAPI properties in a binary encoding
// (attMsgProps for the message, attAttachment for an attachment); the
// others are older forms of the same information. The package works on
// bytes in memory, trusts nothing it reads, and never fails once the
// signature is found: a damaged stream is read as far as it goes.
package tnef

import (
	"encoding/binary"
	"errors"
)

// Signature is the first four bytes of a TNEF stream, little-endian.
const Signature = 0x223E9F78

// ErrNotTNEF: the data does not begin with the TNEF signature.
var ErrNotTNEF = errors.New("tnef: no TNEF signature")

// Attribute ids (the low 16 bits of an attribute's id; [MS-OXTNEF] 2.1.3.3).
const (
	attSubject        = 0x8004
	attBody           = 0x800C
	attAttachData     = 0x800F
	attAttachTitle    = 0x8010
	attAttachRendData = 0x9002
	attMsgProps       = 0x9003
	attAttachment     = 0x9005
	attOemCodepage    = 0x9007
)

// Property types of the MAPI property encoding.
const (
	ptShort    = 0x0002
	ptLong     = 0x0003
	ptFloat    = 0x0004
	ptDouble   = 0x0005
	ptCurrency = 0x0006
	ptAppTime  = 0x0007
	ptError    = 0x000A
	ptBoolean  = 0x000B
	ptObject   = 0x000D
	ptI8       = 0x0014
	ptString8  = 0x001E
	ptUnicode  = 0x001F
	ptSysTime  = 0x0040
	ptCLSID    = 0x0048
	ptBinary   = 0x0102
	mvFlag     = 0x1000
)

// Prop is one MAPI property: its id and type and, for a single value, its
// bytes as stored (a fixed-size value in its little-endian bytes, padding
// removed; a variable-size one as it is). Multi-valued properties and named
// properties (ids from 0x8000) are skipped.
type Prop struct {
	ID, Type uint16
	Value    []byte
}

// Attachment is one attachment: its title (8-bit file name), its data and
// its properties.
type Attachment struct {
	Title string
	Data  []byte
	Props []Prop
}

// Message is what a TNEF stream holds.
type Message struct {
	Codepage    uint32 // attOemCodepage, 0 if absent
	Subject     []byte // attSubject, 8-bit, NUL removed
	Body        []byte // attBody, 8-bit in Codepage
	Props       []Prop // attMsgProps
	Attachments []Attachment
	// Damaged: the stream ends inside an attribute, a checksum does not
	// match, or a property list cannot be read to its end.
	Damaged bool
	// More: there were more attachments or properties than the limit.
	More bool
}

// MaxItems bounds the attachments, and the properties of one list, read.
const MaxItems = 10_000

// Parse reads a TNEF stream. The values it returns are slices of data.
func Parse(data []byte) (*Message, error) {
	le := binary.LittleEndian
	if len(data) < 6 || le.Uint32(data) != Signature {
		return nil, ErrNotTNEF
	}
	m := &Message{}
	var cur *Attachment
	pos := 6 // signature and legacy key
	for pos < len(data) {
		if len(data)-pos < 9 {
			m.Damaged = true
			break
		}
		level := data[pos]
		id := le.Uint32(data[pos+1:])
		n := uint64(le.Uint32(data[pos+5:]))
		pos += 9
		if n > uint64(len(data)-pos) {
			m.Damaged = true
			n = uint64(len(data) - pos)
		}
		value := data[pos : pos+int(n)]
		pos += int(n)
		if len(data)-pos >= 2 {
			if le.Uint16(data[pos:]) != checksum(value) {
				m.Damaged = true
			}
			pos += 2
		} else {
			m.Damaged = true
		}
		switch level {
		case 1: // the message
			switch id & 0xFFFF {
			case attSubject:
				m.Subject = trimNUL(value)
			case attBody:
				m.Body = value
			case attOemCodepage:
				if len(value) >= 4 {
					m.Codepage = le.Uint32(value)
				}
			case attMsgProps:
				m.Props = m.props(value)
			}
		case 2: // an attachment
			if id&0xFFFF == attAttachRendData {
				if len(m.Attachments) >= MaxItems {
					m.More = true
					return m, nil
				}
				m.Attachments = append(m.Attachments, Attachment{})
				cur = &m.Attachments[len(m.Attachments)-1]
				continue
			}
			if cur == nil {
				continue
			}
			switch id & 0xFFFF {
			case attAttachTitle:
				cur.Title = string(trimNUL(value))
			case attAttachData:
				cur.Data = value
			case attAttachment:
				cur.Props = m.props(value)
			}
		}
	}
	return m, nil
}

func checksum(b []byte) uint16 {
	var s uint16
	for _, c := range b {
		s += uint16(c)
	}
	return s
}

func trimNUL(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return b
}

// props reads an encoded property list ([MS-OXTNEF] 2.1.3.4): a count,
// then each property's type, id, the name of a named property, and its
// value or values, each padded to four bytes.
func (m *Message) props(b []byte) []Prop {
	le := binary.LittleEndian
	if len(b) < 4 {
		m.Damaged = true
		return nil
	}
	count := le.Uint32(b)
	pos := 4
	var out []Prop
	need := func(n int) bool {
		if n < 0 || len(b)-pos < n {
			m.Damaged = true
			return false
		}
		return true
	}
	for i := uint32(0); i < count; i++ {
		if len(out) >= MaxItems {
			m.More = true
			return out
		}
		if !need(4) {
			return out
		}
		typ, id := le.Uint16(b[pos:]), le.Uint16(b[pos+2:])
		pos += 4
		if id >= 0x8000 { // named: a GUID, a kind, and a number or a name
			if !need(20) {
				return out
			}
			kind := le.Uint32(b[pos+16:])
			pos += 20
			if !need(4) {
				return out
			}
			if kind == 0 {
				pos += 4
			} else {
				n := int(le.Uint32(b[pos:]))
				pos += 4
				if !need(padded(n)) {
					return out
				}
				pos += padded(n)
			}
		}
		values := 1
		multi := typ&mvFlag != 0
		base := typ &^ mvFlag
		variable := base == ptString8 || base == ptUnicode || base == ptBinary || base == ptObject
		if multi || variable {
			// A count of values precedes multi-valued properties, and
			// variable-size ones even when single-valued.
			if !need(4) {
				return out
			}
			values = int(le.Uint32(b[pos:]))
			pos += 4
			if values < 0 || values > len(b) {
				m.Damaged = true
				return out
			}
		}
		var first []byte
		for v := 0; v < values; v++ {
			var size int
			if variable {
				if !need(4) {
					return out
				}
				size = int(le.Uint32(b[pos:]))
				pos += 4
				if !need(padded(size)) {
					return out
				}
				if v == 0 {
					first = b[pos : pos+size]
				}
				pos += padded(size)
				continue
			}
			switch base {
			case ptShort, ptLong, ptFloat, ptError, ptBoolean:
				size = 4
			case ptDouble, ptCurrency, ptAppTime, ptI8, ptSysTime:
				size = 8
			case ptCLSID:
				size = 16
			default:
				// A type whose size is unknown: the rest cannot be read.
				m.Damaged = true
				return out
			}
			if !need(size) {
				return out
			}
			if v == 0 {
				first = b[pos : pos+size]
			}
			pos += size
		}
		if !multi && id < 0x8000 && first != nil {
			out = append(out, Prop{ID: id, Type: base, Value: first})
		}
	}
	return out
}

func padded(n int) int {
	if n < 0 || n > 1<<30 {
		return -1
	}
	return (n + 3) &^ 3
}

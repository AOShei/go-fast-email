package msgtest

import (
	"encoding/binary"
	"fmt"
	"time"
	"unicode/utf16"
)

// Property types (MS-OXCDATA 2.11.1).
const (
	TypeInt32   = 0x0003
	TypeBool    = 0x000B
	TypeObject  = 0x000D
	TypeString8 = 0x001E
	TypeUnicode = 0x001F
	TypeTime    = 0x0040
	TypeBinary  = 0x0102
)

// Prop is one property. Value is a string (written as PT_UNICODE unless
// Type says PT_STRING8), []byte, int32, bool or time.Time.
type Prop struct {
	ID    uint16
	Type  uint16 // zero: from the value
	Value any
}

// Msg is an Outlook message to write.
type Msg struct {
	Props       []Prop
	Recipients  [][]Prop
	Attachments []Attachment
}

// Attachment is one attachment: its properties, and for an embedded
// message (method 5) the message.
type Attachment struct {
	Props    []Prop
	Embedded *Msg
}

// Build returns the message as a .msg file.
func Build(m Msg) []byte {
	streams := map[string][]byte{}
	m.streams(streams, "", 32)
	return Compound(streams)
}

func (m Msg) streams(out map[string][]byte, prefix string, headerSize int) {
	head := make([]byte, headerSize)
	if headerSize >= 24 {
		le := binary.LittleEndian
		le.PutUint32(head[8:], uint32(len(m.Recipients)))
		le.PutUint32(head[12:], uint32(len(m.Attachments)))
		le.PutUint32(head[16:], uint32(len(m.Recipients)))
		le.PutUint32(head[20:], uint32(len(m.Attachments)))
	}
	writeProps(out, prefix, head, m.Props)
	for i, r := range m.Recipients {
		p := fmt.Sprintf("%s__recip_version1.0_#%08X/", prefix, i)
		writeProps(out, p, make([]byte, 8), r)
	}
	for i, a := range m.Attachments {
		p := fmt.Sprintf("%s__attach_version1.0_#%08X/", prefix, i)
		props := a.Props
		if a.Embedded != nil {
			props = append(props, Prop{ID: 0x3705, Value: int32(5)})
			a.Embedded.streams(out, p+"__substg1.0_3701000D/", 24)
		}
		writeProps(out, p, make([]byte, 8), props)
	}
}

func writeProps(out map[string][]byte, prefix string, head []byte, props []Prop) {
	le := binary.LittleEndian
	stream := append([]byte(nil), head...)
	for _, p := range props {
		entry := make([]byte, 16)
		typ := p.Type
		var data []byte
		variable := false
		switch v := p.Value.(type) {
		case string:
			if typ == TypeString8 {
				data = append([]byte(v), 0)
			} else {
				typ = TypeUnicode
				for _, u := range utf16.Encode([]rune(v)) {
					data = le.AppendUint16(data, u)
				}
				data = append(data, 0, 0)
			}
			variable = true
		case []byte:
			if typ == 0 {
				typ = TypeBinary
			}
			data, variable = v, true
		case int32:
			typ = TypeInt32
			le.PutUint32(entry[8:], uint32(v))
		case bool:
			typ = TypeBool
			if v {
				entry[8] = 1
			}
		case time.Time:
			typ = TypeTime
			// Hundreds of nanoseconds since 1601 (a time.Duration cannot
			// hold four centuries).
			ft := uint64(v.Unix()*10_000_000 + int64(v.Nanosecond()/100) + 116444736000000000)
			le.PutUint64(entry[8:], ft)
		}
		le.PutUint32(entry[0:], uint32(p.ID)<<16|uint32(typ))
		le.PutUint32(entry[4:], 6)
		if variable {
			le.PutUint32(entry[8:], uint32(len(data)))
			out[fmt.Sprintf("%s__substg1.0_%04X%04X", prefix, p.ID, typ)] = data
		}
		stream = append(stream, entry...)
	}
	out[prefix+"__properties_version1.0"] = stream
}

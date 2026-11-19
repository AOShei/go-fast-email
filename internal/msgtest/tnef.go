package msgtest

import (
	"encoding/binary"
	"unicode/utf16"
)

// TNEF returns a TNEF stream ([MS-OXTNEF]) of the given attributes.
func TNEF(attrs ...[]byte) []byte {
	out := binary.LittleEndian.AppendUint32(nil, 0x223E9F78)
	out = binary.LittleEndian.AppendUint16(out, 0x0001)
	for _, a := range attrs {
		out = append(out, a...)
	}
	return out
}

// TNEFAttr returns one attribute: level 1 for the message, 2 for an
// attachment; id is the full 32-bit id (type in the high word).
func TNEFAttr(level byte, id uint32, data []byte) []byte {
	out := []byte{level}
	out = binary.LittleEndian.AppendUint32(out, id)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(data)))
	out = append(out, data...)
	var sum uint16
	for _, c := range data {
		sum += uint16(c)
	}
	return binary.LittleEndian.AppendUint16(out, sum)
}

// TNEFProp is one MAPI property of a TNEF property list. Value is a
// string, []byte (binary, or with Type 0x000D an object), int32, bool or
// uint64 (a time).
type TNEFProp struct {
	ID    uint16
	Type  uint16 // 0 to take it from Value
	Value any
}

// TNEFProps encodes a property list (attMsgProps, attAttachment).
func TNEFProps(props ...TNEFProp) []byte {
	le := binary.LittleEndian
	out := le.AppendUint32(nil, uint32(len(props)))
	pad := func() {
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	for _, p := range props {
		typ := p.Type
		var fixed, variable []byte
		switch v := p.Value.(type) {
		case string:
			if typ == 0 {
				typ = 0x001F
			}
			if typ == 0x001E {
				variable = append([]byte(v), 0)
			} else {
				variable = utf16le(v)
			}
		case []byte:
			if typ == 0 {
				typ = 0x0102
			}
			variable = v
		case int32:
			typ, fixed = 0x0003, le.AppendUint32(nil, uint32(v))
		case bool:
			typ, fixed = 0x000B, []byte{0, 0, 0, 0}
			if v {
				fixed[0] = 1
			}
		case uint64:
			typ, fixed = 0x0040, le.AppendUint64(nil, v)
		}
		if variable == nil && fixed == nil {
			panic("msgtest: TNEFProp value of an unknown type")
		}
		out = le.AppendUint16(out, typ)
		out = le.AppendUint16(out, p.ID)
		if variable != nil {
			out = le.AppendUint32(out, 1)
			out = le.AppendUint32(out, uint32(len(variable)))
			out = append(out, variable...)
			pad()
		} else {
			out = append(out, fixed...)
		}
	}
	return out
}

func utf16le(s string) []byte {
	var out []byte
	for _, u := range utf16.Encode([]rune(s)) {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return append(out, 0, 0)
}

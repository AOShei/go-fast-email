package tnef_test

import (
	"testing"

	"github.com/AOShei/go-fast-email/internal/msgtest"
	"github.com/AOShei/go-fast-email/internal/tnef"
)

func sample() []byte {
	return msgtest.TNEF(
		msgtest.TNEFAttr(1, 0x00089006, []byte{0, 0, 1, 0}), // attTnefVersion
		msgtest.TNEFAttr(1, 0x00069007, []byte{0xE4, 0x04, 0, 0, 0, 0, 0, 0}),
		msgtest.TNEFAttr(1, 0x00018004, []byte("Old subject\x00")),
		msgtest.TNEFAttr(1, 0x00069003, msgtest.TNEFProps(
			msgtest.TNEFProp{ID: 0x0037, Value: "Subject"},
			msgtest.TNEFProp{ID: 0x0E07, Value: int32(1)},
			msgtest.TNEFProp{ID: 0x1009, Value: []byte{1, 2, 3}},
		)),
		msgtest.TNEFAttr(2, 0x00069002, make([]byte, 14)),
		msgtest.TNEFAttr(2, 0x00018010, []byte("A.TXT\x00")),
		msgtest.TNEFAttr(2, 0x0006800F, []byte("attached")),
		msgtest.TNEFAttr(2, 0x00069005, msgtest.TNEFProps(
			msgtest.TNEFProp{ID: 0x3707, Value: "a long name.txt"},
		)),
	)
}

func TestParse(t *testing.T) {
	m, err := tnef.Parse(sample())
	if err != nil {
		t.Fatal(err)
	}
	if m.Damaged || m.Codepage != 1252 || string(m.Subject) != "Old subject" {
		t.Fatalf("%+v", m)
	}
	if len(m.Props) != 3 || m.Props[0].ID != 0x0037 || m.Props[1].Type != 0x0003 || m.Props[1].Value[0] != 1 || string(m.Props[2].Value) != "\x01\x02\x03" {
		t.Fatalf("props %+v", m.Props)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].Title != "A.TXT" || string(m.Attachments[0].Data) != "attached" || len(m.Attachments[0].Props) != 1 {
		t.Fatalf("attachments %+v", m.Attachments)
	}
	if _, err := tnef.Parse([]byte("not tnef")); err == nil {
		t.Error("no error for data without the signature")
	}
	// Cut short anywhere: damaged, never a panic.
	data := sample()
	for n := 6; n < len(data); n++ {
		if _, err := tnef.Parse(data[:n]); err != nil {
			t.Fatalf("cut at %d: %v", n, err)
		}
	}
	data[len(data)-1] ^= 0xFF // the last checksum
	if m, _ := tnef.Parse(data); !m.Damaged {
		t.Error("a wrong checksum is not reported")
	}
}

func FuzzParse(f *testing.F) {
	f.Add(sample())
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := tnef.Parse(data)
		if err != nil {
			return
		}
		for _, a := range m.Attachments {
			if len(a.Data) > len(data) {
				t.Fatal("attachment longer than the input")
			}
		}
	})
}

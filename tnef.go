package gofastemail

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email/internal/charset"
	"github.com/AOShei/go-fast-email/internal/clean"
	"github.com/AOShei/go-fast-email/internal/tnef"
)

// isTNEF reports whether a part's media type is that of a TNEF stream
// (winmail.dat).
func isTNEF(mediatype string) bool {
	return mediatype == "application/ms-tnef" || mediatype == "application/vnd.ms-tnef"
}

// tnefChild hands a TNEF stream (winmail.dat) to ExtractChild, to be read
// by this library: Outlook puts the message's RTF body and its
// attachments there, and no other extractor reads it. It goes through
// ExtractChild so that the depth limit and the budgets apply.
func (r *reader) tnefChild(m *message, name, declared string, data []byte, listed bool) {
	if data == nil || m.cut || len(data) < 4 || binary.LittleEndian.Uint32(data) != tnef.Signature {
		r.child(m, name, declared, data, listed)
		return
	}
	const mime = "application/ms-tnef"
	parent := r.opts.Child
	o := *r.opts
	o.Child = func(ctx context.Context, _, _ string, _ io.ReaderAt, _ int64, co *gofastcore.Options) (*gofastcore.Document, error) {
		co.Child = parent
		return r.e.tnefDocument(ctx, data, co)
	}
	d := o.ExtractChild(r.ctx, name, mime, bytes.NewReader(data), int64(len(data)))
	m.children = append(m.children, len(r.doc.Children))
	r.doc.Children = append(r.doc.Children, d)
	if listed {
		label := name
		if label == "" {
			label = "(" + mime + ")"
		}
		m.attached = append(m.attached, gofastcore.ExpandLigatures(label))
	}
}

// tnefDocument reads a TNEF stream into a document of one part, of kind
// message: the subject as heading, the plain body as blocks, the HTML and
// RTF bodies and the attachments as embedded documents.
func (e *Extractor) tnefDocument(ctx context.Context, data []byte, opts *gofastcore.Options) (*gofastcore.Document, error) {
	t, err := tnef.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("gofastemail: %v: %w", err, gofastcore.ErrMalformed)
	}
	ctx, text := gofastcore.WithTextBudget(ctx, opts.Limits)
	defer text.Close()
	ctx, dec := gofastcore.WithDecodeBudget(ctx, opts.Limits, DefaultMaxDecodedBytes)
	defer dec.Close()
	r := &reader{e: e, ctx: ctx, opts: opts, text: text, dec: dec}
	r.doc = gofastcore.New("tnef")
	r.doc.Source = gofastcore.Source{Format: "tnef", Name: opts.Name, MIME: "application/ms-tnef", Size: int64(len(data))}

	m := &message{inline: true}
	m.part.Kind, m.part.Number = gofastcore.PartMessage, 1
	if t.Damaged {
		m.warn(gofastcore.SeverityWarning, gofastcore.WarnInputRepaired, "the TNEF stream is damaged (cut short, a checksum that does not match, or properties that cannot be read); it is read as far as it goes")
	}
	if t.More {
		m.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, fmt.Sprintf("the TNEF stream has more than %d attachments or properties in a list; the rest are not read", tnef.MaxItems))
	}
	p := tnefProps(m, t.Props, e.maxMessage())
	if cp, ok := p.int32(pidMessageCodepage); ok {
		p.label = codepageLabel(cp)
	} else if t.Codepage != 0 {
		p.label = codepageLabel(int32(t.Codepage))
	}
	// The older attributes stand for the properties where these are absent.
	if _, ok := p.values[uint32(pidSubject)<<16|ptUnicode]; !ok && len(t.Subject) > 0 {
		p.values[uint32(pidSubject)<<16|ptString8] = t.Subject
	}
	if _, ok := p.values[uint32(pidBody)<<16|ptUnicode]; !ok && len(t.Body) > 0 {
		p.values[uint32(pidBody)<<16|ptString8] = t.Body
	}
	subject := clean.Line(p.str(pidSubject))
	r.head(m, subject, nil)
	md := &r.doc.Metadata
	md.Title = gofastcore.ExpandLigatures(subject)
	if v := clean.Line(p.str(pidMessageClass)); v != "" {
		md.Extra = map[string]string{"message-class": v}
	}
	r.outlookBody(m, p)
	for _, a := range t.Attachments {
		if r.ctx.Err() != nil {
			break
		}
		ap := tnefProps(m, a.Props, e.maxMessage())
		ap.label = p.label
		if _, ok := ap.values[uint32(pidAttachData)<<16|ptBinary]; !ok && a.Data != nil {
			ap.values[uint32(pidAttachData)<<16|ptBinary] = a.Data
		}
		title, _ := charset.Decode(p.label, []byte(a.Title))
		r.outlookAttachment(m, ap, title, func(file string, listed bool) {
			// An attached message: an object whose data, after the
			// 16 bytes of its interface id, is a TNEF stream.
			obj, _ := ap.read(pidAttachData, ptObject)
			var inner []byte
			if len(obj) > 16 {
				inner = r.budget(m, obj[16:])
			}
			r.tnefChild(m, file, "application/ms-tnef", inner, listed)
		})
	}
	r.tail(m, -1)
	r.doc.Parts = append(r.doc.Parts, m.part)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.finish(), nil
}

// tnefProps makes the property reader of an Outlook message from the
// properties of a TNEF stream.
func tnefProps(m *message, list []tnef.Prop, limit int64) *props {
	p := &props{values: map[uint32][]byte{}, fixed: map[uint32]uint64{}, limit: limit, m: m}
	for _, pr := range list {
		tag := uint32(pr.ID)<<16 | uint32(pr.Type)
		switch pr.Type {
		case ptInt32, ptBool, ptTime:
			var v [8]byte
			copy(v[:], pr.Value)
			p.fixed[tag] = binary.LittleEndian.Uint64(v[:])
		default:
			p.values[tag] = pr.Value
		}
	}
	return p
}

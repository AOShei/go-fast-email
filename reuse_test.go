package gofastemail_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
)

// reusedMessage is message i of the reuse tests: of a size that grows with
// i, with an encoded subject, a quoted-printable body with a quote and a
// signature, a picture shown in the message in its bytes as they are and
// another in base64, an attached file and an attached message.
func reusedMessage(i int) string {
	pic := fmt.Sprintf("\x89PNG-raw-%d-", i) + strings.Repeat(fmt.Sprintf("%c", 'a'+i%26), i*37%3000)
	pic64 := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("GIF89a-b64-%d", i)))
	body := strings.Repeat(fmt.Sprintf("Line %d of the body, with =C3=A9 in it.\n", i), 1+i%40)
	return fmt.Sprintf("From: Sender %d <s%d@example.org>\nTo: r%d@example.org\nSubject: =?utf-8?q?Subject_=C3=A9_%d?=\nDate: Mon, 3 Feb 2025 10:%02d:00 +0000\nX-Extra-%d: value %d\n  continued %d\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=\"b%d\"\n\n"+
		"--b%d\nContent-Type: text/plain; charset=utf-8\nContent-Transfer-Encoding: quoted-printable\n\n%s\n> quoted %d\n\n-- \nSignature %d\n"+
		"--b%d\nContent-Type: image/png\nContent-Disposition: inline; filename=\"p%d.png\"\nContent-Description: picture %d\nContent-Transfer-Encoding: binary\n\n%s\n"+
		"--b%d\nContent-Type: image/gif\nContent-Transfer-Encoding: base64\n\n%s\n"+
		"--b%d\nContent-Type: text/plain; name=\"a%d.txt\"\nContent-Disposition: attachment\n\nattached text %d\n"+
		"--b%d\nContent-Type: message/rfc822\n\nFrom: inner%d@example.org\nSubject: inner %d\n\ninner body %d\n"+
		"--b%d--\n",
		i, i, i, i, i%60, i, i, i, i,
		i, body, i, i,
		i, i, i, pic,
		i, pic64,
		i, i, i,
		i, i, i, i,
		i)
}

// reusedMailbox is mailbox i: three messages, one found by its
// Content-Length and one with a ">From " line to unquote.
func reusedMailbox(i int) string {
	counted := fmt.Sprintf("counted body %d\nFrom inside the counted body %d\n", i, i)
	return fmt.Sprintf("From a%d@example.org Mon Feb  3 10:00:00 2025\nSubject: first %d\n\nfirst body %d\n>From quoted %d\n\n", i, i, i, i) +
		fmt.Sprintf("From b%d@example.org Mon Feb  3 10:01:00 2025\nSubject: counted %d\nContent-Length: %d\n\n%s\n", i, i, len(counted), counted) +
		fmt.Sprintf("From c%d@example.org Mon Feb  3 10:02:00 2025\n", i) + reusedMessage(i+1000)
}

type reuseInput struct {
	name, data string
}

func reuseInputs(i int) []reuseInput {
	return []reuseInput{{"m.eml", reusedMessage(i)}, {"b.mbox", reusedMailbox(i)}}
}

// reuseOptions are the options documents are read with: with the bytes of
// pictures, and embedded documents given to a dispatcher.
func reuseOptions(name string) *gofastcore.Options {
	var got []seen
	return &gofastcore.Options{Name: name, Spans: true, Images: gofastcore.ImagesData, Child: dispatcher(&got)}
}

func encode(t testing.TB, doc *gofastcore.Document) string {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + doc.Text() + doc.Markdown()
}

// scribbled is a reader of data that, once read, overwrites data, so that
// a document that refers to the caller's bytes shows it.
type scribbled []byte

func (s scribbled) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(s)) {
		return 0, io.EOF
	}
	n := copy(p, s[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// TestBuffersReused: a document is the same after many others, of every
// size, have been read with the memory it was read with.
func TestBuffersReused(t *testing.T) {
	for _, in := range reuseInputs(1) {
		doc, err := gofastemail.Extract(context.Background(), scribbled(in.data), int64(len(in.data)), reuseOptions(in.name))
		if err != nil {
			t.Fatal(err)
		}
		before := encode(t, doc)
		raw := false
		for _, b := range doc.AllBlocks() {
			raw = raw || b.Image != nil && strings.HasPrefix(string(b.Image.Data), "\x89PNG-raw-")
		}
		if !raw {
			t.Fatalf("%s: the picture's bytes are not in the document", in.name)
		}
		for i := 2; i < 200; i++ {
			for _, other := range reuseInputs(i) {
				if _, err := gofastemail.Extract(context.Background(), scribbled(other.data), int64(len(other.data)), reuseOptions(other.name)); err != nil {
					t.Fatal(err)
				}
			}
		}
		if after := encode(t, doc); after != before {
			t.Fatalf("%s: the document changed when others were read after it:\nbefore %.300q\nafter  %.300q", in.name, before, after)
		}
	}
}

// TestBuffersReusedConcurrently: documents read at once by many
// goroutines are each what they are when read alone.
func TestBuffersReusedConcurrently(t *testing.T) {
	const n = 64
	want := map[string]string{}
	for i := range n {
		for _, in := range reuseInputs(i) {
			doc, err := gofastemail.Extract(context.Background(), scribbled(in.data), int64(len(in.data)), reuseOptions(in.name))
			if err != nil {
				t.Fatal(err)
			}
			want[fmt.Sprint(in.name, i)] = encode(t, doc)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			type held struct {
				key string
				doc *gofastcore.Document
			}
			var docs []held
			for k := range 4 * n {
				i := (k*7 + g) % n
				for _, in := range reuseInputs(i) {
					doc, err := gofastemail.Extract(context.Background(), scribbled(in.data), int64(len(in.data)), reuseOptions(in.name))
					if err != nil {
						errs <- err.Error()
						return
					}
					docs = append(docs, held{fmt.Sprint(in.name, i), doc})
				}
			}
			// Encoded only after every one is read.
			for _, d := range docs {
				if got := encode(t, d.doc); got != want[d.key] {
					errs <- fmt.Sprintf("%s differs from when it was read alone", d.key)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
}

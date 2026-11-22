// Command gofastemail extracts email (.eml, .mbox, Outlook .msg, TNEF
// winmail.dat) into the shared document model of the go-fast libraries and
// prints it as JSON, Markdown or plain text.
//
//	gofastemail [flags] file-or-directory...
//
// A directory stands for the files in it and below it whose extension is
// .eml, .mbox, .mbx or .msg. A file named on the command line is read
// whatever its name, its format decided from its bytes, so a winmail.dat
// is read when it is named. With one input the JSON is indented; with
// several, each document is one line.
// Only the documents go to standard output: everything said about them
// goes to standard error, and the exit status is 1 if any input could not
// be read.
//
// Embedded documents are extracted too: attached messages by this
// library; HTML, XHTML, Markdown and plain text bodies and attachments,
// and CSV, TSV, Jupyter notebook and EPUB attachments, by go-fast-html;
// RTF ones by go-fast-rtf. Other attachments are listed.
// Markdown and text include them, each after a line naming it, unless
// -children=false. The library itself reads none of these formats: a
// program passes its own Options.Child.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"

	"github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-email"
	"github.com/AOShei/go-fast-html"
	"github.com/AOShei/go-fast-rtf"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// The extensions of the files this command picks out of a directory.
var extensions = map[string]bool{".eml": true, ".mbox": true, ".mbx": true, ".msg": true}

// run is the command: it reads args and writes documents to stdout and
// messages to stderr, and returns the exit status.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("gofastemail", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		format     = flags.String("format", "json", "what to print: json, markdown or text")
		output     = flags.String("o", "", "write to this file instead of standard output")
		parts      = flags.String("parts", "", "messages to read, by number: 3, or 2-5 (a mailbox's messages count from 1)")
		password   = flags.String("password", "", "password passed to the extractor of encrypted attachments")
		maxBytes   = flags.Int64("max-decoded-bytes", 0, "bytes decoded from base64 and quoted-printable in a file (0: the default, 2 GiB; -1: no limit)")
		maxText    = flags.Int64("max-text-bytes", 0, "bytes of text a document and the files embedded in it may hold (0: the default, 256 MiB; -1: no limit)")
		maxMessage = flags.Int64("max-message-bytes", 0, "bytes of one message held in memory (0: the default, 256 MiB; -1: no limit)")
		quiet      = flags.Bool("quiet", false, "say nothing about files that were read; failures are still reported")
		version    = flags.Bool("version", false, "print the version and exit")
	)
	// The render flags shared by every go-fast command (COMMAND_FLAGS.md).
	renderFlags := gofastcore.AddRenderFlags(flags)
	flags.Lookup("part-markers").Usage += " (here, each message)"
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: gofastemail [flags] file-or-directory...")
		fmt.Fprintln(stderr, "Extracts .eml, .mbox and .msg files, and winmail.dat files named on the command line. A directory stands for the email files in it and below it.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *version {
		fmt.Fprintln(stdout, "gofastemail", moduleVersion())
		return 0
	}
	fail := func(format string, args ...any) int {
		fmt.Fprintf(stderr, "gofastemail: "+format+"\n", args...)
		return 2
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return 2
	}
	if *format != "json" && *format != "markdown" && *format != "text" {
		return fail("unknown format %q: json, markdown or text", *format)
	}
	opts := gofastcore.Options{Password: *password}
	opts.Limits.MaxDecodedBytes, opts.Limits.MaxTextBytes = *maxBytes, *maxText
	if *parts != "" {
		first, last, ok := strings.Cut(*parts, "-")
		if !ok {
			last = first
		}
		a, err1 := strconv.Atoi(strings.TrimSpace(first))
		b, err2 := strconv.Atoi(strings.TrimSpace(last))
		if err1 != nil || err2 != nil || a < 1 || b < a {
			return fail("-parts wants a number or a range such as 2-5, not %q", *parts)
		}
		opts.Parts = gofastcore.PartRange{First: a, Last: b}
	}
	extractor := &gofastemail.Extractor{MaxMessageBytes: *maxMessage}
	opts.Child = dispatcher(extractor)

	var inputs []string
	status := 0
	for _, arg := range flags.Args() {
		info, err := os.Stat(arg)
		if err != nil {
			fmt.Fprintf(stderr, "gofastemail: %v\n", err)
			status = 1
			continue
		}
		if !info.IsDir() {
			inputs = append(inputs, arg)
			continue
		}
		var found []string
		err = filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				fmt.Fprintf(stderr, "gofastemail: %v\n", err)
				status = 1
				return nil
			}
			if !d.IsDir() && extensions[strings.ToLower(filepath.Ext(path))] {
				found = append(found, path)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(stderr, "gofastemail: %v\n", err)
			status = 1
		}
		sort.Strings(found)
		inputs = append(inputs, found...)
	}

	out := stdout
	if *output != "" {
		file, err := os.Create(*output)
		if err != nil {
			fmt.Fprintf(stderr, "gofastemail: %v\n", err)
			return 1
		}
		defer file.Close()
		out = file
	}
	w := bufio.NewWriter(out)
	defer w.Flush()
	// The same options render every document this command prints.
	render := &gofastcore.RenderOptions{}
	renderFlags.Apply(render)

	written := 0
	for _, path := range inputs {
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "gofastemail: interrupted")
			return 1
		}
		o := opts
		o.Name = filepath.Base(path)
		doc, err := extractor.ExtractFile(ctx, path, &o)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			status = 1
			continue
		}
		if !*quiet {
			if note := remarks(doc); note != "" {
				fmt.Fprintf(stderr, "%s: %s\n", path, note)
			}
		}
		switch *format {
		case "json":
			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(false)
			if len(inputs) == 1 {
				enc.SetIndent("", "  ")
			}
			err = enc.Encode(doc)
		case "markdown":
			if len(inputs) > 1 {
				if written > 0 {
					fmt.Fprintln(w)
				}
				fmt.Fprintf(w, "<!-- %s -->\n\n", strings.ReplaceAll(path, "--", "- -"))
			}
			if err = doc.WriteMarkdown(w, render); err == nil {
				_, err = fmt.Fprintln(w)
			}
		default:
			if len(inputs) > 1 {
				if written > 0 {
					fmt.Fprintln(w)
				}
				fmt.Fprintf(w, "==> %s <==\n", path)
			}
			if err = doc.WriteText(w, render); err == nil {
				_, err = fmt.Fprintln(w)
			}
		}
		if err == nil {
			err = w.Flush()
		}
		if err != nil {
			fmt.Fprintf(stderr, "gofastemail: writing %s: %v\n", path, err)
			return 1
		}
		written++
	}
	return status
}

// dispatcher returns the command's Options.Child: attached messages are
// read by this library; HTML, Markdown, plain text, CSV, TSV, Jupyter
// notebooks and EPUB by go-fast-html; RTF by go-fast-rtf; everything else
// is listed. Core's depth limit applies
// through ExtractChild.
func dispatcher(extractor *gofastemail.Extractor) gofastcore.ChildFunc {
	return func(ctx context.Context, name, mimeType string, r io.ReaderAt, size int64, o *gofastcore.Options) (*gofastcore.Document, error) {
		t := mediaType(mimeType)
		if t == "" || t == "application/octet-stream" {
			// Outlook keeps no type for many attachments, and mail
			// programs send files as octet-stream: the extension says.
			t = byExtension[strings.ToLower(filepath.Ext(name))]
		}
		switch t {
		case "message/rfc822", "message/global", "application/vnd.ms-outlook":
			return extractor.Extract(ctx, r, size, o)
		case "text/html", "application/xhtml+xml":
			return (&gofasthtml.Extractor{Format: "html", Charset: charsetOf(mimeType)}).Extract(ctx, r, size, o)
		case "text/markdown":
			return (&gofasthtml.Extractor{Format: "md", Charset: charsetOf(mimeType)}).Extract(ctx, r, size, o)
		case "text/plain":
			return (&gofasthtml.Extractor{Format: "txt", Charset: charsetOf(mimeType)}).Extract(ctx, r, size, o)
		case "text/csv", "text/tab-separated-values":
			format := "csv"
			if t == "text/tab-separated-values" {
				format = "tsv"
			}
			return (&gofasthtml.Extractor{Format: format, Charset: charsetOf(mimeType), Header: headerPresent(mimeType)}).Extract(ctx, r, size, o)
		case "application/x-ipynb+json":
			return (&gofasthtml.Extractor{Format: "ipynb"}).Extract(ctx, r, size, o)
		case "application/epub+zip":
			return (&gofasthtml.Extractor{Format: "epub"}).Extract(ctx, r, size, o)
		case "application/rtf", "text/rtf":
			return (&gofastrtf.Extractor{}).Extract(ctx, r, size, o)
		}
		return nil, fmt.Errorf("gofastemail: no extractor for %q: %w", mimeType, gofastcore.ErrUnsupported)
	}
}

// byExtension gives the media type that an attachment declared
// application/octet-stream, or with no type, is read as, by the extension
// of its name.
var byExtension = map[string]string{
	".eml": "message/rfc822", ".msg": "application/vnd.ms-outlook",
	".htm": "text/html", ".html": "text/html", ".xhtml": "application/xhtml+xml",
	".txt": "text/plain", ".md": "text/markdown", ".rtf": "application/rtf",
	".csv": "text/csv", ".tsv": "text/tab-separated-values", ".tab": "text/tab-separated-values",
	".ipynb": "application/x-ipynb+json", ".epub": "application/epub+zip",
}

// remarks sums up what a document's warnings say that a person running
// the command would want to know without reading the JSON: anything of
// severity error.
func remarks(doc *gofastcore.Document) string {
	counts := map[string]int{}
	for _, w := range doc.AllWarnings() {
		if w.Severity == gofastcore.SeverityError {
			counts[w.Code]++
		}
	}
	if len(counts) == 0 {
		return ""
	}
	codes := make([]string, 0, len(counts))
	for code := range counts {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for i, code := range codes {
		if n := counts[code]; n > 1 {
			codes[i] = fmt.Sprintf("%s (%d)", code, n)
		}
	}
	return "warnings: " + strings.Join(codes, ", ")
}

// mediaType is the media type of a MIME type, without its parameters, in
// lower case.
func mediaType(v string) string {
	if t, _, err := mime.ParseMediaType(v); err == nil {
		return t
	}
	t, _, _ := strings.Cut(v, ";")
	return strings.ToLower(strings.TrimSpace(t))
}

// charsetOf returns the charset parameter of a media type, as the
// message declares it for a body or attachment ("" if none). go-fast-html
// uses it as the transport-layer encoding: after a byte order mark, before
// <meta>.
func charsetOf(v string) string {
	if _, params, err := mime.ParseMediaType(v); err == nil {
		return params["charset"]
	}
	return ""
}

// headerPresent reports whether a CSV or TSV media type declares that the
// first row is a header row, with RFC 4180's parameter header=present.
func headerPresent(v string) bool {
	if _, params, err := mime.ParseMediaType(v); err == nil {
		return strings.EqualFold(params["header"], "present")
	}
	return false
}

// moduleVersion is the version this command was built from.
func moduleVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

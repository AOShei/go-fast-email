# go-fast-email

Extracts email (`.eml`, `.mbox`, Outlook `.msg`, TNEF `winmail.dat`) into
the shared go-fast document model,
[`gofastcore.Document`](https://github.com/AOShei/go-fast-core).
The library uses the standard library and go-fast-core only; its command
also uses go-fast-html and go-fast-rtf.

**Speed and memory.** Messages are small, so the heap stays small and Go's garbage collector runs often: with many workers its pauses, not the reading, take most of the time. Memory can be traded for speed with `GOGC` in the environment (or `debug.SetGCPercent` in a program); the command and the library keep Go's default, 100. On 7,551 messages (56 MB), each read from its file as a pipeline does (wall time, peak memory; [`bench/`](bench/README.md)):

| `GOGC` | One worker | Peak | 24 workers | Peak |
|---|---|---|---|---|
| 100 (default) | 0.49 s | 22 MB | 0.42 s | 33 MB |
| 200 | 0.43 s | 24 MB | 0.16 s | 63 MB |
| 400 | 0.41 s | 33 MB | 0.12 s | 125 MB |

**Status: v0.2.0 (2026-11-23).** How it was built is in
[`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md) (development history);
the changes are in [`CHANGELOG.md`](CHANGELOG.md).

| Format | State |
|---|---|
| `.eml` (RFC 5322, MIME) | Headers with RFC 2047 encoded words and RFC 2231 parameters; base64, quoted-printable, 7bit, 8bit, binary; every multipart kind; text/plain bodies as paragraphs, quotes and signature; HTML bodies, alternatives, attachments and attached messages handed to the caller's extractor |
| `.mbox` | mboxo, mboxrd and mboxcl2; one part per message, read one message at a time |
| `.msg` (MS-OXMSG) | Subject, sender, recipients, dates, transport headers, plain body; HTML body and compressed RTF body (MS-OXRTFCP) handed to the caller's extractor; attachments by value, attached messages (read recursively), OLE objects and attachments by reference listed |
| `winmail.dat` (MS-OXTNEF) | Inside a message or as a file: subject, message class, plain body; HTML and RTF bodies and attachments as for `.msg`, attached messages read recursively |

## Using it

```go
import (
    "github.com/AOShei/go-fast-core"
    "github.com/AOShei/go-fast-email"
)

doc, err := gofastemail.ExtractFile(ctx, "message.eml", nil)
doc, err := gofastemail.Extract(ctx, readerAt, size, &gofastcore.Options{Child: myDispatcher})
fmt.Println(doc.Markdown())
```

The format is decided from the bytes, never from the name: a compound file
is an Outlook message, a file that begins `From ` is a mailbox, a file that
begins with the TNEF signature is a winmail.dat (format `tnef`), a file
that begins with a header field is a message.

**Extractors do not import each other.** Every attachment, every attached
message, and every HTML or RTF body is handed to `Options.Child` through
`opts.ExtractChild` (with the two exceptions below), and the result is
appended to `Document.Children`. Without a `Child` function each is listed
with its name and size and a `child.not_extracted` warning. The media
type given to the dispatcher is the part's `Content-Type` value; for text
it carries the charset parameter (`text/html; charset=iso-8859-1`), which
the child's extractor needs.

Two kinds of embedded document are read by this library itself, with or
without a `Child` function, and `Child` is not called for them: a
winmail.dat that begins with the TNEF signature, and a message attached
inside one, which is a TNEF stream too (see Bodies below; media type
`application/ms-tnef`); and an Outlook message attached to an Outlook
message, which is a storage of the same file, not a file of its own
(media type `application/vnd.ms-outlook`). Both still go through
`ExtractChild`, so that the depth limit and the budgets apply, and what
they hold goes to `Child` in turn. An attached `.eml` (`message/rfc822`)
is not one of them: it goes to `Child` like any other attachment.

`cmd/gofastemail` prints JSON, Markdown or text:

```
go run ./cmd/gofastemail -format markdown message.eml
```

The library itself reads no HTML, RTF or other format of a body or
attachment: a program passes its own `Options.Child`. The command passes
one that reads, as children:

| Media type | Read by |
|---|---|
| `message/rfc822`, `message/global`, `application/vnd.ms-outlook` | this library (attached messages) |
| `text/html`, `application/xhtml+xml` | go-fast-html, format `html` |
| `text/markdown` | go-fast-html, format `md` |
| `text/plain` (alternatives and attachments) | go-fast-html, format `txt` |
| `text/csv`, `text/tab-separated-values` | go-fast-html, format `csv` or `tsv`; the parameter `header=present` (RFC 4180) makes the first row a header row |
| `application/x-ipynb+json` | go-fast-html, format `ipynb` (Jupyter notebooks) |
| `application/epub+zip` | go-fast-html, format `epub` |
| `application/rtf`, `text/rtf` (including the decompressed RTF body of a `.msg`) | go-fast-rtf |

The `charset` parameter the message gives a text part (from its
`Content-Type`, or `PR_INTERNET_CPID` in a `.msg`) goes to go-fast-html as
`Extractor.Charset`, the transport-layer encoding: it wins over a
`<meta>` declaration and loses only to a byte order mark. It applies to
HTML, Markdown, plain text, CSV and TSV; a notebook is JSON, in UTF-8,
and an EPUB's chapters declare their own. An attachment that declares no
set is left to go-fast-html, which reads a `<meta>` declaration and
otherwise recognises the set from the bytes where one is certain; where it
cannot (`gofasthtml.ErrEncodingUnknown`), the command's `-charset`, if
given, is used, and otherwise the attachment is listed with
`child.unsupported` and a message that says to give it.

An attachment declared `application/octet-stream`, or with no type (as
Outlook keeps many), is read by the extension of its name: `.eml`,
`.msg`, `.htm`, `.html`, `.xhtml`, `.txt`, `.md`, `.rtf`, `.csv`, `.tsv`,
`.tab`, `.ipynb` and `.epub` as the types above. A declared type is never second-guessed.

Every other attachment is listed with its name and size
(`child.unsupported`). Core's depth limit applies. Only `cmd/gofastemail`
imports go-fast-html and go-fast-rtf; the library package imports core
and the standard library only, and a test checks that.

The command takes files and directories. A directory stands for the files
in it and below it with the extension `.eml`, `.mbox`, `.mbx` or `.msg`; a
file named on the command line is read whatever its name, so a
winmail.dat is read when it is named. With one input the JSON is
indented; with several, each document is one line of JSON, and in
Markdown and text a line naming the file comes before each document.
Only the documents go to standard output. A document with warnings of
severity error is named on standard error with their codes. The exit
status is 1 if an input could not be read, 2 for a usage error.

| Flag | Effect |
|---|---|
| `-format` | `json` (the default), `markdown` or `text` |
| `-o file` | write to the file instead of standard output |
| `-parts n`, `-parts a-b` | read only these messages, by number (a mailbox's messages count from 1) |
| `-charset` | sets `Extractor.Charset`: the character set of text that declares none and is not UTF-8, used instead of the one recognised from its bytes; also for an HTML, text or CSV attachment whose set go-fast-html cannot determine |
| `-password` | sets `Options.Password`, which reaches every extractor the command runs, those of embedded documents included; none of the formats the command reads (this library, go-fast-html, go-fast-rtf) uses it today |
| `-max-message-bytes` | `Extractor.MaxMessageBytes`: bytes of one message held in memory (0: the default, 256 MiB; -1: no limit) |
| `-max-decoded-bytes` | `Limits.MaxDecodedBytes`: bytes decoded in a file and its embedded documents (0: the default, 2 GiB; -1: no limit) |
| `-max-text-bytes` | `Limits.MaxTextBytes`: bytes of text in a document and its embedded documents (0: the default, 256 MiB; -1: no limit) |
| `-quiet` | say nothing on standard error about documents that were read; failures are still reported |
| `-version` | print the version and exit |

It also has the render flags every go-fast command has. They change
Markdown and text only; JSON always has everything.

| Flag | Effect |
|---|---|
| `-furniture` | in Markdown and text, include running headers and footers |
| `-stored[=kinds]` | in Markdown and text, include stored data (data the file keeps but shows nowhere): all of it (alone, `=true` or `=all`), or with =kinds only those kinds (`=markup_comment`, `=field_code,macro`; comma or `+`), wherever they are; `=false` or `=` turns it off, and a name that is not a kind is an error listing the kinds |
| `-part-markers` | in Markdown and text, put a line before each part saying which part it is (here, each message) |
| `-exclude-display[=reasons]` | in Markdown and text, leave out what the file declares not displayed: all of it (alone, `=true` or `=all`), or only the reasons named (`=hidden`, `=no_ink`, `=hidden,no_ink`); `=false` or `=` turns it off, and any other value is an error |
| `-children` (default true) | in Markdown and text, include embedded documents (attachments, members), each after a line naming it; -children=false leaves them out |

This library itself produces no running headers, stored part or display
reason; the flags matter for documents that carry them, such as an HTML
body read by go-fast-html. With `-children`, core renders each child after
the message's own parts, depth first, after a line naming it
(`[child 1: report.rtf]` in text, `<!-- child 1: report.rtf -->` in
Markdown); an HTML-only message's text is therefore in its child.

The kinds `-stored` accepts are core's `gofastcore.StoredKindNames()`,
which include those go-fast-html (`manifest_item`, `stylesheet`) and
go-fast-rtf (`generator`, `custom_property`) write; this library writes
none itself.
An HTML body's `<!-- comments -->` are stored `markup_comment` by
go-fast-html: left out by default, shown with `-stored=markup_comment` or
`-stored`. HTML that an Outlook message keeps
encapsulated in RTF is handed on by go-fast-rtf as a `body.html` child,
and its text comes from there.

## How a message maps

Every message is a part of kind `message`. An `.eml` or `.msg` is a
document of one part; a mailbox has a part for each message, numbered by
its place in the file. A part's `Name` is the subject. Its blocks are:

1. A `heading` of level 1 with the subject.
2. A `paragraph` with `Attrs["role"] = "headers"`: the headers a mail
   program shows, one per line (`From`, `To`, `Cc`, `Bcc`, `Date`), and a
   line `Attachments: ...` with the names of the attachments a mail
   program lists. `Attrs["children"]` lists, comma separated, the indexes
   in `Document.Children` of every document that came from this message,
   which is how the children of a mailbox's messages are told apart.
3. In a mailbox only: a `note` with `Attrs["type"] = "headers"` holding
   every other header (and the `From ` line).
4. The body.

For a single message (`.eml`, `.msg`), `Metadata` has the subject as
`Title`, the From header as the one entry of `Authors`, the date as
`Created`, and every header in `Extra` under its lowercase name (a header
given more than once has its values joined by `\n`). For `.msg`, `Extra`
holds the transport headers Outlook keeps, `message-class` and
`message-id`.

### Bodies

- **text/plain** becomes blocks. Paragraphs are separated by blank lines;
  a line break inside a paragraph is kept as `\n`, since in plain-text
  mail the line breaks are what the reader sees. `format=flowed` (RFC
  3676) is joined, with space-stuffing and `delsp` undone. Lines quoted
  with `>` are `quote` blocks whose `Level` is the depth of quoting. The
  signature, after a line `-- `, is kept as paragraphs with
  `Attrs["role"] = "signature"`. A text/plain part with `Content-Disposition:
  inline` after the first is extracted the same way.
- **text/html** goes to `ExtractChild`.
- **multipart/alternative**: the first text/plain alternative gives the
  blocks; every other alternative goes to `ExtractChild`, and the part
  carries `email.alternatives`, since a mail program may show the HTML
  instead and it can say something else. Without a text/plain alternative
  each alternative is an embedded document only.
- **image/\*** parts a mail program shows in the message (not
  `Content-Disposition: attachment`, and not inside an alternative that
  does not give the blocks), and Outlook attachments of an image type
  left out of the attachment list (`PR_ATTACHMENT_HIDDEN`: pictures shown
  in the body), are `image` blocks at their
  place, unless `Options.Images` is `ImagesNone`. The block's text is the
  `Content-Description`, `Attrs["name"]` the file name, `Image.ID`
  `sha256-` and 24 hex digits of the bytes' SHA-256, the pixel size read
  from the header of a PNG, JPEG or GIF, and with `ImagesData` the bytes.
  Each is an embedded document as well.
- **winmail.dat** (`application/ms-tnef`, or a part of that name, that
  begins with the TNEF signature; [MS-OXTNEF]) is where Outlook puts a
  message's RTF body and its attachments. It is read by this library,
  through `ExtractChild` all the same (so the depth limit and budgets
  apply), into an embedded document of format `tnef`: one `message`
  part with the subject as heading and the plain body (`PR_BODY`, or the
  old `attBody` in its code page) as blocks; the HTML and compressed RTF
  bodies, the attachments and attached messages (read recursively) as its
  own embedded documents, as for an Outlook message. Named and
  multi-valued properties are not read. TNEF has been tested only on
  streams the tests build, written from [MS-OXTNEF], never on a
  winmail.dat written by Outlook: the public corpus has none.
- **message/rfc822** and every other part go to `ExtractChild`, named by
  their `filename` or `name` parameter.
- **Outlook:** `PR_BODY` gives the blocks; `PR_HTML` (as `text/html`, with
  the charset of `PR_INTERNET_CPID`) and `PR_RTF_COMPRESSED`, decompressed,
  (as `application/rtf`) go to `ExtractChild`, with `email.alternatives`
  when there is more than one.

### Character sets

UTF-8, US-ASCII, ISO-8859-1, ISO-8859-15, windows-874, windows-1250 to
1258 and Mac OS Roman are decoded, and UTF-16 in `.msg` files; the same
code pages where an Outlook message or TNEF stream names one for its 8-bit
strings. ISO-8859-1, ISO-8859-9 and ISO-8859-11 (TIS-620) are decoded as
windows-1252, windows-1254 and windows-874, as the WHATWG Encoding
Standard does. KOI8-R, KOI8-U and the other ISO-8859 parts (2, 3, 4, 5, 6,
7, 8, 10, 13, 14, 16) are not decoded yet: no table verified against its
mapping file was at hand. Text labelled US-ASCII, or not labelled, that is
valid UTF-8 is read as UTF-8. A character set this library does not
decode is not guessed at: bytes outside ASCII become U+FFFD and the part
has a `text.unmapped_glyphs` warning that names the set (none when the
text is all ASCII, since then nothing is lost). The tables' sources are in
[`NOTICE`](NOTICE).

**Text that declares no character set** (a body with no `charset`, header
text outside encoded words, an Outlook string with no code page), or
declares US-ASCII, and is not valid UTF-8 is read in the set its bytes are
**certainly** in, with the warning `text.encoding_detected` naming it:
UTF-8 with damaged bytes, UTF-16 without a byte order mark, Shift_JIS,
EUC-KR, GBK, Big5, windows-1251, or windows-1252 for text whose accented
letters stand alone in words and show nothing of another Latin code page.
The tests, their thresholds, and how they were measured are go-fast-html's
(its README, "Text that declares no encoding"; this library has a copy of
the code, since libraries do not import each other): on Wikipedia text in
30 languages, no input of 1 KiB or more read in a wrong set. Where no set
is certain, the bytes outside ASCII are U+FFFD, as before, and
`text.unmapped_glyphs` says to give the set. `Extractor.Charset` (command:
`-charset`) gives the set of such text instead of the one recognised from
its bytes; valid UTF-8 stays UTF-8, and a set the text declares wins. The
test reads at most 32 KiB of the text, so its time is bounded: 2 to 9 µs
on 1.5 KiB.

The double-byte code pages are decoded from their generated tables in
`internal/charset/tables` (see the `README.txt` there): Shift_JIS (code
page 932; labels `shift_jis`, `sjis`, `ms932`, `windows-31j`, `x-sjis`,
`csshiftjis`, `ms_kanji`), GBK and GB2312 (936; `gbk`, `gb2312`, `chinese`, `csgb2312`, `gb_2312-80`,
`iso-ir-58`, `x-gbk`, `cp936`), EUC-KR (949; `euc-kr`, `ks_c_5601-1987`,
`windows-949`, `cseuckr`, `korean`, `cp949`) and Big5 (950; `big5`,
`big5-hkscs`, `cn-big5`, `csbig5`, `x-x-big5`, `cp950`), in headers,
encoded words, text/plain bodies and the 8-bit strings of `.msg` and TNEF
by their code page. A lead byte without a valid trail byte, or at the end
of the text, is U+FFFD with the warning above.

GB18030 (`gb18030`, code page 54936), EUC-JP (`euc-jp`,
`cseucpkdfmtjapanese`, `x-euc-jp`, code page 51932) and ISO-2022-JP
(`iso-2022-jp`, `csiso2022jp`, code pages 50220 to 50222) are decoded as
the WHATWG Encoding Standard's decoders do, everywhere the double-byte
sets are, from the tables `gen/gen_cjk_extra.py` writes. GB18030 has its
four-byte sequences, supplementary characters included; EUC-JP its
half-width katakana and JIS X 0212; ISO-2022-JP switches between ASCII,
JIS-Roman, JIS X 0208 and half-width katakana by escape sequence, and each
encoded word starts in ASCII. Bytes that make no character are U+FFFD
with the warning above. The labels `gbk` and `gb2312` stay on code page
936, which has GB18030's two-byte characters but not its four-byte
sequences (the Encoding Standard decodes them with the GB18030 decoder).
KOI8 is not decoded and keeps the warning.

In a `.msg`, 8-bit strings are read in `PR_MESSAGE_CODEPAGE`, else in
`PR_INTERNET_CPID`. When that is UTF-8 and a string is not valid UTF-8,
it is read in the ANSI code page of the message's locale
(`PR_MESSAGE_LOCALE_ID`, by primary language: windows-1250 to 1258 or
874), as Outlook writes them.

## What the file declares

The go-fast libraries report what a file declares about the display of
its content (`Display`: `hidden`, `no_ink`); they do not judge whether
text is visible or harmful, and the absence of a reason says nothing
about either. Threat detection is outside these libraries.

**This library reports no display reason.** Email declares none of its
own:

- Headers a mail program does not show (`X-*`, `Received`, `Reply-To`,
  `Message-ID`, the MIME headers, Outlook's transport headers), the MIME
  preamble and the epilogue are not unshown content: they keep their
  provenance. Headers are in `Metadata.Extra` (a single message) or a
  `note` of type `headers` (a mailbox); the preamble and epilogue are
  `note`s of type `mime_preamble` and `mime_epilogue`. Whatever their
  length or content.
- A text/plain body declares nothing about display.
- HTML and RTF bodies, and every attachment, go to `ExtractChild`; their
  extractor reports what their format declares.
- `PR_ATTACHMENT_HIDDEN` (Outlook, TNEF) sets no reason: it leaves an
  attachment out of the attachment list, usually a picture shown in the
  body. The attachment is extracted, and not named on the `Attachments:`
  line.
- An alternative body that is not the text/plain one is an embedded
  document; `email.alternatives` on the part says the message has
  alternatives, since a mail program may show the other one.

Colour, size, position, overlap and the length of unshown data are not
judged. Characters are kept as they are, within core's text conventions:
zero-width characters, Unicode tag characters, bidirectional controls and
variation selectors are in the text of headers, metadata, notes and
bodies unchanged.

## Warnings

Warnings state facts about extraction (damage, limits, unsupported
features); none is about unshown content. A warning that can be given for
many values (character sets, transfer encodings, media types, places
outside the message, sets of alternatives) is given once per message,
with the number of distinct values and the first eight; a bound that cuts
many items is reported once per message, with their number.

| Code | When |
|---|---|
| `email.alternatives` (warning) | The message has alternative bodies |
| `email.unknown_transfer_encoding` (warning) | A part declares a transfer encoding other than base64, quoted-printable, 7bit, 8bit or binary; its bytes are taken as they are |
| `text.unmapped_glyphs` | A character set not decoded, bytes not valid in the one declared, or bytes outside ASCII in text that declares no set and whose set is not certain |
| `text.encoding_detected` | Text that declares no character set (or US-ASCII) and is not UTF-8 was read in the set recognised from its bytes, which the message names; one warning per message, listing the sets when there are several |
| `text.control_characters_removed` | Control characters with no meaning as text were removed from the message (info): from its body (text/plain, an Outlook body, stored text) and header values (subject, names and addresses as shown, image descriptions), after decoding. Tab and line ends are not counted; file names, message ids and the `From ` line of a mailbox are names, not text, and are not counted. The message gives the count. Terminal colour codes in build logs are the usual source: their ESC is removed |
| `input.repaired` | A header section ended by a line that is not a header; a multipart body without closing boundary or without any boundary; base64 with characters outside its alphabet; an unreadable compressed RTF body; a damaged TNEF stream (cut short, a wrong checksum, an unreadable property list); an Outlook compound file whose short streams cannot be read (the properties in them are missing; severity error) |
| `input.external_reference` | An Outlook attachment by reference names a file outside the message; a `message/external-body` part names content elsewhere (its URL or file is in the message; nothing is fetched) |
| `limit.exceeded` (error) | Any limit below, and the fixed bounds after it |
| `part.no_text` | A message with no text at all |

## Limits

| Limit | Default | What happens |
|---|---|---|
| `Limits.MaxInputBytes` | 16 GiB | `ErrLimitExceeded` |
| `Limits.MaxDecodedBytes` | 2 GiB, shared with embedded documents; in an Outlook message, every byte read from a property stream counts, since streams can share sectors | Later parts are listed, not read |
| `Limits.MaxTextBytes` | core's 256 MiB, shared with embedded documents; each message of a mailbox takes `BlockCost` | The part is cut; later messages of a mailbox are not read |
| `Limits.MaxParts` | 1,000,000 messages in a mailbox | The first are read |
| `Limits.MaxDepth` | core's 4 | Deeper documents are listed |
| `Extractor.MaxMessageBytes` | 256 MiB held in memory for one message | The message, or an Outlook property, is read to that length |
| `Extractor.MaxHeaderBytes` | 1 MiB per header section | The section is cut |
| `Extractor.MaxMIMEDepth` | 32 nested multiparts | Deeper ones are not read |
| `Extractor.MaxMIMEParts` | 10,000 MIME parts per message | The rest are not read |

A message is held in memory, with at most one decoded part beside it. A
mailbox is scanned a line at a time and read one message at a time.

Fixed bounds on what goes into the output, each reported with
`limit.exceeded` (severity error), once per bound and message with a
count, where it cuts:

- A part name, made from the subject: 4,096 bytes, cut on a character
  boundary (the heading has the subject in full).
- An attachment's file name, which is also the name of its embedded
  document: 4,096 bytes. A longer one is cut in the middle; its extension
  is kept.
- RFC 2231 pieces numbered above 999, of the parameters this library
  reads, are not read.
- An encoded word: 4,096 bytes of encoded text and a character set name
  of 100 bytes. A longer one is left as written.
- A mailbox `From ` line: its first 1,024 bytes in the headers note.
- An image description: 4,096 bytes.
- The character set name passed on in the media type of an embedded
  document: 63 bytes (the longest registered name is 45). A longer one is
  not passed on.
- Outlook messages: 10,000 recipients and 10,000 attachments; 65,536
  entries in the property stream of a message, recipient or attachment
  (the fixed-length values past them, such as dates, numbers and flags,
  are not read); and the bounds of a header section for the transport
  headers. A compound file with more than 262,144 directory entries is
  refused with `ErrLimitExceeded`.
- TNEF streams: 10,000 attachments, and 10,000 properties in a list.

Bounds on data used only inside the library, which lose nothing that
would be in the output and need no warning:

- Parameters of a header: past the 64th only `boundary`, `charset`,
  `name`, `filename`, `format`, `delsp`, `access-type`, `url` and `site`
  are read (to the end of the header); the others are not used.
- Quote depth: 64. Deeper quoting is a quote of depth 64 with the `>`
  marks beyond it kept in its text.
- Telling a message from other input: the first 64 KiB are looked at for
  a header field.
- Reading a mailbox: 64 KiB at a time; a longer line is read in pieces.
- Warnings themselves (they describe the output, they are not content):
  a warning for many values names the first 8 and counts up to 10,000
  distinct values; a character set name, transfer encoding or access type
  quoted in a warning is cut to 64 bytes, the list of alternative
  bodies to 512, and a URL or path of content outside the message to
  4,096 (with a note of the cut in the warning).

## Mailboxes

A `From ` line separates messages where it begins the file or follows a
blank line. Where a message has a `Content-Length` header and the byte it
points to is the end of the file or the next `From ` line, the message ends
there (mboxcl2) and its body is taken as written. Otherwise a line that
begins with one or more `>` and then `From ` loses one `>` (mboxrd). An
mboxo file, which quotes only `From `, reads the same except that a line
that began `>From ` in the original loses its `>`: the two forms cannot be
told apart.

## Not read

- S/MIME and PGP: signed and encrypted parts are embedded documents for the
  caller's dispatcher; nothing is verified or decrypted.
- uuencoded attachments inside text bodies, `message/partial`, and
  `message/external-body` (passed on as embedded documents, never fetched).
- Outlook named properties (`__nameid_version1.0`), multi-valued
  properties, and the CRC of compressed RTF.
- Character sets other than those above, including KOI8.
- Byte offsets (`Loc`) for blocks: bodies are decoded before they become
  blocks, so offsets into the file would not be meaningful.

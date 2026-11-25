# go-fast-email

Extracts email (`.eml`, `.mbox`, Outlook `.msg`, TNEF `winmail.dat`),
calendars (`.ics`) and contact lists (`.vcf`) into the shared go-fast
document model,
[`gofastcore.Document`](https://github.com/AOShei/go-fast-core).
The library uses the standard library and go-fast-core only; its command
also uses go-fast-html and go-fast-rtf.

**Speed and memory.** Messages are small, so the heap stays small and Go's garbage collector runs often: with many workers its pauses, not the reading, take most of the time. Memory can be traded for speed with `GOGC` in the environment (or `debug.SetGCPercent` in a program); the command and the library keep Go's default, 100. On 7,551 messages (56 MB), each read from its file as a pipeline does (wall time, peak memory; [`bench/`](bench/README.md)):

| `GOGC` | One worker | Peak | 24 workers | Peak |
|---|---|---|---|---|
| 100 (default) | 0.46 s | 20 MB | 0.25 s | 42 MB |
| 200 | 0.41 s | 26 MB | 0.12 s | 130 MB |
| 400 | 0.40 s | 39 MB | 0.09 s | 228 MB |

**Status: v0.2.1 (2026-11-24).** How it was built is in
[`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md) (development history);
the changes are in [`CHANGELOG.md`](CHANGELOG.md).

| Format | State |
|---|---|
| `.eml` (RFC 5322, MIME) | Headers with RFC 2047 encoded words and RFC 2231 parameters; base64, quoted-printable, 7bit, 8bit, binary; every multipart kind; text/plain bodies as paragraphs, quotes and signature; HTML bodies, alternatives, attachments and attached messages handed to the caller's extractor |
| `.mbox` | mboxo, mboxrd and mboxcl2; one part per message, read one message at a time |
| `.msg` (MS-OXMSG) | Subject, sender, recipients, dates, transport headers, plain body; HTML body and compressed RTF body (MS-OXRTFCP) handed to the caller's extractor; attachments by value, attached messages (read recursively), OLE objects and attachments by reference listed |
| `winmail.dat` (MS-OXTNEF) | Inside a message or as a file: subject, message class, plain body; HTML and RTF bodies and attachments as for `.msg`, attached messages read recursively |
| `.ics` (RFC 5545, vCalendar 1.0) | Events, to-dos, journal entries, free/busy and availability: summary, details, descriptions and comments, alarms; Outlook's HTML description and inline attachments handed to the caller's extractor; every other property in attributes; byte offsets and UID on every block |
| `.vcf` (RFC 6350, 2426, vCard 2.1) | Each card: name, details with their types and Apple's labels, notes, inline photos and logos as images; every other property in attributes; byte offsets and UID on every block |

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

The format is decided from the bytes, never from the name: a file that
begins (after a byte order mark and white space, in any case, perhaps
with a group such as `home.` before it) `BEGIN:VCARD` is a contact list
(`vcf`), and one that begins `BEGIN:` with `VCALENDAR` or a component a
calendar holds (`VEVENT`, `VTODO`, `VJOURNAL`, `VFREEBUSY`,
`VAVAILABILITY`, `VTIMEZONE`: events exported without their calendar) a
calendar (`ics`); a compound file is an Outlook message, a file that begins `From ` is a mailbox, a file that
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
| `text/calendar`, `application/ics`, `text/x-vcalendar`, `text/vcard`, `text/x-vcard`, `text/directory` | this library (calendars, such as an invitation's `text/calendar` alternative, and contact lists); the part's `charset` parameter is the child's `Extractor.Charset`, the set of values with no `CHARSET` of their own |
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
`.tab`, `.ipynb`, `.epub`, `.ics`, `.ifb`, `.vcs`, `.vcf` and `.vcard` as
the types above. A declared type is never second-guessed.

Every other attachment is listed with its name and size
(`child.unsupported`). Core's depth limit applies. Only `cmd/gofastemail`
imports go-fast-html and go-fast-rtf; the library package imports core
and the standard library only, and a test checks that.

The command takes files and directories. A directory stands for the files
in it and below it with the extension `.eml`, `.mbox`, `.mbx`, `.msg`,
`.ics`, `.ifb`, `.vcs`, `.vcf` or `.vcard`; a
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

## Content lines (calendars and contact lists)

Both formats are read by one parser of content lines (RFC 5545 section
3.1, RFC 6350 section 3.2, and vCard 2.1 and vCalendar 1.0 as phones and
Outlook write them):

- Lines end with CRLF, LF or CR. A line end followed by one space or tab
  is removed (unfolding). A quoted-printable value (vCard 2.1, vCalendar
  1.0) continues across a line that ends with `=` (a soft line break),
  except onto a line that begins `BEGIN:` or is `END:` and the name of an
  open component (in any case): phone exporters end a value with `=`, and
  that line ends the value instead (counted as `input.repaired`).
- `[group.]NAME[;PARAM=value[,value]...]:value`; names in any case;
  quoted parameter values; RFC 6868 caret escapes; vCard 2.1 parameters
  without a name (`TEL;CELL;PREF`) are TYPE values, and
  `QUOTED-PRINTABLE`, `BASE64`, `B`, `7BIT` and `8BIT` written that way are
  the ENCODING.
- `ENCODING=QUOTED-PRINTABLE` and `ENCODING=B`/`BASE64` are decoded within
  the decode budget; `CHARSET=` is the value's character set. A value
  without one that is not UTF-8 is read as a message's text that declares
  none (`Extractor.Charset`, else the set the bytes are certainly in, with
  `text.encoding_detected`).
- TEXT values are unescaped (`\\`, `\;`, `\,`, `\n`, `\N`); `N`, `ADR`,
  `ORG` and `GENDER` are split at unescaped `;`, lists (`CATEGORIES`,
  `NICKNAME`, the components of `N`) at unescaped `,`, always before
  unescaping, so an escaped separator is content (`N:O\,Brien;Pat` is
  `Pat O,Brien`).
- Recovery, each reported once per document with a count as
  `input.repaired` (info): an `END` that matches no open component is
  skipped; a component with no `END` ends where the component around it
  ends, or at the end of the file; a line that is not a content line (no
  colon, or no name before it, as Outlook writes a description with bare
  line breaks) continues the value of the property before it, joined with
  a line break.

## How a calendar maps

A calendar is one part of kind `body` (`Options.Parts` selects part 1 only,
as for an `.eml`). `Metadata.Title` is `X-WR-CALNAME`,
`Metadata.Application` is `PRODID`, and every other property of
`VCALENDAR` is in `Extra` under its lowercase name (a repeated name has
its values joined by `\n`). `Extra["timezones"]` holds the `TZID` of each
`VTIMEZONE`; nothing else of a time zone is read (its properties and its
`STANDARD` and `DAYLIGHT` components are rules for computing times).

Each `VEVENT`, `VTODO`, `VJOURNAL`, `VFREEBUSY`, `VAVAILABILITY` (and any
other component of the calendar), in file order, then the components
nested in it other than alarms (`AVAILABLE`), gives:

1. A `heading` of level 1: `SUMMARY`.
2. A `paragraph` of details, one `Label: value` line each, in this order:
   `Start` (DTSTART), `End` (DTEND), `Due`, `Duration`, `Completed`,
   `Repeats` (RRULE as written, several joined by `; `), `Location`,
   `Organizer`, `Attendees` (joined by `, `), `Status`, `Categories`,
   `URL`, `Free/busy`, and an `Alarm` line per `VALARM` (its `TRIGGER` as
   written, then its `DESCRIPTION` or `SUMMARY`). A person is
   `CN <address>`, or the address, without `mailto:`. Dates are ISO 8601:
   `20261125T100000Z` is `2026-11-25T10:00:00Z`, a local time with a TZID
   is `2026-11-25T10:00:00 (Europe/Paris)`, a DATE is `2026-11-25`; a
   value that does not parse stays as written. A line `Attachments: ...`
   names the inline attachments.
3. `DESCRIPTION` and `COMMENT`, each in file order, as paragraphs split at
   blank lines with their line breaks kept, as a plain body.
4. Embedded documents: `X-ALT-DESC;FMTTYPE=text/html` (Outlook's HTML
   description) as `text/html; charset=utf-8`, with `email.alternatives`
   when there is a DESCRIPTION too; an `ATTACH` with inline data, named by
   `X-FILENAME` or `FILENAME`, of type `FMTTYPE` or
   `application/octet-stream`. An `ATTACH` given by a URI is
   `input.external_reference` (nothing is fetched).

The first block of a component carries in `Attrs` every property not
used above, under its lowercase name, as written after unescaping (a
repeated name joined by `\n`; base64 data as written): `uid`, `dtstamp`,
`exdate`, `x-...`, an `ATTACH` URI, an empty value. A grouped property
keeps its group in its key (`item1.x-abrelatednames`), so it lines up
with the group's other properties (`item1.x-ablabel`). A `VALARM`'s
properties other than those used in its line are there as
`alarm.<name>` (`alarm.action`). `Attrs["component"]` is the component's
kind (`vevent`), and `Attrs["children"]` the indexes in
`Document.Children` of the documents that came from it. A property whose
key would be one of the library's own (`component`, `children`, and in
`Extra` `cards` and `timezones`) is kept under `property.` and its key
(`property.component`).

Every parameter of every property, used in the text or not, is there
too, under the property's key, a dot and the parameter's lowercase name:
`attendee.partstat`, `attendee.cn`, `dtstart.tzid`,
`x-apple-structured-location.x-address`, `adr.label`,
`alarm.trigger.related`. Several values of one parameter are joined by
`,` as written, a parameter given more than once (two attendees) by
`\n`; values are as the parameter gives them, with RFC 6868's `^n`, `^^`
and `^'` decoded. `ENCODING` and `CHARSET` (instructions for decoding)
are not kept, nor a card's `TYPE` values written in its details line.
The parameters of a calendar's own properties are in `Extra` the same
way.

Every block has `Loc.Anchor` = the component's `UID`, and
`Loc.Offset`/`Length` = the bytes it came from: the heading, `SUMMARY`'s
line(s); the details, the component from `BEGIN` to `END`; a paragraph,
its `DESCRIPTION`'s or `COMMENT`'s lines.

The heading is the first `SUMMARY` that is not empty; an empty one is in
the attributes. A component whose only text is an embedded document (an
HTML description, inline attachments) gets its details paragraph with
the line `Attachments: ...`, the HTML description named `(text/html)`,
so its attributes and `Attrs["children"]` have a block.

A component with no text at all (no summary, no details, no description,
no attachment, no HTML description) gives no block, so its attributes
have no place: such
components are counted in one `email.component_without_text` warning
(info) with their kinds. The part has `part.no_text` only when the whole
calendar gives no block.

## How a contact list maps

A contact list is one part of kind `body`. `Metadata` takes nothing from
the cards (a file holds many people); `Extra["cards"]` is their number,
and a property outside any card is in `Extra` too. Each `VCARD`, in file
order (a card nested in one, a vCard 2.1 `AGENT`, after it), gives:

1. A `heading` of level 1: `FN`; without it, the components of `N` in
   the order prefix, given, additional, family, suffix; without either,
   the first component of `ORG`.
2. A `paragraph` of details, one `Label: value` line each, in this order:
   `Organization` (ORG's components joined by `, `), `Title`, `Role`,
   `Nickname`, `Email`, `Phone` (TEL), `Address` (ADR's non-empty
   components joined by `, `), `URL`, `Birthday`, `Anniversary`, `Gender`,
   `Categories`, `IMPP`. TYPE values follow in parentheses, lowercase
   (`Phone: +33 1 23 45 67 89 (cell, pref)`); Apple's label of a group
   (`item1.X-ABLabel:_$!<Mobile>!$_`) counts as a TYPE (`mobile`), a label
   of the user's own as written.
3. `NOTE`: paragraphs, as a calendar's DESCRIPTION.
4. `PHOTO` and `LOGO` with inline data (base64, or a `data:` URI): an
   `image` block as a message's inline picture (ID from the bytes, pixel
   size, the bytes with `ImagesData`, none with `ImagesNone`). Given by a
   URI: `input.external_reference`, and the URI in the attributes.

Every other property (`uid`, `rev`, `prodid`, `version`, `key`, `n` when
FN is the heading, `x-...`; a grouped one under its group,
`item1.x-abrelatednames`, beside its `item1.x-ablabel`), and every
parameter (`adr.label`, `tel.pref`; TYPE values not in the details
line), is in the attributes of the card's first block as for a
calendar, with `Attrs["component"] = "vcard"`;
`Loc.Anchor` and `Loc.Offset`/`Length` as for a calendar (the details
paragraph spans the card).

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
| `input.repaired` | A header section ended by a line that is not a header; a multipart body without closing boundary or without any boundary; base64 with characters outside its alphabet; an unreadable compressed RTF body; a damaged TNEF stream (cut short, a wrong checksum, an unreadable property list); an Outlook compound file whose short streams cannot be read (the properties in them are missing; severity error); in a calendar or contact list, an unmatched `END`, a missing `END`, lines that are not content lines (see Content lines) |
| `input.external_reference` | An Outlook attachment by reference names a file outside the message; a `message/external-body` part names content elsewhere (its URL or file is in the message; nothing is fetched); a calendar's `ATTACH` or a card's `PHOTO` or `LOGO` is given by a URI |
| `limit.exceeded` (error) | Any limit below, and the fixed bounds after it |
| `part.no_text` | A message, calendar or contact list with no text at all |
| `email.component_without_text` (info) | Components of a calendar or contact list with no text give no block, so their properties are not in the output; the count and the kinds |

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
- Calendars and contact lists (held in memory whole, up to
  `Extractor.MaxMessageBytes`): components nest at most 32 deep
  (`MaxComponentDepth`; deeper ones are skipped with what is in them); a
  content line, unfolded, is cut at 1 MiB (`MaxContentLineBytes`, which
  also bounds an inline photo or attachment); one component, and the
  calendar's own properties, hold at most 100,000 properties and nested
  components (`MaxComponentItems`; a `VCALENDAR` nested inside an event
  is one of the event's components, counted with it). The attributes of a component count
  in the text budget.

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

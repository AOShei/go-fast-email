# Changelog

## Unreleased

### New

- **Calendars (`.ics`) and contact lists (`.vcf`).** `Extract` reads a
  file that begins, after a byte order mark and white space,
  `BEGIN:VCALENDAR` (or `BEGIN:` and a component a calendar holds, such as
  `VEVENT`) as format `ics` (`text/calendar`) and one that begins
  `BEGIN:VCARD` (perhaps after a group) as format `vcf` (`text/vcard`),
  before the test for a message; `Formats()` lists both. One part of kind `body`: per event,
  to-do, journal entry, free/busy time or availability a heading
  (SUMMARY), a paragraph of details, DESCRIPTION and COMMENT paragraphs,
  Outlook's HTML description and inline attachments as embedded
  documents; per card a heading (FN, else N, else ORG), a paragraph of
  details with TYPE values and Apple's labels, NOTE paragraphs, PHOTO
  and LOGO as images. Every other property is in the attributes of the
  component's first block; every block has the byte offsets it came from
  and the component's UID as its anchor. vCard 2.1 and vCalendar 1.0
  quoted-printable, base64 and CHARSET values are decoded. README:
  "How a calendar maps", "How a contact list maps".
- New warning code `email.component_without_text` (info); new constants
  `MaxComponentDepth` (32), `MaxContentLineBytes` (1 MiB),
  `MaxComponentItems` (100,000), `AttrComponent`.
- **Command:** the directory walk takes `.ics`, `.ifb`, `.vcs`, `.vcf`
  and `.vcard`; attachments of type `text/calendar`, `application/ics`,
  `text/x-vcalendar`, `text/vcard`, `text/x-vcard` and `text/directory`
  (and octet-stream ones with those extensions) are read by this
  library, so an invitation's `text/calendar` alternative is a calendar
  child with its blocks. The part's `charset` parameter is passed on as
  `Extractor.Charset` for that child (the set of values with no `CHARSET`
  of their own).
- **Calendars and contacts, after review:**
  - Every parameter of every property is kept in the attributes, under
    `<property>.<parameter>` (`attendee.partstat`, Apple's
    `x-apple-structured-location.x-address`, vCard 4 `adr.label`);
    repeats joined by `\n`. Not kept: `ENCODING`, `CHARSET`, and TYPE
    values written in a card's details line.
  - A grouped property's key keeps its group (`item1.x-abrelatednames`).
    A property whose key would be `component`, `children`, `cards` or
    `timezones` is under `property.` and its key.
  - The heading is the first non-empty `SUMMARY`.
  - A component whose only text is its HTML description gets its details
    paragraph (`Attachments: (text/html)`) with its attributes and
    `Attrs["children"]`.
  - `N` is split at `;` and `,` before unescaping (`O\,Brien` is
    `O,Brien`).
  - A quoted-printable soft break does not continue onto a `BEGIN:` line
    or the `END:` of an open component (`input.repaired`).
  - A `VCALENDAR` nested in an event counts in the event's
    `MaxComponentItems`; components left open when the file ends inside
    a skipped one are counted as missing their `END`; a control
    character in a component's name is removed before the warning and
    `Attrs["component"]`; TYPE values are collected in linear time.

## v0.2.1 (2026-11-24)

- **go-fast-core v0.1.4** (was v0.1.2): `Document.Text` and `Markdown`
  allocate their result once. The output is unchanged.

### Faster

- **A message's header is read once.** A file was read once to see
  whether it begins with a header field and again for the message; the
  first reading now serves both. Extraction of 7,551 messages takes 5–6%
  less CPU (0.283 to 0.267 s, the mean of six runs with the files in
  memory); within the noise of the whole benchmark run.

- **A message's memory is reused for the next.** The bytes of a message,
  the scratch of its header and the reader that finds the messages of a
  mailbox come from pools and go back when the message is read; the
  body's blocks are built straight into the message's part instead of
  three lists in turn; a paragraph is gathered in one reused buffer; a
  mailbox's `>From ` lines are unquoted in place. On 7,551 messages,
  579 MB allocated becomes 355 MB (312 MB with go-fast-core's next
  `Text`), and with 24 workers the batch takes 0.28–0.30 s instead of
  0.44–0.46 s, for a peak of 35–42 MB instead of 31–35 (three runs each,
  interleaved). One worker: 0.51 s instead of 0.53–0.55 s. Output is byte-identical on
  7,672 messages with three option sets. With `Images: ImagesData`, the
  bytes of a picture are now copied, since they may be the message's.
  The tests run with memory overwritten as it is given back, and fail if
  a document refers to it.

- **Fewer allocations.** A message that begins with a mailbox `From `
  line (as mail exported from a list archive does) no longer takes a
  64 KiB read buffer, nor a mailbox a new one after each message, and the
  values of a header section are copied into one buffer instead of one
  each. On 7,551 messages, 948 MB allocated becomes 670 MB (with the
  reading of the files),
  and with 24 workers the batch takes 0.38 s instead of 0.48 s. Output
  is byte-identical on those messages and the corpus's; a test checks
  that header values sharing a buffer cannot overwrite each other.
- **`GOGC` documented** (README): mail makes a small heap, so Go's
  collector runs very often; `GOGC=200` reads the batch with 24 workers
  in 0.15 s for a peak of 65 MB (32 MB at the default). The choice of
  memory for speed is the user's.

## v0.2.0 (2026-11-23)

The character set of text that declares none is recognised where it is
certain; control characters removed are reported; the command reads more
attachments, with go-fast-html v0.3.0 and go-fast-rtf v0.2.0. (An earlier
version of this file dated these changes v0.1.4; no such version was
tagged.)

- **Text that declares no character set** (a body with no `charset`,
  header text outside encoded words, an Outlook string with no code
  page), or declares US-ASCII, and is not valid UTF-8 was read with its
  bytes outside ASCII as U+FFFD. It is now read in the set its bytes are
  certainly in (Shift_JIS, EUC-KR, GBK, Big5, windows-1251, windows-1252,
  UTF-16 without a byte order mark, or UTF-8 with damaged bytes), with the
  warning `text.encoding_detected` (go-fast-core v0.1.2); where no set is
  certain, as before, with `text.unmapped_glyphs` saying how to give it.
  The detection is go-fast-html v0.3.0's, copied.
- **`Extractor.Charset`** (command: `-charset`): the set of such text,
  used instead of the one recognised. The command also reads with it an
  HTML, text or CSV attachment whose set go-fast-html cannot determine
  (`gofasthtml.ErrEncodingUnknown`), which is otherwise listed with
  `child.unsupported`.
- **`text.control_characters_removed`** (info; go-fast-core v0.1.2).
  Control characters with no meaning as text were removed from bodies and
  header values silently; the message's part now carries the warning with
  the count. Tab and line ends are not counted, nor names (file names,
  message ids). The usual source is the terminal colour codes of build
  logs mailed by CI: in the public corpus, a mailing-list archive has
  messages with hundreds of ESC characters. Requires go-fast-core v0.1.2.

- **go-fast-html v0.3.0** (was v0.1.1) and **go-fast-rtf v0.2.0** (was
  v0.1.1) in the command. Their output-cuts, warning-cap and shared-code
  changes, and go-fast-html's encoding detection, reach the documents of
  bodies and attachments they read.
- **New attachment types** read by go-fast-html: `text/csv` and
  `text/tab-separated-values` (a `header=present` parameter gives the
  first row as a header row), `application/x-ipynb+json` (Jupyter
  notebooks) and `application/epub+zip`. An attachment declared
  `application/octet-stream`, or with no type, is read by these types when
  its name ends in `.csv`, `.tsv`, `.tab`, `.ipynb` or `.epub`.
- **Tests.** The worst-case test's time bound is four times longer under
  the race detector, which made one case pass 10 s on a CI runner.

## v0.1.3 (2026-11-22)

Every fixed bound on what goes into the output is reported with
`limit.exceeded` where it cuts, or raised where ordinary mail reaches it
(the series rule "Bounded input" of go-fast-core's README). Output for
input that reaches no bound is unchanged; no file of the corpus changes.

- A part name, made from the subject, is kept to 4,096 bytes, not 256: a
  subject of 90 Chinese characters is 270 bytes and was cut without a
  word. A longer subject gives a name cut on a character boundary and a
  `limit.exceeded`.
- A URL or path of content outside the message is named in its
  `input.external_reference` warning up to 4,096 bytes, not 1,024; past
  that it is cut with a note in the warning. The text of a warning is not
  output, so the cut is not a `limit.exceeded`.
- A character set name longer than 63 bytes, which is not passed on in
  the media type of an embedded document, is reported with
  `limit.exceeded`.
- An Outlook message's compound file is read with up to 262,144
  directory entries, not 65,536 (a message with 10,000 attachments has
  more than 65,536); a file with more is refused with `ErrLimitExceeded`,
  not as damaged.
- In a mailbox, the `Content-Length` of a message (mboxcl2) is looked for
  in a header up to `MaxHeaderBytes`, not 64 KiB, so that a message with
  a longer header is not split at a `From ` line in its body.
- The README lists every fixed bound: those reported, and those on data
  used only inside the library.

## v0.1.2 (2026-11-22)

Fixed bounds that cut or left out content without saying so now say so,
and warnings that could be given without bound are given once per
message. Outputs for input that reaches none of these bounds are
unchanged.

- Built on go-fast-core v0.1.1: a part or document keeps at most 1,000
  warnings, and the rest are counted in one `warnings.omitted` warning.
- The command reads HTML and RTF with go-fast-html v0.1.1 and go-fast-rtf
  v0.1.1, which report their own fixed bounds the same way.
- An attachment's file name (in a message, an Outlook message or a TNEF
  stream) is kept to 4,096 bytes, not 256; a longer one is cut in the
  middle with its extension kept, and the part has one
  `limit.exceeded` with the number of names cut.
- Past 64 parameters in a header, `boundary`, `charset`, `name`,
  `filename`, `format`, `delsp`, `access-type`, `url` and `site` (and
  their RFC 2231 pieces) are still read, to the end of the header; a
  boundary written after the 64th parameter is no longer reported as a
  multipart body with no boundary. The other parameters past the 64th
  are not read, as before, with no warning: this library does not use
  them. RFC 2231 pieces numbered above 999 of the parameters read are
  counted in a `limit.exceeded`.
- An encoded word left as written because its text is longer than 4,096
  bytes or its character set name longer than 100 bytes is reported with
  `limit.exceeded` and a count; a long run with `=?` all through it is
  one cut word per 4,096 bytes, not one per `=?`. A word that begins in
  such a run is decoded as before. In a header's own Content-Type and
  Content-Disposition, read both as a field and for their parameters,
  each such word is counted once.
- The text of headers is decoded as before, with less work: where the
  end of a word is looked for, what was searched for an earlier `=?` is
  not searched again, and where a `=?` is not a word because whitespace
  comes before any `?=`, the search goes on from the first space or tab
  there (no word can begin before it), not 2 bytes on. The work no longer
  grows with the number of `=?` times the bound.
- The parameters of a header are decoded in a fixed order (the plain
  ones in header order, then the pieced ones of RFC 2231 in theirs), so
  the character sets a grouped warning names are the same from run to
  run (they were in map order).
- A mailbox `From ` line longer than 1,024 bytes (without its line
  break) is cut to its first 1,024 bytes, then made text, and reported
  with `limit.exceeded` giving its length in bytes; an image description
  cut to 4,096 bytes is reported with `limit.exceeded`.
- Outlook messages: the entries past the 65,536th of each property
  stream, whose fixed-length values (dates, code pages, attachment
  methods, recipient types, flags) are not read, are reported with one
  `limit.exceeded` per message and their count over all its streams
  (whole entries only); transport headers cut by `MaxHeaderBytes`
  or by the bound of 10,000 fields are reported as the header of a
  message is. Properties read to `MaxMessageBytes` are one
  `limit.exceeded` per message, with their number and sizes (one per
  property before).
- Where the text budget ends at the list of attachments in the headers
  block, the list is left out with `limit.exceeded`, as wherever else
  the budget ends; it was left out without one.
- A URL or path cut to 1,024 bytes in an `input.external_reference`
  message says so.
- `text.unmapped_glyphs` (one per character set before),
  `email.unknown_transfer_encoding`, `input.repaired` for multipart
  bodies without a boundary, without the boundary found or without a
  closing boundary, `input.external_reference` and `email.alternatives`
  are given once per message for each kind, with the number of distinct
  values and the first eight. Values are told apart by their whole text,
  though named cut. One subject with 5,000 unknown character sets gave 5,000
  warnings; it gives one. A value given once keeps the message it had.
  A grouped warning is rewritten only while it is at its place among the
  part's warnings, so a cap on warnings per part leaves it as written.

## v0.1.1 (2026-11-21)

- The command reads HTML and RTF with go-fast-html v0.1.0 and go-fast-rtf
  v0.1.0, in place of the development commits `bad19e7` and `1e98beb`
  that v0.1.0 was tagged with. Since those commits, both libraries
  changed their documentation, took their stored kinds and note types
  from go-fast-core, and moved their own commands to core's render
  flags. The library package is unchanged: it imports only go-fast-core
  and the standard library.

## v0.1.0 (2026-11-21)

The first version.

- Formats: single messages (`.eml`, RFC 5322 and MIME: encoded words,
  RFC 2231 parameters, base64, quoted-printable, every multipart kind,
  `format=flowed`), mailboxes (`.mbox`: mboxo, mboxrd and mboxcl2, read one
  message at a time), Outlook messages (`.msg`, MS-OXMSG, with compressed
  RTF bodies decompressed) and TNEF streams (`winmail.dat`, MS-OXTNEF, in
  a message or as a file). The format is decided from the bytes.
- Every message is a part of kind `message`: subject heading, shown
  headers, text/plain body as paragraphs, quotes and signature, pictures
  shown in the message as image blocks. HTML and RTF bodies, other
  alternatives, attachments and attached messages are embedded documents,
  handed to `Options.Child`; winmail.dat and Outlook messages attached to
  Outlook messages are read by this library.
- Character sets: UTF-8, US-ASCII, ISO-8859-1, -9, -11, -15, windows-874,
  windows-1250 to 1258, Mac OS Roman, UTF-16 in `.msg`, Shift_JIS, GBK and
  GB2312, EUC-KR, Big5, GB18030, EUC-JP and ISO-2022-JP. Others give
  U+FFFD with `text.unmapped_glyphs`.
- Email declares no display of its own: no `Display` reason is reported.
- Limits: core's `MaxInputBytes`, `MaxDecodedBytes`, `MaxTextBytes`,
  `MaxParts` and `MaxDepth`, and `Extractor.MaxMessageBytes`,
  `MaxHeaderBytes`, `MaxMIMEDepth` and `MaxMIMEParts`.
- `cmd/gofastemail` prints JSON, Markdown or text. Its dispatcher reads
  attached messages with this library, HTML, XHTML, Markdown and plain
  text with go-fast-html, and RTF with go-fast-rtf; flags `-format`, `-o`,
  `-parts`, `-password`, `-max-message-bytes`, `-max-decoded-bytes`,
  `-max-text-bytes`, `-quiet`, `-version` and the shared render flags
  `-furniture`, `-stored[=kinds]`, `-part-markers`,
  `-exclude-display[=reasons]` and `-children`.
- The library imports only go-fast-core and the standard library; the
  command adds go-fast-html and go-fast-rtf.
- Pins: go-fast-core v0.1.0 (schema `0.2`); in the command, go-fast-html
  `bad19e7` and go-fast-rtf `1e98beb` (see `go.mod`).

## Before v0.1.0

### Core v0.1.0

- go-fast-core pinned at its v0.1.0 tag. Nothing changes here: since
  `4499357` core changed documentation, and `TextBudget.Take` now takes a
  negative amount as 0, which this library never passes.

### Core 4499357: closed value sets, shared render flags

- go-fast-core pinned at `4499357`. The roles `headers` and `signature`
  and the note types `headers`, `mime_preamble` and `mime_epilogue` are
  now core's (`gofastcore.RoleHeaders`, `RoleSignature`, `NoteHeaders`,
  `NoteMIMEPreamble`, `NoteMIMEEpilogue`); the constants here are the
  same values.
- Command: the render flags are core's `AddRenderFlags`; their names,
  values and errors are unchanged. `-stored` accepts core's
  `StoredKindNames()`, which now include go-fast-html's and
  go-fast-rtf's kinds.

Changes made while the first version was written, newest first. The pins
named are those of the time.

### Core d4cd8e2

- go-fast-core pinned at `d4cd8e2`: `Validate` now rejects an inferred
  `thematic_break`; this library emits only declared ones and passes.

### Newer go-fast-html and go-fast-rtf in the command

- The command pins go-fast-html `bad19e7` (Big5 with HKSCS, `iframe
  srcdoc` read in place) and go-fast-rtf `1e98beb` (encapsulated-HTML
  review fixes, Word's horizontal line as a declared break, no inferred
  breaks), and takes go-fast-rtf's stored kinds from its exported
  `StoredKindNames` instead of naming them itself.

### GB18030, EUC-JP and ISO-2022-JP

- `internal/charset` decodes GB18030 (with four-byte sequences and
  supplementary characters), EUC-JP (with half-width katakana and JIS X
  0212) and ISO-2022-JP as the WHATWG Encoding Standard's decoders do, in
  headers, encoded words, text/plain bodies and `.msg`/TNEF 8-bit strings
  (code pages 54936, 51932, 50220 to 50222). They gave U+FFFD with a
  warning before.
- Tables `gb18030-2.txt`, `gb18030-4.txt`, `jis0208.txt`, `jis0212.txt`
  and their generator `gen/gen_cjk_extra.py`, from Python's codecs,
  parsed on first use.
- GB18030 0xA8BC is U+1E3F and four-byte pointer 7457 U+E7C7, as the
  Encoding Standard has them (the generated table has them swapped).
- `gbk` and `gb2312` stay on code page 936.
- `FuzzDecodeCJK`.

### Stored kinds

- Pins go-fast-core `ebecf3c`, go-fast-html `af97571`, go-fast-rtf
  `259ac69`.
- `-stored[=kinds]`: given alone, as `=true` or `=all`, every kind
  (`Stored`); `=false` or `=` none; `=k1,k2` (comma or `+`) those kinds
  (`StoredKinds`); any other name is an error listing the kinds, exit
  status 2. The kinds are core's, go-fast-html's and go-fast-rtf's
  (`generator`, `custom_property`).
- HTML comments in bodies are now stored `markup_comment` (go-fast-html):
  not in default text, shown with `-stored=markup_comment`.
- HTML encapsulated in an Outlook message's RTF body is read as a
  `body.html` child (go-fast-rtf): `poi-message_1979.msg` and
  `poi-message_1980.msg` give their text from the HTML, with no U+FFFD.

### Double-byte character sets

- `internal/charset` decodes Shift_JIS (cp932), GBK/GB2312 (cp936),
  EUC-KR (cp949) and Big5 (cp950) under their WHATWG labels and `cpNNN`,
  in headers, encoded words, text/plain bodies and `.msg`/TNEF 8-bit
  strings. The decoder and the tables (`internal/charset/tables/cp9NN.txt`,
  identical to go-fast-html's) are copied from go-fast-html `be5c679`.
  `tika-testMSG_chinese.msg` (code page 950) now reads `張毓倫` and
  `格式測試` where it gave U+FFFD. GB18030, ISO-2022-JP, EUC-JP and KOI8
  remain undecoded.
- `TestCharsets` uses `euc-jp`, not `shift_jis`, as its undecoded set.

### The command against real mail

- A `.msg` body's code page (`PR_INTERNET_CPID`, `PR_MESSAGE_CODEPAGE`) is
  passed on by its WHATWG label (`windows-1251`, `shift_jis`, `gbk`,
  `euc-kr`, `big5`, `iso-8859-N`, ...), not as `cpNNN`, which
  go-fast-html did not know. go-fast-html pinned at `be5c679`.

- The command was run over the 121 dev files of the public corpus (eml,
  mbox, msg), text and JSON: every document validates, no child fails,
  the whole run takes well under a second.
- `cmd/gofastemail`: an attachment declared `application/octet-stream`,
  or with no type, is read by its name's extension (`.eml`, `.msg`,
  `.htm(l)`, `.xhtml`, `.txt`, `.md`, `.rtf`). An attached `.eml` and a
  `.txt` in Outlook messages were listed, not read.
- `.msg`: with no `PR_MESSAGE_CODEPAGE` and `PR_INTERNET_CPID` UTF-8, an
  8-bit string that is not valid UTF-8 is read in the ANSI code page of
  `PR_MESSAGE_LOCALE_ID` (POI's `ASCII_UTF-8_CP1252_LCID1031.msg` had
  U+FFFD for `öäü`).
- `text.unmapped_glyphs` for a character set that is not decoded is
  given only when bytes were replaced, not for all-ASCII text.
- New `cmd/gofastemail/corpus_test.go` (`TestCommandCorpus`): the
  command's dispatcher over the dev corpus; fails on a panic, a timeout,
  an invalid document or `child.failed`. Skipped without the corpus.

### Children in the command

- go-fast-core pinned at `787b056` (`RenderOptions.Children`).
- `cmd/gofastemail` reads HTML and RTF bodies and attachments: its
  dispatcher hands `text/html` and `application/xhtml+xml` to go-fast-html
  (`fc557a8`, format `html`), `text/markdown` and `text/plain` to it as
  `md` and `txt`, and `application/rtf` and `text/rtf` to go-fast-rtf
  (`b756076`). Attached messages still go to this library; everything
  else is listed. The library package imports neither; the imports test
  allows them in `cmd/` only.
- A text part's MIME `charset` (or a `.msg` body's `PR_INTERNET_CPID`)
  is passed to go-fast-html as `Extractor.Charset`, so an HTML body in
  windows-1251, Shift_JIS and the like is decoded without a `<meta>`.
- New flag `-children` (default true): Markdown and text include each
  embedded document after a line naming it; `-children=false` leaves them
  out. Previously the command printed the top-level document only.

### Command flags

- `cmd/gofastemail` has the shared render flags of the go-fast commands
  (decided 2026-11-20): `-furniture`, `-stored` and
  `-exclude-display[=reasons]` beside `-part-markers`, with the same
  names, values and help text as the other commands. `-part-markers`
  now has the shared help text. Markdown and text only; JSON is
  unchanged.

### Declared, not judged

- go-fast-core pinned at `05bbddb`: schema `0.2`. `Hidden` is now
  `Display` (JSON `display`), with the reasons `hidden` and `no_ink`;
  `hidden_checked` is gone.
- This library reports no display reason: email declares none of its own.
  Headers, the MIME preamble and the epilogue keep their provenance
  (`Metadata.Extra`, notes of type `headers`, `mime_preamble`,
  `mime_epilogue`) whatever their length.
- Removed: the out-of-proportion rule as applied to headers, preamble and
  epilogue, with its lists of machine headers and its token stripping;
  the separate flagged note of a long header; the `text.unshown_outlier`
  and `text.hidden` warnings; the scan of metadata for invisible
  characters; `HiddenChecked`; the command's `-exclude-hidden` flag.
- Characters are kept as they are (zero-width, tag characters,
  bidirectional controls, variation selectors), as before; a test now says
  so for headers, metadata, mailbox notes and bodies.
- Added `BenchmarkExtract` over the public corpus.

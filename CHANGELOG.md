# Changelog

## v0.1.0 (not yet tagged)

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
- Pins: go-fast-core `4499357` (schema `0.2`); in the command, go-fast-html
  `bad19e7` and go-fast-rtf `1e98beb` (see `go.mod`).

## Before v0.1.0

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

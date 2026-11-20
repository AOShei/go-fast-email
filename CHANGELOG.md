# Changelog

Not yet tagged.

## Unreleased: GB18030, EUC-JP and ISO-2022-JP (branch cjk-more)

- `internal/charset` decodes GB18030 (with four-byte sequences and
  supplementary characters), EUC-JP (with half-width katakana and JIS X
  0212) and ISO-2022-JP as the WHATWG Encoding Standard's decoders do, in
  headers, encoded words, text/plain bodies and `.msg`/TNEF 8-bit strings
  (code pages 54936, 51932, 50220 to 50222). They gave U+FFFD with a
  warning before.
- Tables `gb18030-2.txt`, `gb18030-4.txt`, `jis0208.txt`, `jis0212.txt`
  and their generator `gen/gen_cjk_extra.py`, written by the orchestrator
  from Python's codecs, parsed on first use.
- GB18030 0xA8BC is U+1E3F and four-byte pointer 7457 U+E7C7, as the
  Encoding Standard has them (the generated table has them swapped).
- `gbk` and `gb2312` stay on code page 936.
- `FuzzDecodeCJK`.

## Unreleased: stored kinds (branch stored-kinds)

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

## Unreleased: double-byte character sets (branch dbcs)

- `internal/charset` decodes Shift_JIS (cp932), GBK/GB2312 (cp936),
  EUC-KR (cp949) and Big5 (cp950) under their WHATWG labels and `cpNNN`,
  in headers, encoded words, text/plain bodies and `.msg`/TNEF 8-bit
  strings. The decoder and the tables (`internal/charset/tables/cp9NN.txt`,
  identical to go-fast-html's) are copied from go-fast-html `be5c679`.
  `tika-testMSG_chinese.msg` (code page 950) now reads `張毓倫` and
  `格式測試` where it gave U+FFFD. GB18030, ISO-2022-JP, EUC-JP and KOI8
  remain undecoded.
- `TestCharsets` uses `euc-jp`, not `shift_jis`, as its undecoded set.

## Unreleased: the command against real mail

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

## Unreleased: children in the command

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

## Unreleased: command flags

- `cmd/gofastemail` has the shared render flags of the go-fast commands
  (decided 2026-11-20): `-furniture`, `-stored` and
  `-exclude-display[=reasons]` beside `-part-markers`, with the same
  names, values and help text as the other commands. `-part-markers`
  now has the shared help text. Markdown and text only; JSON is
  unchanged.

## Unreleased: declared, not judged (breaking)

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

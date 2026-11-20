# go-fast-email: implementation plan

Module `github.com/AOShei/go-fast-email`, package `gofastemail`. Extracts
`.eml`, `.mbox` and Outlook `.msg` into `gofastcore.Document` (core pinned at
commit 787b056, schema 0.2). The library: standard library and go-fast-core
only. The command also imports go-fast-html (df14878) and go-fast-rtf
(b756076), to read HTML and RTF bodies and attachments as children.

**Display (declared, not judged; Andrew, 2026-11-20, supersedes runs 3 and
4).** The library reports only what a file declares about display, and
email declares nothing of its own: no `Display` reason anywhere in this
library's output. Headers, preamble and epilogue keep their provenance;
HTML/RTF bodies' extractors report their own format's declarations.
`PR_ATTACHMENT_HIDDEN` sets no reason. No outlier rule, no header lists,
no `HiddenChecked`, no hidden warnings. See
`../HANDOFF_DECLARED_NOT_JUDGED.md` and `../DECLARED_REPORT_EMAIL.md`.

## Mapping (decided in run 1)

- Every message is a part of kind `message`. An `.eml` or `.msg` is one
  document with one part; an `.mbox` is one document with a part per message,
  numbered by its ordinal in the file.
- A message part begins with a `heading` (level 1) holding the subject, then a
  `paragraph` (`Attrs["role"]="headers"`) with the headers a mail client shows
  (From, To, Cc, Bcc, Date), one per line, and an `Attachments:` line with the
  names of the attachments. `Attrs["children"]` on that block lists, comma
  separated, the indexes in `Document.Children` of the documents that came from
  this message (needed in an mbox, where children of all messages share one
  list).
- Every other header (X-*, Received, Reply-To, Message-ID, MIME headers, the
  mbox `From ` line) is, in a mailbox, a `note` block with
  `Attrs["type"]="headers"`, with no display reason.
- For a single message, all headers are also in `Metadata.Extra` (lowercase
  names, repeated headers joined with `\n`); `Title` is the subject, `Authors`
  the From header verbatim, `Created` the Date.
- Body: text/plain becomes blocks (paragraphs split on blank lines, line
  breaks kept as hard breaks `\n`, format=flowed joined, `>` lines as `quote`
  blocks with their depth, the signature after `-- ` as paragraphs with
  `Attrs["role"]="signature"`). text/html, every attachment, every embedded
  message and every non-chosen alternative go through `opts.ExtractChild`.
- multipart/alternative: the first text/plain alternative gives the blocks;
  the others are children; warning `email.alternatives` (severity warning).
- MIME preamble and epilogue text (never shown) is a `note` block of type
  `mime_preamble` / `mime_epilogue`, with no display reason.

- For a single message (.eml, .msg) the headers that are not shown go to
  `Metadata.Extra` only (no block). In an mbox they are the `note` block above,
  since the metadata is the file's.

## Notes for later runs

- The Edit tool turns `\uXXXX` written in a new string into the literal
  character. Write invisible characters in Go source as `\U0000XXXX`, and run
  the hygiene grep before each commit:
  `grep -rnP '[\p{Cf}\p{Co}\p{Zl}\p{Zp}\x{FB00}-\x{FB06}\x{00}-\x{08}\x{0B}-\x{1F}\x{7F}-\x{9F}]|(?![ ])\p{Zs}' --include=*.go .`
- `sed -i` and `cp` from sibling repositories are refused; write files with
  Edit.

## Phases

| Phase | Delivers | Status |
|---|---|---|
| 1. eml | charset, header/encoded-word/RFC 2231 parsing, transfer decoding, multipart walk, text/plain blocks, children, limits, tests, fuzz | done |
| 2. mbox | separator scan (`From ` after a blank line), mboxrd unquoting, Content-Length (mboxcl2), part range, MaxParts, tests, fuzz | done |
| 3. msg | cfb copied from go-fast-ooxml; property streams; recipients; attachments by value, embedded messages, OLE listed; PR_RTF_COMPRESSED (MS-OXRTFCP) to `application/rtf` child; PR_HTML child; tests, fuzz | done (named properties `__nameid_version1.0` and multi-valued properties not read; RTF CRC not checked) |
| 4. cmd | `cmd/gofastemail` printing JSON/Markdown/text like gofastooxml | done |
| 5. README | what is read, mapping, warnings, what the file declares, limits, what is not read | done |
| 6. run 2 | image blocks for pictures shown in a message; TNEF (winmail.dat) read as an embedded document of format `tnef` (internal/tnef); `input.external_reference` for message/external-body; base64 without a copy of the input; imports, source-hygiene and API-snapshot tests | done |
| 7. run 3: hidden signal | outlier flagging of unshown data (superseded by 9) | removed |
| 8. run 4: core outlier rule, corpus | core's outlier rule and machine-header exemptions (superseded by 9); `corpus_test.go` over dev/heldout eml, mbox, msg; compound files with damaged short streams read with `input.repaired` (kept) | partly removed |
| review-1 | independent review: decode budget for every property read, header joins, encoded-word search bound, mbox Content-Length scan, cfb child index, ligatures in header notes | done |
| 9. declared, not judged | core 05bbddb (schema 0.2); no display reason; outlier rule, header lists, token stripping, `HiddenChecked`, metadata scan, `-exclude-hidden` removed; R6 test; corpus fails on any reason; `BenchmarkExtract` | done |
| 10. command flags | `-furniture`, `-stored`, `-exclude-display` added beside `-part-markers`, as `COMMAND_FLAGS.md` (decided 2026-11-20); `TestDisplayFlag`, `TestRenderFlags` | done |
| 11. children | core 787b056 (`RenderOptions.Children`); command dispatcher reads HTML/XHTML/Markdown/plain text with go-fast-html df14878 and RTF with go-fast-rtf b756076; `-children` (default true); imports test allows them in `cmd/` only; `TestBodiesReadAsChildren`, `TestChildrenFlag` | done |
| 12. next | see below | todo |

## Next (in order)

1. Check against real mail: messages from Thunderbird, Outlook, Gmail and
   Apple Mail exports, a real .msg and a real winmail.dat (synthetic
   fixtures only so far; the TNEF property encoding was written from
   [MS-OXTNEF] as remembered, with no copy of the spec at hand).
2. Run 3 added windows-874, 1250-1258 and Mac Roman (tables copied from
   go-fast-rtf, verified there against Python's codecs), and ISO-8859-9/-11
   by the WHATWG aliases. Still not decoded: KOI8-R/U and ISO-8859-2 to -8,
   -10, -13, -14, -16. Generate them from the WHATWG indexes or Unicode
   mapping files, and verify byte for byte (as go-fast-rtf did), with
   provenance. Until then they become U+FFFD with a warning.
3. `.msg` and TNEF: named properties (`__nameid_version1.0`; named entries
   of TNEF property lists), for categories and meeting details (location,
   start, end of an IPM.Appointment / meeting request).
4. Memory: a text/plain body is held a few times over while it is decoded
   (bytes, string, cleaned string, blocks); with the default 256 MiB
   message limit that can reach about 1 GiB for one hostile message. Run 2
   removed the cost per line (textbody had 48 bytes per line break);
   what is left is a constant factor. Decode in a streaming pass, or lower
   the default MaxMessageBytes.
5. A benchmark on a large mailbox.
6. uuencoded attachments inside text/plain bodies (`begin 644 name`):
   today their lines are paragraph text.

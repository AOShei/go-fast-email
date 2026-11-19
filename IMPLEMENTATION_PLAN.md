# go-fast-email: implementation plan

Module `github.com/AOShei/go-fast-email`, package `gofastemail`. Extracts
`.eml`, `.mbox` and Outlook `.msg` into `gofastcore.Document` (core pinned at
commit 031432a). Standard library and go-fast modules only.

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
  mbox `From ` line) is a `note` block with `Attrs["type"]="headers"`, flagged
  `invisible`: a mail client stores it and does not show it.
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
  `mime_preamble` / `mime_epilogue`, flagged `invisible`.

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
| 5. README | what is read, mapping, warnings, hidden reasons, limits, what is not read | done |
| 6. next | see below | todo |

## Next (in order)

1. Check against real mail: messages from Thunderbird, Outlook, Gmail and
   Apple Mail exports, and a real .msg (synthetic fixtures only so far).
2. `.msg`: named properties (`__nameid_version1.0`), so that properties such
   as categories can be read; PR_BODY_HTML as PT_STRING8 with its code page.
3. Inline images in HTML bodies: an `image` block per `image/*` part
   (today they are only children).
4. More single-byte character sets (KOI8-R, windows-1250/1251, ISO-8859-2),
   generated from the Unicode mapping files with provenance.
5. Tests: an API snapshot and an imports test as go-fast-ooxml has; a
   benchmark on a large mailbox.

# go-fast-email

Extracts email (`.eml`, `.mbox`, Outlook `.msg`) into the shared go-fast
document model, [`gofastcore.Document`](https://github.com/AOShei/go-fast-core).
Standard library and go-fast modules only.

**Status: first version, not tagged.** The plan and what is left are in
[`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md).

| Format | State |
|---|---|
| `.eml` (RFC 5322, MIME) | Headers with RFC 2047 encoded words and RFC 2231 parameters; base64, quoted-printable, 7bit, 8bit, binary; every multipart kind; text/plain bodies as paragraphs, quotes and signature; HTML bodies, alternatives, attachments and attached messages handed to the caller's extractor |
| `.mbox` | mboxo, mboxrd and mboxcl2; one part per message, read one message at a time |
| `.msg` (MS-OXMSG) | Subject, sender, recipients, dates, transport headers, plain body; HTML body and compressed RTF body (MS-OXRTFCP) handed to the caller's extractor; attachments by value, attached messages (read recursively), OLE objects and attachments by reference listed |

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
begins with a header field is a message.

**Extractors do not import each other.** Every attachment, every attached
message, and every HTML or RTF body is handed to `Options.Child` through
`opts.ExtractChild`, and the result is appended to `Document.Children`.
Without a `Child` function each is listed with its name and size and a
`child.not_extracted` warning. The media type given to the dispatcher is
the part's `Content-Type` value; for text it carries the charset parameter
(`text/html; charset=iso-8859-1`), which the child's extractor needs. An
Outlook message attached to an Outlook message is a storage of the same
file, not a file of its own, so it is read by this library, through
`ExtractChild` all the same (media type `application/vnd.ms-outlook`), so
that the depth limit and the budgets apply.

`cmd/gofastemail` prints JSON, Markdown or text:

```
go run ./cmd/gofastemail -format markdown message.eml
```

It extracts attached messages and lists every other attachment.

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
   every other header (and the `From ` line), flagged `invisible`.
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
  marked hidden (pictures shown in the body), are `image` blocks at their
  place, unless `Options.Images` is `ImagesNone`. The block's text is the
  `Content-Description`, `Attrs["name"]` the file name, `Image.ID`
  `sha256-` and 24 hex digits of the bytes' SHA-256, the pixel size read
  from the header of a PNG, JPEG or GIF, and with `ImagesData` the bytes.
  Each is an embedded document as well.
- **message/rfc822** and every other part go to `ExtractChild`, named by
  their `filename` or `name` parameter.
- **Outlook:** `PR_BODY` gives the blocks; `PR_HTML` (as `text/html`, with
  the charset of `PR_INTERNET_CPID`) and `PR_RTF_COMPRESSED`, decompressed,
  (as `application/rtf`) go to `ExtractChild`, with `email.alternatives`
  when there is more than one.

### Character sets

UTF-8, US-ASCII, ISO-8859-1, ISO-8859-15 and windows-1252 are decoded, and
UTF-16 in `.msg` files. ISO-8859-1 is decoded as windows-1252, as the WHATWG
Encoding Standard does. Text labelled US-ASCII, or not labelled, that is
valid UTF-8 is read as UTF-8. Every other character set is not guessed at:
bytes outside ASCII become U+FFFD and the part has a `text.unmapped_glyphs`
warning that names the set. The tables' sources are in [`NOTICE`](NOTICE).

## Hidden content

| What | How it is flagged |
|---|---|
| Headers a mail program does not show (`X-*`, `Received`, `Reply-To`, `Message-ID`, the MIME headers) | Single message: `Metadata.Extra`. Mailbox: a `note` of type `headers`, `invisible` |
| The preamble and epilogue of a multipart body, which no mail program shows | `note` of type `mime_preamble` / `mime_epilogue`, `invisible` |
| An alternative body that is not the text/plain one | an embedded document, and `email.alternatives` on the part |
| An Outlook attachment marked hidden (`PR_ATTACHMENT_HIDDEN`) | extracted as an embedded document, not named in the `Attachments:` line |
| Invisible characters anywhere in block text | `invisible_chars` (core's scan); in metadata, a document-level `text.hidden` |

`HiddenChecked` is `invisible+invisible_chars`. The `invisible` check covers
what the message format itself says is not shown; whatever is hidden inside
an HTML or RTF body (`display:none`, white text) is for that body's
extractor to flag. A mail program that shows the HTML alternative shows
none of the text/plain blocks; that is reported by `email.alternatives`,
not by a flag.

## Warnings

| Code | When |
|---|---|
| `email.alternatives` (warning) | The message has alternative bodies |
| `email.unknown_transfer_encoding` (warning) | A part declares a transfer encoding other than base64, quoted-printable, 7bit, 8bit or binary; its bytes are taken as they are |
| `text.unmapped_glyphs` | A character set not decoded, or bytes not valid in the one declared |
| `input.repaired` | A header section ended by a line that is not a header; a multipart body without closing boundary or without any boundary; base64 with characters outside its alphabet; an unreadable compressed RTF body |
| `input.external_reference` | An Outlook attachment by reference names a file outside the message; a `message/external-body` part names content elsewhere (its URL or file is in the message; nothing is fetched) |
| `text.hidden` | See above |
| `limit.exceeded` (error) | Any limit below |
| `part.no_text` | A message with no text at all |

## Limits

| Limit | Default | What happens |
|---|---|---|
| `Limits.MaxInputBytes` | 16 GiB | `ErrLimitExceeded` |
| `Limits.MaxDecodedBytes` | 2 GiB, shared with embedded documents | Later parts are listed, not read |
| `Limits.MaxTextBytes` | core's 256 MiB, shared with embedded documents; each message of a mailbox takes `BlockCost` | The part is cut; later messages of a mailbox are not read |
| `Limits.MaxParts` | 1,000,000 messages in a mailbox | The first are read |
| `Limits.MaxDepth` | core's 4 | Deeper documents are listed |
| `Extractor.MaxMessageBytes` | 256 MiB held in memory for one message | The message, or an Outlook property, is read to that length |
| `Extractor.MaxHeaderBytes` | 1 MiB per header section | The section is cut |
| `Extractor.MaxMIMEDepth` | 32 nested multiparts | Deeper ones are not read |
| `Extractor.MaxMIMEParts` | 10,000 MIME parts per message | The rest are not read |

A message is held in memory, with at most one decoded part beside it. A
mailbox is scanned a line at a time and read one message at a time.
Encoded words, parameters, recipients and attachments of an Outlook
message (10,000) are bounded too.

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
- Character sets other than those above, including all multi-byte ones
  other than UTF-8 and UTF-16.
- Byte offsets (`Loc`) for blocks: bodies are decoded before they become
  blocks, so offsets into the file would not be meaningful.

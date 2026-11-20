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
   every other header (and the `From ` line), not flagged.
   In any message, a header out of proportion (see Hidden content) is a
   `note` of its own, flagged.
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
- **winmail.dat** (`application/ms-tnef`, or a part of that name, that
  begins with the TNEF signature; [MS-OXTNEF]) is where Outlook puts a
  message's RTF body and its attachments. It is read by this library,
  through `ExtractChild` all the same (so the depth limit and budgets
  apply), into an embedded document of format `tnef`: one `message`
  part with the subject as heading and the plain body (`PR_BODY`, or the
  old `attBody` in its code page) as blocks; the HTML and compressed RTF
  bodies, the attachments and attached messages (read recursively) as its
  own embedded documents, as for an Outlook message. Named and
  multi-valued properties are not read.
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
valid UTF-8 is read as UTF-8. Every other character set is not guessed at:
bytes outside ASCII become U+FFFD and the part has a `text.unmapped_glyphs`
warning that names the set. The tables' sources are in [`NOTICE`](NOTICE).

## Hidden content

The hidden flag is a signal of suspicious hiding, as go-fast-core defines
it ("What earns a flag"): data every message keeps and no mail program
shows is extracted and bounded, and not flagged, since a flag on it would
be set on clean and poisoned mail alike. Such data is flagged only where
one item is out of proportion for its kind, by go-fast-core's shared rule
`gofastcore.UnshownOutlier`: more than 30 words or more than 300
characters other than white space (so text without spaces, Chinese or
Japanese, and encoded payloads such as base64 count by their length).
The item is then a `note` flagged `invisible`, and the part has core's
`text.unshown_outlier` warning naming what was found.

Exceptions, so that ordinary delivered mail trips nothing:

- **Machine headers.** Signature, trace and filter headers are long by
  nature: `DKIM-Signature` and `ARC-Message-Signature` carry a signature
  of about 350 characters, Exchange's `X-Microsoft-Antispam-Message-Info`
  and `X-MS-Exchange-AntiSpam-MessageData-0` are kilobytes of base64, a
  Postfix `Received` header's TLS comment is 20 words. For these the rule
  is applied to each comment in parentheses and to the rest separately,
  with machine tokens left out: a token of more than 40 bytes, or one that
  holds `=`, `@`, `;`, `:` or `/`. Prose in them still counts by words and
  characters. The headers, by name: `Received`, `X-Received`,
  `Received-SPF`, `DKIM-Signature`, `DomainKey-Signature`, `ARC-Seal`,
  `ARC-Message-Signature`, `ARC-Authentication-Results`,
  `Authentication-Results` (and `-Original`), `References`,
  `In-Reply-To`, `Autocrypt`, `List-Unsubscribe`, `msip_labels`,
  `Thread-Index`, `X-Instance-Identity`, `X-ASF-DKIM-Sig`,
  `X-Authority-Analysis`, `X-SG-EID`, `X-SG-ID`, `X-CMAE-Envelope`; and
  by vendor prefix: `X-MS-`, `X-Microsoft-`, `X-Forefront-`,
  `X-Exchange-` (Microsoft), `X-Gm-`, `X-Google-` (Gmail), `X-Spam-`
  (SpamAssassin and other filters), `X-Proofpoint-`, `X-IronPort-`,
  `X-Barracuda-`, `X-Mimecast-`, `X-YMail-`, `X-Yahoo-`, `X-QQ-`. Any
  other header, an unknown `X-` header included, is held to the full rule.
- **Preamble and epilogue.** Links, addresses and rules of punctuation
  are left out first, so the footer a mailing list appends after the
  closing boundary of a signed message is routine. An encoded payload
  there still counts by its length.

| What | How it is extracted | Flagged |
|---|---|---|
| Headers a mail program does not show (`X-*`, `Received`, `Reply-To`, `Message-ID`, the MIME headers, Outlook's transport headers) | Single message: `Metadata.Extra`. Mailbox: a `note` of type `headers` | No. An outlier header: also a `note` of its own, `invisible`, and `text.unshown_outlier` |
| The preamble and epilogue of a multipart body ("This is a multi-part message in MIME format.") | `note` of type `mime_preamble` / `mime_epilogue` | No. An outlier: `invisible`, and `text.unshown_outlier` |
| An alternative body that is not the text/plain one | an embedded document, and `email.alternatives` on the part | No (its own extractor flags what it hides) |
| An Outlook attachment marked hidden (`PR_ATTACHMENT_HIDDEN`) | extracted as an embedded document, not named in the `Attachments:` line | No |
| Invisible characters anywhere in block text | as text | `invisible_chars` (core's scan); in metadata, a document-level `text.hidden` |

`HiddenChecked` is `invisible+invisible_chars`. The `invisible` check is
the outlier rule above over all unshown data of the message format; whatever is hidden inside
an HTML or RTF body (`display:none`, white text) is for that body's
extractor to flag. A mail program that shows the HTML alternative shows
none of the text/plain blocks; that is reported by `email.alternatives`,
not by a flag.

## Warnings

| Code | When |
|---|---|
| `email.alternatives` (warning) | The message has alternative bodies |
| `text.unshown_outlier` (warning, core's) | A header, preamble or epilogue that no mail program shows is out of proportion by core's rule (see Hidden content); it is a note flagged `invisible` |
| `email.unknown_transfer_encoding` (warning) | A part declares a transfer encoding other than base64, quoted-printable, 7bit, 8bit or binary; its bytes are taken as they are |
| `text.unmapped_glyphs` | A character set not decoded, or bytes not valid in the one declared |
| `input.repaired` | A header section ended by a line that is not a header; a multipart body without closing boundary or without any boundary; base64 with characters outside its alphabet; an unreadable compressed RTF body; a damaged TNEF stream (cut short, a wrong checksum, an unreadable property list); an Outlook compound file whose short streams cannot be read (the properties in them are missing; severity error) |
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

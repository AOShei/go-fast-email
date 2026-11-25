// Package gofastemail extracts email into the shared go-fast document
// model, gofastcore.Document.
//
// It reads a single message (.eml: RFC 5322 with MIME), a mailbox (.mbox:
// mboxo, mboxrd and mboxcl2), an Outlook message (.msg: MS-OXMSG) and a
// TNEF stream (winmail.dat: MS-OXTNEF). Their gofastcore.Source.Format is
// eml, mbox, msg and tnef. The format is decided from the bytes, never
// from a name: a compound file is an Outlook message, a file that begins
// with an mbox "From " line is a mailbox, a file that begins with the TNEF
// signature is a winmail.dat, and a file that begins with a header field
// is a message.
//
// It also reads calendars (format ics: RFC 5545 and vCalendar 1.0) and
// contact lists (format vcf: RFC 6350, 2426 and vCard 2.1): a file that
// begins, after a byte order mark and white space, with BEGIN:VCALENDAR
// (or a component a calendar holds) or BEGIN:VCARD, tested before the
// test for a message. Each is one part
// of kind body: a heading, a paragraph of details and paragraphs of
// description per event, to-do or card, with every other property in the
// attributes of its first block, and the byte span it came from and its
// UID on every block.
//
// Every message is a part of kind message: a .eml, .msg or winmail.dat is
// a document of one part, a mailbox has a part for each message. A part
// begins with a heading holding the subject and a paragraph with the
// headers a mail program shows; in a mailbox every other header is a note.
// For a single message the headers are in Metadata.Extra. The text/plain
// body (or an Outlook message's PR_BODY) becomes paragraphs, quotes and the
// signature; pictures shown in the message are image blocks, unless
// Options.Images is gofastcore.ImagesNone.
//
// HTML and RTF bodies, other alternatives, attachments and attached
// messages are handed to Options.Child through Options.ExtractChild and
// appended to Document.Children; this package reads none of those formats
// itself. Without a Child function each is listed with a
// child.not_extracted warning, except a winmail.dat inside a message (and
// a message attached inside one) and an Outlook message attached to an
// Outlook message, which this package reads whether or not there is a
// Child function (still through ExtractChild, so the depth limit and the
// budgets apply); what they hold goes to the caller's Child in turn.
//
// Character sets decoded: UTF-8, US-ASCII, ISO-8859-1, -9, -11 and -15,
// windows-874 and windows-1250 to 1258, Mac OS Roman, UTF-16 in .msg files,
// Shift_JIS, GBK and GB2312, EUC-KR, Big5, GB18030, EUC-JP and
// ISO-2022-JP. Any other set is not guessed at: bytes outside ASCII become
// U+FFFD with a text.unmapped_glyphs warning.
//
// The limits are those of gofastcore.Limits (MaxInputBytes, MaxDecodedBytes,
// MaxTextBytes, MaxParts for the messages of a mailbox, MaxDepth) and the
// fields of Extractor (MaxMessageBytes, MaxHeaderBytes, MaxMIMEDepth,
// MaxMIMEParts). Where one is reached the file is read as far as it goes
// and a warning says so; a file larger than MaxInputBytes is an error
// (gofastcore.ErrLimitExceeded).
//
// The package imports only go-fast-core and the standard library. The
// command cmd/gofastemail adds go-fast-html and go-fast-rtf to read HTML,
// plain text, Markdown and RTF bodies and attachments as children.
package gofastemail

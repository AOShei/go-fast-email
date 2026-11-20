# Changelog

Not yet tagged.

## Unreleased: children in the command

- go-fast-core pinned at `787b056` (`RenderOptions.Children`).
- `cmd/gofastemail` reads HTML and RTF bodies and attachments: its
  dispatcher hands `text/html` and `application/xhtml+xml` to go-fast-html
  (`df14878`, format `html`), `text/markdown` and `text/plain` to it as
  `md` and `txt`, and `application/rtf` and `text/rtf` to go-fast-rtf
  (`b756076`). Attached messages still go to this library; everything
  else is listed. The library package imports neither; the imports test
  allows them in `cmd/` only.
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

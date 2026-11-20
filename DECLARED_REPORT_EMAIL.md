# Declared, not judged: go-fast-email

The go-fast-email part of `HANDOFF_DECLARED_NOT_JUDGED.md`, following `DECLARED_HANDOFF_EMAIL.md` and `DECLARED_REPORT_CORE.md`.

This file belongs at the workspace root as `DECLARED_REPORT_EMAIL.md`. Writing outside the library's directory was refused, so it is committed in the repository instead. Please move it.

## 1. Branch and core

- **Branch:** `declared-not-judged` in go-fast-email, from `review-1` (`4a85cf7`). Local commits only, with no push and no pull request:
  - `46ed9e8`: `BenchmarkExtract`, added before the change so that it gives the "before" numbers.
  - `3335459`: the change itself.
  - `de51105`: `go mod tidy`.
  - `b915a95`: README, plan and changelog.
  - The commit that adds this report.
- **Core:** pins go-fast-core **`05bbddb`** (schema 0.2).
- **review-1:** I read the reviewer's eight commits (`c0eb83c`..`4a85cf7`) before starting and kept all their hostile-input fixes. One review test, `TestHeaderNotesExpandLigatures`, lost its outlier-note case, because that note no longer exists. Its mailbox-note case stays.

## 2. Declarations reported (as in the README)

**None.** Email declares no display reason of its own, so nothing in this library's output carries one:

- Headers a mail program does not show, the MIME preamble and the epilogue are not unshown content. They keep their provenance: headers in `Metadata.Extra`, or in a mailbox a `note` of type `headers`; the preamble and epilogue as notes of type `mime_preamble` and `mime_epilogue`. This holds whatever their length.
- A text/plain body declares nothing about display.
- HTML and RTF bodies, and every attachment, go to `ExtractChild`. Their extractor reports what their own format declares.
- `PR_ATTACHMENT_HIDDEN` sets no reason. The attachment is extracted and left off the `Attachments:` line, as before.
- The README states that colour, size, position, overlap and the length of unshown data are not judged, and that characters are kept. It has one sentence saying threat detection is outside the libraries.

## 3. What was removed

**Removed:**
- The outlier rule as this library applied it, and the separate flagged note it made for a long header.
- The machine-header name list (22 names) and vendor-prefix list (15 prefixes).
- The machine-token and link stripping, and the comment splitting for `Received`-style headers.
- The preamble and epilogue filtering.
- The warnings `text.unshown_outlier` and `text.hidden`.
- The scan of metadata for invisible characters, `HiddenChecked`, and the command's `-exclude-hidden` flag. Nothing the command extracts can carry a reason. (The command now has `-exclude-display` with the other shared flags; see "Follow-up decisions".)

**Removed tests:** `TestUnshownOutliers` and `TestUnshownOutliersByLength`, and the old `TestHiddenInHeaders`, which now only tests control characters.

**Rewritten tests:**
- The clean-message test is kept as `TestCleanMessageHasNoReason`. It covers Gmail and Exchange header sets, a mailbox and a `.msg`, and asserts no reason and no hidden warning.
- New `TestLongUnshownDataHasNoReason` replaces the outlier cases. A 36-word header, 320 characters of base64, 320 CJK characters, and a long preamble and epilogue are all extracted with their provenance, with no reason and no warning.
- The corpus test now counts files and blocks by display reason, and fails on any reason.

**Size:** non-test Go code lost about 247 lines and gained 34; test code lost 105 and gained 150 (the new tests). The README section with the header list was replaced by a shorter "What the file declares". `CHANGELOG.md` is new.

## 4. Corpus, before (`review-1`) and after

| Set | Files | Before: files / blocks with a reason | After |
|---|---|---|---|
| dev/eml | 80 | 1 / 1 `invisible` | 0 / 0 |
| dev/mbox | 8 | 0 / 0 | 0 / 0 |
| dev/msg | 33 | 0 / 0 | 0 / 0 |
| heldout/eml | 19 | 0 / 0 | 0 / 0 |
| heldout/mbox | 4 | 0 / 0 | 0 / 0 |
| heldout/msg | 11 | 0 / 0 | 0 / 0 |

- The one dev flag before was `sa-easyham-01558` (`X-Spamadvice`, about 60 words of legal text). That file is still in the corpus and is no longer flagged.
- Warnings `text.hidden=1` and `text.unshown_outlier=1` are gone. Every other warning count is unchanged.
- No panics, Validate failures, timeouts or errors before or after.

## 5. Benchmark

`BenchmarkExtract` measures wall-clock time only: this machine has no instruction counter. The figures are the least `ns/op` of 5 runs of 1 s each.

| File | Before | After |
|---|---|---|
| dev mbox (`apache-users-tomcat-2025-09.mbox`, 1.0 MB) | 11.30 ms, 91,499 allocs | 7.58 ms, 58,793 allocs (−33%) |
| `.eml` (`tika-testEML_embedded_xhtml_and_img.eml`) | 23.7 µs, 329 allocs | 24.8 µs, 314 allocs (noise) |
| `.msg` (`tika-test-outlook2003.msg`) | 176 µs | 168 µs (noise) |

The mailbox gain comes from two removals: the outlier pass over every mailbox header note, and core's per-part invisible-character scan.

## 6. R6 (characters kept)

`TestCharactersKept` in `hidden_test.go` checks this. Its input has tag characters (U+E0049..U+E007F), U+202E/U+202C, five U+200B and variation selectors. The test confirms they survive unchanged, with no reason and no warning, in:
- a header value in `Metadata.Extra`;
- the subject (`Metadata.Title` and the heading);
- an encoded-word `From` (`Metadata.Authors` and the headers block);
- a mailbox's headers note;
- a plain-text body (as a single message and in a mailbox).

No cleaning step of this library strips them: `clean` removes only Cc control characters.

## 7. What did not fit, for Andrew to decide

I set no reason for either case below. Each is a fact the format states, but none switches a piece of text off:

1. **multipart/alternative.** RFC 2046 says a client shows the last alternative it can display, normally the HTML. So when the text/plain alternative gives the blocks, the format implies it is the one not shown. Today this is reported only by the `email.alternatives` warning and its message, with no reason.
2. **Outlook's native body (`PidTagNativeBody`, 0x1016).** This property says which of the plain, RTF and HTML bodies Outlook shows. The others are declared copies that are not shown. This library does not read it. Should a body that is not native carry `hidden`, or should a warning just name the native body?
3. **`PidTagAttachFlags` `attRenderedInBody` and `PidTagRenderingPosition`.** These state that an attachment is drawn in the body, at a position. They are about display, but not about content being switched off. Not read.

There is no other .msg or TNEF property I know of that hides part of a body. Hidden recipients (Bcc) are a header and are extracted.

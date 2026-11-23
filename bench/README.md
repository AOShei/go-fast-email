# Benchmarks against the tools in use today

go-fast-email is measured against the email parsers that data and AI pipelines use:
- from Python, the standard library's `email` package, mail-parser, eml-parser, and Kreuzberg (which has a Rust core);
- from Rust, the `mail-parser` crate.

The tables are in [`comparison.md`](comparison.md). This file says what they show and how to make them again.

## What the comparison shows

The input is 7,551 messages, 56 MB:
- two months of eight Apache mailing lists, as their archives export them;
- the SpamAssassin public corpus (2002 to 2005), which brings HTML bodies, attachments and old character sets.

The go-fast-email figures are for main after #21 (commit 6e85ee0); its output is byte-identical to v0.2.0's.

- **Speed with one worker.** go-fast-email reads every message in 0.49 s.
  - The Rust `mail-parser` crate is faster (0.23 s).
  - Every Python tool is 10 to 35 times slower: mail-parser 4.9 s, `email` 6.0 s, Kreuzberg 6.2 s, eml-parser 16.9 s.
  - Kreuzberg's own CPU time was 2.1 s, but it waits inside some calls: one message took 4.3 s.
- **Speed with 24 workers.**
  - go-fast-email takes 0.42 s at Go's default `GOGC`.
  - The Rust crate takes 0.06 s, Kreuzberg 0.34 s and Python mail-parser 0.47 s.
  - Messages are small, so the heap stays a few MB and Go's garbage collector runs almost 500 times in the batch. With 24 workers its pauses, not the reading, take most of the time.
  - With `GOGC=200` the batch takes 0.16 s with 63 MB at peak, instead of 33 MB. The table is below and in the library's README.
  - The library keeps Go's default: trading memory for speed is the caller's choice.
- **Memory.** go-fast-email peaks at 22 MB with one worker and 33 MB with 24. Over 24 workers the Python tools use 0.6 to 1.8 GB.
- **Accuracy.** The test set is the 6,411 messages whose preferred body is plain text that Python's `email` decodes without error.
  - The score is the share of that body's words in a tool's text. Precision says little here, since the tools add headers.
  - go-fast-email, `email` and eml-parser score 1.0000, Python mail-parser 0.9998, the Rust crate 0.9974 and Kreuzberg 0.9892.
  - `email` is where the reference body comes from, so its own score is 1 by construction.
- **Failures.** None for go-fast-email or any other tool except Python's `email`, which raised on 25 messages.

## `GOGC`

| `GOGC` | One worker | Peak | 24 workers | Peak |
|---|---|---|---|---|
| 100 (default) | 0.49 s | 22 MB | 0.42 s | 33 MB |
| 200 | 0.43 s | 24 MB | 0.16 s | 63 MB |
| 400 | 0.41 s | 33 MB | 0.12 s | 125 MB |

## How it is measured

- Each tool reads every message in one long-lived process, as a pipeline does, first with one worker and then with 24.
  - The time of a message is the call alone, timed after its bytes are read.
  - Wall time, CPU time and peak memory are those of the whole process tree.
- The machine is shared, so `measure.py` waits for it to be quiet before each run.
  - A run is repeated if other processes used it meanwhile.
  - The tables mark a run that stayed disturbed after five attempts.
- Tools that leave an HTML body as HTML have it turned into text by selectolax, the fastest HTML tool in go-fast-html's benchmark.

## Reproducing

The Python tools are pinned in `requirements-text.txt`, the same environment as go-fast-html's benchmark. With `DATA` a directory outside the repository:

```sh
# Messages: eight Apache lists, two months each, and the SpamAssassin corpus.
mkdir -p $DATA/mbox $DATA/sa
for l in airflow-dev camel-users cassandra-user flink-user kafka-users spark-dev tika-dev tomcat-users; do
  for m in 2025-09 2025-10; do
    curl -o $DATA/mbox/$l-$m.mbox "https://lists.apache.org/api/mbox.lua?list=${l#*-}&domain=${l%%-*}.apache.org&d=$m"
  done
done
for f in 20030228_easy_ham 20030228_hard_ham 20030228_spam 20050311_spam_2; do
  curl https://spamassassin.apache.org/old/publiccorpus/$f.tar.bz2 | tar -xj -C $DATA/sa
done
# CORPUS: files to leave out, compared by SHA-256 (an empty directory if none).
python -I bench/make_batch.py $DATA $CORPUS           # pages/*.eml, all.list, manifest.tsv

# Tools.
(cd bench/godoc && go build -o godoc .)
(cd bench/rsdoc && CARGO_TARGET_DIR=../../../bench-env/rust-target cargo build --release)
BENCH_ENV=../bench-env bench/run_all_doc.sh email $DATA/all.list $DATA/results-email \
  go-fast stdlib-email mail-parser eml-parser kreuzberg rs-mail-parser > $DATA/run.log
for g in 100 200 400; do for w in 1 24; do
  GOGC=$g python -I bench/measure.py $DATA/gogc/email/gogc$g-$w/measure.json -- \
    bench/godoc/godoc email $DATA/all.list $DATA/gogc/email/gogc$g-$w $w
done; done

# Accuracy, and the report.
cp $DATA/all.list $DATA/email.list
python -I bench/score_email.py $DATA/email.list $DATA/results-email $DATA/scores-email.json
python -I bench/report_doc.py email $DATA $DATA/gogc > bench/comparison.md
```

`run_doc.py`, `run_all_doc.sh`, `report_doc.py` and `rsdoc` also serve the go-fast-rtf and go-fast-odf benchmarks; `godoc` here runs go-fast-email alone.

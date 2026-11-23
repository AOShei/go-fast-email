"""Build the email batch: Apache list mboxes split into .eml files, plus the
SpamAssassin public corpus. Files whose SHA-256 matches a file of the
go-fast corpus are left out (the held-out set is compared by hash only).

    python -I make_batch.py BENCH_EMAIL_DIR CORPUS_DIR
"""
import hashlib, mailbox, os, sys

root, corpus = sys.argv[1], sys.argv[2]
seen = set()
for d, _, fs in os.walk(corpus):
    for f in fs:
        seen.add(hashlib.sha256(open(os.path.join(d, f), "rb").read()).hexdigest())
out = os.path.join(root, "pages")
os.makedirs(out, exist_ok=True)
rows, skipped = [], 0

def put(name, data, source):
    global skipped
    h = hashlib.sha256(data).hexdigest()
    if h in seen:
        skipped += 1
        return
    seen.add(h)
    p = os.path.abspath(os.path.join(out, name + ".eml"))
    open(p, "wb").write(data)
    rows.append((p, source, len(data)))

for f in sorted(os.listdir(os.path.join(root, "mbox"))):
    box = mailbox.mbox(os.path.join(root, "mbox", f), create=False)
    for i, key in enumerate(box.iterkeys()):
        put(f"{f[:-5]}-{i:04d}", box.get_bytes(key), "apache-lists")
for d in sorted(os.listdir(os.path.join(root, "sa"))):
    p = os.path.join(root, "sa", d)
    if not os.path.isdir(p):
        continue
    for f in sorted(os.listdir(p)):
        if f == "cmds":
            continue
        put(f"sa-{d}-{f.split('.')[0]}", open(os.path.join(p, f), "rb").read(), "spamassassin-" + d)

with open(os.path.join(root, "manifest.tsv"), "w") as m:
    for p, s, n in rows:
        m.write(f"{os.path.basename(p)}\t{s}\t{n}\n")
with open(os.path.join(root, "all.list"), "w") as m:
    for p, _, _ in rows:
        m.write(p + "\n")
print(len(rows), "files,", sum(r[2] for r in rows) / 2**20, "MB;", skipped, "matched the corpus and were left out")

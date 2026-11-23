"""Score the email tools: for each message whose body (the part Python's
email package prefers, policy.default) is text/plain and decodes without
error, the share of its words that a tool's text holds (recall; the tools
add headers, so precision says little). Also, per tool, the messages that
failed and those that gave no text.

    python -I score_email.py LIST RESULTS_DIR OUT.json
"""
import collections, email, email.policy, json, os, re, sys

LIST, R, OUT = sys.argv[1:4]
W = re.compile(r"\w+", re.UNICODE)


def words(s):
    return collections.Counter(w.lower() for w in W.findall(s))


refs = {}
for path in (l.strip() for l in open(LIST) if l.strip()):
    try:
        m = email.message_from_bytes(open(path, "rb").read(), policy=email.policy.default)
        b = m.get_body(preferencelist=("plain", "html"))
        if b is None or b.get_content_type() != "text/plain":
            continue
        c = b.get_content()
    except Exception:
        continue
    if "�" in c:
        continue  # the reference itself could not decode it
    w = words(c)
    if sum(w.values()) >= 5:
        refs[os.path.basename(path)] = w

scores = {}
for tool in sorted(os.listdir(R)):
    if not tool.endswith("-1"):
        continue
    res = [json.loads(l) for l in open(os.path.join(R, tool, "results.jsonl"))]
    failed = sum(1 for r in res if r["err"])
    empty = sum(1 for r in res if not r["err"] and r["chars"] == 0)
    got = tot = 0
    for name, ref in refs.items():
        p = os.path.join(R, tool, "text", name + ".txt")
        t = words(open(p, encoding="utf-8", errors="replace").read()) if os.path.exists(p) else collections.Counter()
        got += sum((ref & t).values())
        tot += sum(ref.values())
    scores[tool[:-2]] = {"files": len(res), "failed": failed, "empty": empty,
                         "plain_bodies": len(refs), "body_recall": got / tot if tot else None}
    print(tool[:-2], scores[tool[:-2]])
json.dump(scores, open(OUT, "w"), indent=1)

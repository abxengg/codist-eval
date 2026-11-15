"""Is a buggy line more surprising to the model than the line it replaced?

Buggy code tends to be less "natural" to a language model than correct code (Ray et al., "On the Naturalness of Buggy
Code", 2015). If that holds for a local model on our planted bugs, Codist could use it to rank lines and files
cheaply. Ollama reports probabilities only for words the model writes itself, not for text it is given, so a line's
surprise is measured by walking through it: at each step the model is shown the code so far and asked for its 20
likeliest next pieces; the piece the line really continues with costs its -log probability, and a piece outside the
20 costs a fixed penalty. The average over the line is its surprise.

    python3 surprise.py <repo> <mutants.json> [model]

mutants.json is what `go run ./mutants -out` writes: for each planted bug the file, line, the line as it was and as
it became. Each line is measured in its file at HEAD, with the 40 lines before it as context.
"""
import json, subprocess, sys, urllib.request, collections

REPO, MUTANTS = sys.argv[1], sys.argv[2]
MODEL = sys.argv[3] if len(sys.argv) > 3 else "qwen3.5:9b"
PENALTY = 12.0  # a piece the model did not have among its 20 likeliest


def next_pieces(prompt):
    body = {"model": MODEL, "prompt": prompt, "raw": True, "stream": False, "think": False, "logprobs": True, "top_logprobs": 20,
            "keep_alive": "30m", "options": {"num_predict": 1, "temperature": 0, "num_ctx": 4096}}
    req = urllib.request.Request("http://localhost:11434/api/generate", json.dumps(body).encode(), {"Content-Type": "application/json"})
    d = json.load(urllib.request.urlopen(req, timeout=300))
    lp = (d.get("logprobs") or [{}])[0]
    return [(c["token"], c["logprob"]) for c in lp.get("top_logprobs", [])]


def surprise(prefix, line):
    done, rest, total, steps = "", line, 0.0, 0
    while rest and steps < 60:
        cands = [(t, p) for t, p in next_pieces(prefix + done) if t and rest.startswith(t)]
        if cands:
            t, p = max(cands, key=lambda c: len(c[0]))
            total += -p
        else:  # not among the 20 likeliest: pay the penalty and move on by one word or one mark
            i = 1
            while i < len(rest) and rest[i].isalnum() == rest[0].isalnum() and not rest[i].isspace():
                i += 1
            t = rest[:i]
            total += PENALTY
        done += t
        rest = rest[len(t):]
        steps += 1
    return total / max(1, steps)


ms = json.load(open(MUTANTS))
files = {}
rows = []
for m in ms:
    path = m["Path"]
    if path not in files:
        files[path] = subprocess.run(["git", "-C", REPO, "show", "HEAD:" + path], capture_output=True, text=True).stdout.split("\n")
    lines = files[path]
    i = m["Line"] - 1
    if lines[i] != m["Was"]:
        continue
    prefix = "\n".join(lines[max(0, i - 40):i]) + "\n"
    was, now = surprise(prefix, m["Was"]), surprise(prefix, m["Now"])
    rows.append((m["Op"], was, now))
    print(f"{m['Op']:11s} was {was:5.2f}  now {now:5.2f}  {'MORE' if now > was else 'less'} surprising   {m['Now'].strip()[:70]}", flush=True)

by = collections.defaultdict(list)
for op, was, now in rows:
    by[op].append(now > was)
    by["all"].append(now > was)
print()
for op, v in sorted(by.items(), key=lambda kv: kv[0] == "all"):
    print(f"{op:11s} the planted line is more surprising than the original in {sum(v)} of {len(v)}")
diff = [now - was for _, was, now in rows]
print(f"mean extra surprise of the planted line: {sum(diff)/max(1,len(diff)):.2f} (per piece, in nats)")

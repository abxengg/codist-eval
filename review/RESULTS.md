# Review measurements

## The 120 development bugs with Codist v0.9.0, 2026-11-18

codist-review v0.13.0 with `qwen3.5:9b`, one reading (`-once`), each bug committed back into its fixed version with a
message that only names the changed files, and an empty store. 119 usable (`retel-io/ari-proxy` is gone from GitHub).

| | bugs of 119 |
|---|---|
| a finding within 3 lines of the bug (located, an upper bound) | 67 |
| named the bug (read against the real fix) | **49** |
| related | 12 |
| right line, wrong reason | 6 |
| named and shown as an alarm, not a note | 28 |

5.2 hours. The old `qwen2.5-coder:14b` run named 51 of 118, but only 25 bugs are named by both; together 75 of 119.
False alarms were not measured (that needs the fixed versions reviewed).

The first try is kept as `results/real-118-qwen3.5-9b-review-v0.13.0-leaky-message.json` and is not comparable: it
committed every bug as "Reintroduce the bug", and Codist reads commit messages. Mockito keeps tests in a top-level
`test/`, which the main-code filter does not skip; 7 located findings were in test files, none decided a label.

Raw results: `results/real-118-qwen3.5-9b-review-v0.13.0.json`, labels `results/labels-qwen3.5-9b-v0.13.0.json`,
logs `results/real-118-run.log` and `results/real-118-retry.log` (26 bugs whose fix diff failed during the first pass,
run again). Rerun with `go run ./review -once -model qwen3.5:9b -out <file> bugs-118.txt` (about 5 hours).

## A second reading, 2026-11-16

`go run ./review` compares one reading of each file with two (`review.Options{Twice: true}`), on the 10 sample bugs
(`../behaviour/bugs.txt`) with `qwen3.5:9b`, 53 minutes: 3 of 10 located with one reading, 3 of 10 with two. No gain, so the
second reading stays for high-risk commits only. The 2026-11-18 run above suggests why: a second model names
different bugs, a second reading by the same model mostly repeats the first
([codist-review#8](https://github.com/abxengg/codist-review/issues/8)).

## Callers as context, 2026-11-07

Does showing the model the code in other files that calls the changed methods help it find bugs?

Same 10 sample bugs as the behaviour experiment (`../behaviour/bugs.txt`), each committed back into its fixed version
and reviewed twice by `qwen2.5-coder:7b`, once without and once with callers (`Reviewer.Callers`). A finding within 3
lines of the bug counts as located (an upper bound).

| | bugs located |
|---|---|
| without callers | 5 of 10 |
| with callers | 4 of 10 |

The one difference (jsoup: located without, no finding with) is within run-to-run variation. No gain, so the option
stays off. Most of these bugs sit inside one method, so the sample may not be where callers matter; Spring service
code, where a changed method breaks its callers, is the place to measure it again.

Raw results: `results/callers-7b-2026-11-07.json`. Rerun with `go run ./review -model qwen2.5-coder:7b ../behaviour/bugs.txt`.

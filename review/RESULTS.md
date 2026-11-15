# Review measurements

## A second reading, not measured yet

`go run ./review` now compares one reading of each file with two (`review.Options{Twice: true}`, codist-review
v0.11.0), on the same bugs. The callers option it used to compare was removed from codist-review after the result
below. Run it on the 10-bug sample before trusting the second reading: about an hour with `qwen2.5-coder:7b`.

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

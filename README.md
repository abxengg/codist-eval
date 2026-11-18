# codist-eval

Measurements for [Codist](https://github.com/abxengg/codist): what its review and its ideas actually catch, on real
bugs from open-source Java projects.

## behaviour: does running the code catch bugs?

The question: if Codist runs the methods a commit changed, on the code before and after, with the same inputs, how
often does a real bug show a difference, and how often does a harmless change show one?

- `changed/` lists the methods a commit changed and the project methods that call them, using
  [codist-java](https://github.com/abxengg/codist-java).
- `harness/Harness.java`, written once and never by the model, calls them on both versions in separate class
  loaders and compares what a caller can see: return value, exception, and arguments after the call.
- Inputs are **A**, standard edge cases per type, or **B**, plain values the local model suggests after reading the
  commit.

Results so far, in [behaviour/RESULTS.md](behaviour/RESULTS.md): on 10 benchmark bugs, A shows the real difference in
4 and A with B in 5; on 22 refactoring commits that should not change behaviour, neither shows a false alarm.

### Run it

Needs Java 21, Maven, Go, git and Python 3; B also needs a model server such as Ollama. The benchmark repositories
are cloned to `~/.cache/codist-bench/repos` (as the Codist benchmark does).

```bash
cd behaviour
./setup.sh bugs.txt          # check out and compile each commit and its parent, list changed methods
./run.sh A bugs.txt          # edge cases
LIST=bugs.txt python3 suggest.py && ./run.sh B bugs.txt   # the model's values
```

`select_refactors.py` picks the refactoring commits; `refactors-labels.txt` is how each was labelled by hand before
any result.

## review: what the review finds on real bugs

`go run ./review` takes a list of real bugs (`bugs-118.txt`: the 120 development bugs of the Defects4J and
GitBug-Java benchmark Codist used before, one line each: id, clone folder, fixed commit). For each it checks out the
fixed commit, reverses the fix in the main code, commits it with a neutral message ("Update <files>"), and reviews it
from an empty store. A finding within 3 lines of a line the bug adds counts as located; whether it **names** the bug
is labelled by hand. `-once` reviews once; without it, one reading is compared with two. A stopped run carries on.

```bash
go run ./review -once -model qwen3.5:9b -out results/run.json review/bugs-118.txt
```

Result with Codist v0.9.0 and `qwen3.5:9b`, 2026-11-18: of 119 bugs, a finding near 67, **the bug named in 49**, a
related point in 12, the right line for the wrong reason in 6; 28 of the 49 shown as alarms. `qwen2.5-coder:14b`
named 51 of 118 in 2026-11-04, but mostly different bugs: 25 both, 75 together. The fixed versions were not reviewed,
so there is no false-alarm count yet ([#3](https://github.com/abxengg/codist-eval/issues/3)). Results and labels:
[review/results](review/results), notes in [review/RESULTS.md](review/RESULTS.md).

## testbed: test projects with planted bugs

Each script builds a small project commit by commit, with bugs planted in some commits and clean commits (a feature,
a fix, a refactor) in between. Every commit compiles. The answer key is written here, outside the project, so
Codist never sees it.

| Script | Project | Planted bugs | Clean commits |
|---|---|---|---|
| `make_records_lab.py` | [records-lab](https://github.com/AbiXnash/records-lab): a Spring Boot gateway serving an employee directory as JSON, XML, CSV and GraphQL over JDBC | 19 | 5 |
| `make_shop_lab.py` | [shop-lab](https://github.com/AbiXnash/shop-lab): two Spring Boot services (orders, payments) with Feign, Redis, Spring Batch and JDBC | 24 | 6 |
| `make_maze_lab.py` | [maze-lab](https://github.com/AbiXnash/maze-lab): Glowmaze, an original maze-chase game in Java 21 Swing, written to a design spec | 19 | 3 |

Bugs come in kinds: known Java mistakes, logic slips, ones that break the project's written rules, and ones only
careful reading finds. `score_shop_lab.py` scores a running Codist against any answer key: a bug is found when an
alarm (not a note) lands within 6 lines of it, and a clean commit fails if it gets any alarm.

```bash
python3 testbed/make_maze_lab.py ~/dev/maze-lab          # build the project
codist ~/dev/maze-lab                                    # let Codist review every commit
python3 testbed/score_shop_lab.py http://localhost:8080 testbed/maze-lab-truth.json
```

Latest results, Codist v0.8.0 with `qwen3.5:9b` on a 16 GB laptop, each from an empty data folder:

| Project | Found | Clean commits with an alarm |
|---|---|---|
| records-lab | 18 of 19 | 1 of 5 |
| shop-lab | 21 of 24 | 0 of 6 |
| maze-lab | 18 of 19 | 0 of 3 |

Earlier runs with `qwen2.5-coder:14b` are kept as [records-lab-run2.txt](testbed/records-lab-run2.txt) (18 of 19),
[shop-lab-run3.txt](testbed/shop-lab-run3.txt) (20 of 24) and [maze-lab-run2.txt](testbed/maze-lab-run2.txt)
(15 of 19). The changed numbers the game's spec states, its misses then, are now caught without the model. `triage/` holds the experiment behind the feedback triage model.

## Licence

Source available under the [PolyForm Noncommercial License 1.0.0](LICENSE): free for any noncommercial purpose, such
as personal study, research, hobby projects and testing, and for use by charities, schools, public research and
public health bodies and government institutions. **Using it for a business, or in anything sold, needs a commercial
licence** from the author: open an issue at [github.com/abxengg/codist](https://github.com/abxengg/codist/issues)
to ask. Third-party code included in Codist keeps its own licence.

Required Notice: Copyright 2026 Abinash (https://github.com/AbiXnash)

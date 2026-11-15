// mutants plants small bugs in a repository's own code, one commit each in a scratch copy, and counts what Codist
// catches: by running the code, by its checks, and (with -model) by the model. It answers "how much of what goes
// wrong here would Codist see?" for one repository, without waiting for real bugs. Planted bugs are simpler than real
// ones, so the numbers are an upper bound on recall, and they say nothing about false alarms.
//
//	go run ./mutants -repo ~/dev/some-repo -n 40 [-model qwen2.5-coder:14b] [-out results.json]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	behaviour "github.com/abxengg/codist-behaviour"
	git "github.com/abxengg/codist-git"
	model "github.com/abxengg/codist-model"
	review "github.com/abxengg/codist-review"
	"github.com/abxengg/codist-review/store"
)

// operator is one kind of planted bug: a pattern on a line of code and what to put in its place.
type operator struct {
	name string
	re   *regexp.Regexp
	to   func(m []string) string
}

var operators = []operator{
	{"boundary", regexp.MustCompile(` (<=|>=|<|>) `), func(m []string) string {
		return " " + map[string]string{"<=": "<", ">=": ">", "<": "<=", ">": ">="}[m[1]] + " "
	}},
	{"equality", regexp.MustCompile(` (==|!=) `), func(m []string) string { return " " + map[string]string{"==": "!=", "!=": "=="}[m[1]] + " " }},
	{"logic", regexp.MustCompile(` (&&|\|\|) `), func(m []string) string { return " " + map[string]string{"&&": "||", "||": "&&"}[m[1]] + " " }},
	{"arithmetic", regexp.MustCompile(` ([-+*]) `), func(m []string) string { return " " + map[string]string{"+": "-", "-": "+", "*": "/"}[m[1]] + " " }},
	{"number", regexp.MustCompile(`\b([2-9]|[1-9]\d{1,3})\b`), func(m []string) string {
		var n int
		fmt.Sscan(m[1], &n)
		return fmt.Sprint(n + 1)
	}},
	{"limit", regexp.MustCompile(`Math\.(max|min)\(`), func(m []string) string { return "Math." + map[string]string{"max": "min", "min": "max"}[m[1]] + "(" }},
	{"return", regexp.MustCompile(`return (true|false);`), func(m []string) string {
		return "return " + map[string]string{"true": "false", "false": "true"}[m[1]] + ";"
	}},
}

type mutant struct {
	Op, Path, Was, Now, Message string
	Line                        int
	Tidy                        bool // the commit message says the change keeps results
	Built                       bool
	Differ                      int      // calls whose result changed when the code was run
	Caught                      []string // what raised an issue: check, spec, run, model
}

func main() {
	repoDir := flag.String("repo", "", "the repository to plant bugs in (a scratch copy is used)")
	n := flag.Int("n", 30, "how many bugs to plant")
	modelName := flag.String("model", "", "also have this model review each planted bug")
	url := flag.String("url", "http://localhost:11434", "the model server")
	seed := flag.Int64("seed", 1, "which bugs are picked")
	out := flag.String("out", "", "write every planted bug and what caught it as JSON")
	learnMode := flag.Bool("learn", false, "measure learned checks: for each kind of bug, plant one and fix it so Codist learns a check, then plant others of that kind and count what the check catches without the model")
	flag.Parse()
	if os.Getenv("CODIST_LOG") == "debug" { // show why a check was or was not learned
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	if *repoDir == "" {
		fmt.Fprintln(os.Stderr, "name a repository with -repo")
		os.Exit(2)
	}
	work, err := os.MkdirTemp("", "codist-mutants-")
	check(err)
	defer os.RemoveAll(work)
	scratch := filepath.Join(work, "repo")
	run("", "git", "clone", "-q", *repoDir, scratch)
	repo, err := git.Open(scratch)
	check(err)
	defer repo.Close()
	st, err := store.Open(filepath.Join(work, "db"), "mutants")
	check(err)
	defer st.Close()
	rev := &review.Reviewer{Repo: repo, Store: st, MaxFiles: 8, Contracts: true}
	if rules, err := os.ReadFile(filepath.Join(scratch, ".codist", "rules.md")); err == nil {
		rev.Rules = string(rules)
	}
	if *modelName != "" {
		rev.Model = model.New(*url, *modelName, 8192)
	}
	runner := &behaviour.Runner{Repo: repo, Work: filepath.Join(work, "behaviour")}

	mutants := pick(repo, rand.New(rand.NewSource(*seed)), *n)
	if *learnMode {
		if rev.Model == nil {
			fmt.Fprintln(os.Stderr, "-learn needs -model: the model writes the checks")
			os.Exit(2)
		}
		learnRun(rev, repo, scratch, st, mutants)
		return
	}
	fmt.Printf("%d bugs planted in %s\n", len(mutants), *repoDir)
	for i := range mutants {
		m := &mutants[i]
		full := filepath.Join(scratch, filepath.FromSlash(m.Path))
		src, err := os.ReadFile(full)
		check(err)
		lines := strings.Split(string(src), "\n")
		lines[m.Line-1] = m.Now
		check(os.WriteFile(full, []byte(strings.Join(lines, "\n")), 0o644))
		run(scratch, "git", "-c", "user.name=Mutant", "-c", "user.email=mutant@codist", "commit", "-q", "-am", m.Message)
		sha := repo.Head()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		rep := runner.Run(ctx, sha)
		m.Built, m.Differ = rep.Status == "done", rep.Differ
		raw, _ := json.Marshal(rep)
		if rev.Model != nil {
			raw = rev.Intent(ctx, sha, raw)
		}
		check(st.SetBehaviour(sha, raw))
		if err := rev.Review(ctx, sha, review.Options{RulesOnly: rev.Model == nil}); err != nil {
			fmt.Fprintln(os.Stderr, "review:", err)
		}
		cancel()
		seen := map[string]bool{}
		for _, f := range st.Findings(sha) {
			if f.Doubt != "" || f.Path != m.Path || f.Line < m.Line-6 || f.Line > m.Line+6 {
				continue
			}
			by := "check"
			switch {
			case f.Source == "model":
				by = "model"
			case f.Rule == "behaviour-change":
				by = "run"
			case f.Rule == "spec-number":
				by = "spec"
			}
			if !seen[by] {
				seen[by] = true
				m.Caught = append(m.Caught, by)
			}
		}
		sort.Strings(m.Caught)
		fmt.Printf("%3d %-10s %-28s line %-4d tidy=%-5v built=%-5v differ=%-3d caught=%v\n", i+1, m.Op, filepath.Base(m.Path), m.Line, m.Tidy, m.Built, m.Differ, m.Caught)
		run(scratch, "git", "reset", "-q", "--hard", "HEAD~1")
	}
	report(mutants, rev.Model != nil)
	if *out != "" {
		raw, _ := json.MarshalIndent(mutants, "", " ")
		check(os.WriteFile(*out, raw, 0o644))
	}
}

// pick chooses up to n lines of main code to change, spread over the operators.
func pick(repo *git.Repo, rnd *rand.Rand, n int) []mutant {
	files, err := repo.Files("HEAD")
	check(err)
	var paths []string
	for _, p := range files {
		if strings.HasSuffix(p, ".java") && strings.Contains(p, "src/main/") {
			paths = append(paths, p)
		}
	}
	byOp := map[string][]mutant{}
	for p, src := range repo.ReadAll("HEAD", paths) {
		code := git.CodeLines(src)
		for i, l := range strings.Split(string(src), "\n") {
			t := strings.TrimSpace(l)
			// Only lines that do something: no strings (texts are not logic), no declarations of the class or its
			// constants, no annotations.
			if i >= len(code) || !code[i] || strings.Contains(l, `"`) || strings.HasPrefix(t, "@") || strings.Contains(t, " class ") ||
				strings.Contains(t, "static final") || strings.HasPrefix(t, "for (") {
				continue
			}
			for _, op := range operators {
				loc := op.re.FindStringSubmatchIndex(l)
				if loc == nil {
					continue
				}
				m := op.re.FindStringSubmatch(l)
				byOp[op.name] = append(byOp[op.name], mutant{Op: op.name, Path: p, Line: i + 1, Was: l, Now: l[:loc[0]] + op.to(m) + l[loc[1]:]})
			}
		}
	}
	var out []mutant
	names := make([]string, 0, len(byOp))
	for name := range byOp {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names { // in a fixed order, so the same seed plants the same bugs
		ms := byOp[name]
		sort.Slice(ms, func(i, j int) bool {
			return ms[i].Path < ms[j].Path || ms[i].Path == ms[j].Path && ms[i].Line < ms[j].Line
		})
		rnd.Shuffle(len(ms), func(i, j int) { ms[i], ms[j] = ms[j], ms[i] })
	}
	for round := 0; len(out) < n; round++ {
		added := false
		for _, name := range names {
			if round < len(byOp[name]) && len(out) < n {
				m := byOp[name][round]
				class := strings.TrimSuffix(filepath.Base(m.Path), ".java")
				// Half the commits say they only tidy, half say nothing about results: a real bug comes with either.
				if m.Tidy = len(out)%2 == 0; m.Tidy {
					m.Message = "Simplify " + class
				} else {
					m.Message = "Update " + class
				}
				out = append(out, m)
				added = true
			}
		}
		if !added {
			break
		}
	}
	return out
}

func report(ms []mutant, withModel bool) {
	type row struct{ planted, built, differ, issue, run, check, model int }
	rows := map[string]*row{}
	names := []string{}
	add := func(key string, m mutant) {
		r := rows[key]
		if r == nil {
			r = &row{}
			rows[key] = r
			names = append(names, key)
		}
		r.planted++
		if m.Built {
			r.built++
		}
		if m.Differ > 0 {
			r.differ++
		}
		if len(m.Caught) > 0 {
			r.issue++
		}
		for _, c := range m.Caught {
			switch c {
			case "run":
				r.run++
			case "model":
				r.model++
			default:
				r.check++
			}
		}
	}
	for _, m := range ms {
		add(m.Op, m)
	}
	for _, m := range ms {
		add(map[bool]string{true: "message: tidy", false: "message: neutral"}[m.Tidy], m)
	}
	for _, m := range ms {
		add("all", m)
	}
	fmt.Printf("\n%-18s %8s %8s %14s %12s %10s %10s", "", "planted", "ran", "run differed", "issue raised", "by run", "by check")
	if withModel {
		fmt.Printf(" %10s", "by model")
	}
	fmt.Println()
	for _, k := range names {
		r := rows[k]
		fmt.Printf("%-18s %8d %8d %14d %12d %10d %10d", k, r.planted, r.built, r.differ, r.issue, r.run, r.check)
		if withModel {
			fmt.Printf(" %10d", r.model)
		}
		fmt.Println()
	}
}

func run(dir, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v: %s\n", name, args, out)
		os.Exit(1)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

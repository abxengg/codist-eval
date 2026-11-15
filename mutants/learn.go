package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	git "github.com/abxengg/codist-git"
	review "github.com/abxengg/codist-review"
	"github.com/abxengg/codist-review/store"
)

// learnRun measures learned checks. For each kind of planted bug, the first of its kind is planted and then fixed
// in a commit whose message says so; reviewing that fix with the model may teach a check. Then up to three other
// bugs of the same kind are planted elsewhere, one at a time, and reviewed with the checks only: a learned check
// that reports the planted line catches it. At the end the learned checks are run over the untouched code: every
// line they match there is a false alarm.
func learnRun(rev *review.Reviewer, repo *git.Repo, scratch string, st *store.Store, ms []mutant) {
	byOp := map[string][]mutant{}
	var ops []string
	for _, m := range ms {
		if len(byOp[m.Op]) == 0 {
			ops = append(ops, m.Op)
		}
		byOp[m.Op] = append(byOp[m.Op], m)
	}
	plant := func(m mutant, msg string) {
		full := filepath.Join(scratch, filepath.FromSlash(m.Path))
		src, err := os.ReadFile(full)
		check(err)
		lines := strings.Split(string(src), "\n")
		lines[m.Line-1] = m.Now
		check(os.WriteFile(full, []byte(strings.Join(lines, "\n")), 0o644))
		run(scratch, "git", "-c", "user.name=Mutant", "-c", "user.email=mutant@codist", "commit", "-q", "-am", msg)
	}
	fmt.Printf("%-11s %-8s %-50s %s\n", "kind", "learned", "pattern", "others caught")
	totalOthers, totalCaught, learnedKinds := 0, 0, 0
	for _, op := range ops {
		group := byOp[op]
		if len(group) < 2 {
			continue
		}
		teacher := group[0]
		class := strings.TrimSuffix(filepath.Base(teacher.Path), ".java")
		before := len(st.Learned())
		plant(teacher, "Update "+class)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		_ = rev.Review(ctx, repo.Head(), review.Options{RulesOnly: true})
		run(scratch, "git", "-c", "user.name=Mutant", "-c", "user.email=mutant@codist", "revert", "--no-edit", "HEAD")
		run(scratch, "git", "-c", "user.name=Mutant", "-c", "user.email=mutant@codist", "commit", "-q", "--amend", "-m", "Fix "+class)
		if err := rev.Review(ctx, repo.Head(), review.Options{}); err != nil {
			fmt.Fprintln(os.Stderr, "review:", err)
		}
		cancel()
		learned := st.Learned()
		pattern := "-"
		if len(learned) > before {
			pattern = learned[len(learned)-1].Pattern
			learnedKinds++
		}
		caught, others := 0, 0
		for _, m := range group[1:min(4, len(group))] {
			if m.Path == teacher.Path && m.Line == teacher.Line {
				continue
			}
			others++
			plant(m, "Update "+strings.TrimSuffix(filepath.Base(m.Path), ".java"))
			_ = rev.Review(context.Background(), repo.Head(), review.Options{RulesOnly: true})
			for _, f := range st.Findings(repo.Head()) {
				if strings.HasPrefix(f.Rule, "learned-") && f.Path == m.Path && f.Line == m.Line {
					caught++
					break
				}
			}
			run(scratch, "git", "reset", "-q", "--hard", "HEAD~1")
		}
		totalOthers += others
		totalCaught += caught
		fmt.Printf("%-11s %-8v %-50s %d of %d\n", op, pattern != "-", cut(pattern, 50), caught, others)
	}
	// False alarms: what the learned checks match in the code as it is.
	falseHits := 0
	files, _ := repo.Files("HEAD")
	for _, lc := range st.Learned() {
		n := 0
		for _, p := range files {
			if !strings.HasSuffix(p, ".java") || !strings.Contains(p, "src/main/") {
				continue
			}
			src, _ := repo.FileAt("HEAD", p)
			n += countMatches(lc.Pattern, string(src))
		}
		falseHits += n
		fmt.Printf("  check %s %q matches %d lines of the clean code\n", lc.ID, lc.Pattern, n)
	}
	fmt.Printf("\nkinds with a learned check: %d of %d; other bugs of those kinds caught without the model: %d of %d; lines of clean code matched: %d\n",
		learnedKinds, len(ops), totalCaught, totalOthers, falseHits)
}

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

package main

import (
	"regexp"
	"strings"
)

// countMatches counts the code lines (not comments or blank) a pattern matches.
func countMatches(pattern, src string) int {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range strings.Split(src, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") {
			continue
		}
		if re.MatchString(l) {
			n++
		}
	}
	return n
}

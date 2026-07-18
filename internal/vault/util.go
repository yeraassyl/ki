// Package vault owns the on-disk shape of a ki vault: one directory per
// bucket, a single newest-first log.md of one-liners, and one file per dump.
// Everything here is deterministic parsing and placement; the LLM's output
// enters only as pre-validated content (a title and a list of steps).
package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slug converts text into a filename-safe slug (max ~50 chars).
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = strings.Trim(s[:50], "-")
	}
	if s == "" {
		s = "dump"
	}
	return s
}

// FirstLine returns the first non-empty line of s, trimmed, with any markdown
// header marks or list bullets stripped (it is used as a title fallback).
func FirstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimSpace(ln)
		t = strings.TrimLeft(t, "#")
		t = strings.TrimPrefix(strings.TrimSpace(t), "- ")
		if t = strings.TrimSpace(t); t != "" {
			return t
		}
	}
	return "dump"
}

func uniquePath(dir, base string) string {
	p := filepath.Join(dir, base+".md")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	for i := 2; ; i++ {
		p := filepath.Join(dir, fmt.Sprintf("%s-%d.md", base, i))
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
}

// splitLines splits file data into lines without dropping content; a trailing
// newline yields a final empty element so joinLines can round-trip the file.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(string(data), "\n")
}

// joinLines is the inverse of splitLines, normalizing to a trailing newline.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	s := strings.Join(lines, "\n")
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

// tickLine flips one checkbox line from open to done, preserving every other
// byte of the line.
func tickLine(lines []string, i int) error {
	if i < 0 || i >= len(lines) {
		return fmt.Errorf("line %d out of range", i)
	}
	if !strings.HasPrefix(lines[i], "- [ ] ") {
		return fmt.Errorf("line %d is not an open checkbox: %q", i, lines[i])
	}
	lines[i] = "- [x] " + lines[i][len("- [ ] "):]
	return nil
}

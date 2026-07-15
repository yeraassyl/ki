// Package store holds the artifact models and the deterministic rendering of the
// thread index. It reads thread/standalone frontmatter and projects _index.md
// from it, so the index is a pure function of the ki root's contents.
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"ki/internal/config"
	"ki/internal/fm"
)

// Thread is an in-progress or completed artifact thread.
type Thread struct {
	Name        string
	Status      string
	Updated     string
	Hook        string
	HookDerived bool // true when Hook came from ## State, not a `hook:` field
	Jira        string
	Branch      string
	Repo        string
	Tags        []string
}

// Standalone is a one-off root artifact file.
type Standalone struct {
	File   string
	Title  string
	Date   string
	Type   string
	Status string
}

var (
	standaloneRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_.*\.md$`)
	wsRe         = regexp.MustCompile(`\s+`)
)

// ScanThreads reads every thread directory's permanent.md. Directories starting
// with `_` or `.`, and any without a permanent.md, are skipped.
func ScanThreads(c config.Config) ([]Thread, error) {
	entries, err := os.ReadDir(c.ThreadsPath())
	if err != nil {
		return nil, err
	}
	var out []Thread
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.ThreadsPath(), e.Name(), "permanent.md"))
		if err != nil {
			continue
		}
		f, body := fm.Parse(data)
		t := Thread{
			Name:    firstNonEmpty(f.Get("thread"), e.Name()),
			Status:  firstNonEmpty(f.Get("status"), "in-progress"),
			Updated: f.Get("updated"),
			Jira:    f.Get("jira"),
			Branch:  f.Get("branch"),
			Repo:    f.Get("repo"),
			Tags:    f.List("tags"),
		}
		if h := f.Get("hook"); h != "" {
			t.Hook = h
		} else {
			t.Hook = DeriveHook(body)
			t.HookDerived = true
		}
		out = append(out, t)
	}
	return out, nil
}

// ScanStandalones reads the dated root artifact files.
func ScanStandalones(c config.Config) ([]Standalone, error) {
	entries, err := os.ReadDir(c.ThreadsPath())
	if err != nil {
		return nil, err
	}
	var out []Standalone
	for _, e := range entries {
		if e.IsDir() || !standaloneRe.MatchString(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.ThreadsPath(), e.Name()))
		if err != nil {
			continue
		}
		f, _ := fm.Parse(data)
		out = append(out, Standalone{
			File:   e.Name(),
			Title:  firstNonEmpty(f.Get("title"), strings.TrimSuffix(e.Name(), ".md")),
			Date:   f.Get("date"),
			Type:   f.Get("type"),
			Status: f.Get("status"),
		})
	}
	return out, nil
}

// CountArchive returns the number of markdown files in _archive/.
func CountArchive(c config.Config) int {
	entries, err := os.ReadDir(c.ArchivePath())
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

// CountDerivedHooks reports how many threads had their hook derived rather than
// read from a `hook:` field (a signal that rewriting the index would be lossy).
func CountDerivedHooks(ts []Thread) int {
	n := 0
	for _, t := range ts {
		if t.HookDerived {
			n++
		}
	}
	return n
}

// DeriveHook extracts a one-line hook from a permanent.md body's ## State
// section: the first non-empty paragraph, whitespace-collapsed and truncated.
func DeriveHook(body string) string {
	inState := false
	var buf []string
	for _, ln := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "## ") {
			if inState {
				break
			}
			if strings.EqualFold(trimmed, "## State") {
				inState = true
			}
			continue
		}
		if inState {
			if trimmed == "" {
				if len(buf) > 0 {
					break
				}
				continue
			}
			buf = append(buf, trimmed)
		}
	}
	return truncate(collapseWS(strings.Join(buf, " ")), 160)
}

// RenderIndex projects _index.md from the scanned threads and standalones.
func RenderIndex(threads []Thread, standalones []Standalone, archiveCount int) string {
	inprog := filterStatus(threads, false)
	done := filterStatus(threads, true)
	sortByUpdatedDesc(inprog)
	sortByUpdatedDesc(done)
	sortStandalonesDesc(standalones)

	var b strings.Builder
	b.WriteString("# Agent Artifacts\n\n")
	b.WriteString("One line per thread → its `permanent.md` (the living state, only file `/continue` reads). History in each thread's `changelog.md` and in `_archive/`.\n\n")

	b.WriteString("## In-Progress Threads\n\n")
	if len(inprog) == 0 {
		b.WriteString("_No in-progress threads._\n\n")
	} else {
		for _, t := range inprog {
			b.WriteString(threadLine(t, "updated"))
		}
		b.WriteByte('\n')
	}

	b.WriteString("## Completed Threads\n\n")
	if len(done) == 0 {
		b.WriteString("_None yet._\n\n")
	} else {
		for _, t := range done {
			b.WriteString(threadLine(t, "completed"))
		}
		b.WriteByte('\n')
	}

	b.WriteString("## Standalone\n\n")
	if len(standalones) == 0 {
		b.WriteString("_None yet._\n\n")
	} else {
		for _, s := range standalones {
			b.WriteString(standaloneLine(s))
		}
		b.WriteByte('\n')
	}

	b.WriteString("## Archive\n\n")
	if archiveCount > 0 {
		noun := "artifacts"
		if archiveCount == 1 {
			noun = "artifact"
		}
		fmt.Fprintf(&b, "`_archive/` holds %d retired per-step %s from before the two-file-per-thread migration. Dig there only for a specific superseded detail.\n", archiveCount, noun)
	} else {
		b.WriteString("_Empty._\n")
	}
	return b.String()
}

func threadLine(t Thread, verb string) string {
	return fmt.Sprintf("- [%s](%s/permanent.md) — %s %s · %s · [log](%s/changelog.md)\n",
		t.Name, t.Name, verb, t.Updated, t.Hook, t.Name)
}

func standaloneLine(s Standalone) string {
	if s.Type != "" {
		return fmt.Sprintf("- [%s](%s) — %s\n", s.Title, s.File, s.Type)
	}
	return fmt.Sprintf("- [%s](%s)\n", s.Title, s.File)
}

func isComplete(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "complete")
}

func filterStatus(ts []Thread, complete bool) []Thread {
	var out []Thread
	for _, t := range ts {
		if isComplete(t.Status) == complete {
			out = append(out, t)
		}
	}
	return out
}

func sortByUpdatedDesc(ts []Thread) {
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].Updated > ts[j].Updated })
}

func sortStandalonesDesc(ss []Standalone) {
	sort.SliceStable(ss, func(i, j int) bool { return ss[i].Date > ss[j].Date })
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func collapseWS(s string) string { return strings.TrimSpace(wsRe.ReplaceAllString(s, " ")) }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

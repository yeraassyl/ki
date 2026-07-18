package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// entryRe matches one one-liner log line: `- [ ] YYYY-MM-DD HH:MM text`.
// Lines that don't match are preserved verbatim and ignored by counts, so the
// file stays safe to hand-edit in any editor.
var entryRe = regexp.MustCompile(`^- \[([ xX])\] (\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}) (.+)$`)

// LogEntry is one parsed one-liner. Line indexes into Log.Lines so the entry
// can be ticked in place.
type LogEntry struct {
	Done bool   `json:"done"`
	Date string `json:"date"`
	Time string `json:"time"`
	Text string `json:"text"`
	Line int    `json:"-"`
}

// Log is a bucket's log.md: raw lines plus the parsed entries.
type Log struct {
	Path    string
	Lines   []string
	Entries []LogEntry
}

// LoadLog reads and parses a log file; a missing file is an empty log.
func LoadLog(path string) (Log, error) {
	l := Log{Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return l, err
	}
	l.Lines = splitLines(data)
	l.Entries = parseEntries(l.Lines)
	return l, nil
}

func parseEntries(lines []string) []LogEntry {
	var out []LogEntry
	for i, ln := range lines {
		m := entryRe.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		out = append(out, LogEntry{
			Done: m[1] != " ",
			Date: m[2],
			Time: m[3],
			Text: m[4],
			Line: i,
		})
	}
	return out
}

// Prepend inserts a new open entry at the top (newest first).
func (l *Log) Prepend(date, tm, text string) {
	line := fmt.Sprintf("- [ ] %s %s %s", date, tm, text)
	l.Lines = append([]string{line}, l.Lines...)
	l.Entries = parseEntries(l.Lines)
}

// Tick marks the entry at Lines index i as done.
func (l *Log) Tick(i int) error {
	if err := tickLine(l.Lines, i); err != nil {
		return err
	}
	l.Entries = parseEntries(l.Lines)
	return nil
}

// Save writes the log back to disk, creating the bucket dir as needed.
func (l Log) Save() error {
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(l.Path, []byte(joinLines(l.Lines)), 0o644)
}

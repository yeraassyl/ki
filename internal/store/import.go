package store

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Chunk is one date-scoped slice of an imported file. The chunker is the
// deterministic half of import: date headers and size limits are resolved here,
// while splitting a chunk into discrete items is the classifier's job.
type Chunk struct {
	Date string // YYYY-MM-DD the text was written, "" if unknown
	Text string
}

// maxChunkLen caps how much text one classifier call receives; oversized chunks
// are split at blank-line boundaries.
const maxChunkLen = 8000

var (
	isoHeaderRe = regexp.MustCompile(`^#{0,6}\s*(\d{4}-\d{2}-\d{2})\s*:?\s*$`)
	dmyHeaderRe = regexp.MustCompile(`^#{0,6}\s*(\d{1,2})[./](\d{1,2})[./](\d{4})\s*:?\s*$`)
	fileDateRe  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
)

// FileDate extracts a YYYY-MM-DD date from a file name (e.g. 2026-05-24.md),
// or "" when the name carries none.
func FileDate(name string) string {
	return fileDateRe.FindString(filepath.Base(name))
}

// headerDate reports whether the line is a date header and returns its date.
func headerDate(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if m := isoHeaderRe.FindStringSubmatch(t); m != nil {
		return m[1], true
	}
	if m := dmyHeaderRe.FindStringSubmatch(t); m != nil {
		return fmt.Sprintf("%s-%s-%s", m[3], pad2(m[2]), pad2(m[1])), true
	}
	return "", false
}

func pad2(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

// SplitChunks slices content into date-scoped chunks. Lines that are date
// headers (`## 2026-05-24`, bare `2026-05-24`, `24.05.2026`) start a new chunk
// stamped with that date; text before any header (or a file with no headers at
// all) gets fallbackDate. Chunks longer than maxChunkLen are further split at
// blank-line boundaries, keeping their date.
func SplitChunks(content, fallbackDate string) []Chunk {
	var chunks []Chunk
	cur := Chunk{Date: fallbackDate}
	flush := func() {
		if strings.TrimSpace(cur.Text) != "" {
			chunks = append(chunks, Chunk{Date: cur.Date, Text: strings.TrimSpace(cur.Text)})
		}
		cur.Text = ""
	}
	for _, ln := range strings.Split(content, "\n") {
		if d, ok := headerDate(ln); ok {
			flush()
			cur.Date = d
			continue
		}
		cur.Text += ln + "\n"
	}
	flush()

	var out []Chunk
	for _, ch := range chunks {
		out = append(out, splitOversize(ch)...)
	}
	return out
}

func splitOversize(ch Chunk) []Chunk {
	if len(ch.Text) <= maxChunkLen {
		return []Chunk{ch}
	}
	paras := strings.Split(ch.Text, "\n\n")
	var out []Chunk
	var buf strings.Builder
	flush := func() {
		if strings.TrimSpace(buf.String()) != "" {
			out = append(out, Chunk{Date: ch.Date, Text: strings.TrimSpace(buf.String())})
		}
		buf.Reset()
	}
	for _, p := range paras {
		if buf.Len() > 0 && buf.Len()+len(p)+2 > maxChunkLen {
			flush()
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(p)
	}
	flush()
	return out
}

// Package view projects vault data into the per-bucket board: open one-liners
// split fresh/aging, dump progress, counts. The text renderer is deterministic
// (fixed column budget, no terminal probing) so output is stable in pipes.
package view

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"ki/internal/config"
	"ki/internal/vault"
)

// Options tunes Build and Render.
type Options struct {
	Days     int  // fresh/aging boundary in days (default 7)
	ShowDone bool // include done one-liners in text output
}

// Entry is one one-liner in the board.
type Entry struct {
	Text    string `json:"text"`
	Date    string `json:"date"`
	Time    string `json:"time"`
	Done    bool   `json:"done"`
	AgeDays int    `json:"age_days"`
}

// DumpView is one dump in the board.
type DumpView struct {
	Title   string       `json:"title"`
	Path    string       `json:"path"`
	Created string       `json:"created"`
	AgeDays int          `json:"age_days"`
	Open    int          `json:"open"`
	Done    int          `json:"done"`
	Steps   []vault.Step `json:"steps"`
}

// BucketView is one bucket's board section.
type BucketView struct {
	Name      string     `json:"name"`
	Desc      string     `json:"desc"`
	Open      int        `json:"open"`
	Done      int        `json:"done"`
	Oneliners []Entry    `json:"oneliners"`
	Dumps     []DumpView `json:"dumps"`
}

// Board is the whole view: one section per requested bucket, config order.
type Board struct {
	Buckets []BucketView `json:"buckets"`
}

// Build scans the requested buckets and assembles the board. Ages are
// computed against now's calendar date.
func Build(c config.Config, buckets []config.Bucket, now time.Time) (Board, error) {
	board := Board{Buckets: []BucketView{}}
	for _, b := range buckets {
		bd, err := vault.ScanBucket(c, b)
		if err != nil {
			return Board{}, err
		}
		bv := BucketView{Name: b.Name, Desc: b.Desc, Oneliners: []Entry{}, Dumps: []DumpView{}}
		bv.Open, bv.Done = bd.Counts()
		for _, e := range bd.Log.Entries {
			bv.Oneliners = append(bv.Oneliners, Entry{
				Text: e.Text, Date: e.Date, Time: e.Time, Done: e.Done,
				AgeDays: ageDays(e.Date, now),
			})
		}
		for _, d := range bd.Dumps {
			done, total := d.Progress()
			path := d.Path
			if rel, err := filepath.Rel(c.Root(), d.Path); err == nil {
				path = rel
			}
			bv.Dumps = append(bv.Dumps, DumpView{
				Title: d.Title, Path: path, Created: d.Created,
				AgeDays: ageDays(d.Created, now),
				Open:    total - done, Done: done, Steps: d.Steps,
			})
		}
		board.Buckets = append(board.Buckets, bv)
	}
	return board, nil
}

// ageDays counts calendar days from a YYYY-MM-DD date to now (0 for today or
// an unparseable date, so odd hand-edits stay in the fresh column).
func ageDays(date string, now time.Time) int {
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(date), now.Location())
	if err != nil {
		return 0
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	d := int(today.Sub(t).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

const colWidth = 46

// Render returns the text board.
func Render(board Board, opts Options) string {
	if opts.Days <= 0 {
		opts.Days = 7
	}
	var b strings.Builder
	for i, bv := range board.Buckets {
		if i > 0 {
			b.WriteString("\n")
		}
		renderBucket(&b, bv, opts)
	}
	return b.String()
}

func renderBucket(b *strings.Builder, bv BucketView, opts Options) {
	fmt.Fprintf(b, "%s — %s  (%d open / %d done)\n", bv.Name, bv.Desc, bv.Open, bv.Done)
	b.WriteString(strings.Repeat("─", colWidth+24) + "\n")

	var fresh, aging, done []string
	for _, e := range bv.Oneliners {
		line := fmt.Sprintf("- [%s] %s %s %s", tick(e.Done), shortDate(e.Date), e.Time, e.Text)
		switch {
		case e.Done:
			done = append(done, line)
		case e.AgeDays >= opts.Days:
			aging = append(aging, line)
		default:
			fresh = append(fresh, line)
		}
	}

	switch {
	case len(fresh) > 0 && len(aging) > 0:
		renderColumns(b, fmt.Sprintf("fresh (<%dd)", opts.Days), fresh, fmt.Sprintf("aging (≥%dd)", opts.Days), aging)
	case len(fresh) > 0:
		fmt.Fprintf(b, "fresh (<%dd)\n", opts.Days)
		for _, ln := range fresh {
			b.WriteString(ln + "\n")
		}
	case len(aging) > 0:
		fmt.Fprintf(b, "aging (≥%dd)\n", opts.Days)
		for _, ln := range aging {
			b.WriteString(ln + "\n")
		}
	case len(bv.Dumps) == 0 && len(done) == 0:
		b.WriteString("(empty)\n")
	}

	if opts.ShowDone && len(done) > 0 {
		b.WriteString("done:\n")
		for _, ln := range done {
			b.WriteString(ln + "\n")
		}
	}

	if len(bv.Dumps) > 0 {
		b.WriteString("dumps:\n")
		for _, d := range bv.Dumps {
			fmt.Fprintf(b, "  %-40s (%d/%d)  %s\n", truncate(d.Title, 40), d.Done, d.Done+d.Open, ageStr(d.AgeDays))
		}
	}
}

func renderColumns(b *strings.Builder, leftHead string, left []string, rightHead string, right []string) {
	rows := len(left)
	if len(right) > rows {
		rows = len(right)
	}
	fmt.Fprintf(b, "%s│ %s\n", pad(leftHead), rightHead)
	for i := 0; i < rows; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		fmt.Fprintf(b, "%s│ %s\n", pad(l), truncate(r, colWidth))
	}
}

func pad(s string) string {
	s = truncate(s, colWidth)
	return s + strings.Repeat(" ", colWidth-len([]rune(s))+1)
}

func truncate(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}

func tick(done bool) string {
	if done {
		return "x"
	}
	return " "
}

// shortDate drops the year for display (files keep the full date).
func shortDate(d string) string {
	if len(d) == len("2006-01-02") {
		return d[5:]
	}
	return d
}

func ageStr(days int) string {
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "1d ago"
	default:
		return fmt.Sprintf("%dd ago", days)
	}
}

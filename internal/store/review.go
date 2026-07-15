package store

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ki/internal/config"
	"ki/internal/fm"
)

// ReviewOptions tunes a Digest.
type ReviewOptions struct {
	StaleDays   int // no-due items older than this are "stale" (default 14)
	SoonDays    int // items due within this many days are "due soon" (default 7)
	Bucket      string
	Topic       string
	OnlyOverdue bool
}

// ReviewItem is one flagged item in a digest.
type ReviewItem struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Created   string `json:"created,omitempty"`
	Due       string `json:"due,omitempty"`
	AgeDays   int    `json:"age_days,omitempty"`
	DueInDays int    `json:"due_in_days,omitempty"` // negative = overdue
}

// Review is the digest result.
type Review struct {
	Overdue []ReviewItem   `json:"overdue"`
	DueSoon []ReviewItem   `json:"due_soon"`
	Stale   []ReviewItem   `json:"stale"`
	Counts  map[string]int `json:"open_counts"`
}

// ScanItems loads every jot item across configured buckets.
func ScanItems(c config.Config) ([]Item, error) {
	var out []Item
	for _, b := range c.Buckets {
		dir := c.BucketPath(b.Name)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			f, body := fm.Parse(data)
			out = append(out, Item{
				ID:      f.Get("id"),
				Title:   firstNonEmpty(f.Get("title"), FirstLine(body)),
				Type:    firstNonEmpty(f.Get("type"), b.Name),
				Status:  firstNonEmpty(f.Get("status"), "open"),
				Created: f.Get("created"),
				Due:     f.Get("due"),
				Tags:    f.List("tags"),
				Topic:   firstNonEmpty(f.Get("topic"), f.Get("thread")),
				Source:  f.Get("source"),
				Body:    strings.TrimSpace(body),
				Path:    p,
			})
		}
	}
	return out, nil
}

// Digest surfaces open items needing attention: overdue, due soon, or stale.
func Digest(c config.Config, opts ReviewOptions, now time.Time) (Review, error) {
	if opts.StaleDays <= 0 {
		opts.StaleDays = 14
	}
	if opts.SoonDays <= 0 {
		opts.SoonDays = 7
	}
	items, err := ScanItems(c)
	if err != nil {
		return Review{}, err
	}
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	rev := Review{Counts: map[string]int{}}
	for _, it := range items {
		if it.Status != "" && it.Status != "open" {
			continue
		}
		if c.BucketNoReview(it.Type) {
			continue
		}
		if opts.Bucket != "" && it.Type != opts.Bucket {
			continue
		}
		if opts.Topic != "" && it.Topic != opts.Topic {
			continue
		}
		rev.Counts[it.Type]++

		ri := ReviewItem{Path: relPath(c, it.Path), Type: it.Type, Title: it.Title, Created: it.Created, Due: it.Due}

		if due, ok := parseDate(it.Due, loc); ok {
			d := daysBetween(today, due)
			ri.DueInDays = d
			switch {
			case d < 0:
				rev.Overdue = append(rev.Overdue, ri)
			case d <= opts.SoonDays:
				rev.DueSoon = append(rev.DueSoon, ri)
			}
			continue
		}
		if created, ok := parseDate(it.Created, loc); ok {
			age := daysBetween(created, today)
			ri.AgeDays = age
			if age >= opts.StaleDays {
				rev.Stale = append(rev.Stale, ri)
			}
		}
	}
	if opts.OnlyOverdue {
		rev.DueSoon = nil
		rev.Stale = nil
	}
	sort.SliceStable(rev.Overdue, func(i, j int) bool { return rev.Overdue[i].Due < rev.Overdue[j].Due })
	sort.SliceStable(rev.DueSoon, func(i, j int) bool { return rev.DueSoon[i].Due < rev.DueSoon[j].Due })
	sort.SliceStable(rev.Stale, func(i, j int) bool { return rev.Stale[i].Created < rev.Stale[j].Created })
	return rev, nil
}

func parseDate(s string, loc *time.Location) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func daysBetween(from, to time.Time) int {
	return int(math.Round(to.Sub(from).Hours() / 24))
}

func relPath(c config.Config, abs string) string {
	if r, err := filepath.Rel(c.Root(), abs); err == nil {
		return r
	}
	return abs
}

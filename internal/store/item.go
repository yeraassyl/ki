package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"ki/internal/config"
	"ki/internal/fm"
)

// Classification is the LLM's structured verdict for a captured note. It is the
// contract between the (generative) classifier and the (deterministic) writer:
// `ki jot --json` accepts exactly this shape, so a skill can produce the JSON
// and let the CLI place it.
type Classification struct {
	Bucket  string   `json:"bucket"`
	Title   string   `json:"title"`
	Due     string   `json:"due,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Topic   string   `json:"topic,omitempty"`
	Body    string   `json:"body"`
	Created string   `json:"created,omitempty"` // YYYY-MM-DD override; used by import to keep original dates
}

// Item is a captured note ready to be written to a bucket.
type Item struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Type    string   `json:"type"`
	Status  string   `json:"status"`
	Created string   `json:"created,omitempty"`
	Due     string   `json:"due,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Topic   string   `json:"topic,omitempty"`
	Source  string   `json:"source,omitempty"`
	Body    string   `json:"body"`
	Path    string   `json:"path,omitempty"` // absolute when loaded from disk; empty when constructed for writing
}

var (
	slugRe = regexp.MustCompile(`[^a-z0-9]+`)
	dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// Slug converts text into a filename-safe slug (max ~50 chars).
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = strings.Trim(s[:50], "-")
	}
	if s == "" {
		s = "note"
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
	return "note"
}

// NewItem builds an Item from a Classification, stamping id/created/status.
// A valid cl.Created overrides today so imported items keep their original date.
func NewItem(cl Classification, today, source string) Item {
	title := strings.TrimSpace(cl.Title)
	if title == "" {
		title = FirstLine(cl.Body)
	}
	body := cl.Body
	if strings.TrimSpace(body) == "" {
		body = title
	}
	created := today
	if dateRe.MatchString(strings.TrimSpace(cl.Created)) {
		created = strings.TrimSpace(cl.Created)
	}
	return Item{
		ID:      created + "-" + Slug(title),
		Title:   title,
		Type:    cl.Bucket,
		Status:  "open",
		Created: created,
		Due:     strings.TrimSpace(cl.Due),
		Tags:    cl.Tags,
		Topic:   strings.TrimSpace(cl.Topic),
		Source:  source,
		Body:    strings.TrimRight(body, "\n"),
	}
}

// Render returns the item's markdown (frontmatter + body) with a stable field order.
func (it Item) Render() string {
	f := fm.New()
	f.Set("id", it.ID)
	f.Set("title", it.Title)
	f.Set("type", it.Type)
	f.Set("status", it.Status)
	f.Set("created", it.Created)
	if it.Due != "" {
		f.Set("due", it.Due)
	}
	if len(it.Tags) > 0 {
		f.SetList("tags", it.Tags)
	}
	if it.Topic != "" {
		f.Set("topic", it.Topic)
	}
	f.Set("source", it.Source)
	return f.Render("\n" + it.Body + "\n")
}

// ParseItem reconstructs an Item from a rendered file (used after $EDITOR).
func ParseItem(data []byte) (Item, error) {
	f, body := fm.Parse(data)
	it := Item{
		ID:      f.Get("id"),
		Title:   f.Get("title"),
		Type:    f.Get("type"),
		Status:  f.Get("status"),
		Created: f.Get("created"),
		Due:     f.Get("due"),
		Tags:    f.List("tags"),
		Topic:   firstNonEmpty(f.Get("topic"), f.Get("thread")),
		Source:  f.Get("source"),
		Body:    strings.TrimSpace(body),
	}
	if it.Type == "" {
		return Item{}, fmt.Errorf("edited item is missing a `type` field")
	}
	if it.Title == "" {
		it.Title = FirstLine(it.Body)
	}
	return it, nil
}

// WriteItem writes the item to its bucket at a collision-free path and returns
// the absolute path written.
func WriteItem(c config.Config, it Item) (string, error) {
	if !c.HasBucket(it.Type) {
		return "", fmt.Errorf("unknown bucket %q (configured: %s)", it.Type, strings.Join(c.BucketNames(), ", "))
	}
	dir := c.BucketPath(it.Type)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := uniquePath(dir, it.ID)
	if err := os.WriteFile(path, []byte(it.Render()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// CloseItem sets an item's status to done (stamping `closed:`), preserving every
// other frontmatter field and the body verbatim. Returns the updated item.
func CloseItem(path, today string) (Item, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Item{}, err
	}
	f, body := fm.Parse(data)
	if s := f.Get("status"); s != "" && s != "open" {
		return Item{}, fmt.Errorf("item is already %q: %s", s, path)
	}
	f.Set("status", "done")
	f.Set("closed", today)
	if err := os.WriteFile(path, []byte(f.Render(body)), 0o644); err != nil {
		return Item{}, err
	}
	it, err := ParseItem([]byte(f.Render(body)))
	if err != nil {
		return Item{}, err
	}
	it.Path = path
	return it, nil
}

func uniquePath(dir, id string) string {
	base := filepath.Join(dir, id+".md")
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base
	}
	for i := 2; ; i++ {
		p := filepath.Join(dir, fmt.Sprintf("%s-%d.md", id, i))
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
}

package vault

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"ki/internal/fm"
)

// stepRe matches one dump step line: `- [ ] text` (no timestamp).
var stepRe = regexp.MustCompile(`^- \[([ xX])\] (.+)$`)

// Step is one checkbox step of a dump. Line indexes into Dump.Lines so the
// step can be ticked in place without touching any other byte of the file.
type Step struct {
	Done bool   `json:"done"`
	Text string `json:"text"`
	Line int    `json:"-"`
}

// Dump is one braindump file: LLM-produced title and steps, deterministically
// named and placed. Lines holds the whole file verbatim.
type Dump struct {
	Path    string
	Title   string
	Created string
	Steps   []Step
	Lines   []string
}

// LoadDump reads and parses one dump file.
func LoadDump(path string) (Dump, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Dump{}, err
	}
	d := Dump{Path: path, Lines: splitLines(data)}

	f, _ := fm.Parse(data)
	d.Title = f.Get("title")
	d.Created = f.Get("created")
	if d.Title == "" {
		d.Title = strings.TrimSuffix(filepath.Base(path), ".md")
	}

	// Steps are scanned below the frontmatter so a stray checkbox-looking
	// frontmatter line can never be ticked.
	start := 0
	if len(d.Lines) > 0 && strings.TrimRight(d.Lines[0], "\r") == "---" {
		for i := 1; i < len(d.Lines); i++ {
			if strings.TrimRight(d.Lines[i], "\r") == "---" {
				start = i + 1
				break
			}
		}
	}
	for i := start; i < len(d.Lines); i++ {
		m := stepRe.FindStringSubmatch(d.Lines[i])
		if m == nil {
			continue
		}
		d.Steps = append(d.Steps, Step{Done: m[1] != " ", Text: m[2], Line: i})
	}
	return d, nil
}

// Progress returns done and total step counts.
func (d Dump) Progress() (done, total int) {
	for _, s := range d.Steps {
		if s.Done {
			done++
		}
	}
	return done, len(d.Steps)
}

// Tick marks the step at Lines index i as done.
func (d *Dump) Tick(i int) error {
	if err := tickLine(d.Lines, i); err != nil {
		return err
	}
	for j := range d.Steps {
		if d.Steps[j].Line == i {
			d.Steps[j].Done = true
		}
	}
	return nil
}

// Save writes the dump back to disk verbatim.
func (d Dump) Save() error {
	return os.WriteFile(d.Path, []byte(joinLines(d.Lines)), 0o644)
}

// RenderDump renders a fresh dump file from LLM content.
func RenderDump(title, created string, steps []string) string {
	f := fm.New()
	f.Set("title", title)
	f.Set("created", created)
	var b strings.Builder
	b.WriteString("\n# " + title + "\n\n")
	for _, s := range steps {
		b.WriteString("- [ ] " + strings.TrimSpace(s) + "\n")
	}
	return f.Render(b.String())
}

// WriteDump writes a new dump into dir at a collision-free path derived from
// the title and returns the path. `log` is reserved for the one-liner file.
func WriteDump(dir, title, created string, steps []string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	slug := Slug(title)
	if slug == "log" {
		slug = "log-dump"
	}
	path := uniquePath(dir, slug)
	if err := os.WriteFile(path, []byte(RenderDump(title, created, steps)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

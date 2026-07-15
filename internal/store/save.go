package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ki/internal/config"
	"ki/internal/fm"
)

// canonicalSections is the fixed render order for a permanent.md body. Any
// section not listed is appended afterwards in alphabetical order.
var canonicalSections = []string{"State", "Decisions", "Open Questions", "Key Files / References", "Next Prompt"}

// SaveRequest is the content contract for `ki save`. The LLM (a skill, or a
// human via a future editor flow) produces it; the CLI stamps `updated`, merges
// any omitted frontmatter from the existing file, and places everything.
type SaveRequest struct {
	Thread    string            `json:"thread"`
	Status    string            `json:"status,omitempty"`
	Hook      string            `json:"hook,omitempty"`
	Jira      string            `json:"jira,omitempty"`
	Branch    string            `json:"branch,omitempty"`
	Repo      string            `json:"repo,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Related   []string          `json:"related,omitempty"`
	Sections  map[string]string `json:"sections"`
	Changelog string            `json:"changelog,omitempty"`
}

// SaveResult reports what a Save did (or, on dry-run, would do).
type SaveResult struct {
	Thread        string
	PermanentPath string
	ChangelogPath string
	IndexPath     string
	Created       bool
	Permanent     string // rendered permanent.md content
}

func (r SaveRequest) validate() error {
	if strings.TrimSpace(r.Thread) == "" {
		return fmt.Errorf("save: thread is required")
	}
	if strings.ContainsAny(r.Thread, `/\`) || strings.Contains(r.Thread, "..") {
		return fmt.Errorf("save: invalid thread name %q", r.Thread)
	}
	if len(r.Sections) == 0 {
		return fmt.Errorf("save: at least one section is required")
	}
	return nil
}

// Save upserts a thread's permanent.md, prepends its changelog, and rebuilds the
// index. `updated` is stamped to today; omitted frontmatter is merged from the
// existing file. On overwrite the previous permanent.md is backed up to .bak.
// With dryRun, nothing is written and the rendered content is returned.
func Save(c config.Config, req SaveRequest, today string, dryRun bool) (SaveResult, error) {
	if err := req.validate(); err != nil {
		return SaveResult{}, err
	}
	dir := filepath.Join(c.ThreadsPath(), req.Thread)
	permPath := filepath.Join(dir, "permanent.md")

	var existing *fm.FM
	created := true
	if data, err := os.ReadFile(permPath); err == nil {
		existing, _ = fm.Parse(data)
		created = false
	}

	res := SaveResult{
		Thread:        req.Thread,
		PermanentPath: permPath,
		ChangelogPath: filepath.Join(dir, "changelog.md"),
		IndexPath:     c.IndexPath(),
		Created:       created,
		Permanent:     req.renderPermanent(existing, today),
	}
	if dryRun {
		return res, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	if !created {
		if data, err := os.ReadFile(permPath); err == nil {
			if err := os.WriteFile(permPath+".bak", data, 0o644); err != nil {
				return res, err
			}
		}
	}
	if err := os.WriteFile(permPath, []byte(res.Permanent), 0o644); err != nil {
		return res, err
	}
	if strings.TrimSpace(req.Changelog) != "" {
		if err := prependChangelog(res.ChangelogPath, req.Thread, today, strings.TrimSpace(req.Changelog)); err != nil {
			return res, err
		}
	}
	if err := updateIndex(c); err != nil {
		return res, err
	}
	return res, nil
}

func (r SaveRequest) renderPermanent(existing *fm.FM, today string) string {
	f := fm.New()
	f.Set("thread", r.Thread)
	f.Set("status", firstNonEmpty(r.Status, getFM(existing, "status"), "in-progress"))
	f.Set("updated", today)
	f.Set("hook", firstNonEmpty(r.Hook, getFM(existing, "hook")))
	if v := firstNonEmpty(r.Jira, getFM(existing, "jira")); v != "" {
		f.Set("jira", v)
	}
	if v := firstNonEmpty(r.Branch, getFM(existing, "branch")); v != "" {
		f.Set("branch", v)
	}
	if v := firstNonEmpty(r.Repo, getFM(existing, "repo")); v != "" {
		f.Set("repo", v)
	}
	if rel := chooseList(r.Related, existing, "related"); len(rel) > 0 {
		f.SetList("related", rel)
	}
	if tags := chooseList(r.Tags, existing, "tags"); len(tags) > 0 {
		f.SetList("tags", tags)
	}
	return f.Render(renderSections(r.Sections))
}

func renderSections(secs map[string]string) string {
	var b strings.Builder
	written := map[string]bool{}
	emit := func(h string) {
		body, ok := secs[h]
		if !ok {
			return
		}
		written[h] = true
		b.WriteString("\n## " + h + "\n\n" + strings.TrimRight(body, "\n") + "\n")
	}
	for _, h := range canonicalSections {
		emit(h)
	}
	var extra []string
	for h := range secs {
		if !written[h] {
			extra = append(extra, h)
		}
	}
	sort.Strings(extra)
	for _, h := range extra {
		b.WriteString("\n## " + h + "\n\n" + strings.TrimRight(secs[h], "\n") + "\n")
	}
	return b.String()
}

// prependChangelog inserts a newest-first entry under the changelog header,
// creating the file if needed, without reading the whole log into meaning.
func prependChangelog(path, thread, today, entry string) error {
	line := "- " + today + ": " + entry
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return os.WriteFile(path, []byte("# "+thread+" — changelog\n\n"+line+"\n"), 0o644)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	header := "# " + thread + " — changelog"
	rest := lines
	if len(lines) > 0 && strings.HasPrefix(lines[0], "# ") {
		header = lines[0]
		rest = lines[1:]
	}
	for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
		rest = rest[1:]
	}
	var b strings.Builder
	b.WriteString(header + "\n\n" + line + "\n")
	for _, r := range rest {
		b.WriteString(r + "\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// updateIndex rebuilds _index.md from thread frontmatter (lossless now that
// permanent.md carries `hook:`), backing up the previous index.
func updateIndex(c config.Config) error {
	threads, err := ScanThreads(c)
	if err != nil {
		return err
	}
	standalones, err := ScanStandalones(c)
	if err != nil {
		return err
	}
	rendered := RenderIndex(threads, standalones, CountArchive(c))
	idx := c.IndexPath()
	if data, e := os.ReadFile(idx); e == nil {
		if e := os.WriteFile(idx+".bak", data, 0o644); e != nil {
			return e
		}
	}
	return os.WriteFile(idx, []byte(rendered), 0o644)
}

func getFM(f *fm.FM, key string) string {
	if f == nil {
		return ""
	}
	return f.Get(key)
}

func chooseList(req []string, existing *fm.FM, key string) []string {
	if req != nil {
		return req
	}
	if existing != nil {
		return existing.List(key)
	}
	return nil
}

// Package search implements ki's deterministic tiered recall: it scans every
// markdown file once, scores each against the query across identifier / tag /
// title / meta / body tiers, and (optionally) expands the top hits along the
// in-memory [[wikilink]] graph in both directions. The LLM supplies only the
// query terms; ranking and traversal are fixed here.
package search

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"ki/internal/config"
	"ki/internal/fm"
)

// Hit is one ranked search result.
type Hit struct {
	Path    string   `json:"path"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
	Excerpt string   `json:"excerpt,omitempty"`
}

// Options tunes a Find call.
type Options struct {
	Limit      int
	Graph      bool
	IncludeAll bool     // include _archive, changelogs, backups
	Kinds      []string // restrict to these kinds; empty = all
	Topic      string   // restrict to docs carrying this topic
}

type doc struct {
	path     string
	kind     string
	name     string // lowercased graph key (thread dir or file stem)
	topic    string // lowercased `topic:` (or legacy `thread:`) frontmatter
	title    string
	idText   string // lowercased id-ish fields + filename stem
	tags     []string
	metaText string // lowercased other frontmatter values
	bodyLC   string
	bodyRaw  string
	links    []string // lowercased [[wikilink]] targets
}

var wikilinkRe = regexp.MustCompile(`\[\[([^\]|#]+)`)

// Find returns ranked hits for query.
func Find(c config.Config, query string, opts Options) ([]Hit, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil, nil
	}
	docs, err := buildDocs(c, opts.IncludeAll)
	if err != nil {
		return nil, err
	}
	if opts.Topic != "" {
		want := strings.ToLower(opts.Topic)
		filtered := docs[:0]
		for _, d := range docs {
			if d.topic == want || d.name == want {
				filtered = append(filtered, d)
			}
		}
		docs = filtered
	}

	kindFilter := toSet(opts.Kinds)
	hits := map[string]*Hit{}
	for _, d := range docs {
		sc, reasons, excerpt := scoreDoc(d, terms)
		if sc == 0 {
			continue
		}
		if kindFilter != nil && !kindFilter[d.kind] {
			continue
		}
		hits[d.path] = &Hit{Path: d.path, Kind: d.kind, Title: d.title, Score: sc, Reasons: reasons, Excerpt: excerpt}
	}

	if opts.Graph {
		expandGraph(docs, hits)
	}

	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

func buildDocs(c config.Config, includeAll bool) ([]doc, error) {
	root := c.Root()
	var docs []doc
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			base := d.Name()
			if strings.HasPrefix(base, ".") || (strings.HasPrefix(base, "_") && !includeAll) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if skipFile(rel, d.Name(), includeAll) {
			return nil
		}
		data, e := os.ReadFile(p)
		if e != nil {
			return nil
		}
		docs = append(docs, makeDoc(c, rel, data))
		return nil
	})
	return docs, err
}

func skipFile(rel, name string, includeAll bool) bool {
	if name == "_index.md" || strings.HasSuffix(name, ".bak") || strings.HasSuffix(name, ".original.md") {
		return true
	}
	if !includeAll && (name == "changelog.md" || strings.Contains(rel, "/_archive/")) {
		return true
	}
	return false
}

func makeDoc(c config.Config, rel string, data []byte) doc {
	f, body := fm.Parse(data)
	kind := classifyKind(c, rel)

	name := stem(rel)
	if kind == "thread" {
		rest := strings.TrimPrefix(rel, c.ThreadsDir+"/")
		if i := strings.Index(rest, "/"); i >= 0 {
			name = rest[:i]
		}
	}
	title := firstNonEmpty(f.Get("title"), f.Get("thread"), name)

	idParts := make([]string, 0, 6)
	for _, k := range []string{"id", "jira", "branch", "thread", "repo"} {
		if v := f.Get(k); v != "" {
			idParts = append(idParts, v)
		}
	}
	idParts = append(idParts, stem(rel))

	meta := make([]string, 0, len(f.Keys))
	for _, k := range f.Keys {
		meta = append(meta, f.Get(k))
	}

	return doc{
		path:     rel,
		kind:     kind,
		name:     strings.ToLower(name),
		topic:    strings.ToLower(firstNonEmpty(f.Get("topic"), f.Get("thread"))),
		title:    title,
		idText:   strings.ToLower(strings.Join(idParts, " ")),
		tags:     lowerAll(f.List("tags")),
		metaText: strings.ToLower(strings.Join(meta, " ")),
		bodyLC:   strings.ToLower(body),
		bodyRaw:  body,
		links:    extractLinks(body),
	}
}

func classifyKind(c config.Config, rel string) string {
	if strings.HasPrefix(rel, c.ThreadsDir+"/") {
		rest := rel[len(c.ThreadsDir)+1:]
		switch {
		case strings.HasPrefix(rest, "_archive/"):
			return "archive"
		case strings.HasSuffix(rest, "/permanent.md"):
			return "thread"
		case strings.HasSuffix(rest, "/changelog.md"):
			return "changelog"
		case !strings.Contains(rest, "/"):
			return "standalone"
		default:
			return "thread-file"
		}
	}
	if strings.HasPrefix(rel, c.JotRoot+"/") {
		return "jot"
	}
	return "note"
}

func extractLinks(body string) []string {
	matches := wikilinkRe.FindAllStringSubmatch(body, -1)
	var out []string
	for _, g := range matches {
		t := strings.ToLower(strings.TrimSpace(g[1]))
		t = strings.TrimSuffix(t, ".md")
		if i := strings.LastIndex(t, "/"); i >= 0 {
			t = t[i+1:]
		}
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func scoreDoc(d doc, terms []string) (int, []string, string) {
	seen := map[string]bool{}
	var reasons []string
	addReason := func(r string) {
		if !seen[r] {
			seen[r] = true
			reasons = append(reasons, r)
		}
	}

	total, matchedTerms, excerpt := 0, 0, ""
	for _, term := range terms {
		matched := false
		if strings.Contains(d.idText, term) {
			total += 80
			addReason("id")
			matched = true
		}
		if containsAny(d.tags, term) {
			total += 40
			addReason("tag:" + term)
			matched = true
		}
		if strings.Contains(strings.ToLower(d.title), term) {
			total += 25
			addReason("title")
			matched = true
		}
		if strings.Contains(d.metaText, term) {
			total += 8
			addReason("meta")
			matched = true
		}
		if strings.Contains(d.bodyLC, term) {
			total += 10
			addReason("body")
			matched = true
			if excerpt == "" {
				excerpt = firstMatchingLine(d.bodyRaw, term)
			}
		}
		if matched {
			matchedTerms++
		}
	}
	if matchedTerms == 0 {
		return 0, nil, ""
	}
	if len(terms) > 1 && matchedTerms == len(terms) {
		total += 20
		addReason("all-terms")
	}
	return total, reasons, excerpt
}

// expandGraph adds [[wikilink]] neighbours (out-links and back-links) of the
// current direct hits, at a low score so they rank below real matches.
func expandGraph(docs []doc, hits map[string]*Hit) {
	byName := map[string]int{}
	pathToDoc := map[string]doc{}
	for i, d := range docs {
		if _, ok := byName[d.name]; !ok {
			byName[d.name] = i
		}
		pathToDoc[d.path] = d
	}

	// Snapshot the direct hits before mutating the map.
	type seed struct{ name, title, path string }
	var seeds []seed
	for p, h := range hits {
		seeds = append(seeds, seed{pathToDoc[p].name, h.Title, p})
	}

	add := func(path, srcTitle string) {
		if h, ok := hits[path]; ok {
			h.Score += 3
			h.Reasons = append(h.Reasons, "linked:"+srcTitle)
			return
		}
		d := pathToDoc[path]
		hits[path] = &Hit{Path: d.path, Kind: d.kind, Title: d.title, Score: 6, Reasons: []string{"linked:" + srcTitle}}
	}

	for _, s := range seeds {
		sd := pathToDoc[s.path]
		for _, ln := range sd.links { // out-links
			if idx, ok := byName[ln]; ok {
				add(docs[idx].path, s.title)
			}
		}
		for _, d := range docs { // back-links
			for _, ln := range d.links {
				if ln == s.name {
					add(d.path, s.title)
					break
				}
			}
		}
	}
}

func firstMatchingLine(body, term string) string {
	for _, ln := range strings.Split(body, "\n") {
		if strings.Contains(strings.ToLower(ln), term) {
			return truncate(strings.TrimSpace(ln), 120)
		}
	}
	return ""
}

func tokenize(q string) []string {
	var out []string
	for _, t := range strings.Fields(strings.ToLower(q)) {
		t = strings.Trim(t, `.,;:!?()[]"'`)
		if len(t) >= 2 {
			out = append(out, t)
		}
	}
	return out
}

func containsAny(list []string, term string) bool {
	for _, x := range list {
		if strings.Contains(x, term) {
			return true
		}
	}
	return false
}

func stem(rel string) string {
	b := rel[strings.LastIndex(rel, "/")+1:]
	return strings.TrimSuffix(b, ".md")
}

func lowerAll(xs []string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, strings.ToLower(x))
	}
	return out
}

func toSet(xs []string) map[string]bool {
	if len(xs) == 0 {
		return nil
	}
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

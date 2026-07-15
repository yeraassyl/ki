// Package fm parses and renders the small YAML subset used in ki frontmatter:
// a leading `---` fence enclosing `key: value` lines, where a value is either a
// scalar or an inline `[a, b, c]` list. Block lists (`  - item`) are also
// accepted on read. Only the constructs ki actually uses are supported,
// so the package has zero third-party dependencies and always builds offline.
package fm

import (
	"bufio"
	"strings"
)

const fmSpecialFirst = "[]{}!&*#|>@%?,'\"`"

// FM is an ordered set of frontmatter fields. Scalars and lists are stored
// separately; Keys preserves definition order so Render is deterministic.
type FM struct {
	Keys  []string
	vals  map[string]string
	lists map[string][]string
}

// New returns an empty FM ready for Set/SetList.
func New() *FM {
	return &FM{vals: map[string]string{}, lists: map[string][]string{}}
}

// Has reports whether key is present as either a scalar or a list.
func (f *FM) Has(k string) bool {
	if _, ok := f.vals[k]; ok {
		return true
	}
	_, ok := f.lists[k]
	return ok
}

// Get returns the scalar value for key (empty string if absent or a list).
func (f *FM) Get(k string) string { return f.vals[k] }

// List returns the list value for key (nil if absent or a scalar).
func (f *FM) List(k string) []string { return f.lists[k] }

func (f *FM) ensureKey(k string) {
	if !f.Has(k) {
		f.Keys = append(f.Keys, k)
	}
}

// Set stores key as a scalar, replacing any prior scalar or list.
func (f *FM) Set(k, v string) {
	f.ensureKey(k)
	delete(f.lists, k)
	f.vals[k] = v
}

// SetList stores key as a list, replacing any prior scalar or list.
func (f *FM) SetList(k string, v []string) {
	f.ensureKey(k)
	delete(f.vals, k)
	f.lists[k] = v
}

// Parse splits data into its frontmatter and the remaining body. If data does
// not open with a `---` fence, it returns an empty FM and the whole input as
// body (so callers can round-trip plain files unchanged).
func Parse(data []byte) (*FM, string) {
	f := New()
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	if !sc.Scan() {
		return f, ""
	}
	if strings.TrimRight(sc.Text(), "\r") != "---" {
		return f, string(data)
	}

	var fmLines []string
	closed := false
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "---" {
			closed = true
			break
		}
		fmLines = append(fmLines, line)
	}
	if !closed {
		// Unterminated fence: not valid frontmatter, treat the file as body.
		return New(), string(data)
	}

	var body strings.Builder
	for sc.Scan() {
		body.WriteString(sc.Text())
		body.WriteByte('\n')
	}
	parseLines(f, fmLines)
	return f, body.String()
}

func parseLines(f *FM, lines []string) {
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue // malformed line; skip
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+1:])
		if key == "" {
			continue
		}

		if val == "" {
			// A block list may follow: indented `- item` lines.
			var items []string
			for i+1 < len(lines) {
				next := strings.TrimSpace(lines[i+1])
				switch {
				case strings.HasPrefix(next, "- "):
					items = append(items, unquote(strings.TrimSpace(next[2:])))
					i++
				case next == "":
					i++
				default:
					goto done
				}
			}
		done:
			if items != nil {
				f.SetList(key, items)
			} else {
				f.Set(key, "")
			}
			continue
		}

		if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
			f.SetList(key, parseInlineList(val))
			continue
		}
		f.Set(key, unquote(val))
	}
}

func parseInlineList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, unquote(strings.TrimSpace(p)))
	}
	return out
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// Render returns the frontmatter block (with enclosing `---` fences) followed
// by body. Lists render inline as `[a, b, c]` to match ki's own style.
func (f *FM) Render(body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range f.Keys {
		if lst, ok := f.lists[k]; ok {
			b.WriteString(k)
			b.WriteString(": [")
			for i, it := range lst {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(quoteIfNeeded(it))
			}
			b.WriteString("]\n")
			continue
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(quoteIfNeeded(f.vals[k]))
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	if body != "" {
		if !strings.HasPrefix(body, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(body)
	}
	return b.String()
}

func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	needs := strings.Contains(s, ": ") ||
		strings.HasSuffix(s, ":") ||
		strings.Contains(s, " #") ||
		s != strings.TrimSpace(s) ||
		strings.IndexAny(s[:1], fmSpecialFirst) == 0
	if needs {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

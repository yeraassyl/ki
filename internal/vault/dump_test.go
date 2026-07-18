package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderAndLoadDump(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteDump(dir, "fix flaky auth tests", "2026-07-18", []string{"reproduce flake locally", "mock clock in auth tests"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "fix-flaky-auth-tests.md" {
		t.Fatalf("path=%s", path)
	}
	d, err := LoadDump(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "fix flaky auth tests" || d.Created != "2026-07-18" {
		t.Fatalf("meta=%q %q", d.Title, d.Created)
	}
	if len(d.Steps) != 2 || d.Steps[0].Done || d.Steps[0].Text != "reproduce flake locally" {
		t.Fatalf("steps=%+v", d.Steps)
	}
	if done, total := d.Progress(); done != 0 || total != 2 {
		t.Fatalf("progress=%d/%d", done, total)
	}
}

func TestDumpTickPreservesFile(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteDump(dir, "migrate tx schema", "2026-07-18", []string{"draft schema", "write migration"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	d, _ := LoadDump(path)
	if err := d.Tick(d.Steps[0].Line); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	want := strings.Replace(string(before), "- [ ] draft schema", "- [x] draft schema", 1)
	if string(after) != want {
		t.Fatalf("tick changed more than one line:\n%q\nwant\n%q", after, want)
	}
	d2, _ := LoadDump(path)
	if done, total := d2.Progress(); done != 1 || total != 2 {
		t.Fatalf("progress=%d/%d", done, total)
	}
}

func TestDumpCollisionAndReservedName(t *testing.T) {
	dir := t.TempDir()
	p1, _ := WriteDump(dir, "same title", "2026-07-18", []string{"a"})
	p2, _ := WriteDump(dir, "same title", "2026-07-18", []string{"b"})
	if p1 == p2 {
		t.Fatal("collision not resolved")
	}
	if filepath.Base(p2) != "same-title-2.md" {
		t.Fatalf("p2=%s", p2)
	}
	// `log` is reserved for the one-liner file.
	p3, _ := WriteDump(dir, "Log", "2026-07-18", []string{"a"})
	if filepath.Base(p3) == "log.md" {
		t.Fatal("dump must not claim log.md")
	}
}

func TestDumpStepsIgnoreFrontmatter(t *testing.T) {
	// A checkbox-looking line inside frontmatter must not become a step.
	raw := "---\ntitle: weird\ncreated: 2026-07-18\nnotes:\n  - [ ] not a step\n---\n\n- [ ] real step\n"
	path := filepath.Join(t.TempDir(), "weird.md")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := LoadDump(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Steps) != 1 || d.Steps[0].Text != "real step" {
		t.Fatalf("steps=%+v", d.Steps)
	}
}

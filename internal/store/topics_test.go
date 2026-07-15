package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ki/internal/config"
)

func TestTopicsAggregation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	cfg := config.Default()

	mkItem(t, cfg, "todo", Item{ID: "a", Title: "a", Status: "open", Created: "2026-07-01", Topic: "auth"})
	mkItem(t, cfg, "idea", Item{ID: "b", Title: "b", Status: "done", Created: "2026-07-03", Topic: "auth"})
	mkItem(t, cfg, "note", Item{ID: "c", Title: "c", Status: "open", Created: "2026-07-02"})

	dir := filepath.Join(cfg.ThreadsPath(), "billing")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	page := "---\nthread: billing\nstatus: in-progress\nupdated: 2026-07-05\nhook: billing rework\n---\n\n## State\n\nx\n"
	if err := os.WriteFile(filepath.Join(dir, "permanent.md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	infos, err := Topics(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("topics=%+v", infos)
	}
	// billing updated 2026-07-05 > auth last 2026-07-03 → billing first
	if infos[0].Topic != "billing" || !infos[0].HasPage || infos[0].Items != 0 {
		t.Fatalf("billing=%+v", infos[0])
	}
	if infos[1].Topic != "auth" || infos[1].Items != 2 || infos[1].Open != 1 || infos[1].HasPage {
		t.Fatalf("auth=%+v", infos[1])
	}

	items, err := ItemsByTopic(cfg, "auth")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Created != "2026-07-01" {
		t.Fatalf("itemsByTopic=%+v", items)
	}
}

func TestCloseItemPreservesFields(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	cfg := config.Default()

	raw := "---\nid: x\ntitle: close me\ntype: todo\nstatus: open\ncreated: 2026-07-01\ncustom: keepme\n---\n\nbody line\n"
	dir := cfg.BucketPath("todo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "x.md")
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	it, err := CloseItem(p, "2026-07-15")
	if err != nil {
		t.Fatal(err)
	}
	if it.Status != "done" {
		t.Fatalf("status=%s", it.Status)
	}
	data, _ := os.ReadFile(p)
	s := string(data)
	for _, want := range []string{"status: done", "closed: 2026-07-15", "custom: keepme", "body line"} {
		if !strings.Contains(s, want) {
			t.Fatalf("closed file missing %q:\n%s", want, s)
		}
	}
	if _, err := CloseItem(p, "2026-07-16"); err == nil {
		t.Fatal("expected error closing an already-done item")
	}
}

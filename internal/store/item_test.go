package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ki/internal/config"
)

func TestNewItemRenderParse(t *testing.T) {
	cl := Classification{
		Bucket: "todo",
		Title:  "Test /voice endpoint, then merge",
		Due:    "2026-07-14",
		Tags:   []string{"voip", "rtoa-6574"},
		Topic: "voip-voice-webhook",
		Body:   "Test /voice endpoint, then merge",
	}
	it := NewItem(cl, "2026-07-13", "jot")
	if it.ID != "2026-07-13-test-voice-endpoint-then-merge" {
		t.Fatalf("id=%s", it.ID)
	}
	if it.Status != "open" {
		t.Fatalf("status=%s", it.Status)
	}

	out := it.Render()
	for _, want := range []string{
		"id: 2026-07-13", "title: ", "type: todo", "status: open",
		"created: 2026-07-13", "due: 2026-07-14", "tags: [voip, rtoa-6574]",
		"topic: voip-voice-webhook", "source: jot",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}

	back, err := ParseItem([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if back.Type != "todo" || back.Due != "2026-07-14" || back.Topic != "voip-voice-webhook" {
		t.Fatalf("parse mismatch: %+v", back)
	}
	if len(back.Tags) != 2 || back.Tags[0] != "voip" {
		t.Fatalf("tags=%v", back.Tags)
	}
}

func TestNewItemDefaultsBodyToTitle(t *testing.T) {
	it := NewItem(Classification{Bucket: "idea", Title: "do a thing"}, "2026-07-13", "manual")
	if it.Body != "do a thing" {
		t.Fatalf("body=%q", it.Body)
	}
}

func TestWriteItemUnique(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	cfg := config.Default()

	it := NewItem(Classification{Bucket: "todo", Title: "same title", Body: "x"}, "2026-07-13", "jot")
	p1, err := WriteItem(cfg, it)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := WriteItem(cfg, it)
	if err != nil {
		t.Fatal(err)
	}
	if p1 == p2 {
		t.Fatal("expected unique paths for duplicate ids")
	}
	if filepath.Dir(p1) != filepath.Join(root, "jot", "todo") {
		t.Fatalf("dir=%s", filepath.Dir(p1))
	}
	if _, err := os.Stat(p2); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p2, "-2.md") {
		t.Fatalf("p2=%s", p2)
	}
}

func TestWriteItemUnknownBucket(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	_, err := WriteItem(config.Default(), NewItem(Classification{Bucket: "nope", Body: "x"}, "2026-07-13", "jot"))
	if err == nil {
		t.Fatal("expected error for unknown bucket")
	}
}

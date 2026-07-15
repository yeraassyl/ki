package store

import (
	"strings"
	"testing"
)

func TestRenderIndexOrderingAndSections(t *testing.T) {
	threads := []Thread{
		{Name: "old", Status: "in-progress", Updated: "2026-06-01", Hook: "older"},
		{Name: "new", Status: "in-progress", Updated: "2026-07-08", Hook: "newer"},
		{Name: "done1", Status: "complete", Updated: "2026-05-01", Hook: "finished"},
	}
	sa := []Standalone{{File: "2026-06-02_x.md", Title: "X thing", Date: "2026-06-02", Type: "implementation"}}

	out := RenderIndex(threads, sa, 16)

	if strings.Index(out, "](new/permanent.md)") > strings.Index(out, "](old/permanent.md)") {
		t.Fatalf("in-progress not sorted newest-first:\n%s", out)
	}
	for _, want := range []string{
		"## In-Progress Threads", "## Completed Threads", "## Standalone",
		"](new/permanent.md)", "](done1/permanent.md)", "X thing", "16 retired",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderIndexEmpty(t *testing.T) {
	out := RenderIndex(nil, nil, 0)
	for _, want := range []string{"_No in-progress threads._", "_None yet._", "_Empty._"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDeriveHook(t *testing.T) {
	body := "## State\n\nBuilt the thing. It works.\n\n## Decisions\n- x\n"
	if h := DeriveHook(body); !strings.HasPrefix(h, "Built the thing") {
		t.Fatalf("hook=%q", h)
	}
}

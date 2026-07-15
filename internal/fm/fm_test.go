package fm

import (
	"strings"
	"testing"
)

func TestParseScalarsAndInlineList(t *testing.T) {
	src := "---\n" +
		"thread: voip-voice-webhook\n" +
		"status: in-progress\n" +
		"updated: 2026-07-08\n" +
		"tags: [twilio, voip, webhook]\n" +
		"related: [voip-integration]\n" +
		"---\n\n## State\nhello\n"
	f, body := Parse([]byte(src))
	if f.Get("thread") != "voip-voice-webhook" {
		t.Fatalf("thread=%q", f.Get("thread"))
	}
	if f.Get("status") != "in-progress" {
		t.Fatalf("status=%q", f.Get("status"))
	}
	tags := f.List("tags")
	if len(tags) != 3 || tags[0] != "twilio" || tags[2] != "webhook" {
		t.Fatalf("tags=%v", tags)
	}
	if !strings.Contains(body, "## State") {
		t.Fatalf("body missing State: %q", body)
	}
}

func TestRoundTrip(t *testing.T) {
	f := New()
	f.Set("thread", "x")
	f.Set("status", "complete")
	f.SetList("tags", []string{"a", "b"})
	out := f.Render("\n## State\nbody\n")

	f2, body := Parse([]byte(out))
	if f2.Get("thread") != "x" || f2.Get("status") != "complete" {
		t.Fatalf("scalars lost:\n%s", out)
	}
	if got := f2.List("tags"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("list lost: %v\n%s", got, out)
	}
	if !strings.Contains(body, "## State") {
		t.Fatalf("body lost: %q", body)
	}
}

func TestColonInValue(t *testing.T) {
	f := New()
	f.Set("hook", "fix: remap 422 to 400 on /accept")
	out := f.Render("")
	f2, _ := Parse([]byte(out))
	if got := f2.Get("hook"); got != "fix: remap 422 to 400 on /accept" {
		t.Fatalf("colon value round-trip failed: %q\n%s", got, out)
	}
}

func TestBlockList(t *testing.T) {
	src := "---\ntags:\n  - one\n  - two\n---\nbody\n"
	f, _ := Parse([]byte(src))
	if got := f.List("tags"); len(got) != 2 || got[0] != "one" {
		t.Fatalf("block list=%v", got)
	}
}

func TestNoFrontmatter(t *testing.T) {
	src := "# just a note\nno fm here\n"
	f, body := Parse([]byte(src))
	if len(f.Keys) != 0 {
		t.Fatalf("unexpected keys %v", f.Keys)
	}
	if body != src {
		t.Fatalf("body changed: %q", body)
	}
}

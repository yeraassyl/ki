package classify

import (
	"strings"
	"testing"

	"ki/internal/config"
)

func TestParseClassificationPlain(t *testing.T) {
	cl, err := parseClassification(`{"bucket":"todo","title":"x","due":"","tags":["a"],"thread":"","body":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if cl.Bucket != "todo" || len(cl.Tags) != 1 {
		t.Fatalf("%+v", cl)
	}
}

func TestParseClassificationFenced(t *testing.T) {
	out := "Here you go:\n```json\n{\"bucket\":\"idea\",\"title\":\"y\",\"body\":\"y\"}\n```\n"
	cl, err := parseClassification(out)
	if err != nil {
		t.Fatal(err)
	}
	if cl.Bucket != "idea" || cl.Title != "y" {
		t.Fatalf("%+v", cl)
	}
}

func TestParseClassificationBad(t *testing.T) {
	if _, err := parseClassification("not json at all"); err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildPromptHasEssentials(t *testing.T) {
	p := buildPrompt(config.Default(), "some note", []string{"voip-voice-webhook"}, "2026-07-13")
	for _, want := range []string{"todo:", "review:", "voip-voice-webhook", "2026-07-13", "minified JSON", "some note"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func TestFallbackBucket(t *testing.T) {
	if got := fallbackBucket(config.Default()); got != "note" {
		t.Fatalf("fallback=%s", got)
	}
}

func TestParseBatch(t *testing.T) {
	out := "```json\n[{\"bucket\":\"todo\",\"title\":\"a\",\"body\":\"a\"},{\"bucket\":\"idea\",\"title\":\"b\",\"body\":\"b\"}]\n```"
	cls, err := parseBatch(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(cls) != 2 || cls[0].Bucket != "todo" || cls[1].Title != "b" {
		t.Fatalf("%+v", cls)
	}
	if _, err := parseBatch("no array here"); err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildBatchPromptHasEssentials(t *testing.T) {
	p := buildBatchPrompt(config.Default(), "some dump", []string{"auth"}, "2026-07-15", "2026-05-24")
	for _, want := range []string{"2026-05-24", "JSON array", "auth", "some dump", "session:"} {
		if !strings.Contains(p, want) {
			t.Fatalf("batch prompt missing %q", want)
		}
	}
}

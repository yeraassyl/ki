package store

import (
	"strings"
	"testing"
)

func TestFileDate(t *testing.T) {
	for name, want := range map[string]string{
		"2026-05-24.md":       "2026-05-24",
		"notes-2025-01-02.md": "2025-01-02",
		"braindump.md":        "",
	} {
		if got := FileDate(name); got != want {
			t.Fatalf("FileDate(%s)=%q want %q", name, got, want)
		}
	}
}

func TestSplitChunksNoHeaders(t *testing.T) {
	chunks := SplitChunks("just a thought\n\nanother one\n", "2026-05-24")
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d want 1", len(chunks))
	}
	if chunks[0].Date != "2026-05-24" {
		t.Fatalf("date=%q", chunks[0].Date)
	}
	if !strings.Contains(chunks[0].Text, "another one") {
		t.Fatalf("text=%q", chunks[0].Text)
	}
}

func TestSplitChunksDateHeaders(t *testing.T) {
	content := "preamble\n\n## 2026-06-01\nfirst day stuff\n\n2026-06-02\nsecond day\n\n# 03.06.2026\nthird day\n"
	chunks := SplitChunks(content, "2026-05-31")
	if len(chunks) != 4 {
		t.Fatalf("chunks=%d want 4: %+v", len(chunks), chunks)
	}
	wantDates := []string{"2026-05-31", "2026-06-01", "2026-06-02", "2026-06-03"}
	for i, w := range wantDates {
		if chunks[i].Date != w {
			t.Fatalf("chunk %d date=%q want %q", i, chunks[i].Date, w)
		}
	}
	if chunks[3].Text != "third day" {
		t.Fatalf("chunk3 text=%q", chunks[3].Text)
	}
}

func TestSplitChunksOversize(t *testing.T) {
	para := strings.Repeat("word ", 600) // ~3000 chars
	content := para + "\n\n" + para + "\n\n" + para + "\n\n" + para
	chunks := SplitChunks(content, "2026-01-01")
	if len(chunks) < 2 {
		t.Fatalf("expected oversize split, got %d chunk(s)", len(chunks))
	}
	for i, ch := range chunks {
		if len(ch.Text) > maxChunkLen {
			t.Fatalf("chunk %d still oversize: %d", i, len(ch.Text))
		}
		if ch.Date != "2026-01-01" {
			t.Fatalf("chunk %d lost date", i)
		}
	}
}

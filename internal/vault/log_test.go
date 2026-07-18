package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadLogMissing(t *testing.T) {
	l, err := LoadLog(filepath.Join(t.TempDir(), "nope", "log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Lines) != 0 || len(l.Entries) != 0 {
		t.Fatalf("missing file should be empty log, got %+v", l)
	}
}

func TestLogParseTolerant(t *testing.T) {
	raw := strings.Join([]string{
		"- [ ] 2026-07-18 14:03 try nuextract3 for extraction",
		"some hand-written note that is not an entry",
		"- [x] 2026-07-17 09:11 fix csv parse edge case",
		"- [ ] no timestamp here, not an entry",
		"- [X] 2026-07-01 08:00 uppercase tick",
		"",
	}, "\n")
	path := filepath.Join(t.TempDir(), "log.md")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 3 {
		t.Fatalf("entries=%d want 3", len(l.Entries))
	}
	if l.Entries[0].Done || l.Entries[0].Text != "try nuextract3 for extraction" || l.Entries[0].Date != "2026-07-18" || l.Entries[0].Time != "14:03" {
		t.Fatalf("entry0=%+v", l.Entries[0])
	}
	if !l.Entries[1].Done || !l.Entries[2].Done {
		t.Fatal("done flags wrong")
	}
	// Round-trip must preserve every byte, including the non-entry lines.
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != raw {
		t.Fatalf("round-trip changed file:\n%q\nwant\n%q", got, raw)
	}
}

func TestLogPrependOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.md")
	l, _ := LoadLog(path)
	l.Prepend("2026-07-17", "10:00", "older")
	l.Prepend("2026-07-18", "11:00", "newer")
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "- [ ] 2026-07-18 11:00 newer\n- [ ] 2026-07-17 10:00 older\n"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
	l2, _ := LoadLog(path)
	if len(l2.Entries) != 2 || l2.Entries[0].Text != "newer" {
		t.Fatalf("entries=%+v", l2.Entries)
	}
}

func TestLogTick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.md")
	raw := "- [ ] 2026-07-18 14:03 alpha\nfree text\n- [ ] 2026-07-17 09:11 beta\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	l, _ := LoadLog(path)
	if err := l.Tick(l.Entries[1].Line); err != nil {
		t.Fatal(err)
	}
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "- [ ] 2026-07-18 14:03 alpha\nfree text\n- [x] 2026-07-17 09:11 beta\n"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// Ticking a non-checkbox or already-done line must fail.
	if err := l.Tick(1); err == nil {
		t.Fatal("expected error ticking free text")
	}
	if err := l.Tick(2); err == nil {
		t.Fatal("expected error ticking done line")
	}
}

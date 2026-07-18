package view

import (
	"strings"
	"testing"
	"time"

	"ki/internal/config"
	"ki/internal/vault"
)

func testVault(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	return config.Config{
		RootDir: root,
		Model:   "haiku",
		Buckets: []config.Bucket{{Name: "miso", Desc: "finance app"}},
	}
}

var now = time.Date(2026, 7, 18, 15, 0, 0, 0, time.Local)

func TestBuildAgeSplitBoundary(t *testing.T) {
	c := testVault(t)
	b := c.Buckets[0]
	l, _ := vault.LoadLog(c.LogPath(b.Name))
	l.Prepend("2026-07-12", "10:00", "six days old")   // 6d -> fresh
	l.Prepend("2026-07-11", "10:00", "seven days old") // 7d -> aging
	l.Prepend("2026-07-18", "10:00", "today")
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}

	board, err := Build(c, c.Buckets, now)
	if err != nil {
		t.Fatal(err)
	}
	bv := board.Buckets[0]
	if bv.Open != 3 || bv.Done != 0 {
		t.Fatalf("counts=%d/%d", bv.Open, bv.Done)
	}
	ages := map[string]int{}
	for _, e := range bv.Oneliners {
		ages[e.Text] = e.AgeDays
	}
	if ages["today"] != 0 || ages["six days old"] != 6 || ages["seven days old"] != 7 {
		t.Fatalf("ages=%v", ages)
	}

	out := Render(board, Options{Days: 7})
	if !strings.Contains(out, "fresh (<7d)") || !strings.Contains(out, "aging (≥7d)") {
		t.Fatalf("expected two columns:\n%s", out)
	}
	// The 7d entry must be on the aging side: same row as the fresh header's
	// first entry or below, right of the divider.
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "seven days old") && !strings.Contains(ln, "│") {
			t.Fatalf("aging entry not in right column: %q", ln)
		}
		if l, r, ok := strings.Cut(ln, "│"); ok && strings.Contains(l, "seven days old") {
			t.Fatalf("aging entry on left: %q | %q", l, r)
		}
	}
}

func TestRenderSingleColumnAndDone(t *testing.T) {
	c := testVault(t)
	b := c.Buckets[0]
	l, _ := vault.LoadLog(c.LogPath(b.Name))
	l.Prepend("2026-07-17", "09:00", "finished thing")
	l.Prepend("2026-07-18", "10:00", "open thing")
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	l2, _ := vault.LoadLog(c.LogPath(b.Name))
	if err := l2.Tick(l2.Entries[1].Line); err != nil {
		t.Fatal(err)
	}
	if err := l2.Save(); err != nil {
		t.Fatal(err)
	}

	board, _ := Build(c, c.Buckets, now)
	out := Render(board, Options{Days: 7})
	if strings.Contains(out, "│") {
		t.Fatalf("no aging entries, expected single column:\n%s", out)
	}
	if strings.Contains(out, "finished thing") {
		t.Fatalf("done hidden by default:\n%s", out)
	}
	if !strings.Contains(out, "(1 open / 1 done)") {
		t.Fatalf("header counts wrong:\n%s", out)
	}

	out = Render(board, Options{Days: 7, ShowDone: true})
	if !strings.Contains(out, "done:") || !strings.Contains(out, "- [x] 07-17 09:00 finished thing") {
		t.Fatalf("--done output wrong:\n%s", out)
	}
}

func TestBoardDumps(t *testing.T) {
	c := testVault(t)
	b := c.Buckets[0]
	path, err := vault.WriteDump(c.BucketPath(b.Name), "fix flaky auth tests", "2026-07-15", []string{"reproduce", "fix"})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := vault.LoadDump(path)
	if err := d.Tick(d.Steps[0].Line); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}

	board, _ := Build(c, c.Buckets, now)
	bv := board.Buckets[0]
	if len(bv.Dumps) != 1 || bv.Dumps[0].Open != 1 || bv.Dumps[0].Done != 1 || bv.Dumps[0].AgeDays != 3 {
		t.Fatalf("dumps=%+v", bv.Dumps)
	}
	if bv.Dumps[0].Path != "miso/fix-flaky-auth-tests.md" {
		t.Fatalf("path=%s", bv.Dumps[0].Path)
	}
	if bv.Open != 1 || bv.Done != 1 {
		t.Fatalf("bucket counts=%d/%d", bv.Open, bv.Done)
	}

	out := Render(board, Options{Days: 7})
	if !strings.Contains(out, "dumps:") || !strings.Contains(out, "(1/2)  3d ago") {
		t.Fatalf("dump line wrong:\n%s", out)
	}
}

func TestRenderEmptyBucket(t *testing.T) {
	c := testVault(t)
	board, _ := Build(c, c.Buckets, now)
	out := Render(board, Options{})
	if !strings.Contains(out, "(empty)") || !strings.Contains(out, "(0 open / 0 done)") {
		t.Fatalf("empty bucket render:\n%s", out)
	}
}

func TestTruncateAndPad(t *testing.T) {
	long := strings.Repeat("x", 60)
	if got := truncate(long, 46); len([]rune(got)) != 46 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncate=%q", got)
	}
	if got := pad("ab"); len([]rune(got)) != 47 {
		t.Fatalf("pad len=%d", len([]rune(got)))
	}
}

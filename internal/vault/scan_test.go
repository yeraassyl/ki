package vault

import (
	"os"
	"path/filepath"
	"testing"

	"ki/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	return config.Config{
		RootDir: root,
		Model:   "haiku",
		Buckets: []config.Bucket{{Name: "miso", Desc: "finance app"}},
	}
}

func TestScanBucketEmpty(t *testing.T) {
	c := testConfig(t)
	bd, err := ScanBucket(c, c.Buckets[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Log.Entries) != 0 || len(bd.Dumps) != 0 {
		t.Fatalf("expected empty bucket, got %+v", bd)
	}
	if open, done := bd.Counts(); open != 0 || done != 0 {
		t.Fatalf("counts=%d/%d", open, done)
	}
}

func TestScanBucketCountsAndOrder(t *testing.T) {
	c := testConfig(t)
	b := c.Buckets[0]

	l, _ := LoadLog(c.LogPath(b.Name))
	l.Prepend("2026-07-17", "10:00", "one")
	l.Prepend("2026-07-18", "11:00", "two")
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteDump(c.BucketPath(b.Name), "older dump", "2026-07-10", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteDump(c.BucketPath(b.Name), "newer dump", "2026-07-18", []string{"c"}); err != nil {
		t.Fatal(err)
	}
	// Skipped files: hidden, underscore, non-md.
	os.WriteFile(filepath.Join(c.BucketPath(b.Name), "_notes.md"), []byte("- [ ] x"), 0o644)
	os.WriteFile(filepath.Join(c.BucketPath(b.Name), ".hidden.md"), []byte("- [ ] x"), 0o644)

	bd, err := ScanBucket(c, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Dumps) != 2 || bd.Dumps[0].Title != "newer dump" {
		t.Fatalf("dumps=%+v", bd.Dumps)
	}
	open, done := bd.Counts()
	if open != 5 || done != 0 {
		t.Fatalf("counts=%d open %d done, want 5/0", open, done)
	}

	// Tick one log entry and one step, recount.
	l2, _ := LoadLog(c.LogPath(b.Name))
	if err := l2.Tick(l2.Entries[0].Line); err != nil {
		t.Fatal(err)
	}
	if err := l2.Save(); err != nil {
		t.Fatal(err)
	}
	d := bd.Dumps[1]
	if err := d.Tick(d.Steps[0].Line); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	bd2, _ := ScanBucket(c, b)
	if open, done := bd2.Counts(); open != 3 || done != 2 {
		t.Fatalf("counts=%d open %d done, want 3/2", open, done)
	}
}

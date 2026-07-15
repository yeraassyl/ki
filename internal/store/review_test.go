package store

import (
	"testing"
	"time"

	"ki/internal/config"
)

func mkItem(t *testing.T, cfg config.Config, bucket string, it Item) {
	t.Helper()
	it.Type = bucket
	if _, err := WriteItem(cfg, it); err != nil {
		t.Fatal(err)
	}
}

func TestDigestOverdueSoonStale(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	cfg := config.Default()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

	mkItem(t, cfg, "todo", Item{ID: "a", Title: "overdue task", Status: "open", Created: "2026-07-01", Due: "2026-07-10"})
	mkItem(t, cfg, "todo", Item{ID: "b", Title: "soon task", Status: "open", Created: "2026-07-12", Due: "2026-07-15"})
	mkItem(t, cfg, "todo", Item{ID: "c", Title: "far task", Status: "open", Created: "2026-07-12", Due: "2026-09-01"})
	mkItem(t, cfg, "idea", Item{ID: "d", Title: "old idea", Status: "open", Created: "2026-06-10"})
	mkItem(t, cfg, "idea", Item{ID: "e", Title: "new idea", Status: "open", Created: "2026-07-12"})
	mkItem(t, cfg, "todo", Item{ID: "f", Title: "done", Status: "done", Created: "2026-01-01", Due: "2026-01-02"})

	rev, err := Digest(cfg, ReviewOptions{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Overdue) != 1 || rev.Overdue[0].Title != "overdue task" {
		t.Fatalf("overdue=%+v", rev.Overdue)
	}
	if rev.Overdue[0].DueInDays != -3 {
		t.Fatalf("dueInDays=%d want -3", rev.Overdue[0].DueInDays)
	}
	if len(rev.DueSoon) != 1 || rev.DueSoon[0].Title != "soon task" {
		t.Fatalf("dueSoon=%+v", rev.DueSoon)
	}
	if len(rev.Stale) != 1 || rev.Stale[0].Title != "old idea" {
		t.Fatalf("stale=%+v", rev.Stale)
	}
	if rev.Counts["todo"] != 3 || rev.Counts["idea"] != 2 {
		t.Fatalf("counts=%v (done item should be excluded)", rev.Counts)
	}
}

func TestDigestSkipsNoReviewBucketsAndFiltersTopic(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	cfg := config.Default()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

	// session bucket is no_review: an ancient open distillate must never surface.
	mkItem(t, cfg, "session", Item{ID: "s", Title: "old distillate", Status: "open", Created: "2026-01-01", Topic: "auth"})
	mkItem(t, cfg, "todo", Item{ID: "a", Title: "auth todo", Status: "open", Created: "2026-06-01", Topic: "auth"})
	mkItem(t, cfg, "todo", Item{ID: "b", Title: "other todo", Status: "open", Created: "2026-06-01"})

	rev, err := Digest(cfg, ReviewOptions{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Counts["session"] != 0 {
		t.Fatalf("session bucket leaked into review: %v", rev.Counts)
	}
	if len(rev.Stale) != 2 {
		t.Fatalf("stale=%+v", rev.Stale)
	}

	rev, err = Digest(cfg, ReviewOptions{Topic: "auth"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Stale) != 1 || rev.Stale[0].Title != "auth todo" {
		t.Fatalf("topic filter: stale=%+v", rev.Stale)
	}
}

func TestDigestOnlyOverdue(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	cfg := config.Default()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

	mkItem(t, cfg, "todo", Item{ID: "a", Title: "overdue", Status: "open", Created: "2026-07-01", Due: "2026-07-10"})
	mkItem(t, cfg, "idea", Item{ID: "d", Title: "old idea", Status: "open", Created: "2026-06-10"})

	rev, err := Digest(cfg, ReviewOptions{OnlyOverdue: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Overdue) != 1 || rev.Stale != nil || rev.DueSoon != nil {
		t.Fatalf("onlyOverdue failed: %+v", rev)
	}
}

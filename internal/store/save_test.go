package store

import (
	"os"
	"strings"
	"testing"

	"ki/internal/config"
)

func TestSaveNewThreadAndLosslessIndex(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	cfg := config.Default()

	req := SaveRequest{
		Thread: "voip-voice-webhook",
		Status: "in-progress",
		Hook:   "POST /voip/voice Twilio webhook; tests green; merge pending",
		Jira:   "RTOA-6574",
		Tags:   []string{"twilio", "voip"},
		Sections: map[string]string{
			"State":       "Built the webhook.",
			"Next Prompt": "Merge and deploy.",
		},
		Changelog: "built the voice webhook",
	}
	res, err := Save(cfg, req, "2026-07-13", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created {
		t.Fatal("expected Created=true")
	}

	perm, err := os.ReadFile(res.PermanentPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"thread: voip-voice-webhook", "status: in-progress", "updated: 2026-07-13",
		"hook: ", "jira: RTOA-6574", "tags: [twilio, voip]",
		"## State", "Built the webhook.", "## Next Prompt", "Merge and deploy.",
	} {
		if !strings.Contains(string(perm), want) {
			t.Fatalf("permanent missing %q:\n%s", want, perm)
		}
	}
	if strings.Index(string(perm), "## State") > strings.Index(string(perm), "## Next Prompt") {
		t.Fatal("sections out of canonical order")
	}

	cl, _ := os.ReadFile(res.ChangelogPath)
	if !strings.Contains(string(cl), "# voip-voice-webhook — changelog") ||
		!strings.Contains(string(cl), "2026-07-13: built the voice webhook") {
		t.Fatalf("changelog:\n%s", cl)
	}

	threads, _ := ScanThreads(cfg)
	if len(threads) != 1 || threads[0].HookDerived {
		t.Fatalf("expected 1 thread with a non-derived hook, got %+v", threads)
	}
	idx, _ := os.ReadFile(cfg.IndexPath())
	if !strings.Contains(string(idx), "POST /voip/voice Twilio webhook") {
		t.Fatalf("index missing hook:\n%s", idx)
	}
}

func TestSaveMergesFrontmatterAndPrependsChangelog(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	cfg := config.Default()

	base := SaveRequest{
		Thread: "t", Status: "in-progress", Hook: "h1", Jira: "RTOA-1",
		Sections: map[string]string{"State": "s1"}, Changelog: "first",
	}
	if _, err := Save(cfg, base, "2026-07-10", false); err != nil {
		t.Fatal(err)
	}

	next := SaveRequest{
		Thread: "t", Hook: "h2",
		Sections: map[string]string{"State": "s2"}, Changelog: "second",
	}
	res, err := Save(cfg, next, "2026-07-13", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created {
		t.Fatal("expected update, not create")
	}

	perm, _ := os.ReadFile(res.PermanentPath)
	for _, want := range []string{"jira: RTOA-1", "updated: 2026-07-13", "hook: h2", "s2"} {
		if !strings.Contains(string(perm), want) {
			t.Fatalf("permanent missing %q:\n%s", want, perm)
		}
	}
	if strings.Contains(string(perm), "s1") {
		t.Fatal("old State body should be replaced")
	}
	if _, err := os.Stat(res.PermanentPath + ".bak"); err != nil {
		t.Fatal("expected a .bak backup of the previous permanent.md")
	}

	cl, _ := os.ReadFile(res.ChangelogPath)
	if strings.Index(string(cl), "second") > strings.Index(string(cl), "first") {
		t.Fatalf("changelog not newest-first:\n%s", cl)
	}
}

func TestSaveDryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	cfg := config.Default()

	req := SaveRequest{Thread: "t", Hook: "h", Sections: map[string]string{"State": "x"}}
	res, err := Save(cfg, req, "2026-07-13", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Permanent, "## State") {
		t.Fatal("dry-run should still render content")
	}
	if _, err := os.Stat(res.PermanentPath); !os.IsNotExist(err) {
		t.Fatal("dry-run must not write permanent.md")
	}
}

func TestSaveRejectsBadThread(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	_, err := Save(config.Default(), SaveRequest{Thread: "../evil", Sections: map[string]string{"State": "x"}}, "2026-07-13", false)
	if err == nil {
		t.Fatal("expected error for bad thread name")
	}
}

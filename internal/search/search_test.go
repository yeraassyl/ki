package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ki/internal/config"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T) config.Config {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)
	writeFile(t, root, "agent-artifacts/voip-voice-webhook/permanent.md",
		"---\nthread: voip-voice-webhook\nstatus: in-progress\njira: RTOA-6574\ntags: [voip, twilio]\n---\n\n## State\nBuilt POST /voice webhook. Continues [[voip-integration]].\n")
	writeFile(t, root, "agent-artifacts/voip-integration/permanent.md",
		"---\nthread: voip-integration\nstatus: in-progress\ntags: [voip]\n---\n\n## State\nVoIP integration groundwork.\n")
	writeFile(t, root, "jot/todo/2026-07-13-test-voice.md",
		"---\nid: 2026-07-13-test-voice\ntype: todo\nstatus: open\ntags: [voip]\n---\n\ntest voice endpoint then merge\n")
	writeFile(t, root, "brain dump/random.md", "unrelated musings about lunch\n")
	writeFile(t, root, "agent-artifacts/_index.md", "# Agent Artifacts\nvoip everything\n")
	return config.Default()
}

func mustFind(t *testing.T, c config.Config, q string, o Options) []Hit {
	t.Helper()
	h, err := Find(c, q, o)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func hitMap(hits []Hit) map[string]Hit {
	m := map[string]Hit{}
	for _, h := range hits {
		m[h.Path] = h
	}
	return m
}

func TestFindByIdentifier(t *testing.T) {
	cfg := setup(t)
	hits := mustFind(t, cfg, "RTOA-6574", Options{})
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Path != "agent-artifacts/voip-voice-webhook/permanent.md" {
		t.Fatalf("top hit = %s", hits[0].Path)
	}
	if !contains(hits[0].Reasons, "id") {
		t.Fatalf("reasons=%v", hits[0].Reasons)
	}
}

func TestFindByTagSkipsIndexAndNoise(t *testing.T) {
	cfg := setup(t)
	m := hitMap(mustFind(t, cfg, "voip", Options{}))
	for _, want := range []string{
		"agent-artifacts/voip-voice-webhook/permanent.md",
		"agent-artifacts/voip-integration/permanent.md",
		"jot/todo/2026-07-13-test-voice.md",
	} {
		if _, ok := m[want]; !ok {
			t.Fatalf("missing %s", want)
		}
	}
	if _, ok := m["agent-artifacts/_index.md"]; ok {
		t.Fatal("_index.md should be skipped")
	}
	if _, ok := m["brain dump/random.md"]; ok {
		t.Fatal("unrelated note should not match")
	}
}

func TestGraphExpand(t *testing.T) {
	cfg := setup(t)
	base := hitMap(mustFind(t, cfg, "RTOA-6574", Options{}))
	if _, ok := base["agent-artifacts/voip-integration/permanent.md"]; ok {
		t.Fatal("neighbour should not match a bare id search without --graph")
	}
	g := hitMap(mustFind(t, cfg, "RTOA-6574", Options{Graph: true}))
	nb, ok := g["agent-artifacts/voip-integration/permanent.md"]
	if !ok {
		t.Fatal("graph did not surface the [[voip-integration]] neighbour")
	}
	if !hasPrefixReason(nb.Reasons, "linked:") {
		t.Fatalf("reasons=%v", nb.Reasons)
	}
}

func TestKindFilter(t *testing.T) {
	cfg := setup(t)
	hits := mustFind(t, cfg, "voip", Options{Kinds: []string{"jot"}})
	if len(hits) != 1 || hits[0].Kind != "jot" {
		t.Fatalf("expected only jot, got %v", hits)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func hasPrefixReason(xs []string, p string) bool {
	for _, x := range xs {
		if strings.HasPrefix(x, p) {
			return true
		}
	}
	return false
}

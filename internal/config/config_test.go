package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultSaveLoad(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)

	if _, err := Load(); err != ErrNotInitialized {
		t.Fatalf("expected ErrNotInitialized, got %v", err)
	}

	if err := Save(Default()); err != nil {
		t.Fatal(err)
	}
	if got := ConfigPath(); got != filepath.Join(root, ".ki", "config.json") {
		t.Fatalf("config path=%s", got)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Buckets) != 6 {
		t.Fatalf("buckets=%d want 6", len(got.Buckets))
	}
	if !got.HasBucket("todo") || !got.HasBucket("review") {
		t.Fatal("missing seed buckets")
	}
	if got.Root() != root {
		t.Fatalf("root=%s want %s", got.Root(), root)
	}
	if got.IndexPath() != filepath.Join(root, "agent-artifacts", "_index.md") {
		t.Fatalf("index path=%s", got.IndexPath())
	}
}

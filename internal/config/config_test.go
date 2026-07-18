package config

import (
	"os"
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
	if len(got.Buckets) != 0 {
		t.Fatalf("buckets=%d want 0 (created via ki bucket add)", len(got.Buckets))
	}
	if got.Model != "haiku" {
		t.Fatalf("model=%s want haiku", got.Model)
	}
	if got.Root() != root {
		t.Fatalf("root=%s want %s", got.Root(), root)
	}
}

func TestBucketLookupAndPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)

	c := Default()
	c.Buckets = []Bucket{{Name: "miso", Desc: "finance app"}, {Name: "home", Desc: "life admin"}}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasBucket("miso") || got.HasBucket("nope") {
		t.Fatal("HasBucket wrong")
	}
	if b, ok := got.FindBucket("home"); !ok || b.Desc != "life admin" {
		t.Fatalf("FindBucket home=%+v ok=%v", b, ok)
	}
	if got.BucketPath("miso") != filepath.Join(root, "miso") {
		t.Fatalf("bucket path=%s", got.BucketPath("miso"))
	}
	if got.LogPath("miso") != filepath.Join(root, "miso", "log.md") {
		t.Fatalf("log path=%s", got.LogPath("miso"))
	}
	if names := got.BucketNames(); len(names) != 2 || names[0] != "miso" {
		t.Fatalf("names=%v", names)
	}
}

func TestLoadLegacyVault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)

	legacy := `{"root":"~/ki","threads_dir":"agent-artifacts","jot_root":"jot","buckets":[{"name":"todo","desc":"x"}]}`
	if err := os.MkdirAll(filepath.Join(root, ".ki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ki", "config.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err != ErrLegacyVault {
		t.Fatalf("expected ErrLegacyVault, got %v", err)
	}
	if !IsLegacyConfig([]byte(legacy)) {
		t.Fatal("IsLegacyConfig should detect v0.7 schema")
	}
	if IsLegacyConfig([]byte(`{"root":"~/ki","model":"haiku"}`)) {
		t.Fatal("IsLegacyConfig false positive on v2 schema")
	}
}

func TestModelDefault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KI_ROOT", root)

	if err := Save(Config{RootDir: root}); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "haiku" {
		t.Fatalf("empty model should default to haiku, got %q", got.Model)
	}
}

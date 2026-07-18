// Package config loads and persists the ki vault configuration. Buckets ARE
// projects: each carries a short description that is the only context the LLM
// gets when breaking a braindump into steps. The config is the single source of
// truth for the bucket set and the vault root; the LLM never invents either.
// Stored as JSON at <root>/.ki/config.json so it needs no third-party parser.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotInitialized is returned by Load when no config file exists yet.
var ErrNotInitialized = errors.New("ki not initialized (run: ki init)")

// ErrLegacyVault is returned by Load when the config on disk still has the
// v0.7 schema (type buckets + jot/threads dirs). `ki init` archives it.
var ErrLegacyVault = errors.New("legacy v0.7 vault detected (run: ki init to archive it and start fresh)")

// Bucket is one project. Desc is required at creation: it is the thin project
// context handed to the LLM for braindump breakdown.
type Bucket struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// Config is the whole ki configuration.
type Config struct {
	RootDir string   `json:"root"`
	Model   string   `json:"model"`
	Buckets []Bucket `json:"buckets"`
}

// Default returns the seed configuration: no buckets yet, they are created
// explicitly with `ki bucket add`.
func Default() Config {
	return Config{RootDir: "~/ki", Model: "haiku"}
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// DiscoverRoot locates the ki root used to find the config file: $KI_ROOT
// if set, else ~/ki.
func DiscoverRoot() string {
	if r := os.Getenv("KI_ROOT"); r != "" {
		return expandHome(r)
	}
	return expandHome("~/ki")
}

// ConfigPath is the absolute path of the config file.
func ConfigPath() string {
	return filepath.Join(DiscoverRoot(), ".ki", "config.json")
}

// Load reads the config, returning ErrNotInitialized if it is absent and
// ErrLegacyVault if it still has the v0.7 schema.
func Load() (Config, error) {
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, ErrNotInitialized
		}
		return Config{}, err
	}
	if IsLegacyConfig(data) {
		return Config{}, ErrLegacyVault
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", ConfigPath(), err)
	}
	if c.RootDir == "" {
		c.RootDir = DiscoverRoot()
	}
	if c.Model == "" {
		c.Model = "haiku"
	}
	return c, nil
}

// IsLegacyConfig reports whether raw config JSON has the v0.7 schema.
func IsLegacyConfig(data []byte) bool {
	var probe struct {
		JotRoot    string `json:"jot_root"`
		ThreadsDir string `json:"threads_dir"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.JotRoot != "" || probe.ThreadsDir != ""
}

// Save writes the config as indented JSON, creating .ki/ as needed.
func Save(c Config) error {
	if err := os.MkdirAll(filepath.Dir(ConfigPath()), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(), append(data, '\n'), 0o644)
}

// Root is the absolute ki root. $KI_ROOT overrides the config field so
// tests and relocations work without editing the file.
func (c Config) Root() string {
	if r := os.Getenv("KI_ROOT"); r != "" {
		return expandHome(r)
	}
	if c.RootDir != "" {
		return expandHome(c.RootDir)
	}
	return DiscoverRoot()
}

// BucketPath is one bucket's directory.
func (c Config) BucketPath(name string) string { return filepath.Join(c.Root(), name) }

// LogPath is the one-liner log file inside a bucket.
func (c Config) LogPath(name string) string { return filepath.Join(c.BucketPath(name), "log.md") }

// BucketNames returns bucket names in config order.
func (c Config) BucketNames() []string {
	out := make([]string, 0, len(c.Buckets))
	for _, b := range c.Buckets {
		out = append(out, b.Name)
	}
	return out
}

// FindBucket returns the named bucket, or false if it is not configured.
func (c Config) FindBucket(name string) (Bucket, bool) {
	for _, b := range c.Buckets {
		if b.Name == name {
			return b, true
		}
	}
	return Bucket{}, false
}

// HasBucket reports whether name is a configured bucket.
func (c Config) HasBucket(name string) bool {
	_, ok := c.FindBucket(name)
	return ok
}

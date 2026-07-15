// Package config loads and persists the ki taxonomy and paths. The config is
// the single source of truth for buckets, directory layout, and content budgets;
// the LLM is only ever *told* this set (it never invents buckets). Stored as JSON
// at <root>/.ki/config.json so it needs no third-party parser.
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

// Bucket is one jot category. Fields lists extra frontmatter keys an item in
// this bucket is expected to carry (e.g. "due" for todos). NoReview excludes
// the bucket from `ki review` digests (session distillates are not actionable).
type Bucket struct {
	Name     string   `json:"name"`
	Desc     string   `json:"desc"`
	Fields   []string `json:"fields,omitempty"`
	NoReview bool     `json:"no_review,omitempty"`
}

// Budgets caps generated content so artifacts stay tight.
type Budgets struct {
	StateWords   int `json:"state_words"`
	DecisionLine int `json:"decision_line"`
}

// Classifier configures the model used to categorize/enrich jot captures.
type Classifier struct {
	Model string `json:"model"`
}

// Config is the whole ki configuration.
type Config struct {
	RootDir  string     `json:"root"`
	ThreadsDir string     `json:"threads_dir"`
	JotRoot    string     `json:"jot_root"`
	Buckets    []Bucket   `json:"buckets"`
	Budgets    Budgets    `json:"budgets"`
	Classifier Classifier `json:"classifier"`
}

// Default returns the seed configuration.
func Default() Config {
	return Config{
		RootDir:  "~/ki",
		ThreadsDir: "agent-artifacts",
		JotRoot:    "jot",
		Buckets: []Bucket{
			{Name: "todo", Desc: "actionable, usually has a deadline", Fields: []string{"due"}},
			{Name: "idea", Desc: "something to act on later, no deadline"},
			{Name: "question", Desc: "an open question to self, needs an answer"},
			{Name: "review", Desc: "pasted PR/review feedback; body kept verbatim"},
			{Name: "note", Desc: "out-of-context braindump / reference"},
			{Name: "session", Desc: "session distillate: state, decisions, next steps from a work session", Fields: []string{"topic"}, NoReview: true},
		},
		Budgets:    Budgets{StateWords: 120, DecisionLine: 1},
		Classifier: Classifier{Model: "haiku"},
	}
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

// Load reads the config, returning ErrNotInitialized if it is absent.
func Load() (Config, error) {
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, ErrNotInitialized
		}
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", ConfigPath(), err)
	}
	if c.RootDir == "" {
		c.RootDir = DiscoverRoot()
	}
	return c, nil
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

// ThreadsPath is the artifact-threads directory.
func (c Config) ThreadsPath() string { return filepath.Join(c.Root(), c.ThreadsDir) }

// IndexPath is the thread index file.
func (c Config) IndexPath() string { return filepath.Join(c.ThreadsPath(), "_index.md") }

// ArchivePath is the retired-artifacts directory.
func (c Config) ArchivePath() string { return filepath.Join(c.ThreadsPath(), "_archive") }

// JotPath is the root of the jot buckets.
func (c Config) JotPath() string { return filepath.Join(c.Root(), c.JotRoot) }

// BucketPath is one bucket's directory.
func (c Config) BucketPath(name string) string { return filepath.Join(c.JotPath(), name) }

// BucketNames returns bucket names in config order.
func (c Config) BucketNames() []string {
	out := make([]string, 0, len(c.Buckets))
	for _, b := range c.Buckets {
		out = append(out, b.Name)
	}
	return out
}

// HasBucket reports whether name is a configured bucket.
func (c Config) HasBucket(name string) bool {
	for _, b := range c.Buckets {
		if b.Name == name {
			return true
		}
	}
	return false
}

// BucketNoReview reports whether the named bucket is excluded from review digests.
func (c Config) BucketNoReview(name string) bool {
	for _, b := range c.Buckets {
		if b.Name == name {
			return b.NoReview
		}
	}
	return false
}

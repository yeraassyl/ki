package store

import (
	"os"
	"path/filepath"
	"sort"

	"ki/internal/config"
)

// TopicInfo aggregates everything known about one topic value: how many items
// carry it, whether a curated page exists, and when it was last touched. Topics
// are emergent — any `topic:` value on an item counts, no page required.
type TopicInfo struct {
	Topic      string `json:"topic"`
	Items      int    `json:"items"`
	Open       int    `json:"open"`
	LastActive string `json:"last_active,omitempty"` // max of item created / page updated
	HasPage    bool   `json:"has_page"`
	PagePath   string `json:"page_path,omitempty"`
}

// Topics aggregates topic values across all items and topic pages, sorted by
// last activity (newest first), then name.
func Topics(c config.Config) ([]TopicInfo, error) {
	items, err := ScanItems(c)
	if err != nil {
		return nil, err
	}
	byName := map[string]*TopicInfo{}
	get := func(name string) *TopicInfo {
		if t, ok := byName[name]; ok {
			return t
		}
		t := &TopicInfo{Topic: name}
		byName[name] = t
		return t
	}
	for _, it := range items {
		if it.Topic == "" {
			continue
		}
		t := get(it.Topic)
		t.Items++
		if it.Status == "" || it.Status == "open" {
			t.Open++
		}
		if it.Created > t.LastActive {
			t.LastActive = it.Created
		}
	}
	threads, err := ScanThreads(c)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, th := range threads {
		t := get(th.Name)
		t.HasPage = true
		t.PagePath = filepath.Join(c.ThreadsPath(), th.Name, "permanent.md")
		if th.Updated > t.LastActive {
			t.LastActive = th.Updated
		}
	}
	out := make([]TopicInfo, 0, len(byName))
	for _, t := range byName {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastActive != out[j].LastActive {
			return out[i].LastActive > out[j].LastActive
		}
		return out[i].Topic < out[j].Topic
	})
	return out, nil
}

// TopicNames returns all known topic values (item topics + page names), for the
// classifier's "may link to" list.
func TopicNames(c config.Config) []string {
	infos, err := Topics(c)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(infos))
	for _, t := range infos {
		out = append(out, t.Topic)
	}
	return out
}

// ItemsByTopic returns the items carrying the topic, oldest first (chronological
// collation order for compression or reload).
func ItemsByTopic(c config.Config, topic string) ([]Item, error) {
	items, err := ScanItems(c)
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, it := range items {
		if it.Topic == topic {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created < out[j].Created
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

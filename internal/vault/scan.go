package vault

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ki/internal/config"
)

// BucketData is everything on disk for one bucket: the one-liner log and the
// dump files, dumps newest first.
type BucketData struct {
	Bucket config.Bucket
	Log    Log
	Dumps  []Dump
}

// ScanBucket loads a bucket's log and dumps. A missing bucket dir is an empty
// bucket. Hidden and `_`-prefixed files are skipped.
func ScanBucket(c config.Config, b config.Bucket) (BucketData, error) {
	bd := BucketData{Bucket: b}
	var err error
	bd.Log, err = LoadLog(c.LogPath(b.Name))
	if err != nil {
		return bd, err
	}
	entries, err := os.ReadDir(c.BucketPath(b.Name))
	if err != nil {
		if os.IsNotExist(err) {
			return bd, nil
		}
		return bd, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "log.md" ||
			strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		d, err := LoadDump(filepath.Join(c.BucketPath(b.Name), name))
		if err != nil {
			continue
		}
		bd.Dumps = append(bd.Dumps, d)
	}
	sort.SliceStable(bd.Dumps, func(i, j int) bool {
		if bd.Dumps[i].Created != bd.Dumps[j].Created {
			return bd.Dumps[i].Created > bd.Dumps[j].Created
		}
		return bd.Dumps[i].Path < bd.Dumps[j].Path
	})
	return bd, nil
}

// Counts returns the bucket's open and done totals across log entries and
// dump steps.
func (bd BucketData) Counts() (open, done int) {
	for _, e := range bd.Log.Entries {
		if e.Done {
			done++
		} else {
			open++
		}
	}
	for _, d := range bd.Dumps {
		dn, total := d.Progress()
		done += dn
		open += total - dn
	}
	return open, done
}

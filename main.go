// Command ki is the deterministic backbone of the artifact/braindump system:
// it owns paths, config, the bucket taxonomy, index projection, capture, and (in
// later phases) search and artifact upsert. The LLM produces content; this binary
// places it. See README.md for the phase roadmap.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ki/internal/classify"
	"ki/internal/config"
	"ki/internal/search"
	"ki/internal/store"
)

const version = "0.7.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	args := os.Args[2:]
	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit(args)
	case "buckets":
		err = cmdBuckets(args)
	case "index":
		err = cmdIndex(args)
	case "jot":
		err = cmdJot(args)
	case "find":
		err = cmdFind(args)
	case "done":
		err = cmdDone(args)
	case "import":
		err = cmdImport(args)
	case "topics":
		err = cmdTopics(args)
	case "topic":
		err = cmdTopic(args)
	case "save":
		err = cmdSave(args)
	case "review":
		err = cmdReview(args)
	case "version", "-v", "--version":
		fmt.Println("ki " + version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `ki — artifact + braindump backbone

usage: ki <command> [flags]

commands:
  init                 create config (~/ki/.ki/config.json) and bucket dirs
  buckets [--json]     print the current bucket taxonomy
  jot "<text>"         classify a note into a bucket, preview, confirm, write
  import <path>        split + classify a braindump file (or folder) into items
  done "<id|terms>"    close the matching open item (status: done)
  find "<terms>"       tiered search across items, topics, and notes
  review [--overdue]   surface open items that are overdue, due soon, or stale
  topics [--json]      list topic values: item counts, last activity, page?
  topic <name>         show a topic's page + items; --full prints collation
  index [--write]      project _index.md from topic-page frontmatter
  save --json <req>    write a topic page (permanent.md + changelog + index)
  version | help

jot flags:
  -b, --bucket NAME    skip the classifier and file directly into NAME
      --due DATE       set/override due date (YYYY-MM-DD)
      --topic NAME     set/override linked topic
      --tag TAG        add a tag (repeatable)
      --json PAYLOAD   write a pre-made Classification JSON (for skills)
      --dry-run        classify and print JSON, write nothing
  -y, --yes            skip the confirm prompt

import flags:
  --dry-run            classify and print the item plan as JSON, write nothing
  -y, --yes            write everything without confirming
  -b, --bucket NAME    skip the classifier; one item per chunk into NAME
  --archive            move the source file(s) to _imported/ after writing

find/review also take --topic NAME to restrict to one topic.
`)
}

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	force := fs.Bool("force", false, "overwrite an existing config with defaults")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	switch {
	case err == config.ErrNotInitialized || *force:
		cfg = config.Default()
		if e := config.Save(cfg); e != nil {
			return e
		}
		fmt.Println("wrote", config.ConfigPath())
	case err != nil:
		return err
	default:
		fmt.Println("config already exists:", config.ConfigPath(), "(use --force to reset)")
	}

	dirs := []string{cfg.ThreadsPath(), cfg.JotPath()}
	for _, b := range cfg.Buckets {
		dirs = append(dirs, cfg.BucketPath(b.Name))
	}
	for _, d := range dirs {
		if e := os.MkdirAll(d, 0o755); e != nil {
			return e
		}
	}
	fmt.Printf("ki ready at %s\n  threads: %s/\n  jot:     %s/ (%s)\n",
		cfg.Root(), cfg.ThreadsDir, cfg.JotRoot, strings.Join(cfg.BucketNames(), ", "))
	return nil
}

func cmdBuckets(args []string) error {
	fs := flag.NewFlagSet("buckets", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output as JSON")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(cfg.Buckets)
	}
	for _, b := range cfg.Buckets {
		extra := ""
		if len(b.Fields) > 0 {
			extra = "  [fields: " + strings.Join(b.Fields, ", ") + "]"
		}
		fmt.Printf("%-10s %s%s\n", b.Name, b.Desc, extra)
	}
	return nil
}

func cmdIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	write := fs.Bool("write", false, "write _index.md (backs up existing to _index.md.bak)")
	force := fs.Bool("force", false, "allow --write even when some hooks were derived")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	threads, err := store.ScanThreads(cfg)
	if err != nil {
		return err
	}
	standalones, err := store.ScanStandalones(cfg)
	if err != nil {
		return err
	}
	rendered := store.RenderIndex(threads, standalones, store.CountArchive(cfg))
	derived := store.CountDerivedHooks(threads)

	if !*write {
		fmt.Print(rendered)
		fmt.Fprintf(os.Stderr, "\n[dry-run] %d threads, %d standalone, %d derived hook(s). Run with --write to save.\n",
			len(threads), len(standalones), derived)
		if derived > 0 {
			fmt.Fprintln(os.Stderr, "[warn] derived hooks won't match your curated ones; add `hook:` frontmatter (P3) before --write, or pass --force.")
		}
		return nil
	}
	if derived > 0 && !*force {
		return fmt.Errorf("%d thread(s) have derived hooks; refusing to overwrite a curated _index.md — re-run with --force to write anyway", derived)
	}

	idx := cfg.IndexPath()
	if data, e := os.ReadFile(idx); e == nil {
		if e := os.WriteFile(idx+".bak", data, 0o644); e != nil {
			return e
		}
	}
	if e := os.WriteFile(idx, []byte(rendered), 0o644); e != nil {
		return e
	}
	fmt.Printf("wrote %s (backup: %s.bak)\n", idx, idx)
	return nil
}

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// parseMixed parses args allowing flags before AND after positionals (stdlib
// flag stops at the first positional, which silently drops trailing flags like
// `ki import file.md -y`). Returns the positional arguments.
func parseMixed(fs *flag.FlagSet, args []string) []string {
	var pos []string
	_ = fs.Parse(args)
	for fs.NArg() > 0 {
		rest := fs.Args()
		pos = append(pos, rest[0])
		_ = fs.Parse(rest[1:])
	}
	return pos
}

func cmdJot(args []string) error {
	fs := flag.NewFlagSet("jot", flag.ExitOnError)
	bucket := fs.String("bucket", "", "bucket name; skips the classifier")
	fs.StringVar(bucket, "b", "", "shorthand for --bucket")
	due := fs.String("due", "", "due date YYYY-MM-DD (override)")
	topic := fs.String("topic", "", "link to a topic (override)")
	var tags multiFlag
	fs.Var(&tags, "tag", "tag (repeatable, override)")
	jsonPayload := fs.String("json", "", "write a pre-made Classification JSON (skips classifier)")
	filePayload := fs.String("file", "", "read a Classification JSON from a file (skips classifier)")
	yes := fs.Bool("yes", false, "skip the confirm prompt")
	fs.BoolVar(yes, "y", false, "shorthand for --yes")
	dryRun := fs.Bool("dry-run", false, "classify and print JSON, write nothing")
	pos := parseMixed(fs, args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	today := time.Now().Format("2006-01-02")

	payload := *jsonPayload
	if payload == "" && *filePayload != "" {
		data, e := os.ReadFile(*filePayload)
		if e != nil {
			return e
		}
		payload = string(data)
	}

	input := strings.TrimSpace(strings.Join(pos, " "))
	if input == "" && payload == "" {
		if data, e := io.ReadAll(os.Stdin); e == nil {
			input = strings.TrimSpace(string(data))
		}
	}

	var cl store.Classification
	switch {
	case payload != "":
		if e := json.Unmarshal([]byte(payload), &cl); e != nil {
			return fmt.Errorf("parse jot JSON: %w", e)
		}
	case *bucket != "":
		if !cfg.HasBucket(*bucket) {
			return fmt.Errorf("unknown bucket %q (configured: %s)", *bucket, strings.Join(cfg.BucketNames(), ", "))
		}
		cl = store.Classification{Bucket: *bucket, Title: store.FirstLine(input), Body: input}
	default:
		if input == "" {
			return fmt.Errorf("nothing to capture (pass text, pipe stdin, or use --json)")
		}
		cl, err = classify.Classify(cfg, input, store.TopicNames(cfg), today)
		if err != nil {
			return err
		}
	}

	if *due != "" {
		cl.Due = *due
	}
	if *topic != "" {
		cl.Topic = *topic
	}
	if len(tags) > 0 {
		cl.Tags = []string(tags)
	}

	if *dryRun {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(cl)
	}

	it := store.NewItem(cl, today, sourceOf(payload, *bucket))

	if !*yes {
		ok, e := confirm(cfg, &it)
		if e != nil {
			return e
		}
		if !ok {
			fmt.Println("cancelled")
			return nil
		}
	}

	path, err := store.WriteItem(cfg, it)
	if err != nil {
		return err
	}
	fmt.Printf("saved → %s  [%s]\n", relToRoot(cfg, path), it.Type)
	return nil
}

func sourceOf(jsonPayload, bucket string) string {
	if bucket != "" && jsonPayload == "" {
		return "manual"
	}
	return "jot"
}

func relToRoot(c config.Config, path string) string {
	if r, err := filepath.Rel(c.Root(), path); err == nil {
		return r
	}
	return path
}

// confirm previews the item and runs the y/e/b/n loop against stdin.
func confirm(cfg config.Config, it *store.Item) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		printPreview(cfg, *it)
		fmt.Print("[Y] save  [e] edit  [b] bucket  [n] cancel > ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			// No TTY / EOF: don't write silently.
			fmt.Println()
			return false, nil
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes", "":
			return true, nil
		case "n", "no", "q":
			return false, nil
		case "b", "bucket":
			pickBucket(cfg, reader, it)
		case "e", "edit":
			if e := editItem(it); e != nil {
				fmt.Fprintln(os.Stderr, "edit failed:", e)
			}
		default:
			fmt.Println("  (choose y, e, b, or n)")
		}
	}
}

func printPreview(cfg config.Config, it store.Item) {
	fmt.Println()
	fmt.Println("┌─ proposed capture ───────────────────────────")
	fmt.Printf("│ bucket : %s\n", it.Type)
	fmt.Printf("│ title  : %s\n", it.Title)
	if it.Due != "" {
		fmt.Printf("│ due    : %s\n", it.Due)
	}
	if it.Topic != "" {
		fmt.Printf("│ topic  : %s\n", it.Topic)
	}
	if len(it.Tags) > 0 {
		fmt.Printf("│ tags   : %s\n", strings.Join(it.Tags, ", "))
	}
	fmt.Printf("│ file   : %s\n", filepath.Join(cfg.JotRoot, it.Type, it.ID+".md"))
	if strings.TrimSpace(it.Body) != "" && it.Body != it.Title {
		fmt.Println("│ body   :")
		for _, ln := range strings.Split(strings.TrimRight(it.Body, "\n"), "\n") {
			fmt.Printf("│   %s\n", ln)
		}
	}
	fmt.Println("└──────────────────────────────────────────────")
}

func pickBucket(cfg config.Config, r *bufio.Reader, it *store.Item) {
	fmt.Println("  buckets:", strings.Join(cfg.BucketNames(), ", "))
	fmt.Print("  bucket > ")
	line, _ := r.ReadString('\n')
	name := strings.TrimSpace(line)
	if name == "" {
		return
	}
	if !cfg.HasBucket(name) {
		fmt.Println("  unknown bucket:", name)
		return
	}
	it.Type = name
}

func editItem(it *store.Item) error {
	ed := os.Getenv("EDITOR")
	if ed == "" {
		ed = "vi"
	}
	tmp, err := os.CreateTemp("", "ki-jot-*.md")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(it.Render()); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	cmd := exec.Command(ed, tmpPath)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return err
	}
	parsed, err := store.ParseItem(data)
	if err != nil {
		return err
	}
	*it = parsed
	return nil
}

func cmdFind(args []string) error {
	fs := flag.NewFlagSet("find", flag.ExitOnError)
	limit := fs.Int("limit", 10, "max results")
	graph := fs.Bool("graph", false, "expand top hits with [[linked]] neighbours")
	all := fs.Bool("all", false, "include _archive, changelogs, and backups")
	asJSON := fs.Bool("json", false, "output JSON")
	topic := fs.String("topic", "", "restrict to docs carrying this topic")
	var kinds multiFlag
	fs.Var(&kinds, "kind", "restrict to kind: thread|standalone|jot|note (repeatable)")
	pos := parseMixed(fs, args)

	query := strings.TrimSpace(strings.Join(pos, " "))
	if query == "" {
		return fmt.Errorf(`usage: ki find "<terms>"`)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	hits, err := search.Find(cfg, query, search.Options{
		Limit: *limit, Graph: *graph, IncludeAll: *all, Kinds: []string(kinds), Topic: *topic,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(hits)
	}
	if len(hits) == 0 {
		fmt.Println("no matches")
		return nil
	}
	for i, h := range hits {
		fmt.Printf("%2d. [%s] %s  (score %d)\n", i+1, h.Kind, h.Title, h.Score)
		fmt.Printf("    %s\n", h.Path)
		if len(h.Reasons) > 0 {
			fmt.Printf("    ↳ %s\n", strings.Join(h.Reasons, ", "))
		}
		if h.Excerpt != "" {
			fmt.Printf("    … %s\n", h.Excerpt)
		}
	}
	return nil
}

func cmdSave(args []string) error {
	fs := flag.NewFlagSet("save", flag.ExitOnError)
	jsonPayload := fs.String("json", "", "SaveRequest JSON")
	filePayload := fs.String("file", "", "read SaveRequest JSON from a file")
	dryRun := fs.Bool("dry-run", false, "render and print, write nothing")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	today := time.Now().Format("2006-01-02")

	raw := *jsonPayload
	if raw == "" && *filePayload != "" {
		data, e := os.ReadFile(*filePayload)
		if e != nil {
			return e
		}
		raw = string(data)
	}
	if raw == "" {
		data, _ := io.ReadAll(os.Stdin)
		raw = string(data)
	}
	if len(strings.TrimSpace(raw)) == 0 {
		return fmt.Errorf("save: provide a SaveRequest via --json, --file, or stdin")
	}
	var req store.SaveRequest
	if e := json.Unmarshal([]byte(raw), &req); e != nil {
		return fmt.Errorf("parse save JSON: %w", e)
	}

	res, err := store.Save(cfg, req, today, *dryRun)
	if err != nil {
		return err
	}

	if *dryRun {
		newTag := ""
		if res.Created {
			newTag = " (new thread)"
		}
		fmt.Printf("--- permanent.md (dry-run)%s ---\n", newTag)
		fmt.Print(res.Permanent)
		fmt.Printf("\n--- target: %s ---\n", relToRoot(cfg, res.PermanentPath))
		if strings.TrimSpace(req.Changelog) != "" {
			fmt.Printf("changelog += %s: %s\n", today, strings.TrimSpace(req.Changelog))
		}
		fmt.Println("index would be rebuilt.")
		return nil
	}

	verb := "updated"
	if res.Created {
		verb = "created"
	}
	fmt.Printf("%s thread %q → %s\n", verb, res.Thread, relToRoot(cfg, res.PermanentPath))
	fmt.Printf("  changelog: %s\n  index:     %s\n", relToRoot(cfg, res.ChangelogPath), relToRoot(cfg, res.IndexPath))
	return nil
}

func cmdReview(args []string) error {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	overdue := fs.Bool("overdue", false, "only overdue items")
	stale := fs.Int("stale", 14, "stale threshold in days (items with no due date)")
	soon := fs.Int("soon", 7, "due-soon window in days")
	bucket := fs.String("bucket", "", "restrict to one bucket")
	topic := fs.String("topic", "", "restrict to one topic")
	asJSON := fs.Bool("json", false, "output JSON")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	rev, err := store.Digest(cfg, store.ReviewOptions{
		StaleDays: *stale, SoonDays: *soon, Bucket: *bucket, Topic: *topic, OnlyOverdue: *overdue,
	}, time.Now())
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rev)
	}
	printReview(rev)
	return nil
}

func printReview(rev store.Review) {
	if len(rev.Overdue)+len(rev.DueSoon)+len(rev.Stale) == 0 {
		fmt.Println("clear — nothing overdue, due soon, or stale.")
	}
	if len(rev.Overdue) > 0 {
		fmt.Printf("overdue (%d):\n", len(rev.Overdue))
		for _, r := range rev.Overdue {
			fmt.Printf("  [%s] %s  (%dd overdue, due %s)\n        %s\n", r.Type, r.Title, -r.DueInDays, r.Due, r.Path)
		}
	}
	if len(rev.DueSoon) > 0 {
		fmt.Printf("due soon (%d):\n", len(rev.DueSoon))
		for _, r := range rev.DueSoon {
			fmt.Printf("  [%s] %s  (in %dd, due %s)\n        %s\n", r.Type, r.Title, r.DueInDays, r.Due, r.Path)
		}
	}
	if len(rev.Stale) > 0 {
		fmt.Printf("stale (%d):\n", len(rev.Stale))
		for _, r := range rev.Stale {
			fmt.Printf("  [%s] %s  (%dd old)\n        %s\n", r.Type, r.Title, r.AgeDays, r.Path)
		}
	}
	if len(rev.Counts) > 0 {
		keys := make([]string, 0, len(rev.Counts))
		for k := range rev.Counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s %d", k, rev.Counts[k]))
		}
		fmt.Println("open items:", strings.Join(parts, ", "))
	}
}

func cmdDone(args []string) error {
	fs := flag.NewFlagSet("done", flag.ExitOnError)
	pos := parseMixed(fs, args)
	terms := strings.TrimSpace(strings.Join(pos, " "))
	if terms == "" {
		return fmt.Errorf(`usage: ki done "<id or title terms>"`)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	items, err := store.ScanItems(cfg)
	if err != nil {
		return err
	}
	var open []store.Item
	for _, it := range items {
		if it.Status == "" || it.Status == "open" {
			open = append(open, it)
		}
	}

	var matches []store.Item
	for _, it := range open {
		if it.ID == terms {
			matches = []store.Item{it}
			break
		}
	}
	if len(matches) == 0 {
		toks := strings.Fields(strings.ToLower(terms))
		for _, it := range open {
			hay := strings.ToLower(it.ID + " " + it.Title)
			all := true
			for _, t := range toks {
				if !strings.Contains(hay, t) {
					all = false
					break
				}
			}
			if all {
				matches = append(matches, it)
			}
		}
	}

	switch len(matches) {
	case 0:
		return fmt.Errorf("no open item matches %q", terms)
	case 1:
		it, err := store.CloseItem(matches[0].Path, time.Now().Format("2006-01-02"))
		if err != nil {
			return err
		}
		fmt.Printf("done → %s  [%s] %s\n", relToRoot(cfg, it.Path), it.Type, it.Title)
		return nil
	default:
		fmt.Fprintf(os.Stderr, "ambiguous — %d open items match %q:\n", len(matches), terms)
		for _, m := range matches {
			fmt.Fprintf(os.Stderr, "  [%s] %s\n        id: %s\n", m.Type, m.Title, m.ID)
		}
		return fmt.Errorf("narrow the terms or pass an exact id")
	}
}

func cmdTopics(args []string) error {
	fs := flag.NewFlagSet("topics", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output JSON")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	infos, err := store.Topics(cfg)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(infos)
	}
	if len(infos) == 0 {
		fmt.Println("no topics yet — link items with `ki jot --topic NAME` or a topic: field")
		return nil
	}
	for _, t := range infos {
		page := ""
		switch {
		case t.HasPage:
			page = "  · page"
		case t.Items >= 3:
			page = "  · no page yet (worth compiling one)"
		}
		fmt.Printf("%-28s %2d items (%d open)  last %s%s\n", t.Topic, t.Items, t.Open, t.LastActive, page)
	}
	return nil
}

func cmdTopic(args []string) error {
	fs := flag.NewFlagSet("topic", flag.ExitOnError)
	full := fs.Bool("full", false, "print full file contents (chronological collation)")
	asJSON := fs.Bool("json", false, "output JSON")
	pos := parseMixed(fs, args)

	name := strings.TrimSpace(strings.Join(pos, " "))
	if name == "" {
		return fmt.Errorf("usage: ki topic <name> [--full]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	items, err := store.ItemsByTopic(cfg, name)
	if err != nil {
		return err
	}
	pagePath := filepath.Join(cfg.ThreadsPath(), name, "permanent.md")
	pageData, pageErr := os.ReadFile(pagePath)
	if pageErr != nil && len(items) == 0 {
		return fmt.Errorf("no page and no items for topic %q (see: ki topics)", name)
	}

	if *asJSON {
		out := struct {
			Topic string       `json:"topic"`
			Page  string       `json:"page,omitempty"`
			Items []store.Item `json:"items"`
		}{Topic: name, Items: items}
		if pageErr == nil {
			out.Page = relToRoot(cfg, pagePath)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	if *full {
		if pageErr == nil {
			fmt.Printf("===== page: %s =====\n%s\n", relToRoot(cfg, pagePath), pageData)
		}
		for _, it := range items {
			data, e := os.ReadFile(it.Path)
			if e != nil {
				continue
			}
			fmt.Printf("===== %s =====\n%s\n", relToRoot(cfg, it.Path), data)
		}
		return nil
	}

	if pageErr == nil {
		fmt.Println("page:", relToRoot(cfg, pagePath))
	} else {
		fmt.Println("page: none (items are the source of truth; compile one when reloads get heavy)")
	}
	fmt.Printf("items (%d, oldest first):\n", len(items))
	for _, it := range items {
		status := ""
		if it.Status != "" && it.Status != "open" {
			status = "  (" + it.Status + ")"
		}
		fmt.Printf("  %s  [%s] %s%s\n        %s\n", it.Created, it.Type, it.Title, status, relToRoot(cfg, it.Path))
	}
	return nil
}

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "classify and print the item plan as JSON, write nothing")
	yes := fs.Bool("yes", false, "write everything without confirming")
	fs.BoolVar(yes, "y", false, "shorthand for --yes")
	bucket := fs.String("bucket", "", "skip the classifier; one item per chunk into NAME")
	fs.StringVar(bucket, "b", "", "shorthand for --bucket")
	archive := fs.Bool("archive", false, "move source file(s) to _imported/ after writing")
	targets := parseMixed(fs, args)

	if len(targets) == 0 {
		return fmt.Errorf("usage: ki import <file-or-folder>... [--dry-run] [-y] [--archive]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *bucket != "" && !cfg.HasBucket(*bucket) {
		return fmt.Errorf("unknown bucket %q (configured: %s)", *bucket, strings.Join(cfg.BucketNames(), ", "))
	}
	today := time.Now().Format("2006-01-02")

	var files []string
	for _, target := range targets {
		fl, e := gatherImportFiles(target)
		if e != nil {
			return e
		}
		files = append(files, fl...)
	}
	if len(files) == 0 {
		return fmt.Errorf("no .md or .txt files found under %s", strings.Join(targets, ", "))
	}

	imported := map[string]bool{}
	if existing, e := store.ScanItems(cfg); e == nil {
		for _, it := range existing {
			imported[it.Source] = true
		}
	}

	topics := store.TopicNames(cfg)
	var cls []store.Classification
	var sources []string
	for _, fp := range files {
		base := filepath.Base(fp)
		if imported["import:"+base] {
			fmt.Fprintf(os.Stderr, "[warn] items with source import:%s already exist — re-importing will create duplicates\n", base)
		}
		data, e := os.ReadFile(fp)
		if e != nil {
			return e
		}
		chunks := store.SplitChunks(string(data), store.FileDate(base))
		for i, ch := range chunks {
			var got []store.Classification
			if *bucket != "" {
				got = []store.Classification{{Bucket: *bucket, Title: store.FirstLine(ch.Text), Body: ch.Text, Created: ch.Date}}
			} else {
				fmt.Fprintf(os.Stderr, "classifying %s — chunk %d/%d…\n", base, i+1, len(chunks))
				got, e = classify.ClassifyBatch(cfg, ch.Text, topics, today, ch.Date)
				if e != nil {
					return e
				}
			}
			cls = append(cls, got...)
			for range got {
				sources = append(sources, "import:"+base)
			}
		}
	}
	if len(cls) == 0 {
		return fmt.Errorf("classifier produced no items")
	}

	if *dryRun {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(cls)
	}

	items := make([]store.Item, len(cls))
	for i, cl := range cls {
		items[i] = store.NewItem(cl, today, sources[i])
	}

	fmt.Printf("\nplanned items (%d):\n", len(items))
	for _, it := range items {
		extra := ""
		if it.Due != "" {
			extra += "  due " + it.Due
		}
		if it.Topic != "" {
			extra += "  topic " + it.Topic
		}
		fmt.Printf("  %s  [%-8s] %s%s\n", it.Created, it.Type, it.Title, extra)
	}

	pick := false
	if !*yes {
		fmt.Print("\n[Y] write all  [i] pick one by one  [n] cancel > ")
		reader := bufio.NewReader(os.Stdin)
		line, e := reader.ReadString('\n')
		if e != nil && line == "" {
			fmt.Println()
			return nil
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes", "":
		case "i", "pick":
			pick = true
		default:
			fmt.Println("cancelled")
			return nil
		}
	}

	written := 0
	for i := range items {
		if pick {
			ok, e := confirm(cfg, &items[i])
			if e != nil {
				return e
			}
			if !ok {
				fmt.Println("  skipped:", items[i].Title)
				continue
			}
		}
		path, e := store.WriteItem(cfg, items[i])
		if e != nil {
			return e
		}
		written++
		fmt.Printf("saved → %s  [%s]\n", relToRoot(cfg, path), items[i].Type)
	}
	fmt.Printf("imported %d/%d items\n", written, len(items))

	if *archive && written > 0 {
		dir := filepath.Join(cfg.Root(), "_imported")
		if e := os.MkdirAll(dir, 0o755); e != nil {
			return e
		}
		for _, fp := range files {
			dst := filepath.Join(dir, filepath.Base(fp))
			if e := os.Rename(fp, dst); e != nil {
				return e
			}
			fmt.Printf("archived source → %s\n", relToRoot(cfg, dst))
		}
	}
	return nil
}

// gatherImportFiles resolves the import target to a list of note files. For a
// directory it walks recursively, skipping hidden and `_`-prefixed dirs.
func gatherImportFiles(target string) ([]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{target}, nil
	}
	var out []string
	err = filepath.WalkDir(target, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		base := d.Name()
		if d.IsDir() {
			if p != target && (strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(base, ".md") || strings.HasSuffix(base, ".txt") {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}


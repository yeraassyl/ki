// Command ki is the deterministic backbone of a small work vault: buckets ARE
// projects, and everything captured is an action item. One-liners land in a
// per-bucket log; braindumps are split by an LLM into step files. The LLM only
// ever produces content (titles, steps); this binary owns every path, name,
// and byte on disk. See README.md.
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
	"regexp"
	"strings"
	"time"

	"ki/internal/breakdown"
	"ki/internal/config"
	"ki/internal/vault"
	"ki/internal/view"
)

const version = "0.8.0"

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
	case "bucket":
		err = cmdBucket(args)
	case "jot":
		err = cmdJot(args)
	case "dump":
		err = cmdDump(args)
	case "view":
		err = cmdView(args)
	case "done":
		err = cmdDone(args)
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
	fmt.Fprint(os.Stderr, `ki — buckets are projects; everything is an action item

usage: ki <command> [flags]

commands:
  init                        create the vault and config; archives a v0.7 vault first
  bucket add <name> "<desc>"  create a project bucket (desc is the LLM's only context)
  bucket list [--json]        list buckets with open/done counts
  jot "<text>" -b NAME        prepend a timestamped one-liner to the bucket log (no LLM)
  dump "<text>" -b NAME       LLM-split a braindump into a titled step file; preview first
  view [-b NAME] [flags]      board: fresh | aging one-liners + dump progress per bucket
  done "<terms>" [-b NAME]    tick the matching open item (log line or dump step)
  version | help

jot/dump read stdin when no text is given (pbpaste | ki jot -b miso).

dump flags:
  -y, --yes        save without the preview prompt
  --dry-run        print the model's JSON, write nothing
  --json PAYLOAD   write a pre-made {"title","steps"} JSON (skips the model)

view flags:
  --all     all buckets (default when -b is absent)
  --done    include done one-liners
  --full    print raw log + dump files (pipe this to an agent)
  --json    machine-readable board
  --days N  fresh/aging boundary in days (default 7)
`)
}

// parseMixed parses args allowing flags before AND after positionals (stdlib
// flag stops at the first positional, which silently drops trailing flags like
// `ki jot "text" -b miso`). Returns the positional arguments.
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

func relToRoot(c config.Config, path string) string {
	if r, err := filepath.Rel(c.Root(), path); err == nil {
		return r
	}
	return path
}

// textFromArgsOrStdin joins positional text, falling back to piped stdin.
func textFromArgsOrStdin(pos []string) string {
	input := strings.TrimSpace(strings.Join(pos, " "))
	if input == "" {
		if data, e := io.ReadAll(os.Stdin); e == nil {
			input = strings.TrimSpace(string(data))
		}
	}
	return input
}

func requireBucket(c config.Config, name string) (config.Bucket, error) {
	configured := strings.Join(c.BucketNames(), ", ")
	if configured == "" {
		configured = "none yet"
	}
	if name == "" {
		return config.Bucket{}, fmt.Errorf("bucket required: pass -b <name> (configured: %s)", configured)
	}
	b, ok := c.FindBucket(name)
	if !ok {
		return config.Bucket{}, fmt.Errorf("unknown bucket %q (configured: %s) — create it: ki bucket add %s \"<desc>\"", name, configured, name)
	}
	return b, nil
}

// ---- init ----

// legacyVaultAt reports whether root holds a v0.7 vault (old config schema or
// a jot/ directory) that should be archived before a fresh init.
func legacyVaultAt(root string) bool {
	if data, err := os.ReadFile(filepath.Join(root, ".ki", "config.json")); err == nil && config.IsLegacyConfig(data) {
		return true
	}
	if fi, err := os.Stat(filepath.Join(root, "jot")); err == nil && fi.IsDir() {
		return true
	}
	return false
}

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	yes := fs.Bool("yes", false, "archive a legacy vault without asking")
	fs.BoolVar(yes, "y", false, "shorthand for --yes")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	root := config.DiscoverRoot()

	if err == nil {
		for _, b := range cfg.Buckets {
			if e := os.MkdirAll(cfg.BucketPath(b.Name), 0o755); e != nil {
				return e
			}
		}
		fmt.Printf("ki ready at %s (buckets: %s)\n", cfg.Root(), bucketsOrHint(cfg))
		return nil
	}

	if err == config.ErrLegacyVault || (err == config.ErrNotInitialized && legacyVaultAt(root)) {
		archive := root + "-archive"
		if _, e := os.Stat(archive); e == nil {
			return fmt.Errorf("cannot archive: %s already exists — move it away first", archive)
		}
		fmt.Printf("legacy v0.7 vault at %s\nit will be renamed to %s and a fresh vault created\n", root, archive)
		if !*yes {
			fmt.Print("proceed? [y/N] > ")
			line, e := bufio.NewReader(os.Stdin).ReadString('\n')
			if e != nil && line == "" {
				fmt.Println()
				return nil
			}
			if s := strings.ToLower(strings.TrimSpace(line)); s != "y" && s != "yes" {
				fmt.Println("cancelled")
				return nil
			}
		}
		if e := os.Rename(root, archive); e != nil {
			return e
		}
		fmt.Printf("archived → %s\n", archive)
	} else if err != config.ErrNotInitialized {
		return err
	}

	if e := config.Save(config.Default()); e != nil {
		return e
	}
	fmt.Printf("wrote %s\n", config.ConfigPath())
	fmt.Printf("ki ready at %s — add a project: ki bucket add <name> \"<one-line description>\"\n", root)
	return nil
}

func bucketsOrHint(c config.Config) string {
	if len(c.Buckets) == 0 {
		return "none — add one: ki bucket add <name> \"<desc>\""
	}
	return strings.Join(c.BucketNames(), ", ")
}

// ---- bucket ----

var bucketNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func cmdBucket(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ki bucket add <name> \"<desc>\" | ki bucket list [--json]")
	}
	switch args[0] {
	case "add":
		return cmdBucketAdd(args[1:])
	case "list", "ls":
		return cmdBucketList(args[1:])
	default:
		return fmt.Errorf("unknown bucket subcommand %q (add, list)", args[0])
	}
}

func cmdBucketAdd(args []string) error {
	fs := flag.NewFlagSet("bucket add", flag.ExitOnError)
	pos := parseMixed(fs, args)
	if len(pos) < 2 {
		return fmt.Errorf(`usage: ki bucket add <name> "<one-line description>" — the desc is the LLM's only project context`)
	}
	name := pos[0]
	desc := strings.TrimSpace(strings.Join(pos[1:], " "))
	if !bucketNameRe.MatchString(name) {
		return fmt.Errorf("bucket name %q must be lowercase letters, digits, and dashes", name)
	}
	if desc == "" {
		return fmt.Errorf("a description is required — the LLM uses it to break down braindumps")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.HasBucket(name) {
		return fmt.Errorf("bucket %q already exists", name)
	}
	cfg.Buckets = append(cfg.Buckets, config.Bucket{Name: name, Desc: desc})
	if err := config.Save(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.BucketPath(name), 0o755); err != nil {
		return err
	}
	logPath := cfg.LogPath(name)
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		if e := os.WriteFile(logPath, nil, 0o644); e != nil {
			return e
		}
	}
	fmt.Printf("bucket %q ready → %s/\n", name, relToRoot(cfg, cfg.BucketPath(name)))
	return nil
}

func cmdBucketList(args []string) error {
	fs := flag.NewFlagSet("bucket list", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output JSON")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	type row struct {
		Name string `json:"name"`
		Desc string `json:"desc"`
		Open int    `json:"open"`
		Done int    `json:"done"`
	}
	rows := make([]row, 0, len(cfg.Buckets))
	for _, b := range cfg.Buckets {
		bd, e := vault.ScanBucket(cfg, b)
		if e != nil {
			return e
		}
		open, done := bd.Counts()
		rows = append(rows, row{Name: b.Name, Desc: b.Desc, Open: open, Done: done})
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	if len(rows) == 0 {
		fmt.Println("no buckets yet — add one: ki bucket add <name> \"<desc>\"")
		return nil
	}
	for _, r := range rows {
		fmt.Printf("%-14s %s  (%d open / %d done)\n", r.Name, r.Desc, r.Open, r.Done)
	}
	return nil
}

// ---- jot ----

func cmdJot(args []string) error {
	fs := flag.NewFlagSet("jot", flag.ExitOnError)
	bucket := fs.String("bucket", "", "bucket to jot into")
	fs.StringVar(bucket, "b", "", "shorthand for --bucket")
	pos := parseMixed(fs, args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	b, err := requireBucket(cfg, *bucket)
	if err != nil {
		return err
	}
	text := strings.Join(strings.Fields(textFromArgsOrStdin(pos)), " ")
	if text == "" {
		return fmt.Errorf("nothing to jot (pass text or pipe stdin)")
	}
	l, err := vault.LoadLog(cfg.LogPath(b.Name))
	if err != nil {
		return err
	}
	now := time.Now()
	l.Prepend(now.Format("2006-01-02"), now.Format("15:04"), text)
	if err := l.Save(); err != nil {
		return err
	}
	fmt.Printf("jotted → %s\n", relToRoot(cfg, l.Path))
	return nil
}

// ---- dump ----

func cmdDump(args []string) error {
	fs := flag.NewFlagSet("dump", flag.ExitOnError)
	bucket := fs.String("bucket", "", "bucket to dump into")
	fs.StringVar(bucket, "b", "", "shorthand for --bucket")
	yes := fs.Bool("yes", false, "skip the confirm prompt")
	fs.BoolVar(yes, "y", false, "shorthand for --yes")
	dryRun := fs.Bool("dry-run", false, "print the model's JSON, write nothing")
	jsonPayload := fs.String("json", "", "write a pre-made {\"title\",\"steps\"} JSON (skips the model)")
	pos := parseMixed(fs, args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	b, err := requireBucket(cfg, *bucket)
	if err != nil {
		return err
	}
	today := time.Now().Format("2006-01-02")

	var bd breakdown.Breakdown
	if *jsonPayload != "" {
		bd, err = breakdown.Parse(*jsonPayload)
		if err != nil {
			return err
		}
		if bd.Title == "" || len(bd.Steps) == 0 {
			return fmt.Errorf("dump --json needs a non-empty title and steps")
		}
	} else {
		text := textFromArgsOrStdin(pos)
		if text == "" {
			return fmt.Errorf("nothing to dump (pass text or pipe stdin)")
		}
		bd, err = breakdown.Run(cfg, b, text)
		if err != nil {
			return err
		}
	}

	if *dryRun {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(bd)
	}

	if !*yes {
		ok, e := confirmDump(cfg, b.Name, today, &bd)
		if e != nil {
			return e
		}
		if !ok {
			fmt.Println("cancelled")
			return nil
		}
	}

	path, err := vault.WriteDump(cfg.BucketPath(b.Name), bd.Title, today, bd.Steps)
	if err != nil {
		return err
	}
	fmt.Printf("saved → %s  (%d steps)\n", relToRoot(cfg, path), len(bd.Steps))
	return nil
}

// confirmDump previews the dump and runs the y/e/n loop against stdin.
func confirmDump(cfg config.Config, bucketName, today string, bd *breakdown.Breakdown) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		printDumpPreview(cfg, bucketName, *bd)
		fmt.Print("[Y] save  [e] edit  [n] cancel > ")
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
		case "e", "edit":
			if e := editDump(today, bd); e != nil {
				fmt.Fprintln(os.Stderr, "edit failed:", e)
			}
		default:
			fmt.Println("  (choose y, e, or n)")
		}
	}
}

func printDumpPreview(cfg config.Config, bucketName string, bd breakdown.Breakdown) {
	fmt.Println()
	fmt.Println("┌─ proposed dump ──────────────────────────────")
	fmt.Printf("│ bucket : %s\n", bucketName)
	fmt.Printf("│ file   : %s\n", filepath.Join(bucketName, vault.Slug(bd.Title)+".md"))
	fmt.Printf("│ title  : %s\n", bd.Title)
	fmt.Println("│ steps  :")
	for _, s := range bd.Steps {
		fmt.Printf("│   - [ ] %s\n", s)
	}
	fmt.Println("└──────────────────────────────────────────────")
}

// editDump opens the rendered dump in $EDITOR and parses the result back.
func editDump(today string, bd *breakdown.Breakdown) error {
	ed := os.Getenv("EDITOR")
	if ed == "" {
		ed = "vi"
	}
	tmp, err := os.CreateTemp("", "ki-dump-*.md")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(vault.RenderDump(bd.Title, today, bd.Steps)); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	cmd := exec.Command(ed, tmpPath)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	d, err := vault.LoadDump(tmpPath)
	if err != nil {
		return err
	}
	steps := make([]string, 0, len(d.Steps))
	for _, s := range d.Steps {
		steps = append(steps, s.Text)
	}
	if len(steps) == 0 {
		return fmt.Errorf("edited dump has no `- [ ]` steps")
	}
	bd.Title, bd.Steps = d.Title, steps
	return nil
}

// ---- view ----

func cmdView(args []string) error {
	fs := flag.NewFlagSet("view", flag.ExitOnError)
	bucket := fs.String("bucket", "", "one bucket only")
	fs.StringVar(bucket, "b", "", "shorthand for --bucket")
	all := fs.Bool("all", false, "all buckets (default when -b is absent)")
	showDone := fs.Bool("done", false, "include done one-liners")
	full := fs.Bool("full", false, "print raw log + dump files")
	asJSON := fs.Bool("json", false, "output JSON")
	days := fs.Int("days", 7, "fresh/aging boundary in days")
	_ = fs.Parse(args)
	_ = all // --all is the default; the flag exists so the invocation reads naturally

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	buckets := cfg.Buckets
	if *bucket != "" {
		b, e := requireBucket(cfg, *bucket)
		if e != nil {
			return e
		}
		buckets = []config.Bucket{b}
	}
	if len(buckets) == 0 {
		fmt.Println("no buckets yet — add one: ki bucket add <name> \"<desc>\"")
		return nil
	}

	if *full {
		return printFull(cfg, buckets)
	}

	board, err := view.Build(cfg, buckets, time.Now())
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(board)
	}
	fmt.Print(view.Render(board, view.Options{Days: *days, ShowDone: *showDone}))
	return nil
}

// printFull dumps the raw files of each bucket — the collation to pipe into
// an agent session.
func printFull(cfg config.Config, buckets []config.Bucket) error {
	for _, b := range buckets {
		fmt.Printf("## %s — %s\n", b.Name, b.Desc)
		paths := []string{cfg.LogPath(b.Name)}
		bd, err := vault.ScanBucket(cfg, b)
		if err != nil {
			return err
		}
		for _, d := range bd.Dumps {
			paths = append(paths, d.Path)
		}
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil || len(strings.TrimSpace(string(data))) == 0 {
				continue
			}
			fmt.Printf("===== %s =====\n%s\n", relToRoot(cfg, p), strings.TrimRight(string(data), "\n"))
		}
	}
	return nil
}

// ---- done ----

// candidate is one open item `ki done` could tick: a log entry or a dump step.
type candidate struct {
	bucket string
	label  string // human-readable: text plus dump title context
	hay    string // lowercase match target
	path   string
	tick   func() error
}

func cmdDone(args []string) error {
	fs := flag.NewFlagSet("done", flag.ExitOnError)
	bucket := fs.String("bucket", "", "restrict to one bucket")
	fs.StringVar(bucket, "b", "", "shorthand for --bucket")
	pos := parseMixed(fs, args)
	terms := strings.TrimSpace(strings.Join(pos, " "))
	if terms == "" {
		return fmt.Errorf(`usage: ki done "<terms>" [-b <bucket>]`)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	buckets := cfg.Buckets
	if *bucket != "" {
		b, e := requireBucket(cfg, *bucket)
		if e != nil {
			return e
		}
		buckets = []config.Bucket{b}
	}

	var cands []candidate
	for _, b := range buckets {
		bd, e := vault.ScanBucket(cfg, b)
		if e != nil {
			return e
		}
		log := bd.Log
		for _, entry := range log.Entries {
			if entry.Done {
				continue
			}
			line := entry.Line
			l := log // copy per closure; Lines slice is shared, Save writes the file
			cands = append(cands, candidate{
				bucket: b.Name,
				label:  entry.Text,
				hay:    strings.ToLower(entry.Text),
				path:   log.Path,
				tick: func() error {
					if err := l.Tick(line); err != nil {
						return err
					}
					return l.Save()
				},
			})
		}
		for i := range bd.Dumps {
			d := bd.Dumps[i]
			for _, step := range d.Steps {
				if step.Done {
					continue
				}
				line := step.Line
				dd := d
				cands = append(cands, candidate{
					bucket: b.Name,
					label:  step.Text + "  (dump: " + d.Title + ")",
					hay:    strings.ToLower(d.Title + " " + step.Text),
					path:   d.Path,
					tick: func() error {
						if err := dd.Tick(line); err != nil {
							return err
						}
						return dd.Save()
					},
				})
			}
		}
	}

	toks := strings.Fields(strings.ToLower(terms))
	var matches []candidate
	for _, c := range cands {
		all := true
		for _, t := range toks {
			if !strings.Contains(c.hay, t) {
				all = false
				break
			}
		}
		if all {
			matches = append(matches, c)
		}
	}

	switch len(matches) {
	case 0:
		return fmt.Errorf("no open item matches %q", terms)
	case 1:
		if err := matches[0].tick(); err != nil {
			return err
		}
		fmt.Printf("done → %s  [%s] %s\n", relToRoot(cfg, matches[0].path), matches[0].bucket, matches[0].label)
		return nil
	default:
		fmt.Fprintf(os.Stderr, "ambiguous — %d open items match %q:\n", len(matches), terms)
		for _, m := range matches {
			fmt.Fprintf(os.Stderr, "  [%s] %s\n", m.bucket, m.label)
		}
		return fmt.Errorf("narrow the terms")
	}
}

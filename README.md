# ki

A small CLI for capturing notes into plain markdown files. I built it because
my braindumps kept dying in monthly notes I never reopened, and because my
agent sessions needed a place to save their state that wasn't tied to one tool.
It's built for my own workflow first — it might fit yours, it might not.

The one design rule: **an LLM only ever produces content, never structure.**
Paths, categories, file naming, search ranking, index projection — all
deterministic and owned by this binary. The model is called in exactly one
place (classification) and everything it returns is validated, previewed, and
placed by code. If the classifier is wrong, you see it before anything is
written.

No third-party dependencies. Storage is plain markdown with a little
frontmatter — any editor or note app works on top of it. `[[wikilinks]]` are
understood by search but are just text.

## How it thinks

- Everything captured is an **item**: a one-line todo, a pasted code review, or
  a whole session distillate. One file each, append-only, sorted into a small
  set of configurable **buckets** (todo / idea / question / review / note /
  session by default).
- A **topic** is just a `topic:` value on items — linking is cheap, and a topic
  needs no setup to exist. A curated **topic page** is optional and comes later,
  only if a topic recurs enough to be worth compressing. Items stay the source
  of truth; pages are disposable summaries.

## Install

Needs Go. For classification, any [Claude Code](https://claude.com/claude-code)
install (it shells out to `claude -p` with a small model); everything except
`jot`'s auto-classification and `import` works without it.

```sh
git clone https://github.com/yeraassyl/ki && cd ki
go test ./...
go install .          # → $(go env GOPATH)/bin/ki, put that on PATH
ki init               # creates ~/ki/.ki/config.json and the bucket dirs
```

The root defaults to `~/ki` (override with `$KI_ROOT`). `init` is
non-destructive — safe to run inside an existing notes folder.

## Using it

Capture (the main loop — keep a terminal open, type when a thought hits):

```sh
ki jot "ask ops about the cert rotation before friday"
```

The classifier picks a bucket, extracts a title, resolves "friday" to a date.
You get a preview and a `[Y] save [e] edit [b] bucket [n] cancel` prompt —
nothing is written without your eyes on it. Skip the model entirely with
`-b`: `ki jot -b todo --due 2026-07-20 "renew certs"`. Pipe works too:
`pbpaste | ki jot`.

Close things:

```sh
ki done "cert rotation"     # matches open items by id or title terms
```

Get things back:

```sh
ki find "certs"             # tiered search: id > tag > title > body
ki find "auth" --graph      # follow [[wikilinks]] out and back
ki review                   # overdue / due soon / stale, with open counts
```

Drain old notes into the system:

```sh
ki import old-notes/2026-05-24.md --archive
```

Splits on date headers if present (falls back to a date in the filename), asks
the model to cut each chunk into discrete items, previews the whole plan, and
retires the source file to `_imported/`. Relative dates resolve against when
the note was written, not today.

Topics accrete on their own — inspect them when you're curious:

```sh
ki topics                   # every topic value, item counts, last activity
ki topic auth-refactor      # one topic's page + items, chronological
ki topic auth-refactor --full   # full collation, ready to pipe to an LLM
```

## Agent sessions

The end-of-session flow I use with Claude Code skills: the agent distills the
session (state, decisions, next steps) and saves it as a regular item in the
`session` bucket, linked to a topic:

```sh
ki jot --json '{"bucket":"session","topic":"auth-refactor","title":"...","body":"..."}' -y
```

A fresh session reloads with `ki topic auth-refactor --full`. The JSON flags
(`jot --json`, `save --json`) are the whole integration surface — any tool
that can produce JSON and run a command can drive this, so the skills stay a
few lines each. `ki save` writes a curated topic page (`permanent.md` +
changelog) when a topic has earned one.

## Commands

| command | what it does |
|---|---|
| `ki init` | write config and create bucket dirs (idempotent) |
| `ki jot "<text>"` | classify into a bucket, preview, confirm, write |
| `ki import <path>…` | split + classify old note files into items |
| `ki done "<id\|terms>"` | close the matching open item |
| `ki find "<terms>"` | ranked search; `--graph`, `--topic`, `--kind`, `--json` |
| `ki review` | overdue / due-soon / stale digest; `--topic`, `--bucket` |
| `ki topics` | list topic values with counts and last activity |
| `ki topic <name>` | one topic's page + items; `--full` for the collation |
| `ki save --json <req>` | write a curated topic page |
| `ki index [--write]` | project `_index.md` from topic-page frontmatter |
| `ki buckets` | show the taxonomy the classifier is handed |

Most commands take `--json` for machine-readable output.

## Config

`~/ki/.ki/config.json` — edit buckets (name, description, extra fields,
`no_review` to keep a bucket out of digests), paths, and the classifier model.
The classifier binary is `claude` by default; override with `$CLAUDE_BIN`.

## Layout

```
~/ki/.ki/config.json                   taxonomy + paths
~/ki/jot/<bucket>/YYYY-MM-DD-slug.md   items, one file each
~/ki/agent-artifacts/<topic>/          optional curated topic pages
~/ki/_imported/                        originals retired by import --archive
```

## Status

Works for me, daily. Interfaces may still shift. Known rough edges: the
classifier command is hardcoded to the Claude CLI (a pluggable command template
is planned), and content budgets in the config aren't enforced yet.

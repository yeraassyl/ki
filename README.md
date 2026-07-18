# ki

A small CLI for a work vault of plain markdown files. Buckets ARE projects
(`miso`, `ki-cli`, `home`), and everything captured is an action item — no
todo/idea/note taxonomy, no categorizing. It's built for my own workflow
first — it might fit yours, it might not.

The one design rule: **an LLM only ever produces content, never structure.**
Paths, file naming, parsing, rendering — all deterministic and owned by this
binary. The model is called in exactly one place (breaking a braindump into
steps) and everything it returns is previewed before a byte is written.

No third-party dependencies. Storage is plain markdown — any editor or note
app works on top of it.

## How it thinks

- A **bucket** is a project. Creating one requires a one-line description —
  that description is the only project context the LLM ever gets.
- A **one-liner** is a timestamped checkbox line in the bucket's single
  `log.md`, newest first. Captured instantly, no LLM.
- A **dump** is a braindump the LLM has split into a titled file of terse
  checkbox steps. The steps are pointers, not explanations — the real context
  lives in the project repo, and an agent working there will understand them.
- Done state is just the checkbox. Tick it from the CLI, your editor, or let
  an agent do it as it finishes things.

## Install

Needs Go. For `dump`, any [Claude Code](https://claude.com/claude-code)
install (it shells out to `claude -p` with a small model); everything else
works without it.

```sh
git clone https://github.com/yeraassyl/ki && cd ki
go test ./...
go install .          # → $(go env GOPATH)/bin/ki, put that on PATH
ki init               # creates ~/ki and .ki/config.json
```

The root defaults to `~/ki` (override with `$KI_ROOT`). If `init` finds a
v0.7 vault there, it renames it to `~/ki-archive` (with confirmation) and
starts fresh.

## Using it

Create a project:

```sh
ki bucket add miso "personal finance app; Go backend, LLM transaction parsing"
ki bucket add home "personal reminders and life admin"
```

Capture one-liners (no LLM, instant):

```sh
ki jot "try nuextract3 for extraction" -b miso
ki jot "collect documents for visa extension" -b home
```

Dump a braindump (the one LLM call — split into steps, preview, confirm):

```sh
ki dump "auth tests are flaky, probably the clock. also the retry hack needs to go" -b miso
```

```
┌─ proposed dump ──────────────────────────────
│ bucket : miso
│ file   : miso/fix-flaky-auth-tests.md
│ title  : fix flaky auth tests
│ steps  :
│   - [ ] reproduce flake locally
│   - [ ] mock clock in auth tests
│   - [ ] delete retry hack
└──────────────────────────────────────────────
[Y] save  [e] edit  [n] cancel >
```

See where things stand:

```sh
ki view              # every bucket: fresh | aging columns, dump progress
ki view -b miso      # one project
ki view -b miso --full   # raw files — pipe this to an agent
```

```
miso — personal finance app; Go backend, LLM transaction parsing  (4 open / 2 done)
──────────────────────────────────────────────────────────────────────
fresh (<7d)                                    │ aging (≥7d)
- [ ] 07-18 14:03 try nuextract3 for extraction│ - [ ] 07-02 profile the parser
dumps:
  fix-flaky-auth-tests                     (1/3)  3d ago
```

Close things:

```sh
ki done "nuextract"          # ticks the matching log line or dump step
ki done "retry hack" -b miso
```

## Agents

`ki view -b <bucket> --json` is the integration surface: a Claude Code
session in the project repo loads the open items, works through them with
full repo context, and ticks them off with `ki done`. The skill in
[`skills/ki-work`](skills/ki-work/SKILL.md) does exactly that:

```sh
mkdir -p ~/.claude/skills && ln -s "$PWD/skills/ki-work" ~/.claude/skills/ki-work
```

Then inside any project repo: `/ki-work` (or "load my ki items").

## Commands

| command | what it does |
|---|---|
| `ki init` | create the vault + config; archives a v0.7 vault first |
| `ki bucket add <name> "<desc>"` | create a project bucket (desc feeds the LLM) |
| `ki bucket list [--json]` | buckets with open/done counts |
| `ki jot "<text>" -b NAME` | prepend a timestamped one-liner to the bucket log |
| `ki dump "<text>" -b NAME` | LLM-split a braindump into a step file; preview first |
| `ki view [-b NAME] [--done] [--full] [--json] [--days N]` | the board |
| `ki done "<terms>" [-b NAME]` | tick the matching open item |

`jot` and `dump` read stdin when no text is given (`pbpaste | ki jot -b miso`).
`dump` also takes `--dry-run` (print the model's JSON) and `--json PAYLOAD`
(write a pre-made `{"title","steps"}`, skipping the model).

## Layout

```
~/ki/.ki/config.json       buckets (name + desc) and the model
~/ki/<bucket>/log.md       one-liners, newest first
~/ki/<bucket>/<slug>.md    dumps, one file per braindump
```

A log line is `- [ ] 2026-07-18 14:03 text`. Lines that don't match that
shape are ignored by counts and preserved verbatim, so the files stay safe to
hand-edit.

## Config

`~/ki/.ki/config.json` — the bucket list and the model used for `dump`
(`haiku` by default). The model binary is `claude`; override with
`$CLAUDE_BIN`.

## Status

Works for me, daily. Interfaces may still shift.

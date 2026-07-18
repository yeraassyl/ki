---
name: ki-work
description: Load the open ki work items for this repo's project bucket and work through them in the repo, ticking them done. Use when the user says "/ki-work", "load my ki items", "work on the ki backlog", or asks what's pending for this project in ki.
---

# ki-work

Work through the open items of this project's ki bucket. The items are terse
pointers written for someone with full repo context — that's you, here.

## Steps

1. **Resolve the bucket.** If the user named one, use it. Otherwise run
   `ki bucket list --json` and match a bucket name against the repo directory
   name (`basename` of the git root). No clear match → ask the user which
   bucket.

2. **Load the board:** `ki view -b <bucket> --json`. Open work is every
   `oneliners[]` entry and every `dumps[].steps[]` entry with `"done": false`.
   For extra context on a dump, read its file (`path` is relative to the vault
   root, `~/ki` unless `$KI_ROOT` says otherwise).

3. **Confirm scope.** Show the open items briefly and confirm which to take on
   (default: whatever the user asked for; otherwise propose the fresh ones
   first).

4. **Work each item in the repo.** The item text is a pointer, not a spec —
   locate the real context in the code before acting.

5. **Tick items as they finish**, one at a time, using distinctive words from
   the item text:

   ```sh
   ki done "<distinctive terms>" -b <bucket>
   ```

   If ki reports the match as ambiguous, add more terms from the item and
   retry. Never edit vault files directly — `ki done` is the writer.

6. **Capture follow-ups** discovered while working (a bug found on the way, a
   refactor worth doing) as new one-liners:

   ```sh
   ki jot "<terse follow-up>" -b <bucket>
   ```

Don't invent work items that aren't in the board or asked for by the user, and
don't tick anything you didn't actually finish.

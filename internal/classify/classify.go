// Package classify turns a raw captured note into a structured Classification by
// shelling out to the Claude CLI in headless print mode. The prompt is fixed and
// versioned here; the bucket taxonomy and model come from config. This is the one
// generative step in the jot pipeline — everything downstream is deterministic.
package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"ki/internal/config"
	"ki/internal/store"
)

// bin is the Claude CLI binary; override with $CLAUDE_BIN.
func bin() string {
	if b := os.Getenv("CLAUDE_BIN"); b != "" {
		return b
	}
	return "claude"
}

// Classify asks the model to categorize input into one of the configured buckets
// and returns a structured Classification. topics is the list of topic values it
// may link to; today is YYYY-MM-DD for resolving relative due dates.
func Classify(c config.Config, input string, topics []string, today string) (store.Classification, error) {
	out, err := run(c, buildPrompt(c, input, topics, today), 90*time.Second)
	if err != nil {
		return store.Classification{}, err
	}
	cl, err := parseClassification(out)
	if err != nil {
		return store.Classification{}, err
	}
	// An unknown/empty bucket is coerced to a fallback so the user can still
	// see a preview and override it, rather than losing the capture.
	if !c.HasBucket(cl.Bucket) {
		cl.Bucket = fallbackBucket(c)
	}
	return cl, nil
}

// ClassifyBatch splits a chunk of historical text (written on writtenOn, "" if
// unknown) into discrete items and classifies each. Used by `ki import`.
func ClassifyBatch(c config.Config, chunk string, topics []string, today, writtenOn string) ([]store.Classification, error) {
	out, err := run(c, buildBatchPrompt(c, chunk, topics, today, writtenOn), 120*time.Second)
	if err != nil {
		return nil, err
	}
	cls, err := parseBatch(out)
	if err != nil {
		return nil, err
	}
	for i := range cls {
		if !c.HasBucket(cls[i].Bucket) {
			cls[i].Bucket = fallbackBucket(c)
		}
		if cls[i].Created == "" {
			cls[i].Created = writtenOn
		}
	}
	return cls, nil
}

// run executes the classifier subprocess (the single seam where an LLM enters
// the pipeline) and returns its stdout.
func run(c config.Config, prompt string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin(), "-p", prompt, "--model", c.Classifier.Model, "--output-format", "text")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("classifier timed out after %s", timeout)
		}
		return "", fmt.Errorf("running %s: %v: %s", bin(), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func fallbackBucket(c config.Config) string {
	if c.HasBucket("note") {
		return "note"
	}
	if len(c.Buckets) > 0 {
		return c.Buckets[len(c.Buckets)-1].Name
	}
	return "note"
}

// parseClassification extracts the JSON object from model output, tolerating
// surrounding prose or ```json fences.
func parseClassification(out string) (store.Classification, error) {
	s := strings.TrimSpace(out)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j >= i {
			s = s[i : j+1]
		}
	}
	var cl store.Classification
	if err := json.Unmarshal([]byte(s), &cl); err != nil {
		return store.Classification{}, fmt.Errorf("could not parse classifier output as JSON: %v\n--- output ---\n%s", err, strings.TrimSpace(out))
	}
	return cl, nil
}

func writeTaxonomy(b *strings.Builder, c config.Config, topics []string) {
	b.WriteString("Buckets (choose exactly one by name):\n")
	for _, bk := range c.Buckets {
		b.WriteString("- " + bk.Name + ": " + bk.Desc + "\n")
	}
	if len(topics) > 0 {
		b.WriteString("\nActive topics you MAY link to (only if the note clearly belongs to one, else leave empty):\n")
		for _, t := range topics {
			b.WriteString("- " + t + "\n")
		}
	}
}

const fieldRules = `- title: one short line, <= 80 chars, no trailing period.
- tags: 0-5 lowercase tags; include any ticket id (e.g. rtoa-6574) and key tech nouns. No leading "#".
- topic: one of the active topic names above if it clearly belongs, else "".
- body: for the "review" bucket, preserve the input VERBATIM. Otherwise a cleaned, self-contained restatement — stay faithful, do not add facts.
`

func buildPrompt(c config.Config, input string, topics []string, today string) string {
	var b strings.Builder
	b.WriteString("You are a note classifier for a personal knowledge base. ")
	b.WriteString("Classify the captured note below into exactly ONE bucket, extract a concise title, and enrich it.\n\n")
	b.WriteString("Today's date is " + today + ".\n\n")
	writeTaxonomy(&b, c, topics)
	b.WriteString(`
Rules:
- Pick the single best bucket by name from the list above. Never invent a bucket.
- due: only if the note implies a deadline; resolve relative dates (e.g. "tomorrow", "friday") against today into YYYY-MM-DD. Otherwise "".
` + fieldRules + `
Output ONLY a minified JSON object, no prose, no code fences:
{"bucket":"","title":"","due":"","tags":[],"topic":"","body":""}

Captured note:
"""
`)
	b.WriteString(input)
	b.WriteString("\n\"\"\"\n")
	return b.String()
}

func buildBatchPrompt(c config.Config, chunk string, topics []string, today, writtenOn string) string {
	var b strings.Builder
	b.WriteString("You are a note classifier for a personal knowledge base. ")
	b.WriteString("The text below is a raw braindump being imported. Split it into DISCRETE items (a todo, an idea, a question, a self-contained thought each count as one item) and classify every item into exactly one bucket.\n\n")
	b.WriteString("Today's date is " + today + ".\n")
	if writtenOn != "" {
		b.WriteString("The text was originally written on " + writtenOn + "; resolve relative dates against THAT date. Past due dates are fine.\n")
	}
	b.WriteByte('\n')
	writeTaxonomy(&b, c, topics)
	b.WriteString(`
Rules:
- Split at genuine topic shifts; keep consecutive lines about the same thing together as one item. Do not shred coherent passages into fragments, and do not lump unrelated thoughts together.
- Every non-trivial piece of the input must land in exactly one item. Drop only pure noise (stray separators, empty bullets).
- Pick each item's single best bucket by name from the list above. Never invent a bucket.
- due: only if the item states a deadline. Otherwise "".
` + fieldRules + `
Output ONLY a minified JSON array of objects, no prose, no code fences:
[{"bucket":"","title":"","due":"","tags":[],"topic":"","body":""}]

Text to import:
"""
`)
	b.WriteString(chunk)
	b.WriteString("\n\"\"\"\n")
	return b.String()
}

// parseBatch extracts the JSON array from model output, tolerating surrounding
// prose or ```json fences.
func parseBatch(out string) ([]store.Classification, error) {
	s := strings.TrimSpace(out)
	if i := strings.Index(s, "["); i >= 0 {
		if j := strings.LastIndex(s, "]"); j >= i {
			s = s[i : j+1]
		}
	}
	var cls []store.Classification
	if err := json.Unmarshal([]byte(s), &cls); err != nil {
		return nil, fmt.Errorf("could not parse classifier output as JSON array: %v\n--- output ---\n%s", err, strings.TrimSpace(out))
	}
	return cls, nil
}

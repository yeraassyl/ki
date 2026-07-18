// Package breakdown turns a raw braindump into a titled list of terse steps by
// shelling out to the Claude CLI in headless print mode. This is the single
// generative seam in ki: the model produces content (a title and step lines),
// and everything about where that content lands is decided by code.
package breakdown

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
)

// Breakdown is the model's structured verdict for one braindump. `ki dump
// --json` prints exactly this shape on --dry-run, so any tool that can produce
// it can drive the writer.
type Breakdown struct {
	Title string   `json:"title"`
	Steps []string `json:"steps"`
}

// bin is the Claude CLI binary; override with $CLAUDE_BIN.
func bin() string {
	if b := os.Getenv("CLAUDE_BIN"); b != "" {
		return b
	}
	return "claude"
}

// Run asks the model to break the braindump into steps, using only the
// bucket's short description as project context.
func Run(c config.Config, b config.Bucket, braindump string) (Breakdown, error) {
	out, err := run(c, buildPrompt(b, braindump), 90*time.Second)
	if err != nil {
		return Breakdown{}, err
	}
	bd, err := Parse(out)
	if err != nil {
		return Breakdown{}, err
	}
	// A missing title is coerced so the capture is never lost; empty steps
	// mean the model failed the one job it has.
	if strings.TrimSpace(bd.Title) == "" {
		bd.Title = firstWords(braindump, 6)
	}
	if len(bd.Steps) == 0 {
		return Breakdown{}, fmt.Errorf("model returned no steps\n--- output ---\n%s", strings.TrimSpace(out))
	}
	return bd, nil
}

// run executes the model subprocess and returns its stdout.
func run(c config.Config, prompt string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin(), "-p", prompt, "--model", c.Model, "--output-format", "text")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("model timed out after %s", timeout)
		}
		return "", fmt.Errorf("running %s: %v: %s", bin(), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func buildPrompt(b config.Bucket, braindump string) string {
	var s strings.Builder
	s.WriteString("You break a developer's braindump into a short list of actionable steps for a project work log.\n\n")
	s.WriteString("Project: " + b.Name + " — " + b.Desc + "\n")
	s.WriteString(`
Rules:
- 1 to 6 steps, usually 1-3. Each step is ONE short imperative line, <= 100 chars.
- Cover everything the braindump implies; invent nothing it doesn't.
- Keep technical terms, names, and versions exactly as written.
- The reader has full project context; steps are terse pointers, not explanations.
- title: <= 50 chars, terse, names the overall chunk of work. No trailing period.

Output ONLY a minified JSON object, no prose, no code fences:
{"title":"","steps":[""]}

Braindump:
"""
`)
	s.WriteString(braindump)
	s.WriteString("\n\"\"\"\n")
	return s.String()
}

// Parse extracts the JSON object from model output, tolerating surrounding
// prose or ```json fences, and drops blank steps.
func Parse(out string) (Breakdown, error) {
	s := strings.TrimSpace(out)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j >= i {
			s = s[i : j+1]
		}
	}
	var bd Breakdown
	if err := json.Unmarshal([]byte(s), &bd); err != nil {
		return Breakdown{}, fmt.Errorf("could not parse model output as JSON: %v\n--- output ---\n%s", err, strings.TrimSpace(out))
	}
	steps := bd.Steps[:0]
	for _, st := range bd.Steps {
		if t := strings.TrimSpace(st); t != "" {
			steps = append(steps, t)
		}
	}
	bd.Steps = steps
	bd.Title = strings.TrimSpace(bd.Title)
	return bd, nil
}

func firstWords(s string, n int) string {
	fields := strings.Fields(s)
	if len(fields) > n {
		fields = fields[:n]
	}
	return strings.Join(fields, " ")
}

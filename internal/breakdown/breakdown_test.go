package breakdown

import "testing"

func TestParseTolerant(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		title string
		steps int
		fails bool
	}{
		{"clean", `{"title":"fix flaky auth tests","steps":["reproduce flake","mock clock"]}`, "fix flaky auth tests", 2, false},
		{"fenced", "```json\n{\"title\":\"t\",\"steps\":[\"a\"]}\n```", "t", 1, false},
		{"prose around", "Sure, here you go:\n{\"title\":\"t\",\"steps\":[\"a\",\"b\"]}\nHope that helps!", "t", 2, false},
		{"blank steps dropped", `{"title":"t","steps":["a","  ",""]}`, "t", 1, false},
		{"whitespace title trimmed", `{"title":"  t  ","steps":["a"]}`, "t", 1, false},
		{"not json", "I could not do that.", "", 0, true},
		{"broken json", `{"title":"t","steps":[`, "", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bd, err := Parse(c.in)
			if c.fails {
				if err == nil {
					t.Fatalf("expected error, got %+v", bd)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if bd.Title != c.title || len(bd.Steps) != c.steps {
				t.Fatalf("got %+v", bd)
			}
		})
	}
}

func TestFirstWords(t *testing.T) {
	if got := firstWords("fix the flaky auth test suite before friday deploy", 6); got != "fix the flaky auth test suite" {
		t.Fatalf("got %q", got)
	}
	if got := firstWords("short", 6); got != "short" {
		t.Fatalf("got %q", got)
	}
}

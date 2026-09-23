package terminal

import (
	"slices"
	"testing"
)

func texts2(ms []Match) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Text)
	}
	return out
}

func TestFuzzyFind(t *testing.T) {
	history := []string{ // newest first
		"git commit -m fix",
		"go test ./...",
		"git checkout main",
		"docker compose up",
		"grep -r checkout .",
	}
	got := texts2(FuzzyFind("gco", history))
	// Both git commands have g and "co" at word starts; grep's are scattered.
	if len(got) != 3 || got[2] != "grep -r checkout ." {
		t.Fatalf("gco -> %q, want the two git commands before grep", got)
	}
	if slices.Contains(got, "go test ./...") {
		t.Fatalf("gco matched %q, which has no c after the o", got)
	}
	if got := texts2(FuzzyFind("gchk", history)); len(got) == 0 || got[0] != "git checkout main" {
		t.Fatalf("gchk -> %q, want git checkout main first", got)
	}

	if got := texts2(FuzzyFind("", history)); !slices.Equal(got, history) {
		t.Fatalf("empty query should keep everything in order, got %q", got)
	}
	if got := FuzzyFind("zzz", history); len(got) != 0 {
		t.Fatalf("zzz matched %q", texts2(got))
	}
}

func TestFuzzyPositionsAndCase(t *testing.T) {
	m := FuzzyFind("GIT", []string{"git status", "GIT_DIR=x"})
	if len(m) != 1 || m[0].Text != "GIT_DIR=x" {
		t.Fatalf("an uppercase query is case sensitive: %q", texts2(m))
	}
	m = FuzzyFind("gst", []string{"git status"})
	if !slices.Equal(m[0].Positions, []int{0, 4, 5}) {
		t.Fatalf("positions = %v, want [0 4 5]", m[0].Positions)
	}
}

func TestFuzzyPrefersConsecutiveAndRecent(t *testing.T) {
	m := FuzzyFind("make", []string{"m a k e", "make build"})
	if m[0].Text != "make build" {
		t.Fatalf("consecutive letters should win: %q", texts2(m))
	}
	// Equal matches keep the order given (newest first).
	m = FuzzyFind("ls", []string{"ls -la", "ls -lh"})
	if m[0].Text != "ls -la" {
		t.Fatalf("ties should keep the newest first: %q", texts2(m))
	}
}

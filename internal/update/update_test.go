package update

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		current, latest string
		want            bool
	}{
		{"1.0.2", "1.0.3", true},
		{"1.0.2", "v1.1.0", true},
		{"v1.9.9", "2.0.0", true},
		{"1.0.10", "1.0.9", false},
		{"1.0.3", "1.0.3", false},
		{"dev", "9.9.9", false},
		{"1.0.2", "garbage", false},
		{"0.0.42", "1.0.0", true},
	} {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestLatestCachesForADay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doted", "update.json")
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	calls := 0
	fetch := func(tag string) Fetcher {
		return func(context.Context) (string, error) { calls++; return tag, nil }
	}

	got, err := Latest(context.Background(), path, now, fetch("v1.0.3"))
	if err != nil || got != "1.0.3" || calls != 1 {
		t.Fatalf("first check = %q, %v after %d calls", got, err, calls)
	}
	// Within the day the saved answer stands.
	got, _ = Latest(context.Background(), path, now.Add(23*time.Hour), fetch("v1.0.4"))
	if got != "1.0.3" || calls != 1 {
		t.Fatalf("cached check = %q after %d calls", got, calls)
	}
	// After it, GitHub is asked again.
	got, _ = Latest(context.Background(), path, now.Add(25*time.Hour), fetch("v1.0.4"))
	if got != "1.0.4" || calls != 2 {
		t.Fatalf("stale check = %q after %d calls", got, calls)
	}
}

func TestLatestErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.json")
	failing := func(context.Context) (string, error) { return "", errors.New("offline") }
	if _, err := Latest(context.Background(), path, time.Now(), failing); err == nil {
		t.Fatal("a failed fetch should be an error")
	}
	odd := func(context.Context) (string, error) { return "nightly", nil }
	if _, err := Latest(context.Background(), path, time.Now(), odd); err == nil {
		t.Fatal("a tag that isn't a version should be an error")
	}
}

func TestHowTo(t *testing.T) {
	if got := HowTo("/opt/homebrew/Cellar/doted/1.0.2/bin/doted"); got != "brew upgrade doted" {
		t.Errorf("Homebrew install: %q", got)
	}
	if got := HowTo("/Applications/doted.app/Contents/MacOS/doted"); got != "download it from "+ReleasesURL {
		t.Errorf("app install: %q", got)
	}
}

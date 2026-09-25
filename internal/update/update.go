// Package update finds out whether a newer release of doted is out.
//
// The latest release is asked of GitHub at most once a day; the answer is
// kept in the state directory, so starting doted often doesn't ask again.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// LatestURL answers with the newest release that isn't a draft or a
	// prerelease.
	LatestURL = "https://api.github.com/repos/informeai/doted/releases/latest"
	// ReleasesURL is where releases are downloaded from.
	ReleasesURL = "https://github.com/informeai/doted/releases/latest"
	// Every is how long an answer is trusted before asking again.
	Every = 24 * time.Hour

	timeout = 10 * time.Second
)

// Fetcher returns the tag of the latest release, like "v1.2.3".
type Fetcher func(ctx context.Context) (string, error)

// cache is what the state file keeps.
type cache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// Path is the state file: ~/.local/state/doted/update.json, or under
// $XDG_STATE_HOME.
func Path() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "doted", "update.json")
}

// Latest returns the latest release's version ("1.2.3"), from the state
// file at path when it was asked for less than Every ago, else from fetch,
// keeping its answer there.
func Latest(ctx context.Context, path string, now time.Time, fetch Fetcher) (string, error) {
	var c cache
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &c) == nil {
		if age := now.Sub(c.CheckedAt); age >= 0 && age < Every && c.Latest != "" {
			return c.Latest, nil
		}
	}
	tag, err := fetch(ctx)
	if err != nil {
		return "", err
	}
	latest := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if _, ok := parse(latest); !ok {
		return "", fmt.Errorf("unexpected release tag %q", tag)
	}
	if data, err := json.Marshal(cache{CheckedAt: now, Latest: latest}); err == nil {
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
			_ = os.WriteFile(path, data, 0o644) // just asks again next time
		}
	}
	return latest, nil
}

// FetchGitHub asks GitHub for the latest release's tag.
func FetchGitHub(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, LatestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "doted")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("GitHub answered " + resp.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.TagName, nil
}

// Newer reports whether version latest comes after current. A current
// version that isn't a release ("dev", a local build) is never behind.
func Newer(current, latest string) bool {
	c, ok1 := parse(strings.TrimPrefix(current, "v"))
	l, ok2 := parse(strings.TrimPrefix(latest, "v"))
	if !ok1 || !ok2 {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// IsRelease reports whether version is a release's, like "1.2.3".
func IsRelease(version string) bool {
	_, ok := parse(strings.TrimPrefix(version, "v"))
	return ok
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// HowTo says how to upgrade the doted running from executable exe: with
// Homebrew when it was installed with it, else from the releases page.
func HowTo(exe string) string {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	exe = filepath.ToSlash(exe)
	if strings.Contains(exe, "/Cellar/") || strings.Contains(exe, "/Caskroom/") || strings.Contains(exe, "/homebrew/") {
		return "brew upgrade doted"
	}
	return "download it from " + ReleasesURL
}

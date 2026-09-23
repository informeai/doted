//go:build !windows

package clipboard

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestChooseTool(t *testing.T) {
	installed := func(names ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, n := range names {
				if n == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	for name, tt := range map[string]struct {
		goos string
		env  map[string]string
		have []string
		want string // tool name, "" for none
	}{
		"macOS":              {"darwin", nil, nil, "pbcopy"},
		"wayland":            {"linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, []string{"wl-copy", "wl-paste", "xclip"}, "wl-clipboard"},
		"wayland without it": {"linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, []string{"xclip"}, "xclip"},
		"x11 xclip":          {"linux", map[string]string{"DISPLAY": ":0"}, []string{"xclip", "xsel"}, "xclip"},
		"x11 xsel":           {"linux", map[string]string{"DISPLAY": ":0"}, []string{"xsel"}, "xsel"},
		"no display":         {"linux", nil, []string{"xclip"}, ""},
		"display, no tool":   {"linux", map[string]string{"DISPLAY": ":0"}, nil, ""},
	} {
		got, ok := chooseTool(tt.goos, env(tt.env), installed(tt.have...))
		if (tt.want == "") == ok || got.name != tt.want {
			t.Errorf("%s: chose %q (ok %v), want %q", name, got.name, ok, tt.want)
		}
	}
}

func TestPbcopyGetsUTF8(t *testing.T) {
	got, _ := chooseTool("darwin", func(string) string { return "" }, nil)
	if len(got.env) == 0 {
		t.Fatal("pbcopy must run with a UTF-8 locale, or accents break when doted is opened from the Finder")
	}
}

// TestRoundTrip uses the real clipboard, so it only runs when asked:
// DOTED_CLIPBOARD_TEST=1 go test ./internal/clipboard. What was on the
// clipboard is put back afterwards.
func TestRoundTrip(t *testing.T) {
	if os.Getenv("DOTED_CLIPBOARD_TEST") == "" {
		t.Skip("set DOTED_CLIPBOARD_TEST=1 to use the real clipboard")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	before, _ := Read(ctx)
	defer Write(context.Background(), before)

	want := "doted: ação, 日本, emoji 🎉\nsecond line"
	if err := Write(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(ctx)
	if err != nil || got != want {
		t.Fatalf("read back %q, %v; want %q", got, err, want)
	}
}

package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLinkAt(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "main.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "cmd", "main.go")

	cases := []struct {
		text string
		col  int
		want link
		ok   bool
	}{
		{"./cmd/main.go:12:5: undefined: x", 3, link{start: 0, end: 18, file: main, line: 12, col: 5}, true},
		{"see cmd/main.go.", 6, link{start: 4, end: 15, file: main}, true},
		{`File "cmd/main.go", line 3`, 8, link{start: 6, end: 17, file: main}, true},
		{"docs at https://go.dev/doc, ok", 12, link{start: 8, end: 26, url: "https://go.dev/doc"}, true},
		{"(https://example.com/a)", 5, link{start: 1, end: 22, url: "https://example.com/a"}, true},
		{"missing.go:3", 2, link{}, false},
		{"all done", 5, link{}, false},
		{"cmd/main.go", 20, link{}, false},
	}
	for _, c := range cases {
		got, ok := linkAt([]rune(c.text), c.col, dir)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("linkAt(%q, %d) = %+v, %v; want %+v, %v", c.text, c.col, got, ok, c.want, c.ok)
		}
	}
}

func TestLinkCommand(t *testing.T) {
	g := newTestGame(t)
	var opened []string
	g.opener = func(_ *Game, args []string) error { opened = args; return nil }

	g.cfg.Links.Editor = "zed {file}:{line}:{col}"
	g.openLink(link{file: "/a b/main.go", line: 42})
	if !slices.Equal(opened, []string{"zed", "/a b/main.go:42:1"}) {
		t.Fatalf("editor command = %q", opened)
	}

	g.openLink(link{url: "https://go.dev"})
	if opened[len(opened)-1] != "https://go.dev" || !strings.Contains(g.flashText, "go.dev") {
		t.Fatalf("url command = %q, flash %q", opened, g.flashText)
	}
}

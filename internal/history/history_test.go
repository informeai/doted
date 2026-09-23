package history

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAppendAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doted", "history")
	if lines, err := Load(path, 10); err != nil || lines != nil {
		t.Fatalf("missing file: %v %v", lines, err)
	}
	for _, l := range []string{"ls", "git status", "git status", "echo a\nb", "make"} {
		if err := Append(path, l); err != nil {
			t.Fatal(err)
		}
	}
	lines, err := Load(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ls", "git status", "echo a b", "make"} // repeats collapsed, one line each
	if !slices.Equal(lines, want) {
		t.Fatalf("Load = %q, want %q", lines, want)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("history file mode %v, want 0600: it may hold private commands", info.Mode().Perm())
	}
}

func TestLoadKeepsTheNewestAndCompacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	for i := range 30 {
		Append(path, "cmd"+string(rune('a'+i%26))+strings.Repeat("x", i/26))
	}
	lines, err := Load(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 10 || lines[9] != "cmddx" {
		t.Fatalf("Load kept %d lines ending in %q", len(lines), lines[len(lines)-1])
	}
	// 30 lines > 2*10: the file was rewritten with the 10 kept.
	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), "\n"); n != 10 {
		t.Fatalf("file has %d lines after compaction, want 10", n)
	}
}

func TestKeep(t *testing.T) {
	for line, want := range map[string]bool{
		"ls":                   true,
		"":                     false,
		"   ":                  false,
		" export TOKEN=secret": false, // leading space: keep it out
		"git commit -m 'x'":    true,
	} {
		if got := Keep(line); got != want {
			t.Errorf("Keep(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data")
	if got := Path(); got != filepath.Join("/data", "doted", "history") {
		t.Fatalf("Path() = %q", got)
	}
}

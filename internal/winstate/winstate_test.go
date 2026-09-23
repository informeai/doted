package winstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doted", "window.json")
	if _, ok := Load(path); ok {
		t.Fatal("a missing file should load nothing")
	}
	want := Window{Width: 1400, Height: 900, X: 40, Y: 60, HasPosition: true, Monitor: "Built-in Retina Display", Maximized: true}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, ok := Load(path)
	if !ok || got != want {
		t.Fatalf("Load = %+v, %v; want %+v", got, ok, want)
	}
}

func TestLoadRejectsBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"corrupt":   "{not json",
		"zero size": `{"width": 0, "height": 800}`,
	} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, []byte(content), 0o644)
		if w, ok := Load(path); ok {
			t.Errorf("%s: loaded %+v", name, w)
		}
	}
}

func TestPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	if got := Path(); got != filepath.Join("/state", "doted", "window.json") {
		t.Fatalf("Path() = %q", got)
	}
}

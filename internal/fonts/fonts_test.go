package fonts

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

func TestLoadEmbedded(t *testing.T) {
	fam, warns, err := Load("")
	if err != nil || len(warns) != 0 || fam.Name != "Go Mono" {
		t.Fatalf("got %q %v %v", fam.Name, warns, err)
	}
	for i, f := range fam.Faces {
		if f.Source == nil {
			t.Fatalf("variant %d missing", i)
		}
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	mono := filepath.Join(dir, "mono.ttf")
	prop := filepath.Join(dir, "regular.ttf")
	os.WriteFile(mono, gomono.TTF, 0o644)
	os.WriteFile(prop, goregular.TTF, 0o644)

	fam, warns, err := Load(mono)
	if err != nil || len(warns) != 0 || fam.Faces[Bold].Source == nil {
		t.Fatalf("mono: %v %v", warns, err)
	}
	if _, warns, err := Load(prop); err != nil || len(warns) != 1 {
		t.Fatalf("proportional font should warn: %v %v", warns, err)
	}
	if _, _, err := Load(filepath.Join(dir, "missing.ttf")); err == nil {
		t.Fatal("expected error for a missing file")
	}
}

func TestIsPath(t *testing.T) {
	for spec, want := range map[string]bool{
		"JetBrains Mono":          false,
		"Menlo":                   false,
		"~/fonts/Iosevka.ttf":     true,
		"/Library/Fonts/Foo.otf":  true,
		"Iosevka.ttc":             true,
		"fonts/Mono Regular.woff": true,
	} {
		if got := isPath(spec); got != want {
			t.Errorf("isPath(%q) = %v, want %v", spec, got, want)
		}
	}
}

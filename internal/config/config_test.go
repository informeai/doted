package config

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultIsValid(t *testing.T) {
	c := Default()
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	if c.Font.Size != 15 || c.Prompt.Symbol != "> " || c.Colors.Background.RGBA != (color.RGBA{0x14, 0x15, 0x19, 0xff}) {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	c, warns, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil || len(warns) != 0 || c.Font.Size != Default().Font.Size {
		t.Fatalf("got %+v %v %v", c, warns, err)
	}
}

func TestLoadOverridesOnlyWhatIsSet(t *testing.T) {
	path := writeConfig(t, `
[font]
size = 18

[colors]
accent = "#0af"

[colors.bright]
red = "#ff000080"

[shell.env]
EDITOR = "nvim"
`)
	c, warns, err := Load(path)
	if err != nil || len(warns) != 0 {
		t.Fatalf("err %v warns %v", err, warns)
	}
	def := Default()
	if c.Font.Size != 18 || c.Font.LineHeight != def.Font.LineHeight {
		t.Errorf("font = %+v", c.Font)
	}
	if c.Colors.Accent.RGBA != (color.RGBA{0x00, 0xaa, 0xff, 0xff}) || c.Colors.Background != def.Colors.Background {
		t.Errorf("colors = %+v", c.Colors)
	}
	if p := c.Colors.Palette(); p[9] != (color.RGBA{0xff, 0, 0, 0x80}) || p[1] != def.Colors.Normal.Red.RGBA {
		t.Errorf("palette = %v", p)
	}
	if c.Shell.Env["EDITOR"] != "nvim" {
		t.Errorf("env = %v", c.Shell.Env)
	}
}

func TestLoadWarnsAboutUnknownKeys(t *testing.T) {
	_, warns, err := Load(writeConfig(t, "[font]\nsize = 16\nsise = 17\n"))
	if err != nil || len(warns) != 1 || !strings.Contains(warns[0], "font.sise") {
		t.Fatalf("warns %v err %v", warns, err)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := map[string]string{
		"bad color":    "[colors]\nbackground = \"blue\"\n",
		"bad type":     "[font]\nsize = \"big\"\n",
		"out of range": "[font]\nsize = 2\n",
		"bad cursor":   "[cursor]\nstyle = \"beam\"\n",
		"empty prompt": "[prompt]\nsymbol = \"\"\n",
		"bad syntax":   "[font\n",
	}
	for name, content := range tests {
		if _, _, err := Load(writeConfig(t, content)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestWriteDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doted", "config.toml")
	if err := WriteDefault(path); err != nil {
		t.Fatal(err)
	}
	if err := WriteDefault(path); err == nil {
		t.Fatal("WriteDefault overwrote an existing file")
	}
	if _, warns, err := Load(path); err != nil || len(warns) != 0 {
		t.Fatalf("written default doesn't load cleanly: %v %v", warns, err)
	}
}

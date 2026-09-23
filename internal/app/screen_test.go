package app

import (
	"image/color"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/hajimehoshi/ebiten/v2"

	"github.com/informeai/doted/internal/terminal"
)

// fakeKeys is a keyboard where the listed keys are pressed this tick.
type fakeKeys map[ebiten.Key]bool

func (f fakeKeys) fires(k ebiten.Key) bool { return f[k] }
func (f fakeKeys) held(k ebiten.Key) bool  { return f[k] }

func TestJobInput(t *testing.T) {
	type want struct {
		text string
		keys []uv.KeyPressEvent
	}
	for name, tt := range map[string]struct {
		chars      []rune
		keys       fakeKeys
		fullScreen bool
		want       want
	}{
		"typing":           {[]rune("ls"), fakeKeys{}, false, want{text: "ls"}},
		"ctrl+c":           {[]rune("c"), fakeKeys{ebiten.KeyControl: true, ebiten.KeyC: true}, false, want{keys: []uv.KeyPressEvent{{Code: 'c', Mod: uv.ModCtrl}}}},
		"up":               {nil, fakeKeys{ebiten.KeyArrowUp: true}, false, want{keys: []uv.KeyPressEvent{{Code: uv.KeyUp}}}},
		"shift+tab":        {nil, fakeKeys{ebiten.KeyShift: true, ebiten.KeyTab: true}, false, want{keys: []uv.KeyPressEvent{{Code: uv.KeyTab, Mod: uv.ModShift}}}},
		"f5":               {nil, fakeKeys{ebiten.KeyF5: true}, true, want{keys: []uv.KeyPressEvent{{Code: uv.KeyF1 + 4}}}},
		"pgup scrolls":     {nil, fakeKeys{ebiten.KeyPageUp: true}, false, want{}},
		"pgup full screen": {nil, fakeKeys{ebiten.KeyPageUp: true}, true, want{keys: []uv.KeyPressEvent{{Code: uv.KeyPgUp}}}},
		"cmd is doted's":   {[]rune("c"), fakeKeys{ebiten.KeyMeta: true, ebiten.KeyC: true}, true, want{}},
		"esc":              {nil, fakeKeys{ebiten.KeyEscape: true}, true, want{keys: []uv.KeyPressEvent{{Code: uv.KeyEscape}}}},
	} {
		text, keys := jobInput(tt.chars, tt.keys, tt.fullScreen)
		if text != tt.want.text || len(keys) != len(tt.want.keys) {
			t.Errorf("%s: text %q keys %v, want %q %v", name, text, keys, tt.want.text, tt.want.keys)
			continue
		}
		for i := range keys {
			if keys[i].Code != tt.want.keys[i].Code || keys[i].Mod != tt.want.keys[i].Mod {
				t.Errorf("%s: key %d = %+v, want %+v", name, i, keys[i], tt.want.keys[i])
			}
		}
	}
}

func TestVTCell(t *testing.T) {
	if c := vtCell(nil); c.Rune != ' ' {
		t.Fatalf("nil cell = %+v", c)
	}
	c := vtCell(&uv.Cell{Content: "x", Style: uv.Style{
		Fg: ansi.BasicColor(1), Bg: ansi.IndexedColor(208),
		Attrs: uv.AttrBold | uv.AttrReverse, Underline: uv.UnderlineSingle,
	}})
	if c.Rune != 'x' || c.Style.FG != (terminal.Color{Kind: terminal.IndexedColor, Index: 1}) ||
		c.Style.BG != (terminal.Color{Kind: terminal.IndexedColor, Index: 208}) {
		t.Fatalf("cell = %+v", c)
	}
	if want := terminal.Bold | terminal.Inverse | terminal.Underline; c.Style.Attrs != want {
		t.Fatalf("attrs = %b, want %b", c.Style.Attrs, want)
	}
	if got := vtColor(color.RGBA{10, 20, 30, 255}); got != (terminal.Color{Kind: terminal.RGBColor, R: 10, G: 20, B: 30}) {
		t.Fatalf("rgb = %+v", got)
	}
}

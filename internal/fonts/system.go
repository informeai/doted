package fonts

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/go-text/typesetting/font"
)

// macOS ships SF Mono as a hidden variable font (it doesn't show up by name
// in font pickers), so it is loaded from its files directly.
const (
	sfMonoRegular = "/System/Library/Fonts/SFNSMono.ttf"
	sfMonoItalic  = "/System/Library/Fonts/SFNSMonoItalic.ttf"
)

// linuxFallbacks are tried when fontconfig is not available.
var linuxFallbacks = []string{"DejaVu Sans Mono", "Liberation Mono", "Noto Sans Mono", "Ubuntu Mono"}

// systemMonospace resolves the platform's default monospace font: SF Mono
// (then Menlo) on macOS, fontconfig's "monospace" elsewhere.
func systemMonospace() (Family, error) {
	if runtime.GOOS == "darwin" {
		if fam, err := sfMono(); err == nil {
			return fam, nil
		}
		return loadSystem("Menlo")
	}
	if fam, err := fontconfigMonospace(); err == nil {
		return fam, nil
	}
	for _, name := range linuxFallbacks {
		if fam, err := loadSystem(name); err == nil {
			return fam, nil
		}
	}
	return Family{}, errors.New("no system monospace font found")
}

func sfMono() (Family, error) {
	var ld loader
	regular, err := ld.source(sfMonoRegular, 0)
	if err != nil {
		return Family{}, err
	}
	italic, err := ld.source(sfMonoItalic, 0)
	if err != nil {
		italic = regular
	}
	return Family{Name: "SF Mono", Faces: [4]Face{
		Regular:    {Source: regular, Weight: float32(font.WeightNormal)},
		Bold:       {Source: regular, Weight: float32(font.WeightBold)},
		Italic:     {Source: italic, Weight: float32(font.WeightNormal)},
		BoldItalic: {Source: italic, Weight: float32(font.WeightBold)},
	}}, nil
}

// fontconfigMonospace asks fc-match for the file of each monospace variant,
// which honors the user's fontconfig preferences.
func fontconfigMonospace() (Family, error) {
	patterns := [4]string{
		Regular:    "monospace",
		Bold:       "monospace:bold",
		Italic:     "monospace:italic",
		BoldItalic: "monospace:bold:italic",
	}
	weights := [4]font.Weight{font.WeightNormal, font.WeightBold, font.WeightNormal, font.WeightBold}

	var fam Family
	var ld loader
	for v, pattern := range patterns {
		out, err := exec.Command("fc-match", "-f", "%{family[0]}|%{file}|%{index}", pattern).Output()
		if err != nil {
			return Family{}, err
		}
		name, rest, _ := strings.Cut(string(out), "|")
		file, idx, _ := strings.Cut(rest, "|")
		index, _ := strconv.Atoi(idx)
		if file == "" {
			return Family{}, fmt.Errorf("fc-match returned nothing for %q", pattern)
		}
		src, err := ld.source(file, index)
		if err != nil {
			return Family{}, err
		}
		if v == int(Regular) {
			fam.Name = name
		}
		fam.Faces[v] = Face{Source: src, Weight: float32(weights[v])}
	}
	return fam, nil
}

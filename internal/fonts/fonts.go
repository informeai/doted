// Package fonts resolves the configured font family into the four faces
// doted draws with: regular, bold, italic and bold italic.
package fonts

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
)

// Variant indexes Family.Faces.
type Variant int

const (
	Regular Variant = iota
	Bold
	Italic
	BoldItalic
)

// Face is one font variant. Weight, when non-zero, sets the "wght" axis of a
// variable font; static fonts ignore it.
type Face struct {
	Source *text.GoTextFaceSource
	Weight float32
}

// NewFace returns an Ebiten face for f at the given pixel size.
func (f Face) NewFace(size float64) *text.GoTextFace {
	face := &text.GoTextFace{Source: f.Source, Size: size}
	if f.Weight > 0 {
		face.SetVariation(text.MustParseTag("wght"), f.Weight)
	}
	return face
}

type Family struct {
	Name  string
	Faces [4]Face
}

// EmbeddedName selects the built-in font in the config.
const EmbeddedName = "Go Mono"

// Embedded returns Go Mono, which is compiled into the binary.
func Embedded() Family {
	fam := Family{Name: EmbeddedName}
	for i, ttf := range [][]byte{gomono.TTF, gomonobold.TTF, gomonoitalic.TTF, gomonobolditalic.TTF} {
		src, err := text.NewGoTextFaceSource(bytes.NewReader(ttf))
		if err != nil {
			panic(err) // embedded fonts: can only fail if the binary is broken
		}
		fam.Faces[i] = Face{Source: src}
	}
	return fam
}

// Load resolves spec:
//   - empty: the system's monospace font, or the embedded one if there is none
//   - "Go Mono": the embedded font
//   - something that looks like a file path: that file, used for every variant
//   - anything else: an installed font family, looked up by name
//
// Warnings report non-fatal problems, like a proportional font.
func Load(spec string) (fam Family, warnings []string, err error) {
	switch {
	case spec == "":
		if fam, err := systemMonospace(); err == nil {
			return fam, nil, nil
		}
		return Embedded(), nil, nil
	case strings.EqualFold(spec, EmbeddedName):
		return Embedded(), nil, nil
	case isPath(spec):
		fam, err = loadFile(expandHome(spec))
	default:
		fam, err = loadSystem(spec)
	}
	if err != nil {
		return Family{}, nil, err
	}
	if !monospaced(fam.Faces[Regular]) {
		warnings = append(warnings, fmt.Sprintf("font %q is not monospaced; columns will not line up", fam.Name))
	}
	return fam, warnings, nil
}

func isPath(spec string) bool {
	switch strings.ToLower(filepath.Ext(spec)) {
	case ".ttf", ".otf", ".ttc", ".otc":
		return true
	}
	return strings.ContainsAny(spec, `/`+string(filepath.Separator)) || strings.HasPrefix(spec, "~")
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

func loadFile(path string) (Family, error) {
	src, err := (&loader{}).source(path, 0)
	if err != nil {
		return Family{}, err
	}
	face := Face{Source: src}
	return Family{Name: filepath.Base(path), Faces: [4]Face{face, face, face, face}}, nil
}

// loadSystem finds the installed faces of family. The first call scans the
// system fonts and caches an index (in the user cache dir), so it can take a
// few seconds; later calls are fast.
func loadSystem(family string) (Family, error) {
	footprints, err := fontscan.SystemFonts(log.New(io.Discard, "", 0), "")
	if err != nil {
		return Family{}, fmt.Errorf("scanning system fonts: %w", err)
	}
	want := font.NormalizeFamily(family)
	var matches []fontscan.Footprint
	for _, fp := range footprints {
		if fp.Family == want {
			matches = append(matches, fp)
		}
	}
	if len(matches) == 0 {
		return Family{}, fmt.Errorf("font %q is not installed", family)
	}

	targets := [4]font.Aspect{
		Regular:    {Style: font.StyleNormal, Weight: font.WeightNormal},
		Bold:       {Style: font.StyleNormal, Weight: font.WeightBold},
		Italic:     {Style: font.StyleItalic, Weight: font.WeightNormal},
		BoldItalic: {Style: font.StyleItalic, Weight: font.WeightBold},
	}
	fam := Family{Name: family}
	var ld loader
	for v, target := range targets {
		fp := closest(matches, target)
		src, err := ld.source(fp.Location.File, int(fp.Location.Index))
		if err != nil {
			return Family{}, err
		}
		// For variable fonts the file may hold every weight; ask for the one
		// this variant needs.
		fam.Faces[v] = Face{Source: src, Weight: float32(target.Weight)}
	}
	return fam, nil
}

// closest picks the footprint whose style matches and whose weight is nearest.
func closest(fps []fontscan.Footprint, target font.Aspect) fontscan.Footprint {
	best, bestScore := fps[0], math.Inf(1)
	for _, fp := range fps {
		score := math.Abs(float64(fp.Aspect.Weight - target.Weight))
		if fp.Aspect.Style != target.Style {
			score += 10_000
		}
		if score < bestScore {
			best, bestScore = fp, score
		}
	}
	return best
}

// loader parses each font file once, even when several variants live in the
// same file or collection.
type loader struct {
	files map[string][]*text.GoTextFaceSource
}

func (l *loader) source(path string, index int) (*text.GoTextFaceSource, error) {
	srcs, ok := l.files[path]
	if !ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".ttc", ".otc":
			srcs, err = text.NewGoTextFaceSourcesFromCollection(bytes.NewReader(data))
		default:
			var src *text.GoTextFaceSource
			src, err = text.NewGoTextFaceSource(bytes.NewReader(data))
			srcs = []*text.GoTextFaceSource{src}
		}
		if err != nil {
			return nil, fmt.Errorf("loading font %s: %w", path, err)
		}
		if l.files == nil {
			l.files = map[string][]*text.GoTextFaceSource{}
		}
		l.files[path] = srcs
	}
	if index >= len(srcs) {
		return nil, fmt.Errorf("font %s has no face %d", path, index)
	}
	return srcs[index], nil
}

func monospaced(f Face) bool {
	face := f.NewFace(32)
	return math.Abs(text.AdvanceAt("i", 1, face)-text.AdvanceAt("M", 1, face)) < 0.5
}

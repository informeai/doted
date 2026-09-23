package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

const assets = "../../assets/icon"

// maxDelta is the per-channel difference allowed between a committed image
// and a fresh render. Rasterizing floats is not bit-exact across CPUs (arm64
// fuses multiply-adds, amd64 doesn't), which moves a few edge pixels by one
// or two levels; forgetting to regenerate after changing the icon moves far
// more than that.
const maxDelta = 3

// TestGeneratedIconsUpToDate fails when the committed icons don't match the
// geometry in this package: run `go generate ./assets/icon` and commit.
func TestGeneratedIconsUpToDate(t *testing.T) {
	for _, size := range []int{16, 24, 32, 48, 64, 128, 256, 512} {
		name := fmt.Sprintf("png/doted-%d.png", size)
		compare(t, name, readPNG(t, filepath.Join(assets, name)), render(size, tightView))
	}

	ico := read(t, "doted.ico")
	icoImages := icoEntries(t, ico)
	if len(icoImages) != 7 {
		t.Fatalf("doted.ico has %d images, want 7", len(icoImages))
	}
	for _, data := range icoImages {
		img := decode(t, data)
		compare(t, fmt.Sprintf("doted.ico %dpx", img.Bounds().Dx()), img, render(img.Bounds().Dx(), tightView))
	}

	for kind, data := range icnsEntries(t, read(t, "doted.icns")) {
		img := decode(t, data)
		compare(t, "doted.icns "+kind, img, render(img.Bounds().Dx(), fullView))
	}

	// The .syso embeds the same PNGs as the .ico, byte for byte.
	syso, err := os.ReadFile("../../rsrc_windows_amd64.syso")
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range icoImages {
		if !bytes.Contains(syso, data) {
			t.Fatal("rsrc_windows_amd64.syso doesn't match doted.ico")
		}
	}
}

func compare(t *testing.T, name string, got, want image.Image) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Errorf("%s: size %v, want %v", name, got.Bounds(), want.Bounds())
		return
	}
	worst := 0
	b := got.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g := color.RGBAModel.Convert(got.At(x, y)).(color.RGBA)
			w := color.RGBAModel.Convert(want.At(x, y)).(color.RGBA)
			worst = max(worst, delta(g.R, w.R), delta(g.G, w.G), delta(g.B, w.B), delta(g.A, w.A))
		}
	}
	if worst > maxDelta {
		t.Errorf("%s differs from a fresh render by up to %d levels: run go generate ./assets/icon", name, worst)
	}
}

func delta(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// icoEntries returns the PNG data of every image in an .ico file.
func icoEntries(t *testing.T, ico []byte) [][]byte {
	t.Helper()
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	var out [][]byte
	for i := range count {
		entry := ico[6+16*i : 6+16*(i+1)]
		size := binary.LittleEndian.Uint32(entry[8:12])
		offset := binary.LittleEndian.Uint32(entry[12:16])
		out = append(out, ico[offset:offset+size])
	}
	return out
}

// icnsEntries returns the PNG data of every entry in an .icns file, by type.
func icnsEntries(t *testing.T, icns []byte) map[string][]byte {
	t.Helper()
	if string(icns[:4]) != "icns" || int(binary.BigEndian.Uint32(icns[4:8])) != len(icns) {
		t.Fatal("doted.icns has a bad header")
	}
	out := map[string][]byte{}
	for rest := icns[8:]; len(rest) > 0; {
		n := binary.BigEndian.Uint32(rest[4:8])
		out[string(rest[:4])] = rest[8:n]
		rest = rest[n:]
	}
	if len(out) != 10 {
		t.Fatalf("doted.icns has %d entries, want 10", len(out))
	}
	return out
}

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(assets, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return decode(t, b)
}

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

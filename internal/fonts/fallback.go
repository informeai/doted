package fonts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// Fallbacks are the system fonts tried, in order, for characters the main
// font doesn't have: symbols, emoji (in color where the system's emoji font
// is), and Chinese, Japanese and Korean. They're found once; their files
// are opened and read on demand rather than loaded whole, since an emoji
// font can take hundreds of megabytes.
var Fallbacks = sync.OnceValue(func() []*text.GoTextFaceSource {
	var out []*text.GoTextFaceSource
	for _, loc := range fallbackFiles(runtime.GOOS) {
		if src := openSource(loc.file, loc.index); src != nil {
			out = append(out, src)
		}
	}
	return out
})

type fontLocation struct {
	file  string
	index int
}

// fallbackFiles lists where the fallback fonts are on goos.
func fallbackFiles(goos string) []fontLocation {
	switch goos {
	case "darwin":
		return []fontLocation{
			{"/System/Library/Fonts/Apple Symbols.ttf", 0},
			{"/System/Library/Fonts/Apple Color Emoji.ttc", 0},
			{"/System/Library/Fonts/Hiragino Sans GB.ttc", 0},
			{"/System/Library/Fonts/Supplemental/Arial Unicode.ttf", 0},
		}
	case "windows":
		dir := filepath.Join(os.Getenv("WINDIR"), "Fonts")
		return []fontLocation{
			{filepath.Join(dir, "seguisym.ttf"), 0},
			{filepath.Join(dir, "seguiemj.ttf"), 0},
			{filepath.Join(dir, "msyh.ttc"), 0},
		}
	}
	// Elsewhere, ask fontconfig, which knows what's installed and preferred.
	var out []fontLocation
	for _, pattern := range []string{"emoji", "sans-serif:lang=zh-cn", "sans-serif:lang=ja", "sans-serif"} {
		o, err := exec.Command("fc-match", "-f", "%{file}|%{index}", pattern).Output()
		if err != nil {
			break
		}
		file, idx, _ := strings.Cut(string(o), "|")
		index, _ := strconv.Atoi(idx)
		if file != "" {
			out = append(out, fontLocation{file, index})
		}
	}
	return out
}

// openSource opens face index of the font at path for reading on demand, or
// returns nil when it can't.
func openSource(path string, index int) *text.GoTextFaceSource {
	f, err := os.Open(path) // stays open: the font is read from it as needed
	if err != nil {
		return nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ttc", ".otc":
		srcs, err := text.NewGoTextFaceSourcesFromCollection(f)
		if err != nil || index >= len(srcs) {
			f.Close()
			return nil
		}
		return srcs[index]
	}
	src, err := text.NewGoTextFaceSource(f)
	if err != nil {
		f.Close()
		return nil
	}
	return src
}

package app

import (
	"unicode"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
)

// specialKeys are the sequences an xterm sends for non-character keys.
// PageUp/PageDown are kept for scrolling doted's own scrollback.
var specialKeys = []struct {
	key ebiten.Key
	seq string
}{
	{ebiten.KeyEnter, "\r"},
	{ebiten.KeyNumpadEnter, "\r"},
	{ebiten.KeyBackspace, "\x7f"},
	{ebiten.KeyTab, "\t"},
	{ebiten.KeyEscape, "\x1b"},
	{ebiten.KeyArrowUp, "\x1b[A"},
	{ebiten.KeyArrowDown, "\x1b[B"},
	{ebiten.KeyArrowRight, "\x1b[C"},
	{ebiten.KeyArrowLeft, "\x1b[D"},
	{ebiten.KeyHome, "\x1b[H"},
	{ebiten.KeyEnd, "\x1b[F"},
	{ebiten.KeyDelete, "\x1b[3~"},
}

// appendKeyBytes appends to buf the bytes a terminal would send for this
// tick's input: typed characters, Ctrl+letter control codes and special keys.
func appendKeyBytes(buf []byte, chars []rune) []byte {
	ctrl := ebiten.IsKeyPressed(ebiten.KeyControl)
	meta := ebiten.IsKeyPressed(ebiten.KeyMeta)
	if meta {
		return buf // Cmd shortcuts belong to the app, not the program
	}

	if ctrl {
		// Ctrl+A..Ctrl+Z are 0x01..0x1a; Ctrl+C becomes SIGINT in the PTY.
		for k := ebiten.KeyA; k <= ebiten.KeyZ; k++ {
			if repeating(k) {
				buf = append(buf, byte(k-ebiten.KeyA)+1)
			}
		}
	} else {
		for _, r := range chars {
			if !unicode.IsControl(r) {
				buf = utf8.AppendRune(buf, r)
			}
		}
	}

	for _, sk := range specialKeys {
		if repeating(sk.key) {
			buf = append(buf, sk.seq...)
		}
	}
	return buf
}

// Package config loads doted's TOML configuration file.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// defaultFile is both the source of the defaults and the commented template
// written by WriteDefault, so the two can't drift apart.
//
//go:embed default.toml
var defaultFile string

type Config struct {
	Font       Font       `toml:"font"`
	Window     Window     `toml:"window"`
	Prompt     Prompt     `toml:"prompt"`
	Cursor     Cursor     `toml:"cursor"`
	Animation  Animation  `toml:"animation"`
	Scrollback Scrollback `toml:"scrollback"`
	Shell      Shell      `toml:"shell"`
	History    History    `toml:"history"`
	Clipboard  Clipboard  `toml:"clipboard"`
	Links      Links      `toml:"links"`
	Jobs       Jobs       `toml:"jobs"`
	Notify     Notify     `toml:"notify"`
	Status     Status     `toml:"status"`
	Update     Update     `toml:"update"`
	Explain    Explain    `toml:"explain"`
	Colors     Colors     `toml:"colors"`
}

type Clipboard struct {
	System bool `toml:"system"`
}

type Links struct {
	// Editor opens a file clicked in the output: a command line where
	// {file}, {line} and {col} are replaced. Empty picks VS Code when
	// installed, else the system's opener.
	Editor string `toml:"editor"`
}

type Jobs struct {
	// Strip shows each background job as a live card above the input.
	Strip bool `toml:"strip"`
	// StripLines is how many of a job's last lines its card shows.
	StripLines int `toml:"strip_lines"`
	// StripCards is how many cards show; the jobs after them are grouped
	// in one card, and show when navigated to.
	StripCards int `toml:"strip_cards"`
}

type Notify struct {
	Enabled bool `toml:"enabled"`
	// AfterSeconds is how long a command must run to be worth a notification.
	AfterSeconds int `toml:"after_seconds"`
}

type Status struct {
	// Context shows the git branch with what changed and how long the last
	// command took.
	Context bool `toml:"context"`
}

type Explain struct {
	// Tool is the AI command line tool that explains when explain names
	// none: "claude" (Claude Code) or "cursor" (Cursor's agent).
	Tool string `toml:"tool"`
	// Model is passed to Tool; empty is the tool's default.
	Model string `toml:"model"`
	// Language is what it answers in, like "pt-BR"; empty follows $LANG.
	Language string `toml:"language"`
	// Lines is how much of the output's end it gets.
	Lines int `toml:"lines"`
}

type Update struct {
	// Check looks for a newer release of doted once a day and says so in
	// the output.
	Check bool `toml:"check"`
}

type History struct {
	Save  bool `toml:"save"`
	Lines int  `toml:"lines"`
}

type Font struct {
	Family     string  `toml:"family"`
	Size       float64 `toml:"size"`
	LineHeight float64 `toml:"line_height"`
}

type Window struct {
	Width    int     `toml:"width"`
	Height   int     `toml:"height"`
	Remember bool    `toml:"remember"`
	Padding  float64 `toml:"padding"`
}

type PromptStyle string

const (
	PromptBar    PromptStyle = "bar"    // a vertical bar in the accent color
	PromptSymbol PromptStyle = "symbol" // Prompt.Symbol, as text
)

type Prompt struct {
	Style  PromptStyle `toml:"style"`
	Symbol string      `toml:"symbol"`
}

type CursorStyle string

const (
	CursorDot       CursorStyle = "dot"
	CursorBlock     CursorStyle = "block"
	CursorBar       CursorStyle = "bar"
	CursorUnderline CursorStyle = "underline"
)

type Cursor struct {
	Style CursorStyle `toml:"style"`
	// Animate makes the dot hop while typing and the other styles blink.
	Animate bool `toml:"animate"`
	// Blink is Animate's old name, still honored in existing files.
	Blink *bool `toml:"blink"`
}

type Animation struct {
	Enabled   bool `toml:"enabled"`
	FadeInMs  int  `toml:"fade_in_ms"`
	Particles bool `toml:"particles"`
}

type Scrollback struct {
	Lines int `toml:"lines"`
}

type Shell struct {
	Program string            `toml:"program"`
	Env     map[string]string `toml:"env"`
}

type Colors struct {
	Background Color `toml:"background"`
	Foreground Color `toml:"foreground"`
	Muted      Color `toml:"muted"`
	Accent     Color `toml:"accent"`
	Error      Color `toml:"error"`
	Border     Color `toml:"border"`
	Cursor     Color `toml:"cursor"`
	Normal     ANSI  `toml:"normal"`
	Bright     ANSI  `toml:"bright"`
}

type ANSI struct {
	Black   Color `toml:"black"`
	Red     Color `toml:"red"`
	Green   Color `toml:"green"`
	Yellow  Color `toml:"yellow"`
	Blue    Color `toml:"blue"`
	Magenta Color `toml:"magenta"`
	Cyan    Color `toml:"cyan"`
	White   Color `toml:"white"`
}

// Palette returns the 16 ANSI colors in SGR order (normal, then bright).
func (c Colors) Palette() [16]color.RGBA {
	var p [16]color.RGBA
	for i, set := range []ANSI{c.Normal, c.Bright} {
		for j, col := range []Color{set.Black, set.Red, set.Green, set.Yellow, set.Blue, set.Magenta, set.Cyan, set.White} {
			p[i*8+j] = col.RGBA
		}
	}
	return p
}

// Color is a hex color: "#rgb", "#rrggbb" or "#rrggbbaa".
type Color struct{ color.RGBA }

func (c *Color) UnmarshalText(b []byte) error {
	s := strings.TrimPrefix(string(b), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) == 6 {
		s += "ff"
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if len(s) != 8 || err != nil {
		return fmt.Errorf("invalid color %q: use #rrggbb or #rrggbbaa", string(b))
	}
	c.RGBA = color.RGBA{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}
	return nil
}

// Default returns the built-in configuration.
func Default() Config {
	var c Config
	if _, err := toml.Decode(defaultFile, &c); err != nil {
		panic("config: embedded default.toml is invalid: " + err.Error())
	}
	return c
}

// Path is where the configuration lives: $XDG_CONFIG_HOME/doted/config.toml,
// falling back to ~/.config/doted/config.toml.
func Path() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "doted", "config.toml")
}

// Load reads path on top of the defaults. A missing file is not an error.
// Warnings report things that were ignored, such as unknown keys.
func Load(path string) (cfg Config, warnings []string, err error) {
	cfg = Default()
	md, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil, nil
	}
	if err != nil {
		return Default(), nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, key := range md.Undecoded() {
		warnings = append(warnings, fmt.Sprintf("%s: unknown setting %q", path, key.String()))
	}
	// Files written before prompt styles existed that customized the symbol
	// meant for it to show.
	if md.IsDefined("prompt", "symbol") && !md.IsDefined("prompt", "style") {
		cfg.Prompt.Style = PromptSymbol
	}
	if cfg.Cursor.Blink != nil {
		cfg.Cursor.Animate = *cfg.Cursor.Blink
		cfg.Cursor.Blink = nil
		warnings = append(warnings, fmt.Sprintf("%s: cursor.blink is now cursor.animate", path))
	}
	if err := cfg.validate(); err != nil {
		return Default(), warnings, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, warnings, nil
}

func (c Config) validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	check(c.Font.Size >= 6 && c.Font.Size <= 96, "font.size must be between 6 and 96")
	check(c.Font.LineHeight >= 1 && c.Font.LineHeight <= 3, "font.line_height must be between 1 and 3")
	check(c.Window.Width >= 200 && c.Window.Height >= 120, "window width/height must be at least 200×120")
	check(c.Window.Padding >= 0 && c.Window.Padding <= 200, "window.padding must be between 0 and 200")
	check(c.Prompt.Style == PromptBar || c.Prompt.Style == PromptSymbol, "prompt.style must be bar or symbol, got %q", c.Prompt.Style)
	check(c.Prompt.Symbol != "" && !strings.ContainsAny(c.Prompt.Symbol, "\n\r\t"), "prompt.symbol must be a non-empty single line")
	switch c.Cursor.Style {
	case CursorDot, CursorBlock, CursorBar, CursorUnderline:
	default:
		check(false, "cursor.style must be dot, block, bar or underline, got %q", c.Cursor.Style)
	}
	check(c.Animation.FadeInMs >= 0 && c.Animation.FadeInMs <= 5000, "animation.fade_in_ms must be between 0 and 5000")
	check(c.Scrollback.Lines >= 100, "scrollback.lines must be at least 100")
	check(c.History.Lines >= 100, "history.lines must be at least 100")
	check(c.Jobs.StripLines >= 1 && c.Jobs.StripLines <= 10, "jobs.strip_lines must be between 1 and 10")
	check(c.Jobs.StripCards >= 1 && c.Jobs.StripCards <= 9, "jobs.strip_cards must be between 1 and 9")
	return errors.Join(errs...)
}

// WriteDefault writes the commented default configuration to path, refusing
// to overwrite an existing file.
func WriteDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(defaultFile); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

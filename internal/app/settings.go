package app

import (
	"time"

	"github.com/informeai/doted/internal/config"
	"github.com/informeai/doted/internal/fonts"
)

const configPollInterval = time.Second

// Settings is a loaded configuration plus the font family it names.
type Settings struct {
	Config config.Config
	Fonts  fonts.Family

	// Notices are problems to show the user, like unknown keys or a font
	// that could not be found.
	Notices []string
}

// LoadSettings reads the config file and resolves its font. A missing or
// unusable font falls back to the embedded one with a notice; an invalid
// config file is an error.
func LoadSettings(path string) (Settings, error) {
	cfg, warnings, err := config.Load(path)
	if err != nil {
		return Settings{}, err
	}
	s := Settings{Config: cfg, Notices: warnings}
	fam, fontWarnings, err := fonts.Load(cfg.Font.Family)
	if err != nil {
		s.Notices = append(s.Notices, err.Error()+"; using Go Mono")
		fam = fonts.Embedded()
	}
	s.Fonts = fam
	s.Notices = append(s.Notices, fontWarnings...)
	return s, nil
}

// DefaultSettings is used when the config file can't be loaded at startup.
func DefaultSettings() Settings {
	return Settings{Config: config.Default(), Fonts: fonts.Embedded()}
}

type reload struct {
	settings Settings
	err      error
}

// watchConfig reloads the settings in the background whenever the file
// changes; Update picks up the result. Font lookups can be slow, so they
// must stay off the game loop.
func (g *Game) watchConfig() {
	go config.Watch(g.configPath, configPollInterval, func() {
		s, err := LoadSettings(g.configPath)
		g.reloads <- reload{settings: s, err: err}
	})
}

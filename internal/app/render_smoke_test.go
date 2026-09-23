//go:build smoke && unix

// This test opens a real window and drives the game loop through every UI
// state (attached command, background, jobs panel, job view, scrolling), so
// Draw runs against real state. Run it with:
//
//	go test -tags smoke ./internal/app/
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

type smoke struct {
	*Game
	frame  int
	errors []string
}

func (s *smoke) check(ok bool, format string, args ...any) {
	if !ok {
		s.errors = append(s.errors, fmt.Sprintf("frame %d: ", s.frame)+fmt.Sprintf(format, args...))
	}
}

// Update replaces keyboard handling with a scripted session, one step every
// few frames; Draw runs normally in between.
func (s *smoke) Update() error {
	s.frame++
	switch s.frame {
	case 10:
		run(s.Game, "for i in 1 2 3 4 5; do echo line $i; sleep 0.05; done; read _")
		s.check(s.attached != nil, "command not attached")
	case 40:
		s.background()
		s.check(s.attached == nil, "still attached after background")
	case 50:
		run(s.Game, "sleep 30 &")
		run(s.Game, "exit 3 &")
	case 70:
		s.openPanel()
		s.check(len(s.jobs.Listed()) == 3, "panel lists %d jobs, want 3", len(s.jobs.Listed()))
	case 100:
		s.openJob(s.jobs.Listed()[0])
	case 130:
		s.viewing.Write([]byte("\r"))
	case 170:
		s.check(!s.viewing.Running() && s.viewing.LastLine() == "line 5", "job 1 running=%v last=%q", s.viewing.Running(), s.viewing.LastLine())
		s.closeJob()
		s.scrollBy(100)
	case 200:
		s.check(strings.Contains(mainText(s.Game), "[1] done"), "no completion notice for job 1")
		s.jobs.KillAll()
		return ebiten.Termination
	}
	s.jobs.Poll(time.Now(), s.handleJobEvent)
	s.flushNotices()
	s.syncPTYSize()
	return nil
}

func TestMain(m *testing.M) {
	g, err := New(DefaultSettings(), filepath.Join(os.TempDir(), "doted-smoke-config.toml"))
	if err != nil {
		panic(err)
	}
	s := &smoke{Game: g}
	ebiten.SetWindowTitle("doted smoke test")
	ebiten.SetWindowSize(700, 400)
	// Ebiten needs the main goroutine, so the scenario runs here rather than
	// in a Test function.
	if err := ebiten.RunGame(s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, e := range s.errors {
		fmt.Fprintln(os.Stderr, "smoke:", e)
	}
	if len(s.errors) > 0 {
		os.Exit(1)
	}
	os.Exit(m.Run())
}

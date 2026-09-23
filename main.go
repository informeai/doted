package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/informeai/doted/internal/app"
)

func main() {
	g, err := app.New()
	if err != nil {
		log.Fatal(err)
	}

	ebiten.SetWindowTitle("doted")
	ebiten.SetWindowSize(960, 600)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

	if err := ebiten.RunGame(g); err != nil {
		log.Fatal(err)
	}
}

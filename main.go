package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/informeai/doted/internal/app"
	"github.com/informeai/doted/internal/config"
)

func main() {
	configPath := flag.String("config", config.Path(), "path to the configuration file")
	initConfig := flag.Bool("init-config", false, "write the default configuration to -config and exit")
	flag.Parse()

	if *initConfig {
		if err := config.WriteDefault(*configPath); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", *configPath)
		return
	}

	settings, err := app.LoadSettings(*configPath)
	if err != nil {
		settings = app.DefaultSettings()
		settings.Notices = append(settings.Notices, err.Error()+"; using the defaults")
	}

	g, err := app.New(settings, *configPath)
	if err != nil {
		log.Fatal(err)
	}

	ebiten.SetWindowTitle("doted")
	ebiten.SetWindowSize(settings.Config.Window.Width, settings.Config.Window.Height)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

	if err := ebiten.RunGame(g); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

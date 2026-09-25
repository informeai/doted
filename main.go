package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/informeai/doted/assets/icon"
	"github.com/informeai/doted/internal/app"
	"github.com/informeai/doted/internal/config"
)

// version is set at build time with -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	configPath := flag.String("config", config.Path(), "path to the configuration file")
	initConfig := flag.Bool("init-config", false, "write the default configuration to -config and exit")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("doted", version)
		return
	}

	if *initConfig {
		if err := config.WriteDefault(*configPath); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", *configPath)
		return
	}

	app.Version = version
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
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled) // before RestoreWindow: maximizing needs it
	app.RestoreWindow(settings.Config)
	// Closing the window goes through Update, which saves the window's size
	// and stops the running commands.
	ebiten.SetWindowClosingHandled(true)
	// Title bar and taskbar on Windows and Linux; macOS uses the bundle's icon.
	if imgs, err := icon.Images(); err == nil {
		ebiten.SetWindowIcon(imgs)
	}

	if err := ebiten.RunGame(g); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

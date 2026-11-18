package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/sammwyy/startship/internal/config"
	"github.com/sammwyy/startship/internal/server"
	"github.com/sammwyy/startship/internal/version"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version.Version)
		return
	}
	settings, err := config.Load(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "startshipd:", err)
		os.Exit(1)
	}
	if err = settings.Export(); err != nil {
		fmt.Fprintln(os.Stderr, "startshipd:", err)
		os.Exit(1)
	}
	server.Run(settings)
}

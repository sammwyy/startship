package main

import (
	"fmt"
	"github.com/sammwyy/startship/internal/server"
	"github.com/sammwyy/startship/internal/version"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version.Version)
		return
	}
	server.Run()
}

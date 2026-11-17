package config

import (
	"log"
	"os"
)

func Env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func DataDirectory() string {
	if configured := os.Getenv("STARTSHIP_DATA"); configured != "" {
		return configured
	}
	if _, err := os.Stat("./data"); !os.IsNotExist(err) {
		log.Print("Using existing ./data; consider renaming it to ./_data so Go tooling skips it")
		return "./data"
	}
	return "./_data"
}

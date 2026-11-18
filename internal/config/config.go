package config

import "os"

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
	return "./_data"
}

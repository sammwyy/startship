package config

import (
	"os"
	"testing"
)

func TestDataDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("STARTSHIP_DATA", "")
	if got := DataDirectory(); got != "./_data" {
		t.Fatalf("fresh install: got %q", got)
	}
	if err := os.Mkdir("data", 0700); err != nil {
		t.Fatal(err)
	}
	if got := DataDirectory(); got != "./data" {
		t.Fatalf("existing install: got %q", got)
	}
	if err := os.Mkdir("_data", 0700); err != nil {
		t.Fatal(err)
	}
	if got := DataDirectory(); got != "./data" {
		t.Fatalf("both directories exist: got %q", got)
	}
	t.Setenv("STARTSHIP_DATA", "custom-data")
	if got := DataDirectory(); got != "custom-data" {
		t.Fatalf("explicit override: got %q", got)
	}
}

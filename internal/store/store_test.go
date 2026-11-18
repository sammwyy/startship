package store

import (
	"path/filepath"
	"testing"
)

func TestOpenCreatesSchemaIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startship.db")
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var tables int
		if err = s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('records','users','sessions','access_tokens','audit_log','network_rules')").Scan(&tables); err != nil || tables != 6 {
			t.Fatal(tables, err)
		}
		s.DB.Close()
	}
}

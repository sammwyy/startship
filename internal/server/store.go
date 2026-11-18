package server

import (
	"errors"
	"github.com/sammwyy/startship/internal/store"
)

type Record = store.Record
type Store = store.Store

var errInvalid = errors.New("invalid request")

func randomID() string { return store.RandomID() }

func openStore(path string) (*Store, error) {
	return store.Open(path, migrateProjectSources, migrateAccess, migrateDomains, migrateProjectConfig, migrateCertificates, migrateServices, migrateFirewall)
}

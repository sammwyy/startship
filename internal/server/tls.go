package server

import (
	"crypto/tls"
	"fmt"
	"github.com/sammwyy/startship/internal/routing"
)

func (a *Server) storedCertificate(host string) *Certificate {
	host = routing.NormalizeHost(host)
	for _, r := range a.store.List("routes", "") {
		if routing.NormalizeHost(r.Host) == host && r.CertificateID != "" {
			if c, err := a.certificate(r.CertificateID); err == nil && c.usable() && c.covers(host) {
				return &c
			}
		}
	}
	for _, c := range a.certificates("") {
		if c.usable() && c.covers(host) {
			return &c
		}
	}
	return nil
}
func (a *Server) tlsCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if c := a.storedCertificate(hello.ServerName); c != nil {
		pair, err := tls.X509KeyPair([]byte(c.CertPEM), []byte(c.KeyPEM))
		return &pair, err
	}
	if a.legacyCertificate != nil {
		return a.legacyCertificate(hello)
	}
	return nil, fmt.Errorf("no certificate for %q", hello.ServerName)
}

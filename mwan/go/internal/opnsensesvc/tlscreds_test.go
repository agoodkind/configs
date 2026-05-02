package opnsensesvc

import (
	"crypto/x509"
	"encoding/pem"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPKIPin_StableAcrossCalls(t *testing.T) {
	cert, _, err := GenerateCA("pintest")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(cert)
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	a := SPKIPin(parsed)
	b := SPKIPin(parsed)
	if a != b {
		t.Fatalf("SPKIPin not stable: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(a))
	}
}

func TestLoadServerCreds_RoundTripWithPin(t *testing.T) {
	dir := t.TempDir()

	caCertPEM, caKeyPEM, err := GenerateCA("ca")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteBundle(dir, "ca", caCertPEM, caKeyPEM); err != nil {
		t.Fatal(err)
	}

	srvCertPEM, srvKeyPEM, err := IssueLeaf(caCertPEM, caKeyPEM, "server", []string{"localhost"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteBundle(dir, "server", srvCertPEM, srvKeyPEM); err != nil {
		t.Fatal(err)
	}

	leafBlock, _ := pem.Decode(srvCertPEM)
	leaf, err := x509.ParseCertificate(leafBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pin := SPKIPin(leaf)

	pinsPath := filepath.Join(dir, "pins.txt")
	pinsContent := []byte("# comment line ignored\n\n" + pin + "\n")
	if err := writeFile(pinsPath, pinsContent); err != nil {
		t.Fatal(err)
	}

	creds, err := LoadServerCreds(
		filepath.Join(dir, "server.crt"),
		filepath.Join(dir, "server.key"),
		filepath.Join(dir, "ca.crt"),
		pinsPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if creds == nil {
		t.Fatal("nil credentials")
	}
}

func TestLoadServerCreds_RejectsBadPinsFile(t *testing.T) {
	dir := t.TempDir()
	caCertPEM, caKeyPEM, err := GenerateCA("ca")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteBundle(dir, "ca", caCertPEM, caKeyPEM); err != nil {
		t.Fatal(err)
	}
	srvCertPEM, srvKeyPEM, err := IssueLeaf(caCertPEM, caKeyPEM, "server", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteBundle(dir, "server", srvCertPEM, srvKeyPEM); err != nil {
		t.Fatal(err)
	}
	pinsPath := filepath.Join(dir, "pins.txt")
	if err := writeFile(pinsPath, []byte("not-hex\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadServerCreds(
		filepath.Join(dir, "server.crt"),
		filepath.Join(dir, "server.key"),
		filepath.Join(dir, "ca.crt"),
		pinsPath,
	); err == nil || !strings.Contains(err.Error(), "pin") {
		t.Fatalf("expected pin parse error, got %v", err)
	}
}

func TestLoadServerCreds_NoPinsFileIsAllowed(t *testing.T) {
	dir := t.TempDir()
	caCertPEM, caKeyPEM, err := GenerateCA("ca")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteBundle(dir, "ca", caCertPEM, caKeyPEM); err != nil {
		t.Fatal(err)
	}
	srvCertPEM, srvKeyPEM, err := IssueLeaf(caCertPEM, caKeyPEM, "server", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteBundle(dir, "server", srvCertPEM, srvKeyPEM); err != nil {
		t.Fatal(err)
	}
	creds, err := LoadServerCreds(
		filepath.Join(dir, "server.crt"),
		filepath.Join(dir, "server.key"),
		filepath.Join(dir, "ca.crt"),
		"", // no pins
	)
	if err != nil {
		t.Fatal(err)
	}
	if creds == nil {
		t.Fatal("nil credentials")
	}
}

// writeFile is a helper since this package does not import os elsewhere.
func writeFile(path string, content []byte) error {
	return writeBytes(path, content)
}

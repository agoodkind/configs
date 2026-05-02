package opnsensesvc

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateCA_RoundTripsThroughParser(t *testing.T) {
	certPEM, keyPEM, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no CERTIFICATE PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.IsCA {
		t.Fatal("expected IsCA=true")
	}
	if cert.Subject.CommonName != "test-ca" {
		t.Fatalf("CN: got %q", cert.Subject.CommonName)
	}
	if cert.NotAfter.Sub(cert.NotBefore) < 365*24*time.Hour {
		t.Fatal("CA validity too short")
	}

	kBlock, _ := pem.Decode(keyPEM)
	if kBlock == nil {
		t.Fatal("no PRIVATE KEY PEM block")
	}
	if _, err := x509.ParsePKCS8PrivateKey(kBlock.Bytes); err != nil {
		t.Fatalf("parse key: %v", err)
	}
}

func TestIssueLeaf_ChainsToCA(t *testing.T) {
	caCertPEM, caKeyPEM, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	leafCertPEM, _, err := IssueLeaf(caCertPEM, caKeyPEM, "vault", []string{"vault.local"}, []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	// Parse both, verify the leaf chains to the CA.
	caBlock, _ := pem.Decode(caCertPEM)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	leafBlock, _ := pem.Decode(leafCertPEM)
	leafCert, err := x509.ParseCertificate(leafBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := leafCert.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("leaf does not chain to CA: %v", err)
	}
	if leafCert.Subject.CommonName != "vault" {
		t.Fatalf("CN: got %q", leafCert.Subject.CommonName)
	}
}

func TestIssueLeaf_RequiresCommonName(t *testing.T) {
	caCertPEM, caKeyPEM, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := IssueLeaf(caCertPEM, caKeyPEM, "", nil, nil); err == nil {
		t.Fatal("expected error on empty CN")
	}
}

func TestIssueLeaf_RejectsInvalidIPSAN(t *testing.T) {
	caCertPEM, caKeyPEM, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := IssueLeaf(caCertPEM, caKeyPEM, "vault", nil, []string{"not-an-ip"}); err == nil {
		t.Fatal("expected error on invalid IP SAN")
	}
}

func TestWriteBundle_WritesCertAndKey(t *testing.T) {
	dir := t.TempDir()
	cert, key, err := GenerateCA("ca")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteBundle(dir, "ca", cert, key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ca.crt")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "ca.key"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("expected key mode 0600, got %v", st.Mode().Perm())
	}
}

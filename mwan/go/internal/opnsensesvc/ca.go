// Package opnsensesvc implements the mwan-opnsense gRPC service.
//
// The package is build-constrained at the file level. Pure Go files
// (ca, tlscreds, exec, configxml, xpath) build everywhere. Files that
// touch FreeBSD-specific syscalls or device files are tagged
// //go:build freebsd.
package opnsensesvc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const (
	caValidYears   = 5
	leafValidYears = 1
)

// GenerateCA produces a new self-signed CA suitable for issuing leaf
// certs to mwan-opnsense clients and the mwan-opnsense server itself.
// The returned cert and key are PEM-encoded.
func GenerateCA(commonName string) (certPEM, keyPEM []byte, err error) {
	if commonName == "" {
		commonName = "mwan-opnsense-ca"
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("ca: generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("ca: serial: %w", err)
	}

	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"goodkind.io mwan"},
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(caValidYears, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("ca: sign self: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("ca: marshal key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// IssueLeaf signs a new leaf cert from the supplied CA. The leaf is
// usable for both client and server auth so the same machinery
// supports vault-side clients and the mwan-opnsense server.
func IssueLeaf(caCertPEM, caKeyPEM []byte, commonName string, dnsNames, ipSANs []string) (certPEM, keyPEM []byte, err error) {
	if commonName == "" {
		return nil, nil, errors.New("issue: commonName required")
	}

	caCert, caKey, err := decodeCABundle(caCertPEM, caKeyPEM)
	if err != nil {
		return nil, nil, err
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("issue: generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("issue: serial: %w", err)
	}

	ipParsed, err := parseIPSANs(ipSANs)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"goodkind.io mwan"},
		},
		NotBefore:   now.Add(-time.Hour),
		NotAfter:    now.AddDate(leafValidYears, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		DNSNames:    dnsNames,
		IPAddresses: ipParsed,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &priv.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("issue: sign: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("issue: marshal key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// WriteBundle writes the cert and key to dir as files <name>.crt and
// <name>.key. Key file is mode 0600.
func WriteBundle(dir, name string, certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("writebundle: mkdir: %w", err)
	}
	certPath := filepath.Join(dir, name+".crt")
	keyPath := filepath.Join(dir, name+".key")
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return fmt.Errorf("writebundle: cert: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("writebundle: key: %w", err)
	}
	return nil
}

func decodeCABundle(caCertPEM, caKeyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certBlock, _ := pem.Decode(caCertPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, nil, errors.New("ca bundle: no CERTIFICATE PEM block")
	}
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("ca bundle: parse cert: %w", err)
	}

	keyBlock, _ := pem.Decode(caKeyPEM)
	if keyBlock == nil {
		return nil, nil, errors.New("ca bundle: no PRIVATE KEY PEM block")
	}
	rawKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("ca bundle: parse key: %w", err)
	}
	caKey, ok := rawKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("ca bundle: key is not ecdsa")
	}
	return caCert, caKey, nil
}

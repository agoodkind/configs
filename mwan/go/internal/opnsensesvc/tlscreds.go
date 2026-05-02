package opnsensesvc

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc/credentials"
)

// LoadServerCreds builds gRPC server credentials enforcing mTLS plus
// SPKI fingerprint pinning.
//
// certPath: server cert PEM
// keyPath: server key PEM
// caPath: CA cert PEM (verifies client certs)
// pinsPath: optional file containing one hex-encoded sha256 SPKI pin
// per line; lines beginning with # are comments. If empty, only
// CA-trust applies.
func LoadServerCreds(certPath, keyPath, caPath, pinsPath string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("tlscreds: server keypair: %w", err)
	}

	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("tlscreds: read ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("tlscreds: ca PEM had no usable certs")
	}

	pins, err := loadSPKIPinsFile(pinsPath)
	if err != nil {
		return nil, err
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}
	if len(pins) > 0 {
		cfg.VerifyPeerCertificate = makeSPKIPinVerifier(pins)
	}
	return credentials.NewTLS(cfg), nil
}

// SPKIPin computes the hex-encoded sha256 fingerprint of a cert's
// RawSubjectPublicKeyInfo. This is what tools like nginx and curl
// call "spki pin sha256".
func SPKIPin(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}

// loadSPKIPinsFile parses one fingerprint per line, hex-encoded
// sha256. Lines starting with # and blank lines are ignored. Empty
// path returns nil without error (pins disabled).
func loadSPKIPinsFile(path string) (map[string]struct{}, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("tlscreds: open pins: %w", err)
	}
	defer func() { _ = f.Close() }()

	pins := map[string]struct{}{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, err := hex.DecodeString(line); err != nil {
			return nil, fmt.Errorf("tlscreds: pin %q: %w", line, err)
		}
		pins[strings.ToLower(line)] = struct{}{}
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("tlscreds: scan pins: %w", err)
	}
	return pins, nil
}

func makeSPKIPinVerifier(pins map[string]struct{}) func([][]byte, [][]*x509.Certificate) error {
	return func(_ [][]byte, verifiedChains [][]*x509.Certificate) error {
		// At least one chain must contain a leaf whose SPKI is pinned.
		for _, chain := range verifiedChains {
			if len(chain) == 0 {
				continue
			}
			leaf := chain[0]
			if _, ok := pins[strings.ToLower(SPKIPin(leaf))]; ok {
				return nil
			}
		}
		return errors.New("tlscreds: client SPKI not in pin set")
	}
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goodkind.io/mwan/internal/opnsensesvc"
)

// splitCSV splits a comma separated list, trimming whitespace and
// dropping empty entries.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// caInit generates a fresh CA and writes ca.crt + ca.key under outDir.
// Refuses to overwrite an existing CA in that directory.
func caInit(outDir, cn string) error {
	caCertPath := filepath.Join(outDir, "ca.crt")
	caKeyPath := filepath.Join(outDir, "ca.key")
	if _, err := os.Stat(caCertPath); err == nil {
		return fmt.Errorf("refusing to overwrite existing %s", caCertPath)
	}
	if _, err := os.Stat(caKeyPath); err == nil {
		return fmt.Errorf("refusing to overwrite existing %s", caKeyPath)
	}
	certPEM, keyPEM, err := opnsensesvc.GenerateCA(cn)
	if err != nil {
		return err
	}
	if err := opnsensesvc.WriteBundle(outDir, "ca", certPEM, keyPEM); err != nil {
		return err
	}
	return nil
}

// issueBundle reads ca.crt + ca.key from caDir, signs a leaf for cn
// with the supplied DNS/IP SANs, and writes the result as <out>.crt
// and <out>.key.
func issueBundle(caDir, cn, out string, dnsNames, ipSANs []string) error {
	caCert, err := os.ReadFile(filepath.Join(caDir, "ca.crt"))
	if err != nil {
		return fmt.Errorf("read ca.crt: %w", err)
	}
	caKey, err := os.ReadFile(filepath.Join(caDir, "ca.key"))
	if err != nil {
		return fmt.Errorf("read ca.key: %w", err)
	}
	certPEM, keyPEM, err := opnsensesvc.IssueLeaf(caCert, caKey, cn, dnsNames, ipSANs)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out+".crt", certPEM, 0o644); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(out+".key", keyPEM, 0o600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}
	return nil
}

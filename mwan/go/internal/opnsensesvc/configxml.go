package opnsensesvc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/beevik/etree"
)

// ConfigPath is the canonical OPNsense config.xml location. Override
// in tests via the Server's configPath field.
const ConfigPath = "/conf/config.xml"

// BackupDir is where snapshot files land. Created on demand. OPNsense
// itself uses /conf/backup so we land alongside its native backups.
const BackupDir = "/conf/backup"

// readConfig returns the bytes of the named config.xml. Errors are
// wrapped with the path for easier diagnosis.
func readConfig(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("readConfig: %s: %w", path, err)
	}
	return b, nil
}

// writeConfig writes content to path atomically via temp file + rename.
// Caller is responsible for snapshotting first if desired (the server
// layer enforces this for mutating RPCs).
func writeConfig(path string, content []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config.xml.tmp.*")
	if err != nil {
		return fmt.Errorf("writeConfig: tmp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("writeConfig: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("writeConfig: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("writeConfig: close: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("writeConfig: chmod: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("writeConfig: rename: %w", err)
	}
	return nil
}

// backupConfig copies the current config.xml at srcPath to a
// timestamped file under BackupDir and returns the destination path.
// Optional label is appended to the filename for human readability.
func backupConfig(srcPath, backupDir, label string) (string, error) {
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", fmt.Errorf("backupConfig: mkdir: %w", err)
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("backupConfig: open src: %w", err)
	}
	defer func() { _ = src.Close() }()

	stamp := time.Now().UTC().Format("20060102-150405")
	name := stamp + ".xml"
	if label != "" {
		name = stamp + "-" + sanitizeLabel(label) + ".xml"
	}
	destPath := filepath.Join(backupDir, name)

	tmp, err := os.CreateTemp(backupDir, ".backup.tmp.*")
	if err != nil {
		return "", fmt.Errorf("backupConfig: tmp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("backupConfig: copy: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("backupConfig: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("backupConfig: close: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		cleanup()
		return "", fmt.Errorf("backupConfig: chmod: %w", err)
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		cleanup()
		return "", fmt.Errorf("backupConfig: rename: %w", err)
	}
	return destPath, nil
}

// sanitizeLabel scrubs characters that have no business in a filename.
func sanitizeLabel(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s) && i < 32; i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c)
		case c >= '0' && c <= '9':
			out = append(out, c)
		case c == '-' || c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "labeled"
	}
	return string(out)
}

// stripGatewayV6 removes <gatewayv6> from the WAN interface section.
// Returns the modified bytes and whether anything changed.
func stripGatewayV6(input []byte) ([]byte, bool, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(input); err != nil {
		return nil, false, fmt.Errorf("stripGatewayV6: parse: %w", err)
	}
	wan := doc.FindElement("//opnsense/interfaces/wan")
	if wan == nil {
		return input, false, nil
	}
	gw := wan.FindElement("./gatewayv6")
	if gw == nil {
		return input, false, nil
	}
	wan.RemoveChild(gw)

	out, err := doc.WriteToBytes()
	if err != nil {
		return nil, false, fmt.Errorf("stripGatewayV6: serialize: %w", err)
	}
	return out, true, nil
}

// injectGatewayV6 inserts <gatewayv6>name</gatewayv6> into the WAN
// interface section, unless one is already present. Returns the
// modified bytes and whether anything changed.
func injectGatewayV6(input []byte, gatewayName string) ([]byte, bool, error) {
	if gatewayName == "" {
		return nil, false, errors.New("injectGatewayV6: gatewayName required")
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(input); err != nil {
		return nil, false, fmt.Errorf("injectGatewayV6: parse: %w", err)
	}
	wan := doc.FindElement("//opnsense/interfaces/wan")
	if wan == nil {
		return input, false, nil
	}
	if wan.FindElement("./gatewayv6") != nil {
		return input, false, nil
	}
	gw := wan.CreateElement("gatewayv6")
	gw.SetText(gatewayName)

	out, err := doc.WriteToBytes()
	if err != nil {
		return nil, false, fmt.Errorf("injectGatewayV6: serialize: %w", err)
	}
	return out, true, nil
}

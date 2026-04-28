package cutover2

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"time"

	"goodkind.io/mwan/internal/config"
)

// removeGatewayV6 removes <gatewayv6> from the WAN interface in OPNsense
// config.xml via SSH. This prevents system_routing_configure from reinstalling
// the IPv6 static route during FRR stop+start (force_down only prevents IPv4
// reinstallation on FreeBSD). The change survives reboots and config saves.
func removeGatewayV6(ctx context.Context, log *slog.Logger, cfg *config.Config) {
	log.Info("removing gatewayv6 from OPNsense WAN interface config")

	raw, err := opnsenseSSHOutput(ctx, log, cfg, "cat /conf/config.xml")
	if err != nil {
		log.Error("failed to read config.xml from OPNsense", "err", err)
		return
	}

	modified, changed := stripGatewayV6(raw)
	if !changed {
		log.Info("gatewayv6 not found in config.xml, nothing to remove")
		return
	}

	if err := opnsenseSSH(ctx, log, cfg, "cp /conf/config.xml /conf/config.xml.pre-bgp"); err != nil {
		log.Error("failed to backup config.xml", "err", err)
		return
	}

	if err := opnsenseSSHWrite(ctx, log, cfg, "/conf/config.xml", modified); err != nil {
		log.Error("failed to write modified config.xml", "err", err)
		return
	}

	log.Info("gatewayv6 removed from config.xml")
}

// restoreGatewayV6 surgically re-adds <gatewayv6>WAN_GW6</gatewayv6> to the
// WAN interface in OPNsense config.xml. Idempotent and conservative:
//
//   - If <gatewayv6> is already present, no-op.
//   - If the gateway name passed in is empty, no-op.
//   - Mutates only the WAN section's gatewayv6 line, preserving everything
//     else byte-for-byte. Does NOT clobber other config changes the way
//     the previous cp-from-backup implementation did.
//
// Caller is responsible for verifying the gateway record exists on
// OPNsense; if it doesn't (post-decommission state), no DefaultRoute
// will be selected even after this function "succeeds" - the caller
// should refuse to run unfuck in that case (see cmdUnfuck pre-flight).
func restoreGatewayV6(ctx context.Context, log *slog.Logger, cfg *config.Config, gatewayName string) {
	if gatewayName == "" {
		log.Info("restoreGatewayV6: empty gatewayName; skipping")
		return
	}
	log.Info("restoring gatewayv6 to OPNsense WAN interface config",
		"gateway", gatewayName)

	raw, err := opnsenseSSHOutput(ctx, log, cfg, "cat /conf/config.xml")
	if err != nil {
		log.Error("failed to read config.xml from OPNsense", "err", err)
		return
	}

	modified, changed := injectGatewayV6(raw, gatewayName)
	if !changed {
		log.Info("restoreGatewayV6: <gatewayv6> already present in <wan>; no-op")
		return
	}

	if err := opnsenseSSHWrite(ctx, log, cfg, "/conf/config.xml", modified); err != nil {
		log.Error("failed to write modified config.xml", "err", err)
		return
	}
	log.Info("restoreGatewayV6: injected <gatewayv6> back into <wan>",
		"gateway", gatewayName)
}

// injectGatewayV6 inserts a <gatewayv6>name</gatewayv6> element into the
// <wan> interface section of an OPNsense config.xml, immediately before
// the closing </wan> tag. If <gatewayv6> is already present anywhere in
// <wan>, returns the input unchanged.
//
// Mirror of stripGatewayV6: tokenize to find <wan>, scan inside, decide,
// then string-splice to preserve byte-level fidelity.
func injectGatewayV6(input []byte, gatewayName string) ([]byte, bool) {
	decoder := xml.NewDecoder(bytes.NewReader(input))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose

	var inWan bool
	var alreadyHas bool
	var wanCloseStart int64 = -1

	for {
		offset := decoder.InputOffset()
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "wan" {
				inWan = true
				continue
			}
			if inWan && t.Name.Local == "gatewayv6" {
				alreadyHas = true
			}
		case xml.EndElement:
			if t.Name.Local == "wan" && inWan {
				wanCloseStart = offset
				inWan = false
			}
		}
		if alreadyHas || wanCloseStart >= 0 {
			break
		}
	}

	if alreadyHas {
		return input, false
	}
	if wanCloseStart < 0 {
		// no <wan> section at all - bail rather than mangle
		return input, false
	}

	// Find indent: walk back from wanCloseStart over spaces/tabs to find
	// the indent of </wan>, then add 2 spaces of nesting for the new line.
	indentStart := wanCloseStart
	for indentStart > 0 && (input[indentStart-1] == ' ' || input[indentStart-1] == '\t') {
		indentStart--
	}
	indent := string(input[indentStart:wanCloseStart])
	innerIndent := indent + "  "

	insertion := fmt.Sprintf("%s<gatewayv6>%s</gatewayv6>\n", innerIndent, gatewayName)

	var out bytes.Buffer
	out.Write(input[:indentStart])
	out.WriteString(insertion)
	out.Write(input[indentStart:])
	return out.Bytes(), true
}

// stripGatewayV6 removes the <gatewayv6>...</gatewayv6> element from within
// the <wan> interface section of an OPNsense config.xml. It uses the XML
// tokenizer to locate the element precisely while preserving the rest of the
// file byte-for-byte.
func stripGatewayV6(input []byte) ([]byte, bool) {
	// Find the byte offsets of <gatewayv6>...</gatewayv6> inside <wan>
	decoder := xml.NewDecoder(bytes.NewReader(input))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose

	var inWan bool
	var startOffset, endOffset int64
	var found bool

	for {
		offset := decoder.InputOffset()
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "wan" {
				inWan = true
			}
			if inWan && t.Name.Local == "gatewayv6" {
				startOffset = offset
				if err := decoder.Skip(); err != nil {
					break
				}
				endOffset = decoder.InputOffset()
				found = true
				inWan = false
			}
		case xml.EndElement:
			if t.Name.Local == "wan" {
				inWan = false
			}
		}

		if found {
			break
		}
	}

	if !found {
		return input, false
	}

	// Remove the element and any surrounding whitespace/newline
	before := input[:startOffset]
	after := input[endOffset:]

	// Trim the trailing newline from before (the line the element was on)
	before = bytes.TrimRight(before, " \t")
	if len(before) > 0 && before[len(before)-1] == '\n' {
		before = before[:len(before)-1]
	}

	var out bytes.Buffer
	out.Write(before)
	out.WriteByte('\n')
	out.Write(after)

	return out.Bytes(), true
}

// opnsenseSSHOutput runs a command on OPNsense and returns stdout.
// SSHes as the configured user (default agoodkind) and wraps in sudo
// when not root, so callers can issue root-only commands like cat /conf/config.xml.
func opnsenseSSHOutput(ctx context.Context, _ *slog.Logger, cfg *config.Config, cmd string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	host := opnsenseSSHHost(cfg)
	user := opnsenseSSHUser(cfg)
	wrapped := opnsenseSudo(user, cmd)

	sshCmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("%s@%s", user, host), wrapped)
	out, err := sshCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("SSH %s@%s %s: %w", user, host, cmd, err)
	}
	return out, nil
}

// opnsenseSSHWrite writes data to a file on OPNsense via SSH stdin.
// Uses `sudo tee` (not `cat >`) when SSH user is not root: shell redirection
// runs in the unprivileged user's shell, but `sudo tee` writes as root.
func opnsenseSSHWrite(ctx context.Context, _ *slog.Logger, cfg *config.Config, path string, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	host := opnsenseSSHHost(cfg)
	user := opnsenseSSHUser(cfg)

	var remoteCmd string
	if user == "root" {
		remoteCmd = fmt.Sprintf("cat > %s", path)
	} else {
		// `sudo tee` runs as root and reads our stdin. >/dev/null suppresses
		// tee's stdout echo (we don't need the file contents bounced back).
		remoteCmd = fmt.Sprintf("sudo -n tee %s >/dev/null", path)
	}

	sshCmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("%s@%s", user, host), remoteCmd)
	sshCmd.Stdin = bytes.NewReader(data)
	out, err := sshCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("SSH write %s@%s %s: %w (output: %s)", user, host, path, err, string(out))
	}
	return nil
}

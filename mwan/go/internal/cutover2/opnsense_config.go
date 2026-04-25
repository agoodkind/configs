package cutover2

import (
	"bytes"
	"context"
	"encoding/xml"
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
	host := opnsenseSSHHost(cfg)
	log.Info("removing gatewayv6 from OPNsense WAN interface config")

	raw, err := opnsenseSSHOutput(ctx, log, host, "cat /conf/config.xml")
	if err != nil {
		log.Error("failed to read config.xml from OPNsense", "err", err)
		return
	}

	modified, changed := stripGatewayV6(raw)
	if !changed {
		log.Info("gatewayv6 not found in config.xml, nothing to remove")
		return
	}

	if err := opnsenseSSH(ctx, log, host, "cp /conf/config.xml /conf/config.xml.pre-bgp"); err != nil {
		log.Error("failed to backup config.xml", "err", err)
		return
	}

	if err := opnsenseSSHWrite(ctx, log, host, "/conf/config.xml", modified); err != nil {
		log.Error("failed to write modified config.xml", "err", err)
		return
	}

	log.Info("gatewayv6 removed from config.xml")
}

// restoreGatewayV6 restores <gatewayv6> to the WAN interface in OPNsense
// config.xml from the pre-BGP backup created by removeGatewayV6.
func restoreGatewayV6(ctx context.Context, log *slog.Logger, cfg *config.Config) {
	host := opnsenseSSHHost(cfg)
	log.Info("restoring gatewayv6 to OPNsense WAN interface config")
	if err := opnsenseSSH(ctx, log, host, "test -f /conf/config.xml.pre-bgp && cp /conf/config.xml.pre-bgp /conf/config.xml || echo no-backup"); err != nil {
		log.Error("failed to restore config.xml from backup", "err", err)
	}
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
		if err == io.EOF {
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
func opnsenseSSHOutput(ctx context.Context, log *slog.Logger, host, cmd string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	sshCmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("root@%s", host), cmd)
	out, err := sshCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("SSH %s: %w", cmd, err)
	}
	return out, nil
}

// opnsenseSSHWrite writes data to a file on OPNsense via SSH stdin.
func opnsenseSSHWrite(ctx context.Context, log *slog.Logger, host, path string, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	sshCmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("root@%s", host),
		fmt.Sprintf("cat > %s", path))
	sshCmd.Stdin = bytes.NewReader(data)
	out, err := sshCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("SSH write %s: %w (output: %s)", path, err, string(out))
	}
	return nil
}

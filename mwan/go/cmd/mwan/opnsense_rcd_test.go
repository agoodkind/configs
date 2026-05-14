package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const rcSubrStub = `load_rc_config() { :; }
`

func TestRCDWritesDaemonTOML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rcSubrPath := filepath.Join(dir, "rc.subr")
	if err := os.WriteFile(rcSubrPath, []byte(rcSubrStub), 0o600); err != nil {
		t.Fatalf("write rc.subr stub: %v", err)
	}

	scriptPath := filepath.Join("opnsense-src", "etc", "rc.d", "mwan_opnsense")
	scriptData, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read rc.d script: %v", err)
	}

	renderScript := string(scriptData)
	rewrittenScript := strings.Replace(renderScript, ". /etc/rc.subr", `. "${RC_SUBR_STUB}"`, 1)
	if rewrittenScript == renderScript {
		t.Fatal("rc.d script no longer contains the expected rc.subr include")
	}
	renderScript = strings.Replace(rewrittenScript, `run_rc_command "$1"`, `:`, 1)
	if renderScript == rewrittenScript {
		t.Fatal("rc.d script no longer contains the expected run_rc_command tail")
	}

	tempScriptPath := filepath.Join(dir, "mwan_opnsense")
	if err := os.WriteFile(tempScriptPath, []byte(renderScript), 0o700); err != nil {
		t.Fatalf("write temp rc.d script: %v", err)
	}

	daemonDir := filepath.Join(dir, "var-lib-mwan")
	daemonPath := filepath.Join(daemonDir, "daemon.toml")

	// This keeps the contract test portable: only the rc.subr include and
	// run_rc_command tail are neutralized, and the real render function still
	// runs under /bin/sh against temp paths.
	commandText := strings.Join([]string{
		"set -eu",
		`. "${SCRIPT_PATH}"`,
		`daemon_toml_dir="${DAEMON_TOML_DIR}"`,
		`daemon_toml_path="${DAEMON_TOML_PATH}"`,
		`mwan_opnsense_listen_serial="/dev/ttyV0.1"`,
		`mwan_opnsense_baud="921600"`,
		`mwan_opnsense_config_xml="/tmp/config.xml"`,
		`mwan_opnsense_backup_dir="/tmp/backup"`,
		`mwan_opnsense_logfile="/tmp/mwan-opnsense.log"`,
		`mwan_opnsense_state_dir="/tmp/transfers"`,
		`mwan_opnsense_write_daemon_toml`,
	}, "\n")

	command := exec.Command("/bin/sh", "-c", commandText)
	command.Env = append(os.Environ(),
		"RC_SUBR_STUB="+rcSubrPath,
		"SCRIPT_PATH="+tempScriptPath,
		"DAEMON_TOML_DIR="+daemonDir,
		"DAEMON_TOML_PATH="+daemonPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("render daemon.toml: %v\n%s", err, output)
	}

	info, err := os.Stat(daemonPath)
	if err != nil {
		t.Fatalf("stat daemon.toml: %v", err)
	}
	if gotPerm := info.Mode().Perm(); gotPerm != 0o600 {
		t.Fatalf("daemon.toml perm = %#o, want %#o", gotPerm, 0o600)
	}

	data, err := os.ReadFile(daemonPath)
	if err != nil {
		t.Fatalf("read daemon.toml: %v", err)
	}
	got := string(data)
	wantLines := []string{
		"[daemon]",
		`serial_path = "/dev/ttyV0.1"`,
		`baud = 921600`,
		`config_xml_path = "/tmp/config.xml"`,
		`backup_dir = "/tmp/backup"`,
		`logfile = "/tmp/mwan-opnsense.log"`,
		`state_dir = "/tmp/transfers"`,
	}
	for _, wantLine := range wantLines {
		if !strings.Contains(got, wantLine) {
			t.Fatalf("daemon.toml missing %q\n%s", wantLine, got)
		}
	}
}

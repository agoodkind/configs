package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"goodkind.io/mwan/internal/opnsense/validate"
)

// outputFormat names the supported -output-format values.
type outputFormat string

const (
	outputFormatText outputFormat = "text"
	outputFormatJSON outputFormat = "json"
)

type validateFlags struct {
	vmid             int
	stateDir         string
	deployID         string
	baselineOnly     bool
	diffAgainst      string
	outputFormat     outputFormat
	severityFilter   validate.Severity
	envTransport     envTransport
	envGRPCTarget    string
	opnsenseSSHHost  string
	opnsenseJumpHost string
	proxmoxSSHHost   string
	lanClientSSH     string
	opnsenseAddr     string
	apiKey           string
	apiSecret        string
	bgpV4Neighbors   string
	bgpV6Neighbors   string
	opnsenseLAN      string
	mwanSocket       string
	mwanHostSocket   string
	settleAfter      time.Duration
	timeout          time.Duration
}

// runOPNsenseValidate is the entry point invoked by main.go for
// the `mwan opnsense-validate <vmid>` subcommand.
func runOPNsenseValidate(args []string) error {
	flags, vmid, err := parseValidateFlags(args)
	if err != nil {
		return err
	}
	flags.vmid = vmid

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if flags.timeout > 0 {
		var cancelTimeout context.CancelFunc
		ctx, cancelTimeout = context.WithTimeout(ctx, flags.timeout)
		defer cancelTimeout()
	}

	slog.InfoContext(ctx, "opnsense-validate boundary",
		"vmid", flags.vmid, "deploy_id", flags.deployID,
		"mode", validateMode(flags))

	env, err := buildEnvFromValidateFlags(flags)
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-validate build env failed",
			"err", err.Error())
		return fmt.Errorf("build env: %w", err)
	}
	cfg := buildValidateConfigFromFlags(flags)

	var prior *validate.Baseline
	if flags.diffAgainst != "" {
		prior, err = validate.LoadBaseline(flags.diffAgainst)
		if err != nil {
			slog.ErrorContext(ctx, "opnsense-validate load prior baseline failed",
				"path", flags.diffAgainst, "err", err.Error())
			return fmt.Errorf("load prior baseline: %w", err)
		}
	}

	result, err := validate.Run(ctx, cfg, prior, env)
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-validate Run failed",
			"err", err.Error())
		return fmt.Errorf("validate.Run: %w", err)
	}

	if flags.baselineOnly || flags.diffAgainst == "" {
		if err := persistBaseline(flags, result); err != nil {
			return err
		}
	}

	if flags.diffAgainst != "" {
		report := validate.Diff(prior, result)
		if err := persistDiff(flags, report); err != nil {
			return err
		}
		if err := writeDiffOutput(flags, report); err != nil {
			return err
		}
		if report.Verdict == validate.DiffBlocker {
			err := errors.New("opnsense-validate: blocker verdict")
			slog.ErrorContext(ctx, "opnsense-validate blocker verdict",
				"err", err.Error())
			return err
		}
		return nil
	}

	return writeBaselineOutput(flags, result)
}

func validateMode(f validateFlags) string {
	if f.diffAgainst != "" {
		return "diff"
	}
	if f.baselineOnly {
		return "baseline-only"
	}
	return "baseline"
}

func parseValidateFlags(args []string) (validateFlags, int, error) {
	fs := flag.NewFlagSet("opnsense-validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr,
			"usage: mwan opnsense-validate [flags] <vmid>")
		fs.PrintDefaults()
	}
	out := validateFlags{
		outputFormat: outputFormatText,
		envTransport: envTransportSSH,
	}
	transportStr := fs.String("env-transport", string(envTransportSSH),
		"validate transport: ssh|grpc (grpc routes OPNsense ops via mwan-opnsense daemon)")
	fs.StringVar(&out.envGRPCTarget, "env-grpc-target", "",
		"gRPC target socket for --env-transport=grpc (e.g. unix:///var/run/qemu-server/102.mwanrpc)")
	fs.StringVar(&out.stateDir, "state-dir", validate.DefaultStateDir,
		"artefact root (matches MWAN-152's --state-dir)")
	fs.StringVar(&out.deployID, "deploy-id", "",
		"upgrade deploy identifier; defaults to YYYYMMDDhhmmss when empty")
	fs.BoolVar(&out.baselineOnly, "baseline-only", false,
		"capture a baseline and write it under --state-dir; do not diff")
	fs.StringVar(&out.diffAgainst, "diff-against", "",
		"path to a saved baseline to diff against; switches to compare mode")
	formatStr := fs.String("output-format", "text", "text|json")
	severityStr := fs.String("severity-filter", "",
		"only run checks at this severity or worse: blocker|all")
	fs.StringVar(&out.opnsenseSSHHost, "opnsense-ssh", "",
		"ssh destination for the OPNsense guest")
	fs.StringVar(&out.opnsenseJumpHost, "opnsense-jump", "",
		"ssh ProxyJump for OPNsense (optional)")
	fs.StringVar(&out.proxmoxSSHHost, "proxmox-ssh", "",
		"ssh destination for the Proxmox host (vault)")
	fs.StringVar(&out.lanClientSSH, "lan-client-ssh", "",
		"ssh destination for a LAN client used by data-plane probes")
	fs.StringVar(&out.opnsenseAddr, "opnsense-addr", "",
		"host:port for HTTPS GETs against the OPNsense web UI")
	fs.StringVar(&out.apiKey, "api-key", "", "OPNsense API key")
	fs.StringVar(&out.apiSecret, "api-secret", "", "OPNsense API secret")
	fs.StringVar(&out.bgpV4Neighbors, "bgp-v4-neighbors", "",
		"comma-separated v4 BGP peer addresses to assert Established")
	fs.StringVar(&out.bgpV6Neighbors, "bgp-v6-neighbors", "",
		"comma-separated v6 BGP peer addresses to assert Established")
	fs.StringVar(&out.opnsenseLAN, "opnsense-lan", "",
		"OPNsense LAN address used by dig probes")
	fs.StringVar(&out.mwanSocket, "mwan-opnsense-socket", "",
		"unix socket path probed by the gRPC check")
	fs.StringVar(&out.mwanHostSocket, "mwan-opnsense-host-socket", "",
		"unix socket path the bridge listens on")
	fs.DurationVar(&out.settleAfter, "settle-after-upgrade", 5*time.Minute,
		"dwell time before DHCP-related checks run; resolved decision O-1")
	fs.DurationVar(&out.timeout, "timeout", 10*time.Minute,
		"overall runner timeout")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return out, 0, flag.ErrHelp
		}
		slog.Error("opnsense-validate parse flags failed", "err", err.Error())
		return out, 0, fmt.Errorf("parse opnsense-validate flags: %w", err)
	}
	out.outputFormat = outputFormat(*formatStr)
	if out.outputFormat != outputFormatText && out.outputFormat != outputFormatJSON {
		return out, 0, fmt.Errorf("opnsense-validate: --output-format must be text|json")
	}
	transport, err := parseEnvTransport(*transportStr)
	if err != nil {
		return out, 0, fmt.Errorf("opnsense-validate: %w", err)
	}
	out.envTransport = transport
	if *severityStr == "blocker" {
		out.severityFilter = validate.SeverityBlocker
	} else if *severityStr != "" && *severityStr != "all" {
		return out, 0, fmt.Errorf(
			"opnsense-validate: --severity-filter must be blocker|all (got %q)",
			*severityStr)
	}

	rest := fs.Args()
	if len(rest) != 1 {
		return out, 0, errors.New("opnsense-validate: exactly one positional <vmid> required")
	}
	vmid, err := strconv.Atoi(rest[0])
	if err != nil {
		return out, 0, fmt.Errorf("opnsense-validate: invalid vmid %q", rest[0])
	}
	if out.deployID == "" {
		out.deployID = time.Now().UTC().Format("20060102150405")
	}
	return out, vmid, nil
}

func buildEnvFromValidateFlags(f validateFlags) (validate.Env, error) {
	return defaultEnvFactory().build(envTransportConfig{
		Transport:           f.envTransport,
		GRPCTarget:          f.envGRPCTarget,
		OPNsenseSSHHost:     f.opnsenseSSHHost,
		OPNsenseSSHJumpHost: f.opnsenseJumpHost,
		ProxmoxSSHHost:      f.proxmoxSSHHost,
		LANClientSSHHost:    f.lanClientSSH,
		OPNsenseAddr:        f.opnsenseAddr,
	})
}

func buildValidateConfigFromFlags(f validateFlags) validate.Config {
	cfg := validate.Config{
		VMID:                   f.vmid,
		DeployID:               f.deployID,
		StateDir:               f.stateDir,
		BGPv4Neighbors:         splitNonEmpty(f.bgpV4Neighbors),
		BGPv6Neighbors:         splitNonEmpty(f.bgpV6Neighbors),
		OPNsenseLAN:            f.opnsenseLAN,
		MWANOpnsenseSocket:     f.mwanSocket,
		MWANOpnsenseHostSocket: f.mwanHostSocket,
		APIAuth:                nil,
		SettleAfterUpgrade:     f.settleAfter,
		SeverityFilter:         f.severityFilter,
	}
	if f.apiKey != "" || f.apiSecret != "" {
		cfg.APIAuth = &validate.BasicAuth{Username: f.apiKey, Password: f.apiSecret}
	}
	return cfg
}

func splitNonEmpty(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func persistBaseline(f validateFlags, baseline *validate.Baseline) error {
	if err := validate.SaveBaseline(
		f.stateDir, f.vmid, f.deployID,
		validate.PreBaselineFilename, baseline,
	); err != nil {
		slog.Error("opnsense-validate persistBaseline save failed",
			"err", err.Error())
		return fmt.Errorf("persist baseline: %w", err)
	}
	slog.Info("opnsense-validate baseline persisted",
		"state_dir", f.stateDir, "vmid", f.vmid, "deploy_id", f.deployID)
	return nil
}

func persistDiff(f validateFlags, report *validate.DiffReport) error {
	dir := validate.ArtefactPath(f.stateDir, f.vmid, f.deployID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("opnsense-validate persistDiff mkdir failed",
			"dir", dir, "err", err.Error())
		return fmt.Errorf("create artefact dir: %w", err)
	}
	path := filepath.Join(dir, validate.DiffReportFilename)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		slog.Error("opnsense-validate persistDiff encode failed",
			"err", err.Error())
		return fmt.Errorf("encode diff: %w", err)
	}
	cleanPath := filepath.Clean(path)
	if err := os.WriteFile(cleanPath, encoded, 0o600); err != nil {
		slog.Error("opnsense-validate persistDiff write failed",
			"path", cleanPath, "err", err.Error())
		return fmt.Errorf("write diff: %w", err)
	}
	slog.Info("opnsense-validate diff persisted", "path", cleanPath)
	return nil
}

func writeBaselineOutput(f validateFlags, baseline *validate.Baseline) error {
	if f.outputFormat == outputFormatJSON {
		encoded, err := json.MarshalIndent(baseline, "", "  ")
		if err != nil {
			slog.Error("opnsense-validate writeBaselineOutput encode failed",
				"err", err.Error())
			return fmt.Errorf("encode baseline: %w", err)
		}
		_, _ = os.Stdout.Write(encoded)
		_, _ = os.Stdout.Write([]byte("\n"))
		return nil
	}
	validate.SortResultsByID(baseline.Results)
	for _, r := range baseline.Results {
		fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\n",
			r.Outcome, r.Severity, r.CheckID, r.Message)
	}
	counts := validate.CountByOutcome(baseline.Results)
	fmt.Fprintf(os.Stdout, "summary: pass=%d fail=%d skip=%d error=%d\n",
		counts[validate.OutcomePass],
		counts[validate.OutcomeFail],
		counts[validate.OutcomeSkip],
		counts[validate.OutcomeError])
	return nil
}

func writeDiffOutput(f validateFlags, report *validate.DiffReport) error {
	if f.outputFormat == outputFormatJSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			slog.Error("opnsense-validate writeDiffOutput encode failed",
				"err", err.Error())
			return fmt.Errorf("encode diff: %w", err)
		}
		_, _ = os.Stdout.Write(encoded)
		_, _ = os.Stdout.Write([]byte("\n"))
		return nil
	}
	for _, e := range report.Entries {
		fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\n",
			e.Outcome, e.Severity, e.CheckID, e.Reason)
	}
	fmt.Fprintf(os.Stdout, "verdict: %s\n", report.Verdict)
	return nil
}

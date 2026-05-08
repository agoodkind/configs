package validate

import (
	"context"
	"fmt"
	"strings"
)

// CheckIDMWANOpnsenseDaemonRunning is the matrix id for the
// mwan-opnsense daemon liveness check. The remaining constants
// in this block are the matrix ids for the other MWAN-integration
// checks.
const (
	CheckIDMWANOpnsenseDaemonRunning = "mwan_opnsense_daemon_running"
	// CheckIDMWANOpnsenseGRPCResponding probes the OOB gRPC socket.
	CheckIDMWANOpnsenseGRPCResponding = "mwan_opnsense_grpc_responding"
	// CheckIDQGAChannelResponsive verifies the QGA channel is alive.
	CheckIDQGAChannelResponsive = "qga_channel_responsive"
	// CheckIDMWANOpnsenseHostSocketPresent verifies the bridge socket exists.
	CheckIDMWANOpnsenseHostSocketPresent = "mwan_opnsense_host_socket_present"
)

// mwanOpnsenseDaemonRunningCheck pgreps mwan-opnsense on the guest.
type mwanOpnsenseDaemonRunningCheck struct{}

// NewMWANOpnsenseDaemonRunningCheck returns the daemon-running check.
func NewMWANOpnsenseDaemonRunningCheck() Check { return &mwanOpnsenseDaemonRunningCheck{} }

func (c *mwanOpnsenseDaemonRunningCheck) ID() string {
	return CheckIDMWANOpnsenseDaemonRunning
}
func (c *mwanOpnsenseDaemonRunningCheck) Category() Category           { return CategoryMWAN }
func (c *mwanOpnsenseDaemonRunningCheck) Severity() Severity           { return SeverityBlocker }
func (c *mwanOpnsenseDaemonRunningCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *mwanOpnsenseDaemonRunningCheck) Run(ctx context.Context, env Env) Result {
	command := `pgrep -f mwan-opnsense >/dev/null 2>&1; echo $?`
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	value := strings.TrimSpace(cmd.Stdout)
	res.ParsedValue = value
	if value == "0" {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = "mwan-opnsense process not found"
	return res
}

// mwanOpnsenseGRPCRespondingCheck runs `mwan opnsense-probe`
// against the OOB socket on the Proxmox host. The socket path is
// configurable so tests can swap it.
type mwanOpnsenseGRPCRespondingCheck struct {
	socket string
}

// NewMWANOpnsenseGRPCRespondingCheck returns the gRPC probe check.
func NewMWANOpnsenseGRPCRespondingCheck(socket string) Check {
	if socket == "" {
		socket = "/run/mwan-opnsense.sock"
	}
	return &mwanOpnsenseGRPCRespondingCheck{socket: socket}
}

func (c *mwanOpnsenseGRPCRespondingCheck) ID() string {
	return CheckIDMWANOpnsenseGRPCResponding
}
func (c *mwanOpnsenseGRPCRespondingCheck) Category() Category           { return CategoryMWAN }
func (c *mwanOpnsenseGRPCRespondingCheck) Severity() Severity           { return SeverityBlocker }
func (c *mwanOpnsenseGRPCRespondingCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *mwanOpnsenseGRPCRespondingCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	command := fmt.Sprintf("mwan opnsense-probe -target unix://%s -op version", c.socket)
	out, err := env.SSHProxmoxHost(ctx, command)
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	res.RawStdout = out.Stdout
	res.RawExitCode = out.ExitCode
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	if out.ExitCode == 0 {
		res.Outcome = OutcomePass
		res.ParsedValue = "grpc_ok"
		return res
	}
	res.Outcome = OutcomeFail
	res.ParsedValue = "grpc_fail"
	res.Message = fmt.Sprintf("opnsense-probe exit %d", out.ExitCode)
	return res
}

// qgaChannelResponsiveCheck calls `qm guest exec <vmid> -- /bin/true`
// on the Proxmox host.
type qgaChannelResponsiveCheck struct {
	vmid int
}

// NewQGAChannelResponsiveCheck returns the QGA check.
func NewQGAChannelResponsiveCheck(vmid int) Check {
	return &qgaChannelResponsiveCheck{vmid: vmid}
}

func (c *qgaChannelResponsiveCheck) ID() string         { return CheckIDQGAChannelResponsive }
func (c *qgaChannelResponsiveCheck) Category() Category { return CategoryMWAN }
func (c *qgaChannelResponsiveCheck) Severity() Severity { return SeverityBlocker }

func (c *qgaChannelResponsiveCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *qgaChannelResponsiveCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	command := fmt.Sprintf("qm guest exec %d -- /bin/true", c.vmid)
	out, err := env.SSHProxmoxHost(ctx, command)
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	res.RawStdout = out.Stdout
	res.RawExitCode = out.ExitCode
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	if out.ExitCode == 0 {
		res.Outcome = OutcomePass
		res.ParsedValue = "qga_ok"
		return res
	}
	res.Outcome = OutcomeFail
	res.ParsedValue = "qga_fail"
	res.Message = fmt.Sprintf("qm guest exec exit %d", out.ExitCode)
	return res
}

// mwanOpnsenseHostSocketCheck verifies the unix socket the MWAN
// host bridge listens on.
type mwanOpnsenseHostSocketCheck struct {
	socket string
}

// NewMWANOpnsenseHostSocketCheck returns the socket-presence check.
func NewMWANOpnsenseHostSocketCheck(socket string) Check {
	if socket == "" {
		socket = "/run/mwan-opnsense-host.sock"
	}
	return &mwanOpnsenseHostSocketCheck{socket: socket}
}

func (c *mwanOpnsenseHostSocketCheck) ID() string {
	return CheckIDMWANOpnsenseHostSocketPresent
}
func (c *mwanOpnsenseHostSocketCheck) Category() Category           { return CategoryMWAN }
func (c *mwanOpnsenseHostSocketCheck) Severity() Severity           { return SeverityBlocker }
func (c *mwanOpnsenseHostSocketCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *mwanOpnsenseHostSocketCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	command := fmt.Sprintf("test -S %s && echo present || echo missing", c.socket)
	out, err := env.SSHProxmoxHost(ctx, command)
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	res.RawStdout = out.Stdout
	res.RawExitCode = out.ExitCode
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	value := strings.TrimSpace(out.Stdout)
	res.ParsedValue = value
	if value == "present" {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = "mwan-opnsense-host socket missing"
	return res
}

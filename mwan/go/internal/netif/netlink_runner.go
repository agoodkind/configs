//go:build linux

package netif

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
)

// NetlinkRunner is the in-process IPRunner backed by github.com/vishvananda/netlink.
// Step 2 of the netif refactor only declares the type. Subsequent steps wire the
// individual high-level operations (ReconcileAddrs, ReconcileTableDefault,
// ReconcileRules, FindMainRADefault, Monitor) directly onto this struct or onto
// helper functions in the same package, bypassing the IPRunner.Run shellout API.
//
// Why this stub looks the way it does:
//
//   - Run is a CLI-shaped API. Netlink is a typed-syscall API. There is no
//     useful "execute /sbin/ip <args> via netlink" mapping that would be both
//     correct and not a parser. So Run on NetlinkRunner cannot be a clean
//     drop-in replacement.
//   - Instead, the netlink variants of the high-level helpers will live as
//     parallel implementations selected via a feature flag (see Step 3 of the
//     plan). NetlinkRunner.Run remains a no-op that returns ErrNetlinkRunnerRun.
//     Production paths that today call helpers that internally call Run will
//     gain a "use the netlink path" branch in Step 3.
//   - This means callers must NOT pass a NetlinkRunner where they would have
//     used ExecIPRunner expecting Run to work. Step 7 deletes ExecIPRunner; at
//     that point all helpers are netlink-direct and the IPRunner interface is
//     either removed or repurposed as a tiny capability descriptor.
//
// All operations log at slog.LevelDebug at entry and exit, with the originating
// function name, parameter snapshot, and result error.
type NetlinkRunner struct {
	log    *slog.Logger
	dryRun bool
}

// ErrNetlinkRunnerRun is returned by NetlinkRunner.Run. The netlink path does
// not implement the legacy CLI-shaped Run; helpers should call netlink-typed
// operations directly. See package doc and Step 3 of the refactor plan.
var ErrNetlinkRunnerRun = errors.New(
	"netif.NetlinkRunner.Run is not implemented: " +
		"call typed netlink helpers (ReconcileAddrs, ReconcileTableDefault, " +
		"ReconcileRules, FindMainRADefault, NewMonitor) directly. " +
		"This stub exists so NetlinkRunner satisfies the IPRunner interface " +
		"during the staged netif refactor (Step 2-7).",
)

// NewNetlinkRunner constructs a NetlinkRunner. log must be non-nil. The runner
// holds no kernel handles by itself; netlink sockets are opened lazily by the
// individual typed helpers as they are added in subsequent refactor steps.
func NewNetlinkRunner(log *slog.Logger, dryRun bool) *NetlinkRunner {
	if log == nil {
		// Boundary check: a nil logger would silently lose every operation
		// log at the most critical layer. Surface it loudly at construction.
		panic("netif.NewNetlinkRunner: log is required (boundary contract)")
	}
	r := &NetlinkRunner{
		log:    log.With("component", "netlink-runner"),
		dryRun: dryRun,
	}
	r.log.Debug("netlink-runner: constructed", "dry_run", dryRun)
	return r
}

// Run satisfies the IPRunner interface. It always returns ErrNetlinkRunnerRun.
// See type-level doc for why. Logged at WARN to surface any accidental call
// during the transition.
func (r *NetlinkRunner) Run(
	ctx context.Context, timeout time.Duration, args ...string,
) ([]byte, error) {
	// Boundary log: argv is captured for diagnosability.
	argv := append([]string{"ip"}, args...)
	r.log.Warn(
		"netlink-runner: Run called on netlink runner; this is a misconfiguration",
		"argv", argv,
		"timeout_ms", timeout.Milliseconds(),
		"hint", "use typed netlink helpers; see ErrNetlinkRunnerRun",
		"argv_concat", strings.Join(argv, " "),
	)
	return nil, ErrNetlinkRunnerRun
}

// DryRun reports whether mutating netlink operations should be skipped. Used
// by the typed helpers added in subsequent steps; the daemon may inspect this
// to decide whether to log "would have done X" without applying.
func (r *NetlinkRunner) DryRun() bool { return r.dryRun }

// Logger returns the runner's slog. Typed helpers chain ".With(...)" on this
// for per-operation context.
func (r *NetlinkRunner) Logger() *slog.Logger { return r.log }

// Compile-time check: NetlinkRunner satisfies IPRunner.
var _ IPRunner = (*NetlinkRunner)(nil)

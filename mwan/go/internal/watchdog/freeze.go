package watchdog

import (
	"context"
	"strings"
)

// ensureGuestThawed checks the guest agent's filesystem freeze state and
// releases a freeze that outlived its snapshot. Snapshots freeze guest
// filesystems through the guest agent, and a thaw that never lands leaves
// the guest wedged with guest-exec disabled and new logins hanging, so
// maybeSnapshot runs this after every attempt and runStartupChecks runs it
// once at startup. An unreachable agent is a normal outcome (the VM may be
// down or mid-boot) and changes nothing.
func (w *watchdog) ensureGuestThawed(ctx context.Context, phase string) {
	log := w.tracedLogger(ctx)
	status, err := w.ops.VMFSFreezeStatus(ctx, w.cfg.MwanVMID)
	if err != nil {
		log.DebugContext(ctx, "fsfreeze-status unavailable",
			"phase", phase, "err", err)
		return
	}
	if !strings.Contains(status, "frozen") {
		return
	}
	log.WarnContext(ctx, "guest filesystems left frozen; thawing",
		"phase", phase, "status", status)
	if err := w.ops.VMFSFreezeThaw(ctx, w.cfg.MwanVMID); err != nil {
		log.ErrorContext(ctx, "fsfreeze-thaw failed",
			"phase", phase, "err", err)
		return
	}
	after, err := w.ops.VMFSFreezeStatus(ctx, w.cfg.MwanVMID)
	if err != nil {
		log.WarnContext(ctx, "fsfreeze-status recheck unavailable after thaw",
			"phase", phase, "err", err)
		return
	}
	log.InfoContext(ctx, "guest thawed after stuck freeze",
		"phase", phase, "status_after", after)
}

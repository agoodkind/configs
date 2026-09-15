package watchdog

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/notify"
)

// snapshotTestWatchdog returns a watchdog whose snapshot cycle fires on the
// first healthy probe, so a test drives one attempt per maybeSnapshot call.
func snapshotTestWatchdog(t *testing.T, mock *mockOps) *watchdog {
	t.Helper()
	w := newTestWatchdog(t, mock, func(cfg *config.Config) {
		cfg.Watchdog.SnapshotHealthyThreshold = 1
	})
	w.consecutiveHealthy = 1
	w.lastHashCheckOK = true
	return w
}

// TestFailedSnapshotSpacesOutTheNextAttempt covers the retry storm: the
// cycle qualifies again on the very next probe unless a failure records the
// attempt, so a failure that cannot resolve itself was retried every few
// seconds for as long as it lasted.
func TestFailedSnapshotSpacesOutTheNextAttempt(t *testing.T) {
	mock := &mockOps{vmSnapErr: errors.New("snapshot task aborted")}
	w := snapshotTestWatchdog(t, mock)
	now := time.Unix(1_700_000_000, 0)
	w.nowFn = func() time.Time { return now }

	w.maybeSnapshot(context.Background())

	if w.consecutiveSnapshotFails != 1 {
		t.Fatalf("consecutiveSnapshotFails = %d, want 1", w.consecutiveSnapshotFails)
	}
	if !w.snapshotBackoffActive() {
		t.Fatal("a failed snapshot must space out the next attempt")
	}
	if w.consecutiveHealthy != 0 {
		t.Fatalf("consecutiveHealthy = %d, want 0: a failure must not leave the"+
			" cycle qualified to retry immediately", w.consecutiveHealthy)
	}

	// The next probe must not attempt another snapshot while the wait holds.
	w.consecutiveHealthy = 1
	before := mock.vmSnapshotsCalls
	w.maybeSnapshot(context.Background())
	if mock.vmSnapshotsCalls != before {
		t.Fatal("the cycle attempted another snapshot during the wait")
	}
}

// TestSnapshotBackoffGrowsThenStops covers the wait after repeated failures.
func TestSnapshotBackoffGrowsThenStops(t *testing.T) {
	t.Parallel()

	if got := snapshotFailureBackoff(1); got != snapshotFailureBackoffBase {
		t.Fatalf("first backoff = %s, want %s", got, snapshotFailureBackoffBase)
	}
	if got := snapshotFailureBackoff(2); got != 2*snapshotFailureBackoffBase {
		t.Fatalf("second backoff = %s, want %s", got, 2*snapshotFailureBackoffBase)
	}
	if got := snapshotFailureBackoff(20); got != snapshotFailureBackoffCap {
		t.Fatalf("backoff after many failures = %s, want the %s cap",
			got, snapshotFailureBackoffCap)
	}
}

// TestSuccessClearsTheFailureState covers recovery: once a snapshot lands,
// the cycle must return to its normal cadence.
func TestSuccessClearsTheFailureState(t *testing.T) {
	mock := &mockOps{}
	w := snapshotTestWatchdog(t, mock)
	now := time.Unix(1_700_000_000, 0)
	w.nowFn = func() time.Time { return now }
	w.consecutiveSnapshotFails = 2
	// An active wait, so clearing it is observable rather than assumed.
	w.snapshotBackoffUntil = now.Add(10 * time.Minute)
	w.forcedDeletes = 1

	w.noteSnapshotSuccess(context.Background())

	if w.consecutiveSnapshotFails != 0 {
		t.Fatalf("consecutiveSnapshotFails = %d, want 0", w.consecutiveSnapshotFails)
	}
	if !w.snapshotBackoffUntil.IsZero() {
		t.Fatalf("snapshotBackoffUntil = %s, want the zero time",
			w.snapshotBackoffUntil)
	}
	if w.snapshotBackoffActive() {
		t.Fatal("a successful snapshot must clear the wait")
	}
	if w.forcedDeletes != 0 {
		t.Fatalf("forcedDeletes = %d, want 0", w.forcedDeletes)
	}
}

// TestAlertFiresOnlyAfterRepeatedFailures covers the alert threshold. One
// failure is routine; a run of them is what needs a person.
func TestAlertFiresOnlyAfterRepeatedFailures(t *testing.T) {
	mock := &mockOps{vmSnapErr: errors.New("snapshot task aborted")}
	w := snapshotTestWatchdog(t, mock)
	now := time.Unix(1_700_000_000, 0)
	w.nowFn = func() time.Time { return now }
	ctx := context.Background()
	fn := fakeNotifierFrom(t, w)

	for i := 1; i < snapshotFailureAlertThreshold; i++ {
		w.noteSnapshotFailure(ctx, "known-good-x", errors.New("boom"))
		if fn.Active(alertKindSnapshotFailed, w.cfg.MwanVMID) {
			t.Fatalf("alert fired after %d failures, before the threshold of %d",
				i, snapshotFailureAlertThreshold)
		}
	}

	w.noteSnapshotFailure(ctx, "known-good-x", errors.New("boom"))

	if !fn.Active(alertKindSnapshotFailed, w.cfg.MwanVMID) {
		t.Fatalf("no alert after %d failures in a row",
			snapshotFailureAlertThreshold)
	}
}

// fullThinPoolSnapshotError is the Proxmox output from the 2026-09-14 alert,
// when the hypervisor's LVM thin pool was past its autoextend threshold.
const fullThinPoolSnapshotError = "qm snapshot 113 known-good-20260914-231024:" +
	" systemd-run scope qm snapshot: exit status 255: freeze guest filesystem\n" +
	"snapshotting 'drive-scsi0' (local-lvm:vm-113-disk-1)\n" +
	"thaw guest filesystem\n" +
	"snapshot create failed: starting cleanup\n" +
	"lvcreate snapshot 'pve/snap_vm-113-disk-1_known-good-20260914-231024' error:" +
	" Cannot create new thin volume, free space in thin pool pve/data reached threshold."

// lastSnapshotAlert drives noteSnapshotFailure past the alert threshold with
// the given error and returns the event the notifier received.
func lastSnapshotAlert(t *testing.T, cause error) notifyEvent {
	t.Helper()
	w := snapshotTestWatchdog(t, &mockOps{})
	w.nowFn = func() time.Time { return time.Unix(1_700_000_000, 0) }
	ctx := context.Background()
	for range snapshotFailureAlertThreshold {
		w.noteSnapshotFailure(ctx, "known-good-20260914-231024", cause)
	}
	events := fakeNotifierFrom(t, w).snapshot()
	if len(events) != 1 {
		t.Fatalf("notify events = %d, want exactly 1 alert: %+v", len(events), events)
	}
	return events[0]
}

// alertField returns the string value of the named field on an event.
func alertField(t *testing.T, event notifyEvent, key string) string {
	t.Helper()
	for _, field := range event.Fields {
		if field.Key == key {
			return field.Value.String()
		}
	}
	t.Fatalf("alert has no %q field: %+v", key, event.Fields)
	return ""
}

// renderedAlertBody renders an event through the email body builder with the
// alert_kind, alert_key, and transition or resolved fields the notify manager
// adds, which is the body the operator reads.
func renderedAlertBody(event notifyEvent) string {
	message := event.Message
	stateField := slog.Bool("transition", true)
	if event.Resolved {
		message = "RECOVERED: " + message
		stateField = slog.Bool("resolved", true)
	}
	record := slog.NewRecord(time.Unix(1_700_000_000, 0), event.Level, message, 0)
	record.AddAttrs(
		slog.String("alert_kind", event.Kind),
		slog.String("alert_key", event.Key),
		stateField,
	)
	record.AddAttrs(event.Fields...)
	return notify.BuildEmailBody(record, nil)
}

// TestSnapshotAlertNamesTheFullThinPool covers the 2026-09-14 alert: the
// cause was a thin pool past its threshold, and the email must say that and
// what to do, naming the pool, with the original error text intact.
func TestSnapshotAlertNamesTheFullThinPool(t *testing.T) {
	event := lastSnapshotAlert(t, errors.New(fullThinPoolSnapshotError))

	if event.Kind != alertKindSnapshotFailed || event.Key != "113" {
		t.Fatalf("alert kind/key = %q/%q, want %q/113", event.Kind, event.Key,
			alertKindSnapshotFailed)
	}
	if got := alertField(t, event, snapshotAlertRawKey); got != fullThinPoolSnapshotError {
		t.Fatalf("original error text = %q, want the unmodified Proxmox output", got)
	}
	wantBody := "Gateway rollback snapshots failing: disk pool pve/data is full\n" +
		"\n" +
		"Action: Free space in pve/data (for example delete old snapshots) or grow it.\n" +
		"Alert_key: 113\n" +
		"Alert_kind: snapshot-failed\n" +
		"Cause: Disk pool pve/data is too full for a snapshot. Deploy snapshots will fail too.\n" +
		"Failed attempts: 3\n" +
		"Original error text: " + fullThinPoolSnapshotError + "\n" +
		"Snapshot: known-good-20260914-231024\n" +
		"Transition: true"
	if got := renderedAlertBody(event); got != wantBody {
		t.Fatalf("rendered email body:\n%s\n\nwant:\n%s", got, wantBody)
	}
}

// TestSnapshotAlertOutOfDataSpaceNamesThePool covers LVM's other wording for
// an exhausted thin pool.
func TestSnapshotAlertOutOfDataSpaceNamesThePool(t *testing.T) {
	event := lastSnapshotAlert(t, errors.New(
		"qm snapshot 113 known-good-x: exit status 255:"+
			" WARNING: Thin pool pve/data is out of data space."))

	if want := "Gateway rollback snapshots failing: disk pool pve/data is full"; event.Message != want {
		t.Fatalf("headline = %q, want %q", event.Message, want)
	}
	actionText := alertField(t, event, snapshotAlertActionKey)
	if !strings.Contains(actionText, "Free space in pve/data") {
		t.Fatalf("action %q does not name pool pve/data", actionText)
	}
}

// TestSnapshotAlertUnknownCauseKeepsTheOriginalError covers every other
// failure: the email stays generic, points at the original error text, and
// keeps it intact.
func TestSnapshotAlertUnknownCauseKeepsTheOriginalError(t *testing.T) {
	rawOutput := "qm snapshot 113 known-good-x: exit status 255: storage is offline"
	event := lastSnapshotAlert(t, errors.New(rawOutput))

	wantBody := "Gateway rollback snapshots failing\n" +
		"\n" +
		"Action: Read the original error text.\n" +
		"Alert_key: 113\n" +
		"Alert_kind: snapshot-failed\n" +
		"Cause: Proxmox refused the snapshot for an unrecognized reason.\n" +
		"Failed attempts: 3\n" +
		"Original error text: " + rawOutput + "\n" +
		"Snapshot: known-good-20260914-231024\n" +
		"Transition: true"
	if got := renderedAlertBody(event); got != wantBody {
		t.Fatalf("rendered email body:\n%s\n\nwant:\n%s", got, wantBody)
	}
}

// TestSnapshotRecoveryAlertReadsPlainly covers the email that closes the
// alert once a snapshot lands again.
func TestSnapshotRecoveryAlertReadsPlainly(t *testing.T) {
	w := snapshotTestWatchdog(t, &mockOps{})
	w.nowFn = func() time.Time { return time.Unix(1_700_000_000, 0) }
	ctx := context.Background()
	for range snapshotFailureAlertThreshold {
		w.noteSnapshotFailure(ctx, "known-good-x", errors.New("storage is offline"))
	}

	w.noteSnapshotSuccess(ctx)

	events := fakeNotifierFrom(t, w).snapshot()
	recovery := events[len(events)-1]
	if !recovery.Resolved {
		t.Fatalf("last event is not a recovery: %+v", recovery)
	}
	wantBody := "RECOVERED: Gateway rollback snapshots working again\n" +
		"\n" +
		"Alert_key: 113\n" +
		"Alert_kind: snapshot-failed\n" +
		"Resolved: true"
	if got := renderedAlertBody(recovery); got != wantBody {
		t.Fatalf("rendered recovery body:\n%s\n\nwant:\n%s", got, wantBody)
	}
}

// TestStaleGuestLockIsClearedOnlyWhenNoTaskRuns covers the guard on the one
// step that can damage a live operation.
func TestStaleGuestLockIsClearedOnlyWhenNoTaskRuns(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		lock        string
		taskRunning bool
		taskErr     error
		wantUnlock  int
	}{
		{
			name: "no lock leaves the guest alone", lock: "",
			taskRunning: false, taskErr: nil, wantUnlock: 0,
		},
		{
			name: "a stale snapshot-delete lock is cleared", lock: "snapshot-delete",
			taskRunning: false, taskErr: nil, wantUnlock: 1,
		},
		{
			name: "a lock with a running task is left in place", lock: "snapshot-delete",
			taskRunning: true, taskErr: nil, wantUnlock: 0,
		},
		{
			name: "a lock the watchdog does not own is left in place", lock: "backup",
			taskRunning: false, taskErr: nil, wantUnlock: 0,
		},
		{
			name: "an unreadable task list leaves the lock in place", lock: "snapshot",
			taskRunning: false, taskErr: errors.New("pvesh unavailable"), wantUnlock: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockOps{
				guestLock:      tc.lock,
				taskRunning:    tc.taskRunning,
				taskRunningErr: tc.taskErr,
			}
			w := newTestWatchdog(t, mock)

			w.clearStaleGuestLock(context.Background(), "test")

			if mock.unlockCalls != tc.wantUnlock {
				t.Fatalf("unlockCalls = %d, want %d", mock.unlockCalls, tc.wantUnlock)
			}
		})
	}
}

// TestForcedDeleteRequiresTheStorageReason covers the escalation guard: a
// forced delete removes the snapshot entry regardless of what the storage
// layer holds, so it runs only for the one failure it addresses.
func TestForcedDeleteRequiresTheStorageReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		snapshot   string
		deleteErr  error
		running    bool
		wantForced int
	}{
		{
			name:     "the disk snapshot is already gone",
			snapshot: "known-good-20260809-120000",
			deleteErr: errors.New(
				"qm delsnapshot: exit status 255: " + storageSnapshotMissingMarker,
			),
			running: false, wantForced: 1,
		},
		{
			name:      "any other delete failure is left alone",
			snapshot:  "known-good-20260809-120000",
			deleteErr: errors.New("qm delsnapshot: exit status 255: storage is offline"),
			running:   false, wantForced: 0,
		},
		{
			name:     "a deploy snapshot is never forced",
			snapshot: "pre-deploy-20260809T120000",
			deleteErr: errors.New(
				"qm delsnapshot: exit status 255: " + storageSnapshotMissingMarker,
			),
			running: false, wantForced: 0,
		},
		{
			name:     "a running task blocks the escalation",
			snapshot: "known-good-20260809-120000",
			deleteErr: errors.New(
				"qm delsnapshot: exit status 255: " + storageSnapshotMissingMarker,
			),
			running: true, wantForced: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockOps{
				delSnapshotErr: tc.deleteErr,
				taskRunning:    tc.running,
			}
			w := newTestWatchdog(t, mock)

			err := w.deleteSnapshot(context.Background(), tc.snapshot)

			if len(mock.forceDelSnapshotCalls) != tc.wantForced {
				t.Fatalf("forced deletes = %d, want %d",
					len(mock.forceDelSnapshotCalls), tc.wantForced)
			}
			if tc.wantForced == 0 && err == nil {
				t.Fatal("a delete that was not escalated must report its failure")
			}
			if tc.wantForced > 0 && err != nil {
				t.Fatalf("a successful escalation must not report an error: %v", err)
			}
		})
	}
}

// TestForcedDeletesStopAfterTheLimit covers the cap that hands a guest to an
// operator rather than forcing it repeatedly.
func TestForcedDeletesStopAfterTheLimit(t *testing.T) {
	t.Parallel()

	mock := &mockOps{
		delSnapshotErr: errors.New(
			"qm delsnapshot: exit status 255: " + storageSnapshotMissingMarker,
		),
		// Every forced delete fails, which is the case that must still
		// consume the limit rather than retrying without bound.
		forceDelSnapshotErr: errors.New("qm delsnapshot --force: exit status 255"),
	}
	w := newTestWatchdog(t, mock)
	ctx := context.Background()

	for i := range maxConsecutiveForcedDeletes {
		if err := w.deleteSnapshot(ctx, "known-good-20260809-120000"); err == nil {
			t.Fatalf("attempt %d: a failed forced delete must report its failure", i+1)
		}
	}
	if len(mock.forceDelSnapshotCalls) != maxConsecutiveForcedDeletes {
		t.Fatalf("forced deletes = %d, want %d",
			len(mock.forceDelSnapshotCalls), maxConsecutiveForcedDeletes)
	}

	if err := w.deleteSnapshot(ctx, "known-good-20260809-120000"); err == nil {
		t.Fatal("past the limit the delete must report its failure")
	}
	if len(mock.forceDelSnapshotCalls) != maxConsecutiveForcedDeletes {
		t.Fatalf("forced deletes = %d, want the limit to stop further attempts at %d",
			len(mock.forceDelSnapshotCalls), maxConsecutiveForcedDeletes)
	}
}

// TestPruneContinuesPastOneFailingSnapshot covers the rotation: one entry
// that refuses to go must not hold back the others in the same pass.
func TestPruneContinuesPastOneFailingSnapshot(t *testing.T) {
	mock := &mockOps{
		snapshotsOut: []byte(
			"known-good-20260809-100000\n" +
				"known-good-20260809-110000\n" +
				"known-good-20260809-120000\n" +
				"known-good-20260809-130000\n",
		),
		delSnapshotErr: errors.New("qm delsnapshot: exit status 255: storage is offline"),
	}
	w := newTestWatchdog(t, mock, func(cfg *config.Config) {
		cfg.Watchdog.MaxKnownGoodSnapshots = 1
		cfg.Watchdog.MaxTotalSnapshots = 0
	})

	if err := w.pruneSnapshots(context.Background()); err == nil {
		t.Fatal("prune must report the failure it saw")
	}
	if len(mock.delSnapshotCalls) != 3 {
		t.Fatalf("delete attempts = %d, want 3: the pass must continue past"+
			" the first failure", len(mock.delSnapshotCalls))
	}
}

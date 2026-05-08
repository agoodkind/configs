package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrepareCapturesAllArtefacts verifies the happy path: every
// artefact file lands under <state-dir>/<vmid>/<deploy-id>/ with the
// expected content shape.
func TestPrepareCapturesAllArtefacts(t *testing.T) {
	t.Parallel()
	deps, _, _, _, _ := newDeps(t)
	opts := newOpts(t, "101")

	st, err := Prepare(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	deployDir := filepath.Join(opts.StateDir, "101", st.DeployID)

	xml, err := os.ReadFile(filepath.Join(deployDir, ArtefactConfigXMLPre)) //nolint:gosec
	if err != nil {
		t.Fatalf("read config.xml.pre: %v", err)
	}
	if string(xml) != "<config/>" {
		t.Fatalf("config.xml.pre = %q, want %q", string(xml), "<config/>")
	}

	shaBytes, err := os.ReadFile(filepath.Join(deployDir, ArtefactConfigXMLSHA256)) //nolint:gosec
	if err != nil {
		t.Fatalf("read config.xml.pre.sha256: %v", err)
	}
	digest := sha256.Sum256(xml)
	wantSha := hex.EncodeToString(digest[:]) + "  " + ArtefactConfigXMLPre
	if !strings.Contains(string(shaBytes), wantSha) {
		t.Fatalf("sha sidecar = %q, want substring %q", string(shaBytes), wantSha)
	}

	ver, err := os.ReadFile(filepath.Join(deployDir, ArtefactVersion)) //nolint:gosec
	if err != nil {
		t.Fatalf("read version.txt: %v", err)
	}
	if !strings.Contains(string(ver), "OPNsense") {
		t.Fatalf("version.txt = %q, want OPNsense substring", string(ver))
	}

	ifaceBytes, err := os.ReadFile(filepath.Join(deployDir, ArtefactInterfaces)) //nolint:gosec
	if err != nil {
		t.Fatalf("read interfaces.json: %v", err)
	}
	var art interfacesArtefact
	if err := json.Unmarshal(ifaceBytes, &art); err != nil {
		t.Fatalf("interfaces.json parse: %v", err)
	}
	if art.IfconfigAV == "" {
		t.Fatalf("interfaces.json IfconfigAV empty")
	}
	if art.NetstatV4 == "" || art.NetstatV6 == "" {
		t.Fatalf("interfaces.json netstat sections empty: %+v", art)
	}

	bgp, err := os.ReadFile(filepath.Join(deployDir, ArtefactBGPStatus)) //nolint:gosec
	if err != nil {
		t.Fatalf("read bgp_status.json: %v", err)
	}
	if len(bgp) == 0 {
		t.Fatalf("bgp_status.json empty")
	}
}

// TestPrepareBGPCaptureFailureWritesEmptyFile checks that a guest with
// no BGP daemon does not abort Prepare. The artefact file is still
// written so the deploy-dir shape is uniform.
func TestPrepareBGPCaptureFailureWritesEmptyFile(t *testing.T) {
	t.Parallel()
	deps, _, _, x, _ := newDeps(t)
	x.byCommand["vtysh"] = GuestExecResult{ExitCode: 1, Stderr: "vtysh: command not found"}
	opts := newOpts(t, "101")

	st, err := Prepare(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if st.Phase != PhasePrepared {
		t.Fatalf("phase = %q, want prepared", st.Phase)
	}
	bgpPath := filepath.Join(opts.StateDir, "101", st.DeployID, ArtefactBGPStatus)
	body, err := os.ReadFile(bgpPath) //nolint:gosec
	if err != nil {
		t.Fatalf("read bgp_status.json: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("bgp_status.json = %q, want empty", string(body))
	}
}

// TestPrepareBGPCaptureExecErrorWritesEmptyFile checks the same guard
// for transport-level errors (not just non-zero exit). A QGA agent that
// cannot reach the guest at all still must not abort Prepare for this
// best-effort artefact.
func TestPrepareBGPCaptureExecErrorWritesEmptyFile(t *testing.T) {
	t.Parallel()
	deps, _, _, x, _ := newDeps(t)
	if x.errByCommand == nil {
		x.errByCommand = map[string]error{}
	}
	x.errByCommand["vtysh"] = errors.New("transport closed")
	x.byCommand["vtysh"] = GuestExecResult{}
	opts := newOpts(t, "101")

	st, err := Prepare(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if st.Phase != PhasePrepared {
		t.Fatalf("phase = %q, want prepared", st.Phase)
	}
	bgpPath := filepath.Join(opts.StateDir, "101", st.DeployID, ArtefactBGPStatus)
	if _, statErr := os.Stat(bgpPath); statErr != nil {
		t.Fatalf("bgp_status.json missing: %v", statErr)
	}
}

// TestPrepareConfigXMLCaptureFailureAbortsPrepare checks the inverse:
// when the required config.xml capture fails, Prepare returns an error
// and never takes the snapshot.
func TestPrepareConfigXMLCaptureFailureAbortsPrepare(t *testing.T) {
	t.Parallel()
	deps, _, s, x, _ := newDeps(t)
	x.byCommand["cat"] = GuestExecResult{ExitCode: 1, Stderr: "cat: /conf/config.xml: No such file"}
	opts := newOpts(t, "101")

	st, err := Prepare(context.Background(), deps, opts)
	if err == nil {
		t.Fatalf("expected error from config.xml capture failure")
	}
	if st.Phase == PhasePrepared {
		t.Fatalf("phase = %q must not be prepared after capture failure", st.Phase)
	}
	if len(s.snapshots) != 0 {
		t.Fatalf("snapshot must not be taken after capture failure, got %d", len(s.snapshots))
	}
}

// TestPrepareConfigXMLExecErrorAbortsPrepare covers the transport
// error case for the required artefact, matching the non-zero exit
// case above.
func TestPrepareConfigXMLExecErrorAbortsPrepare(t *testing.T) {
	t.Parallel()
	deps, _, s, x, _ := newDeps(t)
	if x.errByCommand == nil {
		x.errByCommand = map[string]error{}
	}
	x.errByCommand["cat"] = errors.New("transport closed")
	x.byCommand["cat"] = GuestExecResult{}
	opts := newOpts(t, "101")

	_, err := Prepare(context.Background(), deps, opts)
	if err == nil {
		t.Fatalf("expected error from config.xml exec error")
	}
	if len(s.snapshots) != 0 {
		t.Fatalf("snapshot must not be taken after capture exec error, got %d", len(s.snapshots))
	}
}

// TestPrepareInterfacesPartialFailureLandsErrFields exercises the
// per-section *_err fields: a netstat invocation that fails records
// the failure in the JSON without aborting the rest of the capture.
func TestPrepareInterfacesPartialFailureLandsErrFields(t *testing.T) {
	t.Parallel()
	deps, _, _, x, _ := newDeps(t)
	x.byCommand["netstat"] = GuestExecResult{ExitCode: 2, Stderr: "netstat: bad family"}
	opts := newOpts(t, "101")

	st, err := Prepare(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(opts.StateDir, "101", st.DeployID, ArtefactInterfaces)) //nolint:gosec
	if err != nil {
		t.Fatalf("read interfaces.json: %v", err)
	}
	var art interfacesArtefact
	if err := json.Unmarshal(body, &art); err != nil {
		t.Fatalf("parse interfaces.json: %v", err)
	}
	if art.IfconfigAV == "" {
		t.Fatalf("ifconfig section should still be captured: %+v", art)
	}
	if art.NetstatV4Err == "" || art.NetstatV6Err == "" {
		t.Fatalf("expected netstat err fields populated: %+v", art)
	}
}

// TestPrepareVersionCaptureFailureWritesEmptyFile mirrors the BGP
// case for version.txt.
func TestPrepareVersionCaptureFailureWritesEmptyFile(t *testing.T) {
	t.Parallel()
	deps, _, _, x, _ := newDeps(t)
	x.byCommand["opnsense-version"] = GuestExecResult{ExitCode: 127, Stderr: "command not found"}
	opts := newOpts(t, "101")

	st, err := Prepare(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(opts.StateDir, "101", st.DeployID, ArtefactVersion)) //nolint:gosec
	if err != nil {
		t.Fatalf("read version.txt: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("version.txt = %q, want empty", string(body))
	}
}

// TestCaptureConfigXMLNilExecReturnsError pins the contract that the
// required artefact capture without an Executor returns the documented
// sentinel.
func TestCaptureConfigXMLNilExecReturnsError(t *testing.T) {
	t.Parallel()
	deps := Deps{Snap: nil, Exec: nil, Validate: nil, Notifier: nil, Clock: nil, Log: nil}
	opts := Options{
		VMID: "101", Target: "26.7", StateDir: t.TempDir(),
		DeployID: "abc", Snapshot: "", AcceptPartial: false,
		DryRunExecute: false, UseBootEnvironment: false, KeepSnapshot: false,
		OlderThan: 0, UpgradeTimeout: 0, PostRollbackTimeout: 0,
	}
	dir := t.TempDir()
	if err := captureConfigXML(context.Background(), deps, opts, dir); !errors.Is(err, errCaptureExecMissing) {
		t.Fatalf("err = %v, want errCaptureExecMissing", err)
	}
}

// TestCaptureBGPStatusNilExecWritesEmptyFile pins that the best-effort
// captures degrade gracefully when no Executor is available.
func TestCaptureBGPStatusNilExecWritesEmptyFile(t *testing.T) {
	t.Parallel()
	deps := Deps{Snap: nil, Exec: nil, Validate: nil, Notifier: nil, Clock: nil, Log: nil}
	opts := Options{
		VMID: "101", Target: "26.7", StateDir: t.TempDir(),
		DeployID: "abc", Snapshot: "", AcceptPartial: false,
		DryRunExecute: false, UseBootEnvironment: false, KeepSnapshot: false,
		OlderThan: 0, UpgradeTimeout: 0, PostRollbackTimeout: 0,
	}
	dir := t.TempDir()
	captureBGPStatus(context.Background(), deps, opts, dir)
	body, err := os.ReadFile(filepath.Join(dir, ArtefactBGPStatus)) //nolint:gosec
	if err != nil {
		t.Fatalf("read bgp_status.json: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("bgp_status.json = %q, want empty", string(body))
	}
}

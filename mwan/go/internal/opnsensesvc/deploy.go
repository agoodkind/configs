package opnsensesvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Deploy paths and constants. See mwan/MWAN-95-SELFDEPLOY-DESIGN.md.
const (
	DefaultBinaryDir  = "/usr/local/sbin"
	BinaryName        = "mwan-opnsense"
	BinarySymlink     = "mwan-opnsense"
	BinaryCurrent     = "mwan-opnsense.current"
	BinaryPrevious    = "mwan-opnsense.previous"
	BinaryStaged      = "mwan-opnsense.staged"
	BinaryStagedTmp   = "mwan-opnsense.staged.tmp"
	DeployStatePath   = "/var/run/mwan-opnsense.deploy.state"
	PendingVerifyPath = "/var/run/mwan-opnsense.pending-verify"
	StateFileMode     = 0o644
	BinaryFileMode    = 0o755
	StagedTmpFileMode = 0o600
	PendingFileMode   = 0o644
	ReExecGraceMillis = 500
	HealthOK          = "ok"
	HealthPending     = "pending"
	MarkerFreshDeploy = "fresh-deploy"
)

// DeployConfig parameterizes filesystem locations for the deploy
// machinery. Defaults are used when fields are zero-valued. Tests
// override these with tmpdirs.
type DeployConfig struct {
	BinaryDir      string // dir holding .current, .previous, .staged
	StatePath      string // colon-delim flag file
	PendingPath    string // marker dropped before re-exec
	ReExecGrace    time.Duration
	ReExecFn       func(argv0 string, argv []string, envv []string) error // override for tests
	WriteStateFile func(path string, content []byte, mode os.FileMode) error
	NowFn          func() time.Time
}

// DeployManager owns the binary swap + state-file lifecycle on the
// daemon side. It is safe for concurrent calls; serial.go ensures
// only one Deploy/Revert is in flight at a time via the server mutex,
// but DeployManager also takes its own mutex defensively.
type DeployManager struct {
	cfg DeployConfig
	log *slog.Logger
	mu  sync.Mutex
}

// NewDeployManager constructs a DeployManager. Empty fields in cfg
// fall back to package defaults.
func NewDeployManager(log *slog.Logger, cfg DeployConfig) *DeployManager {
	if cfg.BinaryDir == "" {
		cfg.BinaryDir = DefaultBinaryDir
	}
	if cfg.StatePath == "" {
		cfg.StatePath = DeployStatePath
	}
	if cfg.PendingPath == "" {
		cfg.PendingPath = PendingVerifyPath
	}
	if cfg.ReExecGrace == 0 {
		cfg.ReExecGrace = ReExecGraceMillis * time.Millisecond
	}
	if cfg.ReExecFn == nil {
		cfg.ReExecFn = syscall.Exec
	}
	if cfg.WriteStateFile == nil {
		cfg.WriteStateFile = atomicWriteFile
	}
	if cfg.NowFn == nil {
		cfg.NowFn = time.Now
	}
	if log == nil {
		log = slog.Default()
	}
	return &DeployManager{cfg: cfg, log: log, mu: sync.Mutex{}}
}

// pathOf returns the absolute path for a binary slot.
func (d *DeployManager) pathOf(name string) string {
	return filepath.Join(d.cfg.BinaryDir, name)
}

// CurrentSHA256 returns the hex-encoded sha256 of .current. Returns
// empty string and nil error if the file doesn't exist (cold boot).
func (d *DeployManager) CurrentSHA256() (string, error) {
	return fileSHA256(d.pathOf(BinaryCurrent))
}

// PreviousSHA256 returns the hex-encoded sha256 of .previous. Returns
// empty string and nil error if the file doesn't exist (no prior deploy).
func (d *DeployManager) PreviousSHA256() (string, error) {
	return fileSHA256(d.pathOf(BinaryPrevious))
}

// Deploy stages binary, verifies sha256, swaps .current and .previous,
// drops pending-verify marker, and re-execs. Returns previousPath and
// stagedSHA256. If re-exec fails, the function returns the error
// (deploy unwound to best-effort consistency).
func (d *DeployManager) Deploy(ctx context.Context, binary []byte, sha256Hex, versionStr string) (previousPath string, stagedSHA256 string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.log.Info("deploy: begin",
		"bytes", len(binary),
		"sha256_hex", sha256Hex,
		"version", versionStr)

	if len(binary) == 0 {
		err = errors.New("deploy: empty binary payload")
		d.log.Error("deploy: invalid", "err", err)
		return "", "", err
	}
	if sha256Hex == "" {
		err = errors.New("deploy: sha256_hex required")
		d.log.Error("deploy: invalid", "err", err)
		return "", "", err
	}

	// Verify sha256 before any filesystem mutation.
	computed := sha256.Sum256(binary)
	computedHex := hex.EncodeToString(computed[:])
	if !strings.EqualFold(computedHex, sha256Hex) {
		err = fmt.Errorf("deploy: sha256 mismatch: got %s, want %s", computedHex, sha256Hex)
		d.log.Error("deploy: sha256 mismatch", "err", err, "got", computedHex, "want", sha256Hex)
		return "", "", err
	}
	stagedSHA256 = computedHex

	// Stage the new binary.
	stagedTmp := d.pathOf(BinaryStagedTmp)
	staged := d.pathOf(BinaryStaged)
	current := d.pathOf(BinaryCurrent)
	previous := d.pathOf(BinaryPrevious)
	previousPath = previous

	if err := writeBinaryAtomic(stagedTmp, binary); err != nil {
		d.log.Error("deploy: stage write failed", "err", err, "path", stagedTmp)
		return "", "", fmt.Errorf("stage write %s: %w", stagedTmp, err)
	}
	if err := os.Rename(stagedTmp, staged); err != nil {
		d.log.Error("deploy: stage rename failed", "err", err, "from", stagedTmp, "to", staged)
		_ = os.Remove(stagedTmp)
		return "", "", fmt.Errorf("stage rename: %w", err)
	}

	// Move current to previous (best effort: tolerate missing current
	// on cold-boot first-deploy by skipping).
	if _, statErr := os.Stat(current); statErr == nil {
		if copyErr := copyFile(current, previous); copyErr != nil {
			d.log.Error("deploy: copy current to previous failed", "err", copyErr)
			_ = os.Remove(staged)
			return "", "", fmt.Errorf("copy current to previous: %w", copyErr)
		}
		d.log.Info("deploy: previous slot updated", "path", previous)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		d.log.Error("deploy: stat current failed", "err", statErr, "path", current)
		_ = os.Remove(staged)
		return "", "", fmt.Errorf("stat current: %w", statErr)
	}

	// Atomic swap: staged becomes current.
	if err := os.Rename(staged, current); err != nil {
		d.log.Error("deploy: current swap failed", "err", err, "from", staged, "to", current)
		_ = os.Remove(staged)
		return "", "", fmt.Errorf("rename staged to current: %w", err)
	}
	d.log.Info("deploy: current binary swapped", "path", current, "sha256", computedHex)

	// Update state file: health=pending, deployed_at=now.
	previousSHA, _ := fileSHA256(previous)
	if writeErr := d.writeState(deployState{
		Active:     computedHex,
		Previous:   previousSHA,
		Version:    versionStr,
		DeployedAt: d.cfg.NowFn().Unix(),
		Health:     HealthPending,
	}); writeErr != nil {
		d.log.Error("deploy: write state failed", "err", writeErr)
		// not fatal; preflight script tolerates missing state
	}

	// Drop pending-verify marker. Preflight script reads this on
	// next start to know whether to revert if health stayed pending.
	if writeErr := d.cfg.WriteStateFile(d.cfg.PendingPath, []byte(MarkerFreshDeploy+"\n"), PendingFileMode); writeErr != nil {
		d.log.Error("deploy: write pending marker failed", "err", writeErr)
		// not fatal; rollback is degraded but deploy can proceed
	}

	d.log.Info("deploy: armed re-exec", "grace_ms", d.cfg.ReExecGrace.Milliseconds())

	// Trigger re-exec asynchronously so the gRPC reply can land first.
	go d.scheduleReExec(ctx)

	return previousPath, stagedSHA256, nil
}

// MarkHealthy clears the pending-verify marker and stamps health=ok.
// Called from the DeployStatus RPC handler on MARK_HEALTHY.
func (d *DeployManager) MarkHealthy(_ context.Context) (active, previous string, deployedAt int64, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.log.Info("deploy: mark healthy")

	state, err := d.readStateLocked()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		d.log.Error("deploy: read state failed", "err", err)
		return "", "", 0, fmt.Errorf("read state: %w", err)
	}
	state.Health = HealthOK
	if state.Active == "" {
		// State file missing or stale: derive from disk.
		current, _ := d.CurrentSHA256()
		state.Active = current
	}
	if state.Previous == "" {
		prev, _ := d.PreviousSHA256()
		state.Previous = prev
	}
	if state.DeployedAt == 0 {
		state.DeployedAt = d.cfg.NowFn().Unix()
	}

	if err := d.writeState(state); err != nil {
		d.log.Error("deploy: write state failed", "err", err)
		return "", "", 0, fmt.Errorf("write state: %w", err)
	}
	if err := os.Remove(d.cfg.PendingPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		d.log.Error("deploy: remove pending marker failed", "err", err, "path", d.cfg.PendingPath)
		return "", "", 0, fmt.Errorf("remove pending marker: %w", err)
	}
	d.log.Info("deploy: healthy", "active", state.Active, "previous", state.Previous)
	return state.Active, state.Previous, state.DeployedAt, nil
}

// Status returns the current deploy state for the DeployStatus RPC.
// Returns derived state if the state file is absent.
func (d *DeployManager) Status(_ context.Context) (active, previous, health string, deployedAt int64, err error) {
	state, readErr := d.readStateLocked()
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		d.log.Error("deploy: status read state failed", "err", readErr)
		return "", "", "", 0, fmt.Errorf("read state: %w", readErr)
	}
	// Always reconcile against on-disk reality.
	current, _ := d.CurrentSHA256()
	prev, _ := d.PreviousSHA256()
	if state.Active == "" {
		state.Active = current
	}
	if state.Previous == "" {
		state.Previous = prev
	}
	if state.Health == "" {
		state.Health = HealthPending
	}
	return state.Active, state.Previous, state.Health, state.DeployedAt, nil
}

// Revert copies .previous over .current, drops pending-verify marker,
// and re-execs. Errors if .previous is absent.
func (d *DeployManager) Revert(ctx context.Context) (revertedTo string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.log.Info("revert: begin")

	current := d.pathOf(BinaryCurrent)
	previous := d.pathOf(BinaryPrevious)

	if _, statErr := os.Stat(previous); statErr != nil {
		d.log.Error("revert: previous absent", "err", statErr, "path", previous)
		return "", fmt.Errorf("previous absent: %w", statErr)
	}

	revertedTo, sumErr := fileSHA256(previous)
	if sumErr != nil {
		d.log.Error("revert: sha256 failed", "err", sumErr, "path", previous)
		return "", fmt.Errorf("sha256 previous: %w", sumErr)
	}

	if copyErr := copyFile(previous, current); copyErr != nil {
		d.log.Error("revert: copy failed", "err", copyErr)
		return "", fmt.Errorf("copy previous to current: %w", copyErr)
	}

	if err := d.writeState(deployState{
		Active:     revertedTo,
		Previous:   revertedTo, // after revert, both slots match
		Version:    "reverted",
		DeployedAt: d.cfg.NowFn().Unix(),
		Health:     HealthPending,
	}); err != nil {
		d.log.Error("revert: write state failed", "err", err)
		// not fatal
	}

	if writeErr := d.cfg.WriteStateFile(d.cfg.PendingPath, []byte(MarkerFreshDeploy+"\n"), PendingFileMode); writeErr != nil {
		d.log.Error("revert: write pending marker failed", "err", writeErr)
	}

	d.log.Info("revert: armed re-exec", "reverted_to_sha256", revertedTo)
	go d.scheduleReExec(ctx)

	return revertedTo, nil
}

// scheduleReExec waits ReExecGrace then re-execs the running process.
// Called as a goroutine so the originating RPC can reply before exec
// replaces the process image.
func (d *DeployManager) scheduleReExec(ctx context.Context) {
	timer := time.NewTimer(d.cfg.ReExecGrace)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		d.log.Warn("re-exec: context cancelled, exec anyway")
	}
	executable, err := os.Executable()
	if err != nil {
		d.log.Error("re-exec: resolve executable failed", "err", err)
		return
	}
	d.log.Warn("re-exec: invoking syscall.Exec", "argv0", executable, "argc", len(os.Args))
	if execErr := d.cfg.ReExecFn(executable, os.Args, os.Environ()); execErr != nil {
		d.log.Error("re-exec: failed", "err", execErr)
	}
}

// deployState is the structured form of the state file.
type deployState struct {
	Active     string
	Previous   string
	Version    string
	DeployedAt int64
	Health     string
}

// writeState atomically rewrites the state file.
func (d *DeployManager) writeState(s deployState) error {
	content := encodeStateFile(s)
	return d.cfg.WriteStateFile(d.cfg.StatePath, []byte(content), StateFileMode)
}

// readStateLocked reads and parses the state file. Caller must hold mu.
func (d *DeployManager) readStateLocked() (deployState, error) {
	data, err := os.ReadFile(d.cfg.StatePath)
	if err != nil {
		return deployState{}, fmt.Errorf("read state file %s: %w", d.cfg.StatePath, err)
	}
	return parseStateFile(string(data)), nil
}

// encodeStateFile renders deployState as colon-delimited key:value
// lines in stable order.
func encodeStateFile(s deployState) string {
	var b strings.Builder
	b.WriteString("active:")
	b.WriteString(s.Active)
	b.WriteString("\n")
	b.WriteString("previous:")
	b.WriteString(s.Previous)
	b.WriteString("\n")
	b.WriteString("version:")
	b.WriteString(s.Version)
	b.WriteString("\n")
	b.WriteString("deployed_at:")
	b.WriteString(strconv.FormatInt(s.DeployedAt, 10))
	b.WriteString("\n")
	b.WriteString("health:")
	b.WriteString(s.Health)
	b.WriteString("\n")
	return b.String()
}

// parseStateFile parses colon-delimited key:value lines. Unknown
// keys are ignored. Missing keys yield zero values.
func parseStateFile(content string) deployState {
	var s deployState
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx <= 0 {
			continue
		}
		key := line[:idx]
		val := line[idx+1:]
		switch key {
		case "active":
			s.Active = val
		case "previous":
			s.Previous = val
		case "version":
			s.Version = val
		case "deployed_at":
			ts, err := strconv.ParseInt(val, 10, 64)
			if err == nil {
				s.DeployedAt = ts
			}
		case "health":
			s.Health = val
		}
	}
	return s
}

// atomicWriteFile writes content to a tmpfile then renames into place.
// Default backing for DeployConfig.WriteStateFile.
func atomicWriteFile(path string, content []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("open %s: %w", tmp, err)
	}
	if _, writeErr := file.Write(content); writeErr != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, writeErr)
	}
	if syncErr := file.Sync(); syncErr != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync %s: %w", tmp, syncErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, closeErr)
	}
	if renameErr := os.Rename(tmp, path); renameErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s into %s: %w", tmp, path, renameErr)
	}
	return nil
}

// writeBinaryAtomic writes the binary content to a tmp path with
// executable permissions and fsyncs before returning.
func writeBinaryAtomic(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, BinaryFileMode)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if _, writeErr := file.Write(content); writeErr != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write %s: %w", path, writeErr)
	}
	if syncErr := file.Sync(); syncErr != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("sync %s: %w", path, syncErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	if chmodErr := os.Chmod(path, BinaryFileMode); chmodErr != nil {
		return fmt.Errorf("chmod %s: %w", path, chmodErr)
	}
	return nil
}

// copyFile copies src to dst, preserving executable mode.
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = source.Close() }()

	tmp := dst + ".tmp"
	target, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, BinaryFileMode)
	if err != nil {
		return fmt.Errorf("open %s: %w", tmp, err)
	}
	if _, copyErr := io.Copy(target, source); copyErr != nil {
		_ = target.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copy: %w", copyErr)
	}
	if syncErr := target.Sync(); syncErr != nil {
		_ = target.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync %s: %w", tmp, syncErr)
	}
	if closeErr := target.Close(); closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, closeErr)
	}
	if renameErr := os.Rename(tmp, dst); renameErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s into %s: %w", tmp, dst, renameErr)
	}
	return nil
}

// fileSHA256 returns the hex-encoded sha256 of a file. Empty string
// + nil error if path doesn't exist.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, copyErr := io.Copy(hasher, file); copyErr != nil {
		return "", fmt.Errorf("hash %s: %w", path, copyErr)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

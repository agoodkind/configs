package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	mwanv1 "github.com/agoodkind/infra-tools/gen/mwan/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var pingReceivedRE = regexp.MustCompile(`(\d+)\s+received`)

type agentServer struct {
	mwanv1.UnimplementedMWANAgentServer
	deployFilePath string
	log            *slog.Logger

	// These are injectable in tests to avoid reading real /proc files.
	// Production code leaves them nil and uses the real /proc paths.
	uptimePath  string
	loadAvgPath string
	meminfoPath string
	statfsPath  string
}

func newAgentServer(deployFilePath string, log *slog.Logger) *agentServer {
	return &agentServer{
		deployFilePath: deployFilePath,
		log:            log,
		uptimePath:     "/proc/uptime",
		loadAvgPath:    "/proc/loadavg",
		meminfoPath:    "/proc/meminfo",
		statfsPath:     "/",
	}
}

func (a *agentServer) GetHealth(
	ctx context.Context,
	_ *mwanv1.GetHealthRequest,
) (*mwanv1.GetHealthResponse, error) {
	resp := &mwanv1.GetHealthResponse{}

	ipv4OK := a.pingExitZero(ctx, "ping", "-c", "1", "-W", "2", "1.1.1.1")
	resp.Ipv4Ok = ipv4OK

	ipv6OK := a.pingExitZero(ctx, "ping6", "-c", "1", "-W", "2", "2606:4700:4700::1111")
	resp.Ipv6Ok = ipv6OK

	ifaces, err := net.Interfaces()
	if err != nil {
		a.log.ErrorContext(ctx, "list network interfaces", "error", err)
	} else {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			st := &mwanv1.WANStatus{Name: iface.Name}
			st.LinkUp = iface.Flags&net.FlagUp != 0
			if st.LinkUp {
				st.Ipv4Reachable = a.pingExitZero(ctx, "ping", "-I", iface.Name,
					"-c", "1", "-W", "2", "1.1.1.1")
				st.Ipv6Reachable = a.pingExitZero(ctx, "ping6", "-I", iface.Name,
					"-c", "1", "-W", "2", "2606:4700:4700::1111")
			}
			resp.WanInterfaces = append(resp.WanInterfaces, st)
		}
	}

	failed, ferr := a.listFailedSystemdUnits(ctx)
	if ferr != nil {
		a.log.WarnContext(ctx, "list failed systemd units", "error", ferr)
	} else {
		resp.FailedUnits = failed
	}

	return resp, nil
}

func (a *agentServer) pingExitZero(ctx context.Context, name string, args ...string) bool {
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, name, args...)
	err := cmd.Run()
	return err == nil
}

func (a *agentServer) listFailedSystemdUnits(ctx context.Context) ([]string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "systemctl", "list-units",
		"--state=failed", "--no-legend", "--no-pager")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "0 loaded") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		unit := strings.TrimPrefix(fields[0], "●")
		unit = strings.TrimSpace(unit)
		if unit == "" || !strings.Contains(unit, ".") {
			continue
		}
		names = append(names, unit)
	}
	if err := sc.Err(); err != nil {
		return names, err
	}
	return names, nil
}

func (a *agentServer) Ping(
	ctx context.Context,
	req *mwanv1.PingRequest,
) (*mwanv1.PingResponse, error) {
	target := strings.TrimSpace(req.GetTarget())
	if target == "" {
		return nil, status.Error(codes.InvalidArgument, "target is required")
	}

	count := req.GetCount()
	if count <= 0 {
		count = 2
	}
	timeoutSec := req.GetTimeoutSeconds()
	if timeoutSec <= 0 {
		timeoutSec = 3
	}

	bin := "ping"
	if strings.Contains(target, ":") {
		bin = "ping6"
	}

	args := []string{
		"-c", strconv.FormatInt(int64(count), 10),
		"-W", strconv.FormatInt(int64(timeoutSec), 10),
	}
	if bind := strings.TrimSpace(req.GetBindInterface()); bind != "" {
		args = append(args, "-I", bind)
	}
	args = append(args, target)

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, bin, args...)
	out, err := cmd.Output()
	stdout := string(out)
	packets := parsePingReceivedCount(stdout)

	resp := &mwanv1.PingResponse{
		PacketsReceived: packets,
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			resp.Success = exitErr.ExitCode() == 0
			return resp, nil
		}
		a.log.ErrorContext(ctx, "ping exec", "binary", bin, "error", err)
		return nil, status.Errorf(codes.Internal, "ping: %v", err)
	}
	resp.Success = true
	return resp, nil
}

func parsePingReceivedCount(stdout string) int32 {
	m := pingReceivedRE.FindStringSubmatch(stdout)
	if len(m) < 2 {
		return 0
	}
	// m[1] is guaranteed to be \d+ by the regex; ParseInt cannot fail here.
	n, _ := strconv.ParseInt(m[1], 10, 32)
	return int32(n)
}

func (a *agentServer) GetConfigState(
	ctx context.Context,
	_ *mwanv1.GetConfigStateRequest,
) (*mwanv1.GetConfigStateResponse, error) {
	path := a.deployFilePath
	raw, err := os.ReadFile(path)
	if err != nil {
		a.log.ErrorContext(ctx, "read deploy file", "path", path, "error", err)
		return nil, status.Errorf(codes.Internal, "read deploy file: %v", err)
	}

	sum := sha256.Sum256(raw)
	hashHex := hex.EncodeToString(sum[:])

	trimmed := strings.TrimSpace(string(raw))
	deployEpoch, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		a.log.ErrorContext(ctx, "parse deploy timestamp", "path", path, "error", err)
		return nil, status.Errorf(codes.Internal, "parse deploy timestamp: %v", err)
	}

	st, err := os.Stat(path)
	if err != nil {
		a.log.ErrorContext(ctx, "stat deploy file", "path", path, "error", err)
		return nil, status.Errorf(codes.Internal, "stat deploy file: %v", err)
	}
	mtime := st.ModTime().Unix()

	return &mwanv1.GetConfigStateResponse{
		ConfigHash:      hashHex,
		LastDeployEpoch: deployEpoch,
		LastChangeEpoch: mtime,
	}, nil
}

func (a *agentServer) GetSystemInfo(
	ctx context.Context,
	_ *mwanv1.GetSystemInfoRequest,
) (*mwanv1.GetSystemInfoResponse, error) {
	host, err := os.Hostname()
	if err != nil {
		a.log.ErrorContext(ctx, "hostname", "error", err)
		return nil, status.Errorf(codes.Internal, "hostname: %v", err)
	}

	uptimeSec, err := readUptimeSecondsFrom(a.uptimePath)
	if err != nil {
		a.log.ErrorContext(ctx, "read uptime", "error", err)
		return nil, status.Errorf(codes.Internal, "uptime: %v", err)
	}

	loadAvg, err := readLoadAverageFrom(a.loadAvgPath)
	if err != nil {
		a.log.ErrorContext(ctx, "read loadavg", "error", err)
		return nil, status.Errorf(codes.Internal, "loadavg: %v", err)
	}

	memTotal, memAvail, err := readMeminfoKBFrom(a.meminfoPath)
	if err != nil {
		a.log.ErrorContext(ctx, "read meminfo", "error", err)
		return nil, status.Errorf(codes.Internal, "meminfo: %v", err)
	}
	memTotalBytes := memTotal * 1024
	memAvailBytes := memAvail * 1024
	memUsedBytes := memTotalBytes - memAvailBytes

	diskTotal, diskAvail, err := statfsDiskBytes(a.statfsPath)
	if err != nil {
		a.log.ErrorContext(ctx, "statfs root", "error", err)
		return nil, status.Errorf(codes.Internal, "disk: %v", err)
	}
	diskUsed := diskTotal - diskAvail

	return &mwanv1.GetSystemInfoResponse{
		Hostname:         host,
		UptimeSeconds:    uptimeSec,
		LoadAverage:      loadAvg,
		MemoryUsedBytes:  memUsedBytes,
		MemoryTotalBytes: memTotalBytes,
		DiskUsedBytes:    diskUsed,
		DiskTotalBytes:   diskTotal,
	}, nil
}

func readUptimeSeconds() (int64, error) {
	return readUptimeSecondsFrom("/proc/uptime")
}

func readUptimeSecondsFrom(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected /proc/uptime format")
	}
	secFloat, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	return int64(secFloat), nil
}

func readLoadAverage() (string, error) {
	return readLoadAverageFrom("/proc/loadavg")
}

func readLoadAverageFrom(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return "", fmt.Errorf("unexpected /proc/loadavg format")
	}
	return strings.Join(fields[:3], " "), nil
}

func readMeminfoKB() (totalKB int64, availKB int64, err error) {
	return readMeminfoKBFrom("/proc/meminfo")
}

func readMeminfoKBFrom(path string) (totalKB int64, availKB int64, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	var gotTotal, gotAvail bool
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			v, ok := parseMeminfoLineKB(line)
			if !ok {
				return 0, 0, fmt.Errorf("parse MemTotal")
			}
			totalKB = v
			gotTotal = true
		}
		if strings.HasPrefix(line, "MemAvailable:") {
			v, ok := parseMeminfoLineKB(line)
			if !ok {
				return 0, 0, fmt.Errorf("parse MemAvailable")
			}
			availKB = v
			gotAvail = true
		}
		if gotTotal && gotAvail {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return 0, 0, err
	}
	if !gotTotal || !gotAvail {
		return 0, 0, fmt.Errorf("meminfo missing MemTotal or MemAvailable")
	}
	return totalKB, availKB, nil
}

func parseMeminfoLineKB(line string) (int64, bool) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return 0, false
	}
	rest := strings.TrimSpace(line[idx+1:])
	fields := strings.Fields(rest)
	if len(fields) < 1 {
		return 0, false
	}
	kb, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, false
	}
	return kb, true
}

func statfsDiskBytes(path string) (total int64, avail int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := int64(st.Bsize)
	total = int64(st.Blocks) * bs
	avail = int64(st.Bavail) * bs
	return total, avail, nil
}

func (a *agentServer) WatchEvents(
	_ *mwanv1.WatchEventsRequest,
	stream mwanv1.MWANAgent_WatchEventsServer,
) error {
	<-stream.Context().Done()
	return stream.Context().Err()
}

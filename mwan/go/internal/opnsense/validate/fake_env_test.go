package validate

import (
	"context"
	"errors"
	"strings"
	"time"
)

// fakeEnv is the test double for Env. It records calls and returns
// scripted responses keyed by command substring (so tests can match
// on prefixes without writing the full vtysh string).
type fakeEnv struct {
	now            time.Time
	opnsenseScript map[string]CommandResult
	proxmoxScript  map[string]CommandResult
	lanScript      map[string]CommandResult
	httpScript     map[string]HTTPResult
	opnsenseCalls  []string
	proxmoxCalls   []string
	lanCalls       []string
	httpCalls      []string
	transportError error
}

func newFakeEnv() *fakeEnv {
	return &fakeEnv{
		now:            time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		opnsenseScript: map[string]CommandResult{},
		proxmoxScript:  map[string]CommandResult{},
		lanScript:      map[string]CommandResult{},
		httpScript:     map[string]HTTPResult{},
	}
}

func (f *fakeEnv) Now() time.Time {
	return f.now
}

func (f *fakeEnv) SSHOPNsense(_ context.Context, command string) (CommandResult, error) {
	f.opnsenseCalls = append(f.opnsenseCalls, command)
	if f.transportError != nil {
		return CommandResult{}, f.transportError
	}
	return scriptedResult(f.opnsenseScript, command), nil
}

func (f *fakeEnv) SSHProxmoxHost(_ context.Context, command string) (CommandResult, error) {
	f.proxmoxCalls = append(f.proxmoxCalls, command)
	if f.transportError != nil {
		return CommandResult{}, f.transportError
	}
	return scriptedResult(f.proxmoxScript, command), nil
}

func (f *fakeEnv) LANClientExec(_ context.Context, command string) (CommandResult, error) {
	f.lanCalls = append(f.lanCalls, command)
	if f.transportError != nil {
		return CommandResult{}, f.transportError
	}
	return scriptedResult(f.lanScript, command), nil
}

func (f *fakeEnv) OPNsenseHTTPSGet(
	_ context.Context,
	path string,
	_ *BasicAuth,
) (HTTPResult, error) {
	f.httpCalls = append(f.httpCalls, path)
	if f.transportError != nil {
		return HTTPResult{}, f.transportError
	}
	for prefix, result := range f.httpScript {
		if strings.Contains(path, prefix) {
			return result, nil
		}
	}
	return HTTPResult{StatusCode: 599}, errors.New("fakeEnv: no scripted HTTP response for " + path)
}

func scriptedResult(script map[string]CommandResult, command string) CommandResult {
	for prefix, result := range script {
		if strings.Contains(command, prefix) {
			return result
		}
	}
	return CommandResult{Stderr: "fakeEnv: no scripted response", ExitCode: 127}
}

package validate

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_AggregatesResultsAndAppliesWhen(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["pfctl -si"] = CommandResult{Stdout: "Enabled\n"}
	env.opnsenseScript["pgrep -f unbound"] = CommandResult{Stdout: "1\n"}
	env.opnsenseScript["pgrep -f mwan-opnsense"] = CommandResult{Stdout: "0\n"}

	cfg := Config{VMID: 101, DeployID: "test"}
	baseline := &Baseline{Plugins: []string{"os-frr"}}
	out, err := Run(context.Background(), cfg, baseline, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Results) == 0 {
		t.Fatal("no results")
	}
	pfHit := false
	taygaCheckSeen := false
	for _, r := range out.Results {
		if r.CheckID == CheckIDPFEnabled && r.Outcome == OutcomePass {
			pfHit = true
		}
		if r.CheckID == "plugin_os_tayga_installed" && r.Outcome == OutcomeSkip {
			taygaCheckSeen = true
		}
	}
	if !pfHit {
		t.Fatal("expected pf_enabled to be present and pass")
	}
	if !taygaCheckSeen {
		t.Fatal("expected os-tayga check to be skipped because baseline lacks the plugin")
	}
}

func TestRun_SeverityFilterRunsOnlyBlockers(t *testing.T) {
	env := newFakeEnv()
	cfg := Config{
		VMID:           1,
		DeployID:       "f",
		SeverityFilter: SeverityBlocker,
	}
	out, err := Run(context.Background(), cfg, nil, env)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range out.Results {
		if r.Severity != SeverityBlocker {
			t.Fatalf("unexpected non-blocker result %s sev=%s", r.CheckID, r.Severity)
		}
	}
}

func TestRun_OutputFormatJSONStable(t *testing.T) {
	env := newFakeEnv()
	cfg := Config{VMID: 1, DeployID: "f", SeverityFilter: SeverityBlocker}
	out, err := Run(context.Background(), cfg, nil, env)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"schema_version":1`) {
		t.Fatalf("expected schema_version field; got %s", string(encoded))
	}
	if !strings.Contains(string(encoded), `"vmid":1`) {
		t.Fatalf("expected vmid field; got %s", string(encoded))
	}
}

func TestSaveAndLoadBaseline_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	pre := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		VMID:          101,
		DeployID:      "abc",
		Results: []Result{
			{CheckID: "x", Outcome: OutcomePass, Severity: SeverityBlocker},
		},
	}
	if err := SaveBaseline(tempDir, 101, "abc", PreBaselineFilename, pre); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tempDir, "101", "abc", PreBaselineFilename)
	loaded, err := LoadBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.VMID != 101 {
		t.Fatalf("vmid=%d", loaded.VMID)
	}
	if len(loaded.Results) != 1 {
		t.Fatalf("results=%d", len(loaded.Results))
	}
}

func TestLoadBaseline_RejectsWrongSchema(t *testing.T) {
	tempDir := t.TempDir()
	wrong := &Baseline{SchemaVersion: 999}
	if err := SaveBaseline(tempDir, 1, "x", PreBaselineFilename, wrong); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tempDir, "1", "x", PreBaselineFilename)
	if _, err := LoadBaseline(path); err == nil {
		t.Fatal("expected schema rejection")
	}
}

func TestArtefactPath_MatchesMWAN152(t *testing.T) {
	got := ArtefactPath("/var/lib/mwan/upgrade", 101, "deploy-1")
	want := "/var/lib/mwan/upgrade/101/deploy-1"
	if got != want {
		t.Fatalf("got=%s want=%s", got, want)
	}
}

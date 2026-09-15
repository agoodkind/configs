package ansible

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// These tests evaluate the OPNsense deploy's restart decision with ansible-core's
// own templar. They read the set_fact tasks and the condition lists from the real
// task file, so a change to any expression in that chain changes what is tested.
// Only the registered results of the tasks that talk to the router and the
// hypervisor are supplied, shaped the way those tasks register them.

const (
	opnsenseDeployTaskRelPath  = "ansible/playbooks/tasks/mwan-opnsense-deploy.yml"
	restartTaskName            = "Restart the daemon onto the installed binary"
	markHealthyTaskName        = "Mark the running daemon binary healthy"
	restartConditionName       = "restart_when"
	markHealthyConditionName   = "mark_healthy_changed_when"
	conditionEvaluatorRelPath  = "testdata/evaluate_task_conditions.py"
	ansiblePlaybookCommandName = "ansible-playbook"
	testReleaseCommit          = "9b12ead3d6525538d098bb80601b373efae6d6d1"
	// The line the testbed router daemon printed at 2026-09-14 23:51 PT, after an
	// earlier install replaced its binary without a restart.
	staleVersionLine = "version=commit=e15d495 dirty=clean binhash=unknown " +
		"commit=e15d495 dirty=false binhash=unknown"
	releaseVersionLine = "version=commit=9b12ead dirty=clean binhash=059926869ac5 " +
		"commit=9b12ead dirty=false binhash=059926869ac5"
)

// conditionList is a when, changed_when, or similar field, which Ansible accepts
// as one expression or a list of them.
type conditionList []string

func (list *conditionList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*list = conditionList{node.Value}
		return nil
	}
	var items []string
	if err := node.Decode(&items); err != nil {
		return err
	}
	*list = items
	return nil
}

// deployTask holds the task fields the restart decision reads.
type deployTask struct {
	Name        string            `yaml:"name"`
	When        conditionList     `yaml:"when"`
	ChangedWhen conditionList     `yaml:"changed_when"`
	Vars        map[string]string `yaml:"vars"`
	SetFact     map[string]string `yaml:"ansible.builtin.set_fact"`
	Block       []deployTask      `yaml:"block"`
}

// factTask is one set_fact task as the evaluator reads it.
type factTask struct {
	When    []string          `json:"when"`
	Vars    map[string]string `json:"vars"`
	SetFact map[string]string `json:"set_fact"`
}

type conditionRequest struct {
	Variables  map[string]any      `json:"variables"`
	Facts      []factTask          `json:"facts"`
	Conditions map[string][]string `json:"conditions"`
}

type conditionResult struct {
	Conditions map[string]bool            `json:"conditions"`
	Facts      map[string]json.RawMessage `json:"facts"`
}

// restartDecision is the part of the task file that decides whether the daemon
// restarts: every set_fact task before the restart block, in file order, the
// restart block's when list, and the mark-healthy task's changed_when list.
type restartDecision struct {
	facts            []factTask
	restartWhen      []string
	markHealthyWhen  []string
	foundRestart     bool
	foundMarkHealthy bool
}

func (decision *restartDecision) collect(tasks []deployTask) {
	for _, task := range tasks {
		switch {
		case task.Name == restartTaskName:
			decision.restartWhen = task.When
			decision.foundRestart = true
		case task.Name == markHealthyTaskName:
			decision.markHealthyWhen = task.ChangedWhen
			decision.foundMarkHealthy = true
		case task.SetFact != nil && !decision.foundRestart:
			decision.facts = append(decision.facts, factTask{
				When:    nonNilStrings(task.When),
				Vars:    nonNilMap(task.Vars),
				SetFact: task.SetFact,
			})
		}
		decision.collect(task.Block)
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return values
}

func readRestartDecision(t *testing.T) restartDecision {
	t.Helper()
	path := filepath.Join(repositoryRootForTest(t), opnsenseDeployTaskRelPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	var tasks []deployTask
	if err := yaml.Unmarshal(contents, &tasks); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var decision restartDecision
	decision.collect(tasks)
	if !decision.foundRestart || len(decision.restartWhen) == 0 {
		t.Fatalf("%s has no task %q with a when list", path, restartTaskName)
	}
	if !decision.foundMarkHealthy || len(decision.markHealthyWhen) == 0 {
		t.Fatalf("%s has no task %q with a changed_when", path, markHealthyTaskName)
	}
	return decision
}

// ansiblePython returns the interpreter that runs ansible-core, read from the
// ansible-playbook entry point's shebang, so the evaluator imports the same
// ansible-core a deploy runs. The entry point is only read, never run.
func ansiblePython(t *testing.T) string {
	t.Helper()
	entryPoint, err := exec.LookPath(ansiblePlaybookCommandName)
	if err != nil {
		t.Fatalf("%s is required: %v", ansiblePlaybookCommandName, err)
	}
	file, err := os.Open(entryPoint)
	if err != nil {
		t.Fatalf("Open %s: %v", entryPoint, err)
	}
	defer func() { _ = file.Close() }()
	firstLine, err := bufio.NewReader(file).ReadString('\n')
	if err != nil {
		t.Fatalf("read shebang of %s: %v", entryPoint, err)
	}
	interpreter, found := strings.CutPrefix(strings.TrimSpace(firstLine), "#!")
	fields := strings.Fields(interpreter)
	if !found || len(fields) == 0 {
		t.Fatalf("%s has no shebang: %q", entryPoint, firstLine)
	}
	if filepath.Base(fields[0]) == "env" && len(fields) > 1 {
		return fields[len(fields)-1]
	}
	return fields[0]
}

func evaluateRestartDecision(
	t *testing.T, decision restartDecision, variables map[string]any,
) conditionResult {
	t.Helper()
	// A task file with no set_fact task before the restart leaves facts nil,
	// which would encode as null rather than an empty list.
	facts := decision.facts
	if facts == nil {
		facts = []factTask{}
	}
	payload, err := json.Marshal(conditionRequest{
		Variables: variables,
		Facts:     facts,
		Conditions: map[string][]string{
			restartConditionName:     decision.restartWhen,
			markHealthyConditionName: decision.markHealthyWhen,
		},
	})
	if err != nil {
		t.Fatalf("encode condition request: %v", err)
	}
	commandContext, cancel := context.WithTimeout(t.Context(), ansiblePlaybookTimeout)
	defer cancel()
	command := exec.CommandContext(
		commandContext, ansiblePython(t), conditionEvaluatorRelPath,
	)
	command.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("evaluate conditions: %v\n%s", err, stderr.String())
	}
	var result conditionResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode evaluator output: %v\n%s", err, output)
	}
	return result
}

// commandResult is a registered ansible.builtin.command or script result.
func commandResult(exitCode int, stdout string, stderr string) map[string]any {
	return map[string]any{
		"changed": false,
		"failed":  false,
		"rc":      exitCode,
		"stdout":  stdout,
		"stderr":  stderr,
	}
}

func TestOPNsenseDeployRestartDecision(t *testing.T) {
	decision := readRestartDecision(t)

	testCases := []struct {
		name            string
		binaryChanged   bool
		instancesExit   int
		version         map[string]any
		wantRestart     bool
		wantMarkChanged bool
	}{
		{
			name:            "installed binary changed",
			binaryChanged:   true,
			instancesExit:   0,
			version:         commandResult(0, releaseVersionLine, ""),
			wantRestart:     true,
			wantMarkChanged: true,
		},
		{
			name:            "instance check failed",
			binaryChanged:   false,
			instancesExit:   1,
			version:         commandResult(0, releaseVersionLine, ""),
			wantRestart:     true,
			wantMarkChanged: true,
		},
		{
			name:            "running daemon reports a stale commit",
			binaryChanged:   false,
			instancesExit:   0,
			version:         commandResult(0, staleVersionLine, ""),
			wantRestart:     true,
			wantMarkChanged: true,
		},
		{
			name:          "version read failed",
			binaryChanged: false,
			instancesExit: 0,
			version: commandResult(
				1, "", "rpc error: code = DeadlineExceeded desc = context deadline exceeded",
			),
			wantRestart:     true,
			wantMarkChanged: true,
		},
		{
			name:            "daemon already runs the release",
			binaryChanged:   false,
			instancesExit:   0,
			version:         commandResult(0, releaseVersionLine, ""),
			wantRestart:     false,
			wantMarkChanged: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			binaryInstall := map[string]any{"changed": testCase.binaryChanged, "failed": false}
			variables := map[string]any{
				"ansible_check_mode":             false,
				"mwan_release_commit":            testReleaseCommit,
				"mwan_opnsense_binary_install":   binaryInstall,
				"mwan_opnsense_instances_before": commandResult(testCase.instancesExit, "", ""),
				"mwan_opnsense_version_before":   testCase.version,
			}
			result := evaluateRestartDecision(t, decision, variables)
			restart, found := result.Conditions[restartConditionName]
			if !found {
				t.Fatalf("evaluator returned no %s verdict: %+v", restartConditionName, result)
			}
			if restart != testCase.wantRestart {
				t.Errorf(
					"restart = %t, want %t (when %q, facts %s)",
					restart, testCase.wantRestart, decision.restartWhen, factsText(result),
				)
			}
			markChanged, found := result.Conditions[markHealthyConditionName]
			if !found {
				t.Fatalf("evaluator returned no %s verdict: %+v", markHealthyConditionName, result)
			}
			if markChanged != testCase.wantMarkChanged {
				t.Errorf(
					"mark-healthy changed = %t, want %t (changed_when %q, facts %s)",
					markChanged, testCase.wantMarkChanged, decision.markHealthyWhen, factsText(result),
				)
			}
		})
	}
}

func factsText(result conditionResult) string {
	encoded, err := json.Marshal(result.Facts)
	if err != nil {
		return err.Error()
	}
	return string(encoded)
}

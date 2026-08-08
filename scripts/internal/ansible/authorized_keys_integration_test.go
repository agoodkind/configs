package ansible

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	githubKeyA = "ssh-ed25519 AAAAGITHUBA github-a"
	githubKeyB = "ssh-rsa AAAAGITHUBB github-b"
	localKey   = "ssh-ed25519 AAAALOCAL local"
	agentKey   = "ssh-ed25519 AAAAAGENT agent"
	extraKey   = `from="2001:db8::1/128" ssh-ed25519 AAAASERVICE sshpiper-upstream`
)

type authorizedKeysHarness struct {
	scriptPath string
	path       string
	home       string
	counter    string
}

func newAuthorizedKeysHarness(t *testing.T) authorizedKeysHarness {
	t.Helper()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	repositoryRoot := filepath.Clean(filepath.Join(workingDirectory, "..", "..", ".."))

	fakeBin := t.TempDir()
	installExecutable(
		t,
		filepath.Join(workingDirectory, "testdata", "fake-curl.sh"),
		filepath.Join(fakeBin, "curl"),
	)
	installExecutable(
		t,
		filepath.Join(workingDirectory, "testdata", "fake-ssh-add.sh"),
		filepath.Join(fakeBin, "ssh-add"),
	)

	home := t.TempDir()
	sshDirectory := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDirectory, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", sshDirectory, err)
	}
	if err := os.WriteFile(
		filepath.Join(sshDirectory, "id_local.pub"),
		[]byte(localKey+"\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile local key: %v", err)
	}

	return authorizedKeysHarness{
		scriptPath: filepath.Join(repositoryRoot, "ansible", "deploy-authorized-keys"),
		path:       fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		home:       home,
		counter:    filepath.Join(t.TempDir(), "curl-count"),
	}
}

func installExecutable(t *testing.T, source string, destination string) {
	t.Helper()

	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", source, err)
	}
	if err := os.WriteFile(destination, contents, 0o700); err != nil {
		t.Fatalf("WriteFile %s: %v", destination, err)
	}
}

func (h authorizedKeysHarness) run(
	t *testing.T,
	githubBody string,
	githubError string,
	arguments ...string,
) ([]byte, error) {
	t.Helper()

	command := exec.Command(h.scriptPath, arguments...)
	command.Env = []string{
		"FAKE_CURL_BODY=" + githubBody,
		"FAKE_CURL_COUNTER=" + h.counter,
		"FAKE_CURL_ERROR=" + githubError,
		"FAKE_SSH_ADD_KEY=" + agentKey,
		"HOME=" + h.home,
		"PATH=" + h.path,
	}
	return command.CombinedOutput()
}

func requireBundleLines(t *testing.T, path string, want []string) {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	got := strings.Split(strings.TrimSpace(string(contents)), "\n")
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("bundle lines = %q, want %q", got, want)
	}
}

func requireNoFile(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("output %s exists after failure", path)
	}
}

func TestDeployAuthorizedKeysWritesGitHubAndRestrictedBundles(t *testing.T) {
	harness := newAuthorizedKeysHarness(t)
	outputDirectory := t.TempDir()
	humanBundle := filepath.Join(outputDirectory, "human")
	combinedBundle := filepath.Join(outputDirectory, "combined")
	extraPublicKey := filepath.Join(t.TempDir(), "upstream.pub")
	if err := os.WriteFile(extraPublicKey, []byte(extraKey+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile extra key: %v", err)
	}

	githubBody := "\r\n" + githubKeyB + "\r\n" + githubKeyA + "\n" + githubKeyA + "\n\n"
	output, err := harness.run(
		t,
		githubBody,
		"",
		"--github-user",
		"agoodkind",
		"--out",
		humanBundle,
		"--extra-pubkey",
		extraPublicKey,
		"--extra-out",
		combinedBundle,
	)
	if err != nil {
		t.Fatalf("deploy-authorized-keys: %v\n%s", err, output)
	}

	requireBundleLines(t, humanBundle, []string{githubKeyA, githubKeyB})
	requireBundleLines(t, combinedBundle, []string{extraKey, githubKeyA, githubKeyB})
}

func TestDeployAuthorizedKeysRejectsFetchFailureWithoutOutputs(t *testing.T) {
	harness := newAuthorizedKeysHarness(t)
	outputDirectory := t.TempDir()
	humanBundle := filepath.Join(outputDirectory, "human")
	combinedBundle := filepath.Join(outputDirectory, "combined")
	extraPublicKey := filepath.Join(t.TempDir(), "upstream.pub")
	if err := os.WriteFile(extraPublicKey, []byte(extraKey+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile extra key: %v", err)
	}

	output, err := harness.run(
		t,
		"",
		"GitHub unavailable",
		"--github-user",
		"agoodkind",
		"--out",
		humanBundle,
		"--extra-pubkey",
		extraPublicKey,
		"--extra-out",
		combinedBundle,
	)
	if err == nil {
		t.Fatalf("deploy-authorized-keys succeeded, want failure\n%s", output)
	}
	if !strings.Contains(string(output), "GitHub unavailable") {
		t.Fatalf("failure did not come from GitHub fetch:\n%s", output)
	}

	requireNoFile(t, humanBundle)
	requireNoFile(t, combinedBundle)
}

func TestDeployAuthorizedKeysRejectsEmptyGitHubKeysWithoutOutputs(t *testing.T) {
	harness := newAuthorizedKeysHarness(t)
	humanBundle := filepath.Join(t.TempDir(), "human")

	if output, err := harness.run(
		t,
		"\r\n\n",
		"",
		"--github-user",
		"agoodkind",
		"--out",
		humanBundle,
	); err == nil {
		t.Fatalf("deploy-authorized-keys succeeded, want failure\n%s", output)
	}

	requireNoFile(t, humanBundle)
}

func TestDeployAuthorizedKeysRequiresArguments(t *testing.T) {
	testCases := []struct {
		name      string
		arguments []string
	}{
		{name: "GitHub user", arguments: []string{"--out", "unused"}},
		{name: "output", arguments: []string{"--github-user", "agoodkind"}},
		{name: "option value", arguments: []string{"--github-user"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newAuthorizedKeysHarness(t)
			if output, err := harness.run(t, githubKeyA+"\n", "", testCase.arguments...); err == nil {
				t.Fatalf("deploy-authorized-keys succeeded, want failure\n%s", output)
			}
		})
	}
}

func TestDeployAuthorizedKeysRequiresPairedExtraOutputs(t *testing.T) {
	testCases := []struct {
		name               string
		includeExtraPubkey bool
		includeExtraOut    bool
	}{
		{
			name:               "extra output",
			includeExtraPubkey: true,
		},
		{
			name:            "extra public key",
			includeExtraOut: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newAuthorizedKeysHarness(t)
			outputDirectory := t.TempDir()
			humanBundle := filepath.Join(outputDirectory, "human")
			combinedBundle := filepath.Join(outputDirectory, "combined")
			extraPublicKey := filepath.Join(t.TempDir(), "upstream.pub")
			if err := os.WriteFile(extraPublicKey, []byte(extraKey+"\n"), 0o600); err != nil {
				t.Fatalf("WriteFile extra key: %v", err)
			}
			arguments := []string{"--github-user", "agoodkind", "--out", humanBundle}
			if testCase.includeExtraPubkey {
				arguments = append(arguments, "--extra-pubkey", extraPublicKey)
			}
			if testCase.includeExtraOut {
				arguments = append(arguments, "--extra-out", combinedBundle)
			}
			if output, err := harness.run(t, githubKeyA+"\n", "", arguments...); err == nil {
				t.Fatalf("deploy-authorized-keys succeeded, want failure\n%s", output)
			}
			requireNoFile(t, humanBundle)
			requireNoFile(t, combinedBundle)
		})
	}
}

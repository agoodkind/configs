package ansible

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type inventoryOutput struct {
	Meta struct {
		Hostvars map[string]map[string]any `json:"hostvars"`
	} `json:"_meta"`
}

func inventoryString(t *testing.T, name string, value any) string {
	t.Helper()
	if text, ok := value.(string); ok {
		return text
	}
	if wrapped, ok := value.(map[string]any); ok {
		if text, ok := wrapped["__ansible_unsafe"].(string); ok {
			return text
		}
	}
	t.Fatalf("%s is not a string: %T", name, value)
	return ""
}

func TestSuburbanInventoryUsesDirectSSH(t *testing.T) {
	ansibleInventory, err := exec.LookPath("ansible-inventory")
	if err != nil {
		t.Skip("ansible-inventory is not installed")
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	repositoryRoot := filepath.Clean(filepath.Join(workingDirectory, "..", "..", ".."))

	command := exec.Command(ansibleInventory, "--list")
	command.Dir = filepath.Join(repositoryRoot, "ansible")
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, err := command.Output()
	if err != nil {
		t.Fatalf("ansible-inventory --list: %v\n%s", err, standardError.String())
	}

	var inventory inventoryOutput
	if err := json.Unmarshal(output, &inventory); err != nil {
		t.Fatalf("decode inventory: %v", err)
	}

	checkedHosts := 0
	for hostname, variables := range inventory.Meta.Hostvars {
		if !strings.HasSuffix(hostname, ".suburban.goodkind.io") {
			continue
		}
		commonArgs := inventoryString(
			t,
			hostname+" ansible_ssh_common_args",
			variables["ansible_ssh_common_args"],
		)
		if strings.Contains(commonArgs, "ProxyJump") {
			t.Errorf("%s SSH args still use ProxyJump: %q", hostname, commonArgs)
		}
		checkedHosts++
	}
	if checkedHosts == 0 {
		t.Fatal("inventory contains no suburban service hosts")
	}

	router := inventory.Meta.Hostvars["router.suburban.goodkind.io"]
	serviceMapping := router["service_mapping"].(map[string]any)
	opnsenseMapping := serviceMapping["opnsense_suburban"].(map[string]any)
	wantHost := opnsenseMapping["ipv6_transit"].(string)
	gotHost := inventoryString(t, "router ansible_host", router["ansible_host"])
	if gotHost != wantHost {
		t.Errorf("router ansible_host = %q, want routed IPv6 %q", gotHost, wantHost)
	}
}

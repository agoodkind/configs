package main

import (
	"slices"
	"testing"

	"goodkind.io/configs/internal/ansible"
)

// TestApplyDeployArgTags pins that both --tags forms parse and that repeated
// flags accumulate into DeployOptions.Tags in order.
func TestApplyDeployArgTags(t *testing.T) {
	var opts ansible.DeployOptions

	// `--tags <value>` consumes two tokens.
	consumed, err := applyDeployArg(&opts, []string{"--tags", "isp-lxcs"}, 0)
	if err != nil {
		t.Fatalf("applyDeployArg(--tags isp-lxcs): %v", err)
	}
	if consumed != 2 {
		t.Fatalf("consumed = %d, want 2", consumed)
	}

	// `--tags=<value>` consumes one token and accumulates.
	consumed, err = applyDeployArg(&opts, []string{"--tags=extra"}, 0)
	if err != nil {
		t.Fatalf("applyDeployArg(--tags=extra): %v", err)
	}
	if consumed != 1 {
		t.Fatalf("consumed = %d, want 1", consumed)
	}

	want := []string{"isp-lxcs", "extra"}
	if !slices.Equal(opts.Tags, want) {
		t.Fatalf("opts.Tags = %v, want %v", opts.Tags, want)
	}
}

// TestParseDeployRelease pins that both --release forms carry the tag through
// to DeployOptions, so a deploy can stage the named release before the play.
func TestParseDeployRelease(t *testing.T) {
	spaced, err := parseDeploy([]string{"deploy-mwan", "--release", "202608160638-3-03cf29a", "--limit", "mwan_suburban_servers"})
	if err != nil {
		t.Fatalf("parseDeploy: %v", err)
	}
	if spaced.ReleaseTag != "202608160638-3-03cf29a" || spaced.Limit != "mwan_suburban_servers" {
		t.Fatalf("parseDeploy = %+v", spaced)
	}
	glued, err := parseDeploy([]string{"deploy-mwan", "--release=v1.2.3"})
	if err != nil {
		t.Fatalf("parseDeploy: %v", err)
	}
	if glued.ReleaseTag != "v1.2.3" {
		t.Fatalf("ReleaseTag = %q", glued.ReleaseTag)
	}
}

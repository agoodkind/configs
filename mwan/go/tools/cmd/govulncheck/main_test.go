package main

import "testing"

func TestIsKnownFalsePositive(t *testing.T) {
	t.Parallel()

	const knownOutput = `=== Symbol Results ===

Vulnerability #1: GO-2026-4736
    GoBGP vulnerable to a denial of service via the NEXT_HOP path attribute in
    github.com/osrg/gobgp
  More info: https://pkg.go.dev/vuln/GO-2026-4736
  Module: github.com/osrg/gobgp/v4
    Found in: github.com/osrg/gobgp/v4@v4.5.0
    Fixed in: N/A
`

	if !isKnownFalsePositive([]byte(knownOutput), "v4.5.0") {
		t.Fatal("expected known false positive to be suppressed")
	}

	if isKnownFalsePositive([]byte(knownOutput), "v4.2.9") {
		t.Fatal("did not expect versions before v4.3.0 to be suppressed")
	}

	const multiVulnOutput = knownOutput + "\nVulnerability #2: GO-2026-9999\n"
	if isKnownFalsePositive([]byte(multiVulnOutput), "v4.5.0") {
		t.Fatal("did not expect multi-vulnerability output to be suppressed")
	}
}

func TestSemverGTE(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		left     string
		right    string
		expected bool
	}{
		{
			name:     "greater patch",
			left:     "v4.3.1",
			right:    "v4.3.0",
			expected: true,
		},
		{
			name:     "equal version",
			left:     "v4.3.0",
			right:    "v4.3.0",
			expected: true,
		},
		{
			name:     "lesser minor",
			left:     "v4.2.9",
			right:    "v4.3.0",
			expected: false,
		},
		{
			name:     "prerelease suffix",
			left:     "v4.5.0-rc1",
			right:    "v4.3.0",
			expected: true,
		},
	}

	for _, testCase := range testCases {
		currentTestCase := testCase
		t.Run(currentTestCase.name, func(t *testing.T) {
			t.Parallel()

			actual := semverGTE(currentTestCase.left, currentTestCase.right)
			if actual != currentTestCase.expected {
				t.Fatalf(
					"semverGTE(%q, %q) = %t, want %t",
					currentTestCase.left,
					currentTestCase.right,
					actual,
					currentTestCase.expected,
				)
			}
		})
	}
}

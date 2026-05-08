package validate

import (
	"strconv"
	"testing"
)

func TestLeaseToleranceN(t *testing.T) {
	cases := []struct {
		baseline int
		want     int
	}{
		{0, 2},
		{5, 2},
		{20, 2},
		{30, 3},
		{100, 10},
		{99, 10},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.baseline), func(t *testing.T) {
			got := LeaseToleranceN(tc.baseline)
			if got != tc.want {
				t.Fatalf("LeaseToleranceN(%d)=%d want %d", tc.baseline, got, tc.want)
			}
		})
	}
}

func TestDiff_DHCPLeaseToleranceBoundary(t *testing.T) {
	// Baseline of 100 leases yields tolerance 10. A drop of
	// exactly 10 (post=90) is within tolerance and passes; a drop
	// of 11 (post=89) exceeds tolerance and is reported.
	preCount := 100
	tol := LeaseToleranceN(preCount)
	if tol != 10 {
		t.Fatalf("expected tolerance 10 got %d", tol)
	}
	pre := &Baseline{
		SchemaVersion:    BaselineSchemaVersion,
		DHCPv4LeaseCount: preCount,
		Results: []Result{
			{
				CheckID:     CheckIDDHCPv4LeasesPresent,
				Category:    CategoryDNSDHCP,
				Severity:    SeverityRegression,
				Outcome:     OutcomePass,
				ParsedValue: "100",
			},
		},
	}
	postWithin := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{
				CheckID:     CheckIDDHCPv4LeasesPresent,
				Category:    CategoryDNSDHCP,
				Severity:    SeverityRegression,
				Outcome:     OutcomePass,
				ParsedValue: "90",
			},
		},
	}
	postBeyond := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{
				CheckID:     CheckIDDHCPv4LeasesPresent,
				Category:    CategoryDNSDHCP,
				Severity:    SeverityRegression,
				Outcome:     OutcomePass,
				ParsedValue: "89",
			},
		},
	}
	rWithin := Diff(pre, postWithin)
	if rWithin.Verdict != DiffPass {
		t.Fatalf("within-tolerance verdict=%s", rWithin.Verdict)
	}
	if rWithin.Entries[0].Outcome != DiffPass {
		t.Fatalf("within-tolerance outcome=%s", rWithin.Entries[0].Outcome)
	}
	rBeyond := Diff(pre, postBeyond)
	if rBeyond.Verdict != DiffRegression {
		t.Fatalf("beyond-tolerance verdict=%s", rBeyond.Verdict)
	}
}

func TestDiff_PassToFailReportsAtSeverity(t *testing.T) {
	pre := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{CheckID: "x", Severity: SeverityBlocker, Outcome: OutcomePass},
		},
	}
	post := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{CheckID: "x", Severity: SeverityBlocker, Outcome: OutcomeFail},
		},
	}
	r := Diff(pre, post)
	if r.Verdict != DiffBlocker {
		t.Fatalf("verdict=%s", r.Verdict)
	}
}

func TestDiff_FailToPassMarkedPreExisting(t *testing.T) {
	pre := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{CheckID: "x", Severity: SeverityRegression, Outcome: OutcomeFail},
		},
	}
	post := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{CheckID: "x", Severity: SeverityRegression, Outcome: OutcomePass},
		},
	}
	r := Diff(pre, post)
	if r.Entries[0].Outcome != DiffPreExisting {
		t.Fatalf("entry outcome=%s", r.Entries[0].Outcome)
	}
	if r.Verdict != DiffPass {
		t.Fatalf("verdict=%s", r.Verdict)
	}
}

func TestDiff_AdvisoryDriftRecorded(t *testing.T) {
	pre := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{
				CheckID:     "plugin_os_frr_version",
				Severity:    SeverityAdvisory,
				Outcome:     OutcomePass,
				ParsedValue: "1.50",
			},
		},
	}
	post := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{
				CheckID:     "plugin_os_frr_version",
				Severity:    SeverityAdvisory,
				Outcome:     OutcomePass,
				ParsedValue: "1.51",
			},
		},
	}
	r := Diff(pre, post)
	if r.Entries[0].Outcome != DiffAdvisoryDrift {
		t.Fatalf("entry outcome=%s", r.Entries[0].Outcome)
	}
	if r.Verdict != DiffAdvisoryDrift {
		t.Fatalf("verdict=%s", r.Verdict)
	}
}

func TestDiff_MissingPostReportedAtSeverity(t *testing.T) {
	pre := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{CheckID: "x", Severity: SeverityRegression, Outcome: OutcomePass},
		},
	}
	post := &Baseline{SchemaVersion: BaselineSchemaVersion}
	r := Diff(pre, post)
	if r.Entries[0].Outcome != DiffMissing {
		t.Fatalf("entry outcome=%s", r.Entries[0].Outcome)
	}
	if r.Verdict != DiffRegression {
		t.Fatalf("verdict=%s", r.Verdict)
	}
}

func TestDiff_SkippedExcludedFromVerdict(t *testing.T) {
	pre := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{CheckID: "x", Severity: SeverityBlocker, Outcome: OutcomeSkip},
		},
	}
	post := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{CheckID: "x", Severity: SeverityBlocker, Outcome: OutcomeSkip},
		},
	}
	r := Diff(pre, post)
	if r.Verdict != DiffPass {
		t.Fatalf("verdict=%s", r.Verdict)
	}
}

func TestDiff_PFRuleCountWithinFiveTolerance(t *testing.T) {
	pre := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{
				CheckID:     CheckIDPFRuleCountWithinTolerance,
				Severity:    SeverityAdvisory,
				Outcome:     OutcomePass,
				ParsedValue: "120",
			},
		},
	}
	post := &Baseline{
		SchemaVersion: BaselineSchemaVersion,
		Results: []Result{
			{
				CheckID:     CheckIDPFRuleCountWithinTolerance,
				Severity:    SeverityAdvisory,
				Outcome:     OutcomePass,
				ParsedValue: "123",
			},
		},
	}
	r := Diff(pre, post)
	if r.Entries[0].Outcome != DiffPass {
		t.Fatalf("expected pass within tolerance got %s", r.Entries[0].Outcome)
	}
}

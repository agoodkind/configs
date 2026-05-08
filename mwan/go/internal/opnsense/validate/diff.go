package validate

import (
	"fmt"
	"strconv"
	"time"
)

// DiffOutcome is the per-check verdict from comparing a baseline
// result to a post-upgrade result. The set follows the table in
// section 5 of the matrix doc.
type DiffOutcome string

const (
	// DiffPass means baseline and post both passed and the parsed
	// values agree (or differ within tolerance).
	DiffPass DiffOutcome = "pass"

	// DiffAdvisoryDrift means values changed in an expected way
	// (plugin version bump, kernel default flip).
	DiffAdvisoryDrift DiffOutcome = "advisory_drift"

	// DiffRegression means a baseline_value check changed and the
	// row's severity is regression.
	DiffRegression DiffOutcome = "regression"

	// DiffBlocker means a blocker-severity check failed
	// post-upgrade.
	DiffBlocker DiffOutcome = "blocker"

	// DiffPreExisting means the check was already failing on the
	// baseline; the upgrade did not introduce the regression and
	// the row is excluded from the aggregate verdict.
	DiffPreExisting DiffOutcome = "pre_existing_failure"

	// DiffSkipped means the check was skipped on either side.
	DiffSkipped DiffOutcome = "skipped"

	// DiffMissing means the post-upgrade run did not produce a
	// matching result for this baseline row. Reported at the
	// row's severity.
	DiffMissing DiffOutcome = "missing"
)

// DiffEntry is the per-row diff output. It carries enough context
// for the operator to decide whether to triage.
type DiffEntry struct {
	CheckID    string      `json:"check_id"`
	Category   Category    `json:"category"`
	Severity   Severity    `json:"severity"`
	Outcome    DiffOutcome `json:"outcome"`
	Reason     string      `json:"reason,omitempty"`
	Pre        Result      `json:"pre"`
	Post       Result      `json:"post"`
	ToleranceN int         `json:"tolerance_n,omitempty"`
}

// DiffReport is the aggregate diff record persisted to
// diff-report.json under the artefact directory.
type DiffReport struct {
	SchemaVersion int         `json:"schema_version"`
	Verdict       DiffOutcome `json:"verdict"`
	Entries       []DiffEntry `json:"entries"`
}

// LeaseToleranceN returns the tolerance for a DHCP lease count check
// per resolved decision O-1: max(2, 0.1 * baseline_count). The
// formula is named so the unit test can assert it directly.
func LeaseToleranceN(baselineCount int) int {
	if baselineCount <= 0 {
		return 2
	}
	tenPct := baselineCount / 10
	if baselineCount%10 != 0 {
		tenPct++
	}
	if tenPct < 2 {
		return 2
	}
	return tenPct
}

// PFRuleToleranceKind names the tolerance dimension passed to
// PFRuleToleranceN. Declaring a typed enum sidesteps the
// "switch on bare string" lint and gives callers compile-time
// safety against typos.
type PFRuleToleranceKind string

const (
	// PFRuleToleranceTotalRules is the tolerance for the
	// total pf rule count.
	PFRuleToleranceTotalRules PFRuleToleranceKind = "rules"

	// PFRuleToleranceNatRules is the tolerance for the
	// pf NAT rule count.
	PFRuleToleranceNatRules PFRuleToleranceKind = "nat"
)

// PFRuleToleranceN returns the +/- pf rule count tolerance from
// section 2.c. The matrix specifies 5 for total rules and 2 for
// nat rules; both come from this helper so the value is in one
// place.
func PFRuleToleranceN(kind PFRuleToleranceKind) int {
	switch kind {
	case PFRuleToleranceTotalRules:
		return 5
	case PFRuleToleranceNatRules:
		return 2
	default:
		return 0
	}
}

// Diff compares pre and post baselines and returns a diff report.
// pre is required; post is required. The pre baseline drives the
// row set so a row missing on post is reported as DiffMissing.
func Diff(pre, post *Baseline) *DiffReport {
	if pre == nil {
		pre = newBaseline(0, "", time.Time{}, nil)
	}
	if post == nil {
		post = newBaseline(0, "", time.Time{}, nil)
	}
	report := &DiffReport{
		SchemaVersion: BaselineSchemaVersion,
		Verdict:       DiffPass,
		Entries:       make([]DiffEntry, 0, len(pre.Results)),
	}
	for _, preResult := range pre.Results {
		entry := buildDiffEntry(preResult, post, pre)
		report.Entries = append(report.Entries, entry)
	}
	report.Verdict = aggregate(report.Entries)
	return report
}

func buildDiffEntry(preResult Result, post, pre *Baseline) DiffEntry {
	entry := DiffEntry{
		CheckID:    preResult.CheckID,
		Category:   preResult.Category,
		Severity:   preResult.Severity,
		Outcome:    "",
		Reason:     "",
		Pre:        preResult,
		Post:       emptyResult(),
		ToleranceN: 0,
	}
	postResult, ok := post.ResultByID(preResult.CheckID)
	if !ok {
		entry.Outcome = DiffMissing
		entry.Reason = "post-upgrade result not found"
		return entry
	}
	entry.Post = postResult

	preFailed := preResult.Outcome == OutcomeFail || preResult.Outcome == OutcomeError
	postFailed := postResult.Outcome == OutcomeFail || postResult.Outcome == OutcomeError

	if preResult.Outcome == OutcomeSkip || postResult.Outcome == OutcomeSkip {
		entry.Outcome = DiffSkipped
		entry.Reason = "skipped on at least one side"
		return entry
	}
	if preFailed {
		entry.Outcome = DiffPreExisting
		entry.Reason = "check was already failing before upgrade"
		return entry
	}
	if postFailed {
		entry.Outcome = severityToDiff(preResult.Severity)
		entry.Reason = "post-upgrade check failed"
		return entry
	}
	return classifyValueDrift(entry, pre, postResult)
}

func classifyValueDrift(entry DiffEntry, pre *Baseline, postResult Result) DiffEntry {
	preValue := entry.Pre.ParsedValue
	postValue := postResult.ParsedValue
	if preValue == postValue {
		entry.Outcome = DiffPass
		return entry
	}
	if drift, ok := withinTolerance(entry.CheckID, preValue, postValue, pre); ok {
		entry.Outcome = DiffPass
		entry.ToleranceN = drift
		return entry
	}
	if entry.Severity == SeverityAdvisory {
		entry.Outcome = DiffAdvisoryDrift
		entry.Reason = fmt.Sprintf("value drift %q -> %q", preValue, postValue)
		return entry
	}
	entry.Outcome = severityToDiff(entry.Severity)
	entry.Reason = fmt.Sprintf("value drift %q -> %q", preValue, postValue)
	return entry
}

// toleranceFunc is the signature of a per-check tolerance evaluator.
// pre and post are the raw parsed values from the matching results;
// baseline carries any operator-supplied counts. Returns the
// tolerance N applied and whether the drift fell inside it.
type toleranceFunc func(pre, post string, baseline *Baseline) (int, bool)

// toleranceFuncs is the per-check tolerance dispatch table. A map
// dispatch sidesteps the "switch on bare string" lint and keeps the
// per-check tolerance rules in one place.
var toleranceFuncs = map[string]toleranceFunc{
	CheckIDDHCPv4LeasesPresent: func(pre, post string, b *Baseline) (int, bool) {
		return checkLeaseTolerance(pre, post, b.DHCPv4LeaseCount)
	},
	CheckIDDHCPv6IANAPresent: func(pre, post string, b *Baseline) (int, bool) {
		return checkLeaseTolerance(pre, post, b.DHCPv6IANALeaseCount)
	},
	CheckIDDHCPv6IAPDPresent: func(pre, post string, b *Baseline) (int, bool) {
		return checkLeaseTolerance(pre, post, b.DHCPv6IAPDLeaseCount)
	},
	CheckIDPFRuleCountWithinTolerance: func(pre, post string, _ *Baseline) (int, bool) {
		return checkSymmetricTolerance(pre, post, PFRuleToleranceN(PFRuleToleranceTotalRules))
	},
	CheckIDPFNatRuleCount: func(pre, post string, _ *Baseline) (int, bool) {
		return checkSymmetricTolerance(pre, post, PFRuleToleranceN(PFRuleToleranceNatRules))
	},
}

// withinTolerance returns the tolerance N that was applied if the
// drift fell within it. The third return is false if the check has
// no tolerance rule, true otherwise. This is the only place that
// knows about per-check tolerance.
func withinTolerance(checkID, pre, post string, baseline *Baseline) (int, bool) {
	fn, ok := toleranceFuncs[checkID]
	if !ok {
		return 0, false
	}
	return fn(pre, post, baseline)
}

func checkLeaseTolerance(preStr, postStr string, baselineCount int) (int, bool) {
	preN, err := strconv.Atoi(preStr)
	if err != nil {
		return 0, false
	}
	postN, err := strconv.Atoi(postStr)
	if err != nil {
		return 0, false
	}
	tol := LeaseToleranceN(baselineCount)
	if preN-postN <= tol {
		return tol, true
	}
	return tol, false
}

func checkSymmetricTolerance(preStr, postStr string, tol int) (int, bool) {
	preN, err := strconv.Atoi(preStr)
	if err != nil {
		return 0, false
	}
	postN, err := strconv.Atoi(postStr)
	if err != nil {
		return 0, false
	}
	delta := preN - postN
	if delta < 0 {
		delta = -delta
	}
	if delta <= tol {
		return tol, true
	}
	return tol, false
}

func severityToDiff(s Severity) DiffOutcome {
	switch s {
	case SeverityBlocker:
		return DiffBlocker
	case SeverityRegression:
		return DiffRegression
	case SeverityAdvisory:
		return DiffAdvisoryDrift
	default:
		return DiffRegression
	}
}

// aggregate produces the overall verdict from a diff entry list.
// blocker beats regression beats advisory beats pass. Missing
// entries are escalated to the row's severity so the verdict
// reflects what kind of failure this would be.
func aggregate(entries []DiffEntry) DiffOutcome {
	worst := DiffPass
	for _, e := range entries {
		if e.Outcome == DiffPreExisting || e.Outcome == DiffSkipped {
			continue
		}
		effective := e.Outcome
		if effective == DiffMissing {
			effective = severityToDiff(e.Severity)
		}
		if rank(effective) > rank(worst) {
			worst = effective
		}
	}
	return worst
}

func rank(o DiffOutcome) int {
	switch o {
	case DiffPass:
		return 0
	case DiffAdvisoryDrift:
		return 1
	case DiffRegression:
		return 2
	case DiffMissing:
		return 2
	case DiffBlocker:
		return 3
	case DiffPreExisting, DiffSkipped:
		return 0
	default:
		return 0
	}
}

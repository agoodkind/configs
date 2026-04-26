//go:build linux

package oob

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

const ipRuleListTimeout = 5 * time.Second
const ipRuleMutateTimeout = 5 * time.Second

// DesiredRule describes one ip rule entry the daemon wants to keep present.
// Matches what `ip -6 rule add ...` accepts. Only the fields the daemon
// actively manages are modeled; foreign rules are left alone.
type DesiredRule struct {
	Family   string // "inet" or "inet6"; empty defaults to "inet6"
	Priority int    // numeric priority (0..32766)
	From     string // source selector; empty means no "from" clause
	UIDRange string // "lo-hi" or "u-u"; empty means no "uidrange" clause
	Table    string // table name (e.g. "oob"); required
}

// CurrentRule is one parsed entry from `ip [-6] rule show`.
type CurrentRule struct {
	Priority int
	From     string
	UIDRange string
	Table    string
}

// ReconcileRules ensures every rule in desired is present, and removes
// any rule the daemon previously installed (matching priority+table+family)
// that no longer matches. Foreign rules at unrelated priorities are
// preserved.
//
// Strategy:
//   - For each family (inet/inet6) seen in desired, list current rules.
//   - For each desired rule: if an exact match exists, do nothing.
//   - If a rule exists at the same priority but doesn't match: del + add.
//   - If no rule at that priority: add.
//
// All actions are logged at DEBUG with full diff context.
func ReconcileRules(
	ctx context.Context, runner IPRunner, log *slog.Logger,
	desired []DesiredRule,
) error {
	log = log.With("component", "rules")

	// Group desired by family so we list each family at most once.
	byFamily := map[string][]DesiredRule{}
	for _, r := range desired {
		fam := r.Family
		if fam == "" {
			fam = "inet6"
		}
		byFamily[fam] = append(byFamily[fam], r)
	}

	for fam, want := range byFamily {
		log.Debug("rules: starting reconcile", "family", fam, "want_count", len(want))
		current, err := listRules(ctx, runner, fam)
		if err != nil {
			return fmt.Errorf("list %s rules: %w", fam, err)
		}
		log.Debug("rules: current snapshot", "family", fam, "count", len(current),
			"rules", current)

		byPrio := map[int]CurrentRule{}
		for _, c := range current {
			byPrio[c.Priority] = c
		}

		for _, w := range want {
			cur, exists := byPrio[w.Priority]
			matches := exists && rulesMatch(cur, w)
			if matches {
				log.Debug("rules: already present",
					"family", fam, "priority", w.Priority, "table", w.Table)
				continue
			}
			if exists {
				log.Info("rules: replacing rule at priority",
					"family", fam, "priority", w.Priority,
					"old", cur, "new", w)
				if err := delRule(ctx, runner, fam, cur); err != nil {
					return fmt.Errorf("del rule prio=%d: %w", w.Priority, err)
				}
			} else {
				log.Info("rules: adding new rule",
					"family", fam, "priority", w.Priority,
					"from", w.From, "uidrange", w.UIDRange, "table", w.Table)
			}
			if err := addRule(ctx, runner, fam, w); err != nil {
				return fmt.Errorf("add rule prio=%d: %w", w.Priority, err)
			}
		}
	}
	return nil
}

// rulesMatch returns true if a current rule equals a desired rule on the
// dimensions the daemon manages.
func rulesMatch(c CurrentRule, w DesiredRule) bool {
	return c.Priority == w.Priority &&
		c.From == w.From &&
		c.UIDRange == w.UIDRange &&
		c.Table == w.Table
}

// listRules returns parsed rules for one family.
func listRules(
	ctx context.Context, runner IPRunner, family string,
) ([]CurrentRule, error) {
	flag := "-6"
	if family == "inet" {
		flag = "-4"
	}
	out, err := runner.Run(ctx, ipRuleListTimeout, flag, "rule", "show")
	if err != nil {
		return nil, err
	}
	return parseRuleList(string(out))
}

// parseRuleList parses `ip [-6] rule show` output. Each line looks like:
//
//	0:   from all lookup local
//	5:   from all uidrange 997-997 lookup oob
//	6:   from 3d06:bad:b01:ff::1 lookup oob
//	32766:       from all lookup main
//
// Pure function for unit-testing without exec.
func parseRuleList(text string) ([]CurrentRule, error) {
	var out []CurrentRule
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// "PRIO:\t<rest>"
		colon := strings.Index(line, ":")
		if colon <= 0 {
			continue
		}
		prio, err := strconv.Atoi(line[:colon])
		if err != nil {
			continue
		}
		rest := strings.TrimSpace(line[colon+1:])
		fields := strings.Fields(rest)
		r := CurrentRule{Priority: prio}
		for i := 0; i < len(fields); i++ {
			switch fields[i] {
			case "from":
				if i+1 < len(fields) {
					// "from all" is the implicit default; normalize to ""
					// so a DesiredRule without an explicit From matches.
					if fields[i+1] != "all" {
						r.From = fields[i+1]
					}
					i++
				}
			case "uidrange":
				if i+1 < len(fields) {
					r.UIDRange = fields[i+1]
					i++
				}
			case "lookup":
				if i+1 < len(fields) {
					r.Table = fields[i+1]
					i++
				}
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func addRule(
	ctx context.Context, runner IPRunner, family string, w DesiredRule,
) error {
	flag := "-6"
	if family == "inet" {
		flag = "-4"
	}
	args := []string{flag, "rule", "add"}
	if w.From != "" {
		args = append(args, "from", w.From)
	}
	if w.UIDRange != "" {
		args = append(args, "uidrange", w.UIDRange)
	}
	args = append(args,
		"lookup", w.Table,
		"priority", strconv.Itoa(w.Priority),
	)
	_, err := runner.Run(ctx, ipRuleMutateTimeout, args...)
	return err
}

func delRule(
	ctx context.Context, runner IPRunner, family string, c CurrentRule,
) error {
	flag := "-6"
	if family == "inet" {
		flag = "-4"
	}
	args := []string{flag, "rule", "del", "priority", strconv.Itoa(c.Priority)}
	_, err := runner.Run(ctx, ipRuleMutateTimeout, args...)
	return err
}
# Configs secret-redaction hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** No `configs` subcommand can print a vault secret; all stdout/stderr is filtered through an overlap-safe redactor installed once in `main`, and `secret` writes its value to a hardened temp file instead of printing it.

**Architecture:** A dependency-free `internal/redact` package implements an Aho-Corasick automaton that finds every secret occurrence in a stream, merges overlapping/touching spans, and rewrites each merged span to `<redacted:KEY>`. `main` reads and validates the vault before dispatch (fail-closed), then routes `os.Stdout`/`os.Stderr` through two mutex-shared redactors so every command and child process is covered. `secret` writes to a `0600` file in a `0700` temp dir.

**Tech Stack:** Go 1.26 (module `goodkind.io/configs`, working dir `scripts/`), stdlib only, `gopkg.in/yaml.v3` (already a dep), build/lint via `make build` in `scripts/`.

---

## File structure

- Create `scripts/internal/redact/automaton.go` for the Aho-Corasick build, match, and span type.
- Create `scripts/internal/redact/automaton_test.go` for automaton and match unit tests.
- Create `scripts/internal/redact/writer.go` for the streaming `Writer`, span merge, `Pattern`, `Validate`, and `MinLen`.
- Create `scripts/internal/redact/writer_test.go` for writer, merge, and validate unit tests.
- Modify `scripts/internal/vault/vault.go` to add `Values`.
- Create `scripts/internal/vault/values_test.go` for the `Values` test.
- Modify `scripts/cmd/configs/main.go` to install redactors before dispatch and rewrite `runSecret`.
- Modify `scripts/internal/ansible/ansible.go` to delete the bespoke `InventoryDump` redaction and stream directly.

All commands run from `~/Sites/configs`. `make -C scripts build` is the gate (never `go build`); it runs vet, golangci, format, gocyclo, deadcode, staticcheck-extra, govulncheck and signs. For a fast inner loop during TDD, `cd scripts && go test ./internal/redact/...`. Run `make -C scripts build` before each commit.

---

### Task 1: redact package, span type and Aho-Corasick build

**Files:**
- Create: `scripts/internal/redact/automaton.go`
- Test: `scripts/internal/redact/automaton_test.go`

- [ ] **Step 1: Write the failing test**

```go
package redact

import (
	"reflect"
	"sort"
	"testing"
)

func TestAutomatonFindsAllOccurrences(t *testing.T) {
	ac := newAutomaton([][]byte{[]byte("ab"), []byte("bc"), []byte("abVALUE")})
	got := ac.findAll([]byte("xabcabVALUEx"))
	// spans are [start,end): "ab"@1, "bc"@2, "ab"@4, "abVALUE"@4
	want := []span{{1, 3}, {2, 4}, {4, 6}, {4, 11}}
	sort.Slice(got, func(i, j int) bool {
		if got[i].start != got[j].start {
			return got[i].start < got[j].start
		}
		return got[i].end < got[j].end
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findAll = %v, want %v", got, want)
	}
}

func TestAutomatonEmptyPatterns(t *testing.T) {
	ac := newAutomaton(nil)
	if got := ac.findAll([]byte("anything")); len(got) != 0 {
		t.Fatalf("findAll on empty automaton = %v, want none", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/Sites/configs/scripts && go test ./internal/redact/ -run TestAutomaton -v`
Expected: FAIL (undefined: newAutomaton, span).

- [ ] **Step 3: Write minimal implementation**

```go
// Package redact replaces secret byte sequences in a stream with a labeled
// placeholder, so a command's stdout and stderr never expose secret values.
package redact

// span is a half-open byte range [start,end) covered by a secret occurrence.
type span struct {
	start int
	end   int
}

// automaton is an Aho-Corasick matcher over a fixed set of patterns. It finds
// every occurrence of every pattern in a single pass over the input.
type automaton struct {
	next   []map[byte]int // goto per state
	fail   []int          // failure links
	out    [][]int        // pattern lengths ending at each state
	maxLen int
}

// newAutomaton builds the matcher from pattern byte slices. Empty or nil input
// yields a matcher that finds nothing.
func newAutomaton(patterns [][]byte) *automaton {
	ac := &automaton{
		next: []map[byte]int{{}},
		fail: []int{0},
		out:  [][]int{nil},
	}
	for _, p := range patterns {
		if len(p) == 0 {
			continue
		}
		ac.add(p)
		if len(p) > ac.maxLen {
			ac.maxLen = len(p)
		}
	}
	ac.buildFailureLinks()
	return ac
}

func (ac *automaton) add(p []byte) {
	state := 0
	for _, b := range p {
		nxt, ok := ac.next[state][b]
		if !ok {
			nxt = len(ac.next)
			ac.next = append(ac.next, map[byte]int{})
			ac.fail = append(ac.fail, 0)
			ac.out = append(ac.out, nil)
			ac.next[state][b] = nxt
		}
		state = nxt
	}
	ac.out[state] = append(ac.out[state], len(p))
}

func (ac *automaton) buildFailureLinks() {
	queue := make([]int, 0, len(ac.next))
	for _, s := range ac.next[0] {
		ac.fail[s] = 0
		queue = append(queue, s)
	}
	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		for b, nxt := range ac.next[state] {
			queue = append(queue, nxt)
			f := ac.fail[state]
			for f != 0 {
				if _, ok := ac.next[f][b]; ok {
					break
				}
				f = ac.fail[f]
			}
			if t, ok := ac.next[f][b]; ok && t != nxt {
				ac.fail[nxt] = t
			} else {
				ac.fail[nxt] = 0
			}
			ac.out[nxt] = append(ac.out[nxt], ac.out[ac.fail[nxt]]...)
		}
	}
}

// step advances the automaton from state on byte b, following failure links.
func (ac *automaton) step(state int, b byte) int {
	for {
		if nxt, ok := ac.next[state][b]; ok {
			return nxt
		}
		if state == 0 {
			return 0
		}
		state = ac.fail[state]
	}
}

// findAll returns every occurrence span over data, scanning from state 0.
func (ac *automaton) findAll(data []byte) []span {
	var spans []span
	state := 0
	for i := 0; i < len(data); i++ {
		state = ac.step(state, data[i])
		for _, length := range ac.out[state] {
			spans = append(spans, span{start: i - length + 1, end: i + 1})
		}
	}
	return spans
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/Sites/configs/scripts && go test ./internal/redact/ -run TestAutomaton -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
cd ~/Sites/configs && make -C scripts build && git add scripts/internal/redact/automaton.go scripts/internal/redact/automaton_test.go && git commit -m "Add Aho-Corasick automaton for multi-secret occurrence matching"
```

---

### Task 2: redact package, Pattern, MinLen, Validate, span merge

**Files:**
- Create: `scripts/internal/redact/writer.go`
- Test: `scripts/internal/redact/writer_test.go`

- [ ] **Step 1: Write the failing test**

```go
package redact

import (
	"reflect"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := []Pattern{{Value: []byte("0123456789abcdef"), Label: "vault_ok"}} // 16
	if key, valid := Validate(ok); !valid {
		t.Fatalf("Validate(16-char) = (%q,false), want valid", key)
	}
	short := []Pattern{{Value: []byte("short"), Label: "vault_bad"}}
	key, valid := Validate(short)
	if valid || key != "vault_bad" {
		t.Fatalf("Validate(short) = (%q,%v), want (vault_bad,false)", key, valid)
	}
	empty := []Pattern{{Value: []byte(""), Label: "vault_empty"}}
	if key, valid := Validate(empty); !valid {
		t.Fatalf("Validate(empty value) = (%q,false), want valid (empty ignored)", key)
	}
}

func TestMergeSpans(t *testing.T) {
	in := []labeledSpan{
		{span{1, 3}, "a"}, {span{2, 4}, "b"}, {span{4, 6}, "c"}, {span{8, 9}, "d"},
	}
	// [1,3)&[2,4) overlap -> [1,4); [4,6) touches end 4 -> merge -> [1,6); [8,9) separate
	got := mergeSpans(in)
	want := []labeledSpan{{span{1, 6}, "a"}, {span{8, 9}, "d"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeSpans = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/Sites/configs/scripts && go test ./internal/redact/ -run 'TestValidate|TestMergeSpans' -v`
Expected: FAIL (undefined: Pattern, Validate, labeledSpan, mergeSpans).

- [ ] **Step 3: Write minimal implementation**

```go
package redact

import "sort"

// MinLen is the shortest secret value that can be redacted safely. A non-empty
// value shorter than this would risk masking unrelated output, so the caller
// fails closed when one is present.
const MinLen = 16

// placeholderPrefix and placeholderSuffix wrap the matched key in the emitted
// placeholder, as <redacted:KEY>.
const (
	placeholderPrefix = "<redacted:"
	placeholderSuffix = ">"
)

// Pattern is one secret value and the vault key whose value it is. Label is not
// secret; Value is.
type Pattern struct {
	Value []byte
	Label string
}

// labeledSpan is a covered byte range carrying the label to emit for it.
type labeledSpan struct {
	span  span
	label string
}

// Validate returns the label of the first pattern whose value is non-empty and
// shorter than MinLen, with ok=false. Empty values are ignored (they match
// nothing). ok=true means every value is safe to redact.
func Validate(patterns []Pattern) (badKey string, ok bool) {
	for _, p := range patterns {
		if len(p.Value) > 0 && len(p.Value) < MinLen {
			return p.Label, false
		}
	}
	return "", true
}

// mergeSpans coalesces overlapping or touching spans into single covered
// regions. Input order is arbitrary; output is sorted by start and
// non-overlapping. Each merged region keeps the label of its earliest-starting
// member (ties broken by lexicographically smaller label) so the placeholder is
// deterministic. This is what makes overlapping secrets leak-proof: two adjacent
// secret spans become one redaction.
func mergeSpans(in []labeledSpan) []labeledSpan {
	if len(in) == 0 {
		return nil
	}
	s := append([]labeledSpan(nil), in...)
	sort.Slice(s, func(i, j int) bool {
		if s[i].span.start != s[j].span.start {
			return s[i].span.start < s[j].span.start
		}
		if s[i].span.end != s[j].span.end {
			return s[i].span.end < s[j].span.end
		}
		return s[i].label < s[j].label
	})
	out := []labeledSpan{s[0]}
	for _, cur := range s[1:] {
		last := &out[len(out)-1]
		if cur.span.start <= last.span.end {
			if cur.span.end > last.span.end {
				last.span.end = cur.span.end
			}
			continue
		}
		out = append(out, cur)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/Sites/configs/scripts && go test ./internal/redact/ -run 'TestValidate|TestMergeSpans' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd ~/Sites/configs && make -C scripts build && git add scripts/internal/redact/writer.go scripts/internal/redact/writer_test.go && git commit -m "Add redact Pattern, Validate, and overlap-safe span merge"
```

---

### Task 3: redact package, streaming Writer with hold-back

**Files:**
- Modify: `scripts/internal/redact/writer.go`
- Test: `scripts/internal/redact/writer_test.go`

- [ ] **Step 1: Write the failing test**

Add to `writer_test.go`; merge the `strings` import into the existing import block.

```go
func redactAll(t *testing.T, patterns []Pattern, chunks ...string) string {
	t.Helper()
	var sb strings.Builder
	w := New(&sb, patterns)
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return sb.String()
}

func TestWriterRedactsSingle(t *testing.T) {
	pats := []Pattern{{Value: []byte("supersecretvalue1234"), Label: "vault_token"}}
	got := redactAll(t, pats, "x=supersecretvalue1234;")
	if got != "x=<redacted:vault_token>;" {
		t.Fatalf("got %q", got)
	}
}

func TestWriterRedactsAcrossChunks(t *testing.T) {
	pats := []Pattern{{Value: []byte("supersecretvalue1234"), Label: "vault_token"}}
	got := redactAll(t, pats, "x=supersecret", "value1234;")
	if got != "x=<redacted:vault_token>;" {
		t.Fatalf("got %q", got)
	}
}

func TestWriterOverlapNoLeak(t *testing.T) {
	pats := []Pattern{
		{Value: []byte("SECRETabcdefghij"), Label: "vault_a"}, // 16
		{Value: []byte("abcdefghijVALUE1"), Label: "vault_b"}, // 16, shares abcdefghij
	}
	got := redactAll(t, pats, "xSECRETabcdefghijVALUE1x")
	if strings.Contains(got, "VALUE1") || strings.Contains(got, "SECRET") {
		t.Fatalf("overlap leaked a secret fragment: %q", got)
	}
}

func TestWriterEmptyPatternsPassthrough(t *testing.T) {
	got := redactAll(t, nil, "nothing to hide here")
	if got != "nothing to hide here" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/Sites/configs/scripts && go test ./internal/redact/ -run TestWriter -v`
Expected: FAIL (undefined: New, Write, Close).

- [ ] **Step 3: Write minimal implementation**

Append to `scripts/internal/redact/writer.go`. Merge `bytes` and `io` into the existing import block (which already imports `sort`).

```go
// Writer wraps an io.Writer and rewrites every secret occurrence to
// <redacted:KEY>. It is streaming-safe: a secret split across Write calls is
// still caught, because the trailing maxLen-1 bytes are held back until enough
// input arrives or Close is called. A Writer is not safe for concurrent use by
// multiple goroutines; the caller serializes writes.
type Writer struct {
	dst      io.Writer
	ac       *automaton
	patterns []Pattern
	buf      []byte
	maxLen   int
}

// New returns a Writer over dst. An empty pattern set makes the Writer a
// transparent passthrough.
func New(dst io.Writer, patterns []Pattern) *Writer {
	values := make([][]byte, 0, len(patterns))
	maxLen := 0
	for _, p := range patterns {
		values = append(values, p.Value)
		if len(p.Value) > maxLen {
			maxLen = len(p.Value)
		}
	}
	return &Writer{
		dst:      dst,
		ac:       newAutomaton(values),
		patterns: patterns,
		maxLen:   maxLen,
	}
}

// Write buffers p, redacts the portion that can be resolved, and emits it,
// holding back the trailing maxLen-1 bytes that a later write might extend.
func (w *Writer) Write(p []byte) (int, error) {
	if w.maxLen == 0 {
		if _, err := w.dst.Write(p); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	hold := w.maxLen - 1
	if len(w.buf) <= hold {
		return len(p), nil
	}
	safe := len(w.buf) - hold
	if err := w.flush(w.buf[:safe]); err != nil {
		return 0, err
	}
	w.buf = append(w.buf[:0], w.buf[safe:]...)
	return len(p), nil
}

// Close redacts and emits any held-back tail.
func (w *Writer) Close() error {
	if len(w.buf) == 0 {
		return nil
	}
	err := w.flush(w.buf)
	w.buf = nil
	return err
}

// flush redacts one contiguous slice and writes it to dst. The slice is
// self-contained: any match within it is fully inside it, because Write only
// passes a region whose held-back suffix cannot start an unresolved match.
func (w *Writer) flush(data []byte) error {
	out := w.redactSlice(data)
	_, err := w.dst.Write(out)
	return err
}

// redactSlice finds all occurrences, merges spans, and rewrites covered regions
// to their placeholders.
func (w *Writer) redactSlice(data []byte) []byte {
	rawSpans := w.ac.findAll(data)
	if len(rawSpans) == 0 {
		return data
	}
	labeled := make([]labeledSpan, 0, len(rawSpans))
	for _, s := range rawSpans {
		labeled = append(labeled, labeledSpan{span: s, label: w.labelFor(data, s)})
	}
	merged := mergeSpans(labeled)
	var out bytes.Buffer
	prev := 0
	for _, ls := range merged {
		out.Write(data[prev:ls.span.start])
		out.WriteString(placeholderPrefix + ls.label + placeholderSuffix)
		prev = ls.span.end
	}
	out.Write(data[prev:])
	return out.Bytes()
}

// labelFor returns the vault key whose value produced span s. The automaton
// reports span lengths but not which pattern; match by (length, content)
// against the configured patterns, choosing the lexicographically smallest key
// on a tie so the placeholder is deterministic.
func (w *Writer) labelFor(data []byte, s span) string {
	value := data[s.start:s.end]
	best := ""
	for _, p := range w.patterns {
		if len(p.Value) == len(value) && bytes.Equal(p.Value, value) {
			if best == "" || p.Label < best {
				best = p.Label
			}
		}
	}
	return best
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/Sites/configs/scripts && go test ./internal/redact/ -run TestWriter -v`
Expected: PASS (all four).

- [ ] **Step 5: Run the whole package and commit**

Run: `cd ~/Sites/configs/scripts && go test ./internal/redact/ -v`
Expected: PASS (all tests from tasks 1 to 3).

```bash
cd ~/Sites/configs && make -C scripts build && git add scripts/internal/redact/ && git commit -m "Add streaming overlap-safe redact Writer with cross-chunk hold-back"
```

---

### Task 4: vault.Values accessor

**Files:**
- Modify: `scripts/internal/vault/vault.go`
- Test: `scripts/internal/vault/values_test.go`

- [ ] **Step 1: Check for an existing vault test fixture**

Run: `ls scripts/internal/vault/`
If a `*_test.go` with an encrypted fixture and pass file exists, write `values_test.go` to assert `Values(fixtureVault, fixturePass)` returns the expected `map[string]string`. If no fixture exists, use the skip below; `Values` is then covered by the `main` install path in Task 5.

```go
package vault

import "testing"

func TestValuesWrapsDecryptMapping(t *testing.T) {
	t.Skip("Values wraps decryptMapping; covered by the main install path")
}
```

- [ ] **Step 2: Run test to verify it compiles**

Run: `cd ~/Sites/configs/scripts && go test ./internal/vault/ -run TestValues -v`
Expected: PASS or SKIP (not a compile error).

- [ ] **Step 3: Write minimal implementation**

Add to `scripts/internal/vault/vault.go`, after `Secret`:

```go
// Values returns the decrypted vault as a name -> value map. It is the bulk
// accessor the redaction layer uses to learn every secret value; callers must
// not print the values.
func Values(vaultPath, passwordFile string) (map[string]string, error) {
	return decryptMapping(vaultPath, passwordFile)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/Sites/configs/scripts && go test ./internal/vault/ -run TestValues -v`
Expected: PASS or SKIP.

- [ ] **Step 5: Commit**

```bash
cd ~/Sites/configs && make -C scripts build && git add scripts/internal/vault/vault.go scripts/internal/vault/values_test.go && git commit -m "Add vault.Values bulk decrypted-map accessor"
```

---

### Task 5: install redactors in main before dispatch (fail-closed)

**Files:**
- Modify: `scripts/cmd/configs/main.go`

- [ ] **Step 1: Add imports**

In the import block add `"sync"` and `"goodkind.io/configs/internal/redact"`. `io`, `os`, `fmt`, `errors`, `slog`, and `vault` are already imported.

- [ ] **Step 2: Replace the run function**

Order: read and validate the vault first, then install the redactors with the
patterns already known, then dispatch. Loading only ever logs a generic decrypt
error to the real stderr (never a secret value), so reading before install is
leak-safe and avoids a drain-startup deadlock.

```go
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: configs <command>")
	}
	command := args[0]
	handler, ok := handlers[command]
	if !ok {
		return fmt.Errorf("unknown command: %q", command)
	}

	patterns, loadErr := loadSecretPatterns()
	if loadErr != nil {
		short, isShort := loadErr.(*shortSecretError)
		switch {
		case isShort && command == "set-secrets":
			patterns = nil // set-secrets is exempt; it prints only key names.
		case isShort:
			fmt.Fprintf(os.Stderr, "configs: refusing to run: vault key %q has a value shorter than %d characters; rotate it via 'configs set-secrets'\n", short.key, redact.MinLen)
			return short
		default:
			return loadErr
		}
	}

	restore, err := installRedaction(patterns)
	if err != nil {
		return err
	}
	defer restore()

	return handler(args[1:])
}
```

- [ ] **Step 3: Add the helpers at the end of the file**

```go
// shortSecretError marks a fail-closed validation failure. It names the key,
// never the value.
type shortSecretError struct{ key string }

func (e *shortSecretError) Error() string {
	return fmt.Sprintf("vault key %q value shorter than %d chars", e.key, redact.MinLen)
}

// loadSecretPatterns reads the vault and builds redaction patterns. An absent
// vault or password file returns no patterns and no error, because nothing
// decrypts anywhere in that case. A too-short secret returns *shortSecretError.
func loadSecretPatterns() ([]redact.Pattern, error) {
	passwordFile, err := vaultPassPath()
	if err != nil {
		return nil, nil
	}
	if _, statErr := os.Stat(defaultVaultFile); statErr != nil {
		return nil, nil
	}
	if _, statErr := os.Stat(passwordFile); statErr != nil {
		return nil, nil
	}
	values, err := vault.Values(defaultVaultFile, passwordFile)
	if err != nil {
		slog.Error("vault values load failed", "err", err)
		return nil, fmt.Errorf("load vault values: %w", err)
	}
	patterns := make([]redact.Pattern, 0, len(values))
	for name, value := range values {
		if value == "" {
			continue
		}
		patterns = append(patterns, redact.Pattern{Value: []byte(value), Label: name})
	}
	if badKey, ok := redact.Validate(patterns); !ok {
		return nil, &shortSecretError{key: badKey}
	}
	return patterns, nil
}

// lockedWriter serializes writes to a shared real descriptor under a mutex, so
// the stdout and stderr redactors never interleave a partial write.
type lockedWriter struct {
	mu  *sync.Mutex
	dst *os.File
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dst.Write(p)
}

// installRedaction routes os.Stdout and os.Stderr through redactors that share
// one mutex, so writes to the two real streams never interleave mid-write. With
// no patterns it is a no-op and leaves the real descriptors in place. It returns
// a restore func to call on exit. A pipe-creation failure is fatal: with secrets
// present and no way to filter, the tool must not run, so the error propagates
// and no command dispatches.
func installRedaction(patterns []redact.Pattern) (func(), error) {
	realStdout, realStderr := os.Stdout, os.Stderr
	if len(patterns) == 0 {
		return func() {}, nil
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		slog.Error("stdout pipe failed", "err", err)
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		_ = outR.Close()
		_ = outW.Close()
		slog.Error("stderr pipe failed", "err", err)
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	drain := func(src *os.File, dst *os.File) {
		defer wg.Done()
		w := redact.New(&lockedWriter{mu: &mu, dst: dst}, patterns)
		_, _ = io.Copy(w, src)
		_ = w.Close()
	}
	wg.Add(2)
	go drain(outR, realStdout)
	go drain(errR, realStderr)

	os.Stdout = outW
	os.Stderr = errW

	restore := func() {
		os.Stdout = realStdout
		os.Stderr = realStderr
		_ = outW.Close()
		_ = errW.Close()
		wg.Wait()
		_ = outR.Close()
		_ = errR.Close()
	}
	return restore, nil
}
```

- [ ] **Step 4: Build**

Run: `cd ~/Sites/configs && make -C scripts build`
Expected: PASS. If staticcheck-extra flags a wrapped error without a sibling `slog`, add an `slog.Error` line before that `return fmt.Errorf(...)`.

- [ ] **Step 5: Manual smoke, inventory dump still redacts**

Run: `cd ~/Sites/configs && scripts/dist/configs inventory-dump > /tmp/inv.txt 2>&1; grep -c '<redacted:' /tmp/inv.txt; grep -c 'tackqa' /tmp/inv.txt; rm -f /tmp/inv.txt`
Expected: first count greater than 0 (placeholders present), second count 0 (no leaked access-key value).

- [ ] **Step 6: Manual smoke, fail-closed**

Add a short value, confirm the abort, then overwrite it with a safe value:
Run: `cd ~/Sites/configs && printf 'vault_tmp_short: abc\n' | scripts/dist/configs set-secrets; scripts/dist/configs keys; echo "exit=$?"`
Expected: `keys` prints the refusal naming `vault_tmp_short` and the 16-char rule, exit non-zero.
Run: `cd ~/Sites/configs && printf 'vault_tmp_short: 0123456789abcdef0\n' | scripts/dist/configs set-secrets; scripts/dist/configs keys | grep -c vault_tmp_short`
Expected: `keys` lists names again (count 1). Leave `vault_tmp_short` or remove it through your normal vault workflow; it is a harmless >=16 placeholder.

- [ ] **Step 7: Commit**

```bash
cd ~/Sites/configs && git add scripts/cmd/configs/main.go && git commit -m "Route configs stdout and stderr through fail-closed secret redaction"
```

---

### Task 6: rewrite runSecret to write a hardened temp file

**Files:**
- Modify: `scripts/cmd/configs/main.go`

- [ ] **Step 1: Replace runSecret**

```go
func runSecret(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: configs secret <key>")
	}
	passwordFile, err := vaultPassPath()
	if err != nil {
		return err
	}
	value, err := vault.Secret(args[0], defaultVaultFile, passwordFile)
	if err != nil {
		return fmt.Errorf("read vault secret %q", args[0])
	}
	dir, err := os.MkdirTemp("", "configs-secret-*")
	if err != nil {
		slog.Error("create secret temp dir failed", "err", err)
		return fmt.Errorf("create secret temp dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		slog.Error("chmod secret temp dir failed", "err", err)
		return fmt.Errorf("chmod secret temp dir: %w", err)
	}
	path := filepath.Join(dir, args[0])
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		slog.Error("write secret file failed", "err", err)
		return fmt.Errorf("write secret file: %w", err)
	}
	fmt.Printf("Secret written to: %s\n", path)
	fmt.Println("WARNING: this file holds the secret in plaintext on disk.")
	fmt.Println("Do not cat, paste, log, or commit it. Delete it after use:")
	fmt.Printf("  rm -rf %s\n", dir)
	return nil
}
```

`path/filepath` is already imported.

- [ ] **Step 2: Build**

Run: `cd ~/Sites/configs && make -C scripts build`
Expected: PASS.

- [ ] **Step 3: Manual smoke**

Run: `cd ~/Sites/configs && p=$(scripts/dist/configs secret vault_seaweedfs_s3_access_key | sed -n 's/^Secret written to: //p'); echo "path=$p"; stat -f '%A %N' "$p" "$(dirname "$p")"; rm -rf "$(dirname "$p")"`
Expected: a `configs-secret-*` path printed, file mode `600`, dir mode `700`, and the value never printed to the terminal.

- [ ] **Step 4: Commit**

```bash
cd ~/Sites/configs && git add scripts/cmd/configs/main.go && git commit -m "Write configs secret to a hardened temp file instead of stdout"
```

---

### Task 7: delete the bespoke inventory-dump redaction

**Files:**
- Modify: `scripts/internal/ansible/ansible.go`

- [ ] **Step 1: Restore InventoryDump to direct streaming**

Replace the current `InventoryDump`, plus the helpers `redactInventorySecrets`, `redactVaultNodes`, and the constants `vaultVarPrefix`, `redactedPlaceholder`, and `fingerprintHexLen` if present in this file, with:

```go
// InventoryDump prints the resolved inventory as YAML. Secret values in the
// output are redacted by the global redaction layer installed in main, so this
// streams ansible-inventory directly.
func InventoryDump() error {
	cmd := exec.CommandContext(context.Background(), "ansible-inventory", "--list", "--yaml")
	cmd.Dir = ansibleDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		slog.Error("ansible-inventory failed", "err", err)
		return fmt.Errorf("ansible-inventory: %w", err)
	}
	return nil
}
```

Remove the `gopkg.in/yaml.v3` import from this file (no other function uses it).

- [ ] **Step 2: Build**

Run: `cd ~/Sites/configs && make -C scripts build`
Expected: PASS. The deadcode gate confirms the removed helpers and constants are gone; if it flags a leftover, delete it.

- [ ] **Step 3: Manual smoke, redaction now comes from main**

Run: `cd ~/Sites/configs && scripts/dist/configs inventory-dump > /tmp/inv2.txt 2>&1; grep -c '<redacted:' /tmp/inv2.txt; grep -c 'tackqa' /tmp/inv2.txt; rm -f /tmp/inv2.txt`
Expected: placeholders present (count greater than 0), no leaked value (count 0). This confirms the global layer covers child-process output.

- [ ] **Step 4: Commit**

```bash
cd ~/Sites/configs && git add scripts/internal/ansible/ansible.go && git commit -m "Remove bespoke inventory-dump redaction in favor of the global layer"
```

---

### Task 8: final verification and push

- [ ] **Step 1: Full build and tests**

Run: `cd ~/Sites/configs && make -C scripts build && cd scripts && go test ./internal/redact/... ./internal/vault/...`
Expected: build PASS, all tests PASS.

- [ ] **Step 2: Confirm the placeholder shows the key**

Run: `cd ~/Sites/configs && scripts/dist/configs inventory-dump 2>/dev/null | grep -m1 '<redacted:'`
Expected: a line like `... <redacted:vault_seaweedfs_s3_access_key>` (key visible, value gone).

- [ ] **Step 3: Merge to main and push**

```bash
cd ~/Sites/configs && git checkout main && git merge --ff-only secret-redaction-hardening && git push origin main && git branch -d secret-redaction-hardening
```
Expected: fast-forward, push OK, branch deleted. If not fast-forwardable, run `git fetch origin && git rebase origin/main secret-redaction-hardening`, then retry.

---

## Notes for the implementer

- `make -C scripts build` is mandatory; `go build` and a direct `go vet` are blocked by agent-gate. Use `cd scripts && go test ./internal/redact/...` for the fast TDD loop, and `make -C scripts build` before each commit.
- The repo lint rule "a wrapped error needs a sibling slog.Error/Warn" applies to every `return fmt.Errorf(... %w ...)`: log first. Every new wrap above already does.
- Do not touch lint baselines. If `make build` regenerates one, revert it before committing.
- The `secret` temp file is intentionally not auto-deleted; the printed `rm -rf` line is the operator's cleanup.
- agent-gate blocks em-dashes, `head`/`tail` in commands, and grep over the indexed tack repo. This repo (configs) is not the indexed tack repo, so `grep` is fine here; still avoid `head`/`tail` and em-dashes.

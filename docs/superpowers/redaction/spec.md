# Configs secret-redaction hardening

## Goal

No `configs` subcommand may print a vault secret to stdout or stderr. A single,
generic, fail-closed redaction layer is installed once in `main` and filters all
output, so the protection is unbypassable for every current and future command
without per-command wiring. The `secret` command stops printing the value to the
terminal and instead writes it to a hardened temp file.

## Non-goals

- Redacting secrets the tool writes to files it manages on remote hosts (the
  rendered `.env`, `s3.json`); those land on the target host by design. This spec
  covers the tool's own stdout/stderr only.
- Protecting against an operator who deliberately reads the `secret` temp file
  and exposes it. The tool hardens perms and warns; downstream handling is the
  operator's responsibility.
- Changing how ansible logs on the remote host.

## Definitions

- "Secret value" is any non-empty value held in the vault file
  (`ansible/inventory/group_vars/all/vault.yml`). Detection keys off decrypted
  values, not names, so a secret copied into a differently named variable or
  embedded in a larger string is still caught.
- `MinLen = 16`. A non-empty secret value shorter than this cannot be redacted
  safely, so its presence triggers fail-closed.
- Placeholder is `<redacted:KEY>`, where `KEY` is the vault key whose value
  matched. Key names are not secret. If a value maps to more than one key, the
  lexicographically first key is used, deterministically.

## Architecture

Three units, each independently testable.

### 1. `internal/redact` (generic, no vault knowledge)

A streaming, multi-pattern redactor over an arbitrary `io.Writer`, backed by an
Aho-Corasick automaton so finding matches is a single pass independent of how
many secrets there are, plus an interval-merge step so overlapping secrets can
never leak a fragment.

- `New(dst io.Writer, patterns []Pattern) *Writer`, where a `Pattern` is a
  `{Value []byte, Label string}` pair. The writer replaces every covered region
  with `<redacted:Label>`.
- Construction builds the Aho-Corasick goto, failure, and output links once from
  all pattern values. Writing scans incoming bytes through the automaton, which
  reports the end position and length of every pattern occurrence.
- Overlap safety by merge: the occurrences are collected as `[start,end)` spans
  and merged when they overlap or touch, then each merged span is redacted as one
  unit. This is the property a plain longest-match or `bytes.ReplaceAll` lacks:
  when secret A's suffix is secret B's prefix and they sit adjacent, neither
  byte of either secret survives, because the two spans merge into one redaction.
  The label on a merged span is the label of its earliest-starting occurrence
  (ties broken by lexicographically first label) so the placeholder is
  deterministic.
- Streaming-safe: the automaton state persists across `Write` calls, and the
  writer holds back the trailing bytes that could still be part of an
  in-progress match or an unresolved merge (at most `maxPatternLen-1` bytes)
  until a later write or `Close` resolves them, so a secret split across two
  writes is still matched.
- An empty pattern set is a transparent passthrough.
- `Validate(patterns []Pattern) (badKey string, ok bool)` reports the first
  pattern whose non-empty value is shorter than `MinLen`, returning its label so
  the caller can build an error that names a key, never a value.

The package depends on nothing in this repo and is fully unit tested without a
vault. A test asserts the overlap case (`SECRETab` + `abVALUE` over
`xSECRETabVALUEx` yields no surviving `VALUE`), which a non-merging matcher
fails.

### 2. Secret source (vault side)

- Add `vault.Values(vaultPath, passwordFile string) (map[string]string, error)`
  returning the decrypted name -> value map, reusing the existing
  `decryptMapping`.
- `main` inverts that into redact `Pattern`s (value plus key label), skipping
  empty values, and resolves a duplicate value to its lexicographically first
  key.

### 3. The install point (`main.run`): read, then hide, then run

Order matters for fail-closed: the secret set is read and validated before any
subcommand runs, so a too-short secret or an unreadable vault stops every
command (including the side-effecting `deploy`) before it does any work. Reading
before installing the pipes is leak-safe because the load step only ever logs a
generic decrypt error (never a secret value) to the still-real stderr, and it
avoids a drain-startup deadlock that an install-first ordering would create when
the patterns never arrive on the fail-closed path.

1. Decrypt the vault, build `Pattern`s, and run `Validate`. This read takes a few
   milliseconds.
   - On a fail-closed result (a non-empty value shorter than `MinLen`, or an
     existing vault that will not decrypt): write the key-named error to stderr
     and exit non-zero, before installing anything or dispatching.
   - `set-secrets` is exempt: it reads the new value from stdin and prints only
     key names, so it cannot leak a value, and it is the path that rotates a
     too-short secret. For it, a fail-closed result is downgraded to running with
     an empty pattern set instead of aborting.
2. Install the redactors with the known patterns: replace `os.Stdout` and
   `os.Stderr` with the write ends of two OS pipes, and start two drain
   goroutines that stream each pipe through a `redact.Writer` into the saved real
   descriptor. The two writers share one mutex so a flush to a real descriptor is
   never interleaved mid-write with the other stream, preserving readable
   ordering. An empty pattern set is a no-op that leaves the real descriptors in
   place. A pipe-creation failure is fatal: with secrets present and no way to
   filter, the tool must not run, so the error propagates and no command
   dispatches.
3. Dispatch the subcommand. `inventory-dump` and `deploy --diff` need no
   command-specific code; their output and their child processes' output (via
   `cmd.Stdout = os.Stdout`) flow through the pipes.
4. On return, close the pipe write ends, wait for the drains to flush their held
   bytes, and restore the real descriptors so nothing is dropped or leaked.

The bespoke `inventory-dump` redaction added earlier is deleted; this layer
replaces it, and `InventoryDump` returns to streaming `ansible-inventory` output
directly.

### 4. `secret` command -> hardened temp file

`runSecret` no longer prints the value. It:

1. Creates a private temp dir in the host-native temp location
   (`os.MkdirTemp(os.TempDir(), "configs-secret-*")`) and chmods it `0700`.
2. Writes the value to a file named after the key inside that dir, chmodded
   `0600`. Vault keys are `vault_[A-Za-z0-9_]`, so the name is filesystem-safe.
3. Prints the file path and a strong, multi-line warning: plaintext on disk; do
   not cat, paste, log, or commit it; delete it after use.
4. Does not auto-delete; the caller needs to read it.

The value never reaches stdout, so the global redactor needs no carve-out for
`secret`; its path-and-warning output contains no secret and passes through.

## Fail-closed semantics

- Vault present and a non-empty value shorter than `MinLen`: validation fails
  before dispatch, the tool prints a key-named error to the real stderr and exits
  non-zero, for every command except `set-secrets`. Because the check precedes
  dispatch, a side-effecting command like `deploy` never starts.
- Vault present but undecryptable (wrong or missing password against an existing
  vault): same fail-closed path. Printing nothing is safer than risking
  unredacted output, and ansible would fail the same way.
- Vault and password file both absent: empty pattern set, tool runs normally,
  because nothing decrypts anywhere in that case.
- Teardown always flushes the redactors before process exit.

## Error handling

- New wrapped errors carry context and are logged once at their origin, per the
  repo log-and-wrap lint rule.
- No error message contains a secret value. A too-short secret is reported by its
  key label only.

## Testing

- `internal/redact` table-driven unit tests: single match; multi-line value; a
  secret split across two `Write` calls (persisted automaton state plus
  hold-back); overlapping secrets where one is a substring of another
  (leftmost-longest); duplicate value mapping to the first key label; empty
  pattern set passthrough; `Validate` accepting `>= MinLen`, rejecting a shorter
  non-empty value, and ignoring an empty value.
- An ordering test: concurrent stdout and stderr writes are never interleaved
  mid-write under the shared mutex.
- A fail-closed test: with a too-short secret in the vault, a dispatched command
  is never reached (validation returns the key-named error before dispatch).
- A `secret` command test: the written file is `0600`, its dir `0700`, the value
  is in the file, and the value is absent from stdout.

## Migration and caller impact

- No caller captures `configs secret` stdout (verified: zero invocations across
  scripts, Makefiles, Rakefile, and tofu), so moving the value to a file breaks
  no automation.
- `inventory-dump` output keeps its shape; secret values now render as
  `<redacted:KEY>` via the shared layer.

## Resolved decisions

- Mechanism: global `os.Stdout`/`os.Stderr` wrap installed in `main`.
- Detection: value-based, tool-wide, via an Aho-Corasick automaton that finds all
  occurrence spans, which are then merged so overlapping secrets cannot leak.
- Load timing: synchronously read and validate the vault first, then install the
  redactors with the known patterns, then dispatch, so fail-closed precedes any
  command's work and the drains never deadlock waiting for patterns.
- Streams: stdout and stderr redacted independently, ordering-locked by a shared
  mutex.
- Placeholder: `<redacted:KEY>`.
- `MinLen`: 16.
- Short-secret gate: fail-closed, with `set-secrets` exempt.
- `secret` output: host-native secure temp dir (`0700`) plus `0600` file, no
  auto-delete, printed path and warning.

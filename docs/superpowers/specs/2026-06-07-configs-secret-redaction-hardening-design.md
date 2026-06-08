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
Aho-Corasick automaton so matching is a single pass independent of how many
secrets there are.

- `New(dst io.Writer, patterns []Pattern) *Writer`, where a `Pattern` is a
  `{Value []byte, Label string}` pair. The writer replaces every occurrence of
  any `Value` with `<redacted:Label>`.
- Construction builds the Aho-Corasick goto, failure, and output links once from
  all pattern values. Writing scans incoming bytes through the automaton.
- Streaming-safe: the automaton state persists across `Write` calls, and the
  writer holds back the trailing bytes that are still a live prefix of some
  pattern (at most `maxPatternLen-1` bytes) until a later write or `Close`
  resolves them, so a secret split across two writes is still matched.
- Match resolution is leftmost-longest: at each position the longest pattern
  ending there wins, and emitted output never contains a fragment of a matched
  secret, including when one secret is a substring of another.
- An empty pattern set is a transparent passthrough.
- `Validate(patterns []Pattern) (badKey string, ok bool)` reports the first
  pattern whose non-empty value is shorter than `MinLen`, returning its label so
  the caller can build an error that names a key, never a value.

The package depends on nothing in this repo and is fully unit tested without a
vault.

### 2. Secret source (vault side)

- Add `vault.Values(vaultPath, passwordFile string) (map[string]string, error)`
  returning the decrypted name -> value map, reusing the existing
  `decryptMapping`.
- `main` inverts that into redact `Pattern`s (value plus key label), skipping
  empty values, and resolves a duplicate value to its lexicographically first
  key.

### 3. The install point (`main.run`) with a lazy barrier

Output protection is installed before any command runs, but the vault decrypt
happens concurrently behind a barrier so output is never emitted before the
secret set is ready.

1. Immediately replace `os.Stdout` and `os.Stderr` with the write ends of two OS
   pipes. From this point nothing can write around the redactor.
2. Start one background loader: decrypt the vault, build `Pattern`s, run
   `Validate`. The result is a ready-signal carrying either the built automaton
   or a fail-closed error.
3. Start two drain goroutines, one per pipe. Each blocks before emitting its
   first byte until the loader signals. On success it streams its pipe through a
   `redact.Writer`; the two writers share one mutex so a flush to a real
   descriptor is never interleaved mid-write with the other stream, preserving
   readable ordering. On loader failure the drains discard any buffered bytes
   (which may contain secrets), the tool writes the key-named error to the saved
   real stderr, and exits non-zero.
4. Dispatch the subcommand. `inventory-dump` and `deploy --diff` need no
   command-specific code; their output and their child processes' output (via
   `cmd.Stdout = os.Stdout`) flow through the pipes.
5. On return, close the pipe write ends, wait for the drains to flush their held
   bytes, and restore the real descriptors so nothing is dropped or leaked.

`set-secrets` is exempt from the fail-closed abort: it reads the new value from
stdin and prints only key names, so it cannot leak a value, and it is the path
that rotates a too-short secret. Under the barrier this means its drains proceed
with an empty pattern set rather than aborting when the loader reports a
too-short secret.

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

- Vault present and a non-empty value shorter than `MinLen`: the loader returns a
  fail-closed error, the drains discard buffered output, the tool prints a
  key-named error to the real stderr and exits non-zero, for every command
  except `set-secrets`.
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
- A barrier test: output written before the loader signals is held, then either
  redacted on success or discarded on a fail-closed load, and never emitted raw.
- An ordering test: concurrent stdout and stderr writes are never interleaved
  mid-write under the shared mutex.
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
- Detection: value-based, tool-wide, via an Aho-Corasick automaton.
- Load timing: lazy background loader behind an output barrier.
- Streams: stdout and stderr redacted independently, ordering-locked by a shared
  mutex.
- Placeholder: `<redacted:KEY>`.
- `MinLen`: 16.
- Short-secret gate: fail-closed, with `set-secrets` exempt.
- `secret` output: host-native secure temp dir (`0700`) plus `0600` file, no
  auto-delete, printed path and warning.

# Configs secret-redaction hardening

## Goal

No `configs` subcommand may print a vault secret to stdout or stderr. A single,
generic, fail-closed redaction layer is installed once in `main` and filters all
output, so the protection is unbypassable for every current and future command
without per-command wiring. The `secret` command stops printing the value to the
terminal and instead writes it to a hardened temp file.

## Non-goals

- Redacting secrets that the tool writes to files it manages on remote hosts
  (the rendered `.env`, `s3.json`); those land on the target host by design. This
  spec covers the tool's own stdout/stderr only.
- Protecting against an operator who deliberately reads the `secret` temp file
  and pastes it somewhere. The tool hardens perms and warns; downstream handling
  is the operator's responsibility.
- Changing how ansible itself logs on the remote host.

## Definitions

- "Secret value" is any non-empty value held in the vault file
  (`ansible/inventory/group_vars/all/vault.yml`). Every secret in this repo lives
  there as a `vault_*` key, and the protection keys off the decrypted values, not
  the names, so a secret copied into a differently named variable or embedded in
  a larger string is still caught.
- `MinLen = 16`. A non-empty secret value shorter than this cannot be redacted
  safely (short values risk mangling unrelated output), so its presence triggers
  fail-closed.

## Architecture

Three units, each independently testable.

### 1. `internal/redact` (generic, no vault knowledge)

A streaming redactor over an arbitrary `io.Writer`.

- `New(dst io.Writer, secrets [][]byte) *Writer` returns a writer that replaces
  every occurrence of any secret byte sequence with the literal `<redacted>`.
- The writer is streaming-safe: it retains a tail of `maxSecretLen-1` bytes
  between `Write` calls so a secret split across two writes is still matched, and
  emits the retained tail on `Close`.
- Secrets are matched longest-first, so when one secret is a substring of
  another the longer match wins and no fragment of the longer secret survives.
- An empty secret set is a transparent passthrough.
- `Validate(secrets [][]byte) error` reports the first non-empty secret shorter
  than `MinLen`. It receives only values; the caller maps a failure back to a key
  name for the error message, so no value reaches an error string.

The package depends on nothing in this repo. It is reusable and fully unit
tested without a vault.

### 2. Secret source (vault side)

- Add `vault.Values(vaultPath, passwordFile string) (map[string]string, error)`
  that returns the decrypted name -> value map, reusing the existing
  `decryptMapping`.
- `main` builds the secret list from the values, skipping empty values.

### 3. The install point (`main.run`)

Before dispatching any subcommand:

1. Load the vault values. If the vault file or password file is absent, the set
   is empty and the tool runs normally, because nothing decrypts anywhere in that
   case (ansible cannot decrypt either, so no secret can appear in output).
2. Run `redact.Validate`. If a non-empty value is shorter than `MinLen`, abort
   with an error that names the offending **key** (never the value). Exception:
   the `set-secrets` subcommand runs anyway (see Fail-closed semantics).
3. Install the redactor: create OS pipes, point `os.Stdout` and `os.Stderr` at
   the pipe write ends, and drain each pipe through a `redact.Writer` into the
   saved real file descriptor. Child processes inherit the pipe because they are
   started with `cmd.Stdout = os.Stdout`, so `inventory-dump` and `deploy --diff`
   are both covered with no command-specific code.
4. Dispatch the subcommand.
5. On return, close the pipe write ends, wait for the drains to flush their
   tails, and restore the real descriptors before exit, so no buffered tail is
   dropped or leaked.

The bespoke `inventory-dump` redaction added earlier is deleted; this layer
replaces it, and `InventoryDump` returns to streaming `ansible-inventory` output
directly (now filtered by the install).

### 4. `secret` command -> hardened temp file

`runSecret` no longer prints the value. It:

1. Creates a private temp directory via the host-native temp location
   (`os.MkdirTemp(os.TempDir(), "configs-secret-*")`) and chmods it to `0700`.
2. Writes the value to a file named after the key inside that dir, chmodded to
   `0600`.
3. Prints the file path and a strong, multi-line warning: the file is plaintext
   on disk, do not cat, paste, log, or commit it, and delete it after use.
4. Does not auto-delete; the caller needs to read it.

Because the value never reaches stdout, the global redactor needs no carve-out
for `secret`; its path-and-warning output contains no secret and passes through
unchanged.

## Fail-closed semantics

- Vault present and a non-empty value shorter than `MinLen`: every command aborts
  with a key-named error, **except** `set-secrets`. `set-secrets` reads the new
  value from stdin and prints only key names, so it cannot leak a value, and it
  is the path that rotates the offending secret. Without this exemption a short
  secret would be unfixable through the tool.
- Vault present but undecryptable (wrong or missing password against an existing
  vault): abort. Printing nothing is safer than risking unredacted output, and
  ansible would fail the same way.
- Vault and password file both absent: empty secret set, tool runs normally.
- Teardown always flushes the redactor before process exit.

## Error handling

- All new wrapped errors carry context and are logged once at their origin, per
  the repo's log-and-wrap lint rule.
- Error messages never contain a secret value. A too-short secret is reported by
  its key name only.

## Testing

- `internal/redact` table-driven unit tests: single scalar match; multi-line
  value; a secret split across two `Write` calls; overlapping secrets where one
  is a substring of another (longest-first); empty secret set passthrough;
  `Validate` accepting `>= MinLen` and rejecting a shorter non-empty value while
  ignoring an empty value.
- A focused test for the `secret` command asserting the written file is `0600`,
  its parent dir `0700`, the value is in the file, and the value is absent from
  stdout.
- The `main` install path mutates process `os.Stdout`; its logic is thin glue, so
  coverage lives in the `redact` unit tests rather than a process-global test.

## Migration and caller impact

- No caller captures `configs secret` stdout (verified: zero invocations across
  scripts, Makefiles, Rakefile, and tofu), so moving the value to a file breaks
  no automation.
- `inventory-dump` output is unchanged in shape; secret values render as
  `<redacted>` exactly as before, now via the shared layer.

## Resolved decisions

- Mechanism: global `os.Stdout`/`os.Stderr` wrap installed in `main`.
- Detection: value-based (decrypted vault values), applied tool-wide.
- `MinLen`: 16.
- Short-secret gate: fail-closed, with `set-secrets` exempt.
- `secret` output: host-native secure temp dir (`0700`) plus `0600` file, no
  auto-delete, printed path and warning.

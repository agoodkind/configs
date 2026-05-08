# mwexec_bg / mwexec deprecation scan (os-frr 1.51, os-tayga 1.5)

Date: 2026-05-08
Scope: MWAN-151 follow-up R9. Scan the two third-party OPNsense plugins
that we deploy (os-frr 1.51, os-tayga 1.5) for any remaining call to the
deprecated PHP wrappers `mwexec()` and `mwexec_bg()` that the OPNsense
26.1 release notes scheduled for removal during the 26.1.x lifecycle.
Read-only investigation, no source changes.

The result, stated up front so the rest of the document can be skimmed:
both plugins are clean. os-frr 1.51 contains zero calls to either
deprecated function and uses the safe replacement `mwexecfm()` in the
three places it shells out. os-tayga 1.5 contains zero references to
the `mwexec` family at all. Our own `mwan/go/` tree contains zero
references. The R9 risk in MWAN-151 can be closed as not applicable.

The scan also picked up the wider context that the deprecation has
already landed: OPNsense core 26.1.3 (released 2026-03-04) physically
removed the two functions, so any plugin still using them would already
be broken on a fully patched 26.1.3+ box, not a future 26.1.x release.

---

## 1. What `mwexec` and `mwexec_bg` are

`mwexec()` and `mwexec_bg()` are PHP wrappers in OPNsense's
`/usr/local/etc/inc/util.inc` that shell out to a command line. They
predate the structured `OPNsense\Core\Shell` API. The 25.7 stable
branch still carries them, both annotated `XXX deprecated`:

- `mwexec($command, $mute = false)` runs the literal command string via
  PHP `exec()`, captures the exit code, and logs the failure unless
  `$mute` is true. Source: `opnsense/core` `stable/25.7` branch,
  `src/etc/inc/util.inc` lines 925-942 (the function and its closing
  brace).
- `mwexec_bg($command, $mute = false)` is a thin shim that prefixes the
  command with `/usr/sbin/daemon -f` and then calls `mwexec()`. Source:
  same file, lines 944-947.

The intended replacements are the printf-style `mwexecf` family that
delegates to `OPNsense\Core\Shell::run_safe`, which escapes arguments
through `escapeshellarg()` rather than substituting them directly into
the command string:

- `mwexecf($format, $args, $mute)` runs a `printf`-formatted command,
  failure-logged.
- `mwexecfm($format, $args)` runs a `printf`-formatted command, failure
  silently muted.
- `mwexecfb($format, $args, $opts)` runs in the background via
  `daemon(8)` with optional pidfile, logfile, and restart support.

The migration message attached to the deprecation is therefore "use
`mwexecf()`, `mwexecfm()`, or `mwexecfb()` instead". The safety
argument is that the new API takes the command and its arguments
separately and escapes the arguments, while the old API took a
single pre-built string and was vulnerable to shell injection if any
argument contained metacharacters. See `26.1.r1` migration notes
(quoted in section 2 below).

The difference between blocking and background:

- `mwexec()` blocks the PHP request until the child process exits and
  returns its exit code. Suitable for fast service-control commands.
- `mwexec_bg()` returns immediately because the command is wrapped in
  `daemon -f`. Suitable for long-running processes the GUI does not
  want to wait on. The replacement is `mwexecfb()`, not `mwexecfm()`.

## 2. Why the deprecation matters for us

The 26.1 release-notes migration block explicitly schedules the two
functions for removal during the 26.1.x lifecycle:

> Due to command line execution safety concerns the historic functions
> mwexec_bg() and mwexec() will be removed in 26.1.x. Make sure your
> custom code is not using them and use mwexecf(), mwexecfb() and
> mwexecfm() instead.

Source: `opnsense/changelog` repo, `community/26.1/26.1.r1` line 40 and
`community/26.1/26.1` line 119. The same paragraph also appears in
the 26.1.r2 file at line 42, so it was carried through release-candidate
notes. The text quoted above is reproduced from those changelog
entries and not edited.

The removal landed in 26.1.3 on 2026-03-04, ahead of the rest of the
26.1.x window:

> backend: removed mwexec() and mwexec_bg() functions following their
> deprecation

Source: `opnsense/changelog`, `community/26.1/26.1.3` line 66.

I confirmed the removal in core source. On `opnsense/core` `master`
(commit `f648476a665117e37f6693b55fdbc701677e33eb`, 2026-05-07), a
recursive grep for `\bmwexec_bg\b` and a `\bmwexec\(` pattern (excluding
`mwexecf`, `mwexecfm`, `mwexecfb`) over `src/` returns zero hits, and
`src/etc/inc/util.inc` lines 944-986 only define `mwexecf`, `mwexecfm`,
and `mwexecfb`. So the deprecated functions are gone from current core,
and any plugin that still calls them by literal name would already
be broken on 26.1.3+, not in a hypothetical future drop.

## 3. os-frr 1.51 scan

I identified os-frr 1.51 as commit `fd6d2de572aa0714406217e8f3486c19df8f622e`
in `opnsense/plugins`, which is the parent of `d0d9a7ffb` (the
1.51 -> 1.52 version bump on 2026-03-27). I extracted just `net/frr/`
from that commit and ran the grep scan over the full tree.

Patterns scanned (case-sensitive):

- `mwexec_bg` (any occurrence)
- `mwexec[[:space:]]*\(` (call sites of bare `mwexec`)
- bare token `mwexec` (broadest sweep)

Results:

- `mwexec_bg`: zero hits.
- `mwexec(`: zero hits.
- bare `mwexec`: four hits, all of which are either `mwexecfm(...)`
  call sites or a documentation line about a previous migration.

The four hits in detail:

| File | Line | Context |
|------|------|---------|
| `net/frr/pkg-descr` | 32 | Plugin changelog entry under version 1.49: "Replace shell_exec() with mwexecfm()". This is documentation, not code. |
| `net/frr/src/etc/rc.syshook.d/carp/50-frr` | 53 | `mwexecfm('/usr/local/etc/rc.d/frr start > /dev/null');` inside the `'MASTER'` branch of the CARP event handler. |
| `net/frr/src/etc/rc.syshook.d/carp/50-frr` | 56 | `mwexecfm('/usr/local/etc/rc.d/frr stop');` inside the `'BACKUP'` branch of the CARP event handler. |
| `net/frr/src/etc/rc.syshook.d/carp/50-frr` | 62 | `mwexecfm('/usr/local/opnsense/scripts/frr/carp_event_handler');` for the non-toggling state-pass path. |

All three call sites are in the CARP syshook script and use
`mwexecfm()`, which is the recommended non-deprecated wrapper. No
`mwexec()` or `mwexec_bg()` exists.

Wider service-control mechanism: os-frr does not use `configd_run` from
PHP at all (zero hits across the tree). Service start, stop, restart,
and reload happen through:

- The `service_control_url()` machinery in core `interfaces` controllers
  via the `'service'` link key in `plugins_register` (see
  `net/frr/src/etc/inc/plugins.inc.d/frr.inc`), which dispatches to the
  configd action `frr restart` or `frr reload`.
- The configd template under
  `net/frr/src/opnsense/service/conf/actions.d/actions_frr.conf` (a
  standard service template; configd handles the daemon process
  management without involving `mwexec*` from PHP at all).

Since neither deprecated function appears, no upstream patch is needed
for os-frr 1.51. The plugin is already on the safe API.

Source: `opnsense/plugins` commit `fd6d2de572aa0714406217e8f3486c19df8f622e`,
path `net/frr/`. Full extracted tree was scanned. The three call sites
and the changelog line are quoted at the line numbers given in the
table.

## 4. os-tayga 1.5 scan

os-tayga 1.5 is the current `master` tip of `opnsense/plugins`
`net/tayga/`. The 1.5 version bump landed in commit `4430e3898` on
2026-03-20 ("net/tayga: relax RFC 6052 restrictions (#5321)"). No newer
version exists, so HEAD source is 1.5.

I extracted `net/tayga/` from HEAD and ran the same three patterns.

Results:

- `mwexec_bg`: zero hits.
- `mwexec(`: zero hits.
- bare `mwexec`: zero hits.

The plugin contains no PHP shell-out calls of any flavor. Service
control is fully handled through the configd action template at
`net/tayga/src/opnsense/service/templates/OPNsense/Tayga/tayga.conf`
and the matching `actions_tayga.conf`, which is the modern path.

No follow-up action is needed for os-tayga.

Source: `opnsense/plugins` `master` HEAD as of 2026-05-08, path
`net/tayga/`. Full extracted tree was scanned.

## 5. Conclusions

For each plugin:

- **os-frr 1.51**: no risk. Zero references to the deprecated
  functions. The CARP syshook already uses `mwexecfm()`. Removal of
  `mwexec*`/`mwexec_bg*` from core has no impact on this plugin.
- **os-tayga 1.5**: no risk. Zero references of any kind to the
  `mwexec` family. The plugin does not shell out from PHP.

Severity for both: **advisory** at most. Neither plugin would block
BGP startup or NAT64 on a 26.1.3+ kernel where the deprecated
functions have been removed. There is no regression and no blocker.

The R9 risk row in MWAN-151 (medium / low) can therefore be downgraded
to "not applicable" and the follow-up ticket suggested in MWAN-151
section 10 item 1 is unnecessary. I recommend recording this scan as
the closure evidence for R9 and adding a one-line note to the MWAN-151
risk register that R9 is closed.

No follow-up tickets are recommended on the strength of this scan
alone. The only related action is the one already on the books in
MWAN-151 section 11: when we run the testbed upgrade to 26.1.x, do a
post-upgrade smoke test that BGP comes up and NAT64 still translates,
which will catch any other regression that the source read does not
predict.

## 6. Cross-check: our own code

The task asked specifically whether any of these names appear in our
own code under `mwan/go/`. A recursive grep for `mwexec` returns zero
hits in `mwan/go/`. The only hits in `mwan/` overall are inside docs:

- `mwan/docs/MWAN-151-26x-changelog-deep-dive.md` lines 500 and 608,
  the existing R9 references that motivate this scan.
- `mwan/docs/MWAN-72-routing-configure-wipes-investigation.md` lines
  65, 66, 93, and 95, which quote `mwexecf(...)` and `mwexecfm(...)`
  call sites from OPNsense core to document `system_routing_configure`
  behavior. Those are quotations of safe-API code, not of the
  deprecated functions.

Our Go code does not call PHP and does not embed any reference to
these names, so the cross-check is clean. There is nothing in our own
codebase that the deprecation can break.

## 7. Sources

OPNsense plugins repo `opnsense/plugins`:

- os-frr 1.51 source tree: commit
  `fd6d2de572aa0714406217e8f3486c19df8f622e`, subtree `net/frr/`.
  Identified as the parent of `d0d9a7ffb` (the 1.51 -> 1.52 Makefile
  version bump on 2026-03-27).
- os-tayga 1.5 source tree: `master` HEAD as of 2026-05-08, subtree
  `net/tayga/`. Version 1.5 introduced in commit `4430e3898`,
  2026-03-20.
- URL for os-frr 1.51 tree:
  https://github.com/opnsense/plugins/tree/fd6d2de572aa0714406217e8f3486c19df8f622e/net/frr
- URL for os-tayga 1.5 tree:
  https://github.com/opnsense/plugins/tree/4430e38986f4556230ab72664260977f15baabc4/net/tayga

OPNsense core repo `opnsense/core`:

- Master tip: commit `f648476a665117e37f6693b55fdbc701677e33eb`,
  2026-05-07. `src/etc/inc/util.inc` lines 944-986 define only
  `mwexecf`, `mwexecfm`, `mwexecfb`.
- Stable 25.7 branch: `origin/stable/25.7`. `src/etc/inc/util.inc`
  lines 925-947 still define `mwexec`, `mwexec_bg`, and `mwexecf_bg`,
  each annotated with the `XXX deprecated` comment.
- URL for the deprecated definitions on stable/25.7:
  https://github.com/opnsense/core/blob/stable/25.7/src/etc/inc/util.inc#L925-L947

OPNsense changelog repo `opnsense/changelog`:

- 26.1 migration notes (deprecation warning text): file
  `community/26.1/26.1.r1` line 40, and `community/26.1/26.1` lines
  79 and 119. URL:
  https://github.com/opnsense/changelog/blob/master/community/26.1/26.1
- 26.1.3 release notes (removal landed): file
  `community/26.1/26.1.3` line 66, dated 2026-03-04. URL:
  https://github.com/opnsense/changelog/blob/master/community/26.1/26.1.3

Local repo references:

- `mwan/docs/MWAN-151-26x-changelog-deep-dive.md` section 9 risk
  register row R9 and section 10 item 1 (the scan that this report
  closes out).
- `mwan/docs/MWAN-72-routing-configure-wipes-investigation.md` for
  pre-existing context on safe-API `mwexecf`/`mwexecfm` usage in
  core's routing path.

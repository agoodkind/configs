# Five: the daemon owns the firewall

The daemon becomes the only thing that writes firewall state. The ruleset
file is deleted and the firewall service is masked. Every rule is generated
from the model.

Depends on translation being typed, because the rules the daemon generates
are the model's rules. This is the only piece that touches live traffic.

## Why there is no file

Two authorities on one set of kernel tables is the problem, and every reason
to keep a small file turns out to be removable.

The reload path is the clearest case. The rendered file begins by flushing
the entire ruleset, so every reload destroys rules the daemon programmed, and
a watcher exists solely to repair that. Stopping the firewall service leaves
the ruleset empty with nothing to repair it. Deleting the file removes both.

The empty translation table the file declares exists only because the
translation module never creates its own table or chains; it sends chain
references with no hook, priority, or type, and its own comments record that
it cannot recover from a bare flush. Teaching it to create them removes the
reason.

The address-request accepts matter only during a window between the file
loading early and the daemon starting late. Moving the daemon earlier removes
the window rather than the need.

## Boot

The daemon takes the slot the firewall service occupies today, before any
interface is configured. This repo's own drop-in records that the stock
firewall service carries that ordering because the firewall must load before
networking, and warns that ordering on the network being up creates a
dependency cycle. The daemon must not wait for the network, which it can
avoid because its rules match interfaces by name and a name match does not
require the interface to exist.

The daemon signals readiness only after its first successful apply, so units
that order themselves after it get the guarantee they already assume.

Startup is two transactions. The first programs the closed baseline: drop
policy, established traffic, loopback, the control messages each family
needs, management access, and the internal routing protocol. The second
programs the full ruleset. Invalid configuration stops the daemon after the
first, so the gateway is closed and reachable rather than open.

The accepted residual is narrow and stated deliberately: if the binary cannot
execute at all, nothing is programmed. The deploy gate and the snapshot
rollback are the recovery for that case.

The daemon cannot load kernel modules, because its unit forbids it. Creating
an address-translation chain normally pulls the backend in on demand, and
today the ruleset file does that as a side effect. The backends must be
declared for load at boot instead. The same gap already bites silently: a
comment claims connection tracking is loaded by a handler, and no such
handler exists anywhere in the tree, so a connection-tracking setting applies
only because the file happened to load the module first.

## Apply discipline

Add the table, add the chain, clear the chain, add the rules, commit once.
Never delete a table or a chain, and never clear a whole table. Both add
operations create without requiring absence, so repeating them updates in
place and emits no delete events, which is what stops the module's own writes
from waking its own watcher. Create on every pass rather than once at
startup, so a cleared ruleset is fully repaired by the watcher instead of
needing a restart.

One trap: a firewall transaction is all or nothing, and the kernel rejects a
chain creation that changes an existing chain's hook, priority, or type. The
day a release changes a priority, every pass would fail and the kernel would
keep whatever it had, silently. Read the live chains at startup, compare, and
fail loudly with the manual remedy.

The watcher matches only table and chain deletions on the owned tables. Rule
deletions are emitted by the module's own chain clears, and matching them is
an endless loop. Set element deletions are emitted by the pinned-destination
refresher every six hours.

No stop or exit path flushes. Rules live in the kernel, not the process, so
a stopped daemon leaves its last programmed ruleset protecting the gateway.

## Address sets

Create each pinned-destination set if absent and never write its contents.
The refresher owns the contents and its timer runs every six hours, so a
module that rewrote them each pass would destroy every resolved name within
one reconcile interval and leave the pin degraded to its seeds until the next
refresh.

Create each set with interval matching and automatic merging. That merging is
metadata the refresher's own transaction reads to combine overlapping ranges,
and the refresher guarantees overlaps by adding single addresses inside
ranges it also adds. Without it the whole refresh fails with a duplicate
entry error.

Move the refresher's ordering onto the daemon and give it restart on failure.
It orders itself after the firewall service today, exits non-zero when its
sets are absent, and first fires two minutes after boot, so with the sets
moving to the daemon its ordering must move with them.

## What the daemon programs

The filter tables, with the fixed accepts, the per-member address-request
accepts narrowed to the configured interfaces, and forwarding permitted
between the internal interface and each member.

The address-translation tables, with each member's instances as
[translation.md](translation.md) defines them, and the steering mark rules.

The marking tables, with per-member ingress marks for reply symmetry, the
pinned-destination sets, the control-plane pin whose target member is named
in configuration, and the connection mark save and restore.

## Losing the pre-flight check

Deleting the file deletes the only pre-flight validation that exists, a
check-mode parse of the rendered ruleset on the target before it lands.
Nothing else in the deploy validates a firewall ruleset.

Replace it with a check that programs the intended ruleset into a throwaway
network namespace and compares it against a stored reference per environment.
That reference doubles as this piece's equivalence artifact. The comparison
excludes kernel handles and counters, which change on every program, and
excludes the refresher-owned set contents, which the refresher rewrites
every six hours; set membership is checked separately, for freshness rather
than equality.

The deploy gate is not a substitute and must be hardened before this piece
lands, not after. It decides on IPv6 alone and returns on its first
successful probe, so a ruleset with no working IPv4 translation passes it,
and a half-broken balancer passes as soon as the retry loop reaches the
working member. Require both families and several consecutive successes, and
assert the intended ruleset against the kernel on the guest after the reboot.

## Drift detection

Add the daemon's configuration file to the watchdog's watched paths. It
hashes the ruleset file today and not the configuration, so moving the
ruleset into configuration without that change leaves the watchdog watching a
file that never changes again. Expect one benign hash change on the deploy
that adds it.

## Acceptance

The ruleset the daemon intends matches the kernel, verified on the testbed
and again on production after the reboot.

For the current provider set the rules are unchanged from the file they
replace, one for one.

A bare ruleset flush is fully repaired, including the tables the translation
module owns, which it cannot do today.

The gateway is closed and reachable when configuration is invalid. A
stopped daemon leaves its last programmed ruleset in the kernel, so the
gateway stays closed and reachable then too.

The firewall service is masked and no path reloads a file.

## Failure modes

The module is listed only in the gateway role. The failover container runs
the same daemon and ships its own ruleset, so a module that claimed the
tables everywhere would fight it.

An operator page tells a human to add rules by hand and says they survive
until the next reload. After this piece they are removed within one reconcile
interval. That page needs correcting in the same change, or the next incident
gets an instruction that quietly stops working.

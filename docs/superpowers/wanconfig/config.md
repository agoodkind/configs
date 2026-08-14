# Two: the configuration format

The rendered configuration becomes the model's own JSON encoding, so one
representation runs from inventory through to the served tree. TOML retires.

Depends on the model existing and being served, because the encoding is the
model's and the served tree is how the change is verified.

## Why the format changes

Two representations of the same thing is the problem this work exists to
remove. A model that describes the daemon while the daemon loads a differently
shaped file recreates it one layer up, and the two drift the moment someone
edits one of them.

There is a second gain. A file in the model's encoding can be checked against
the schema before it ever reaches the gateway. That matters because the piece
that moves firewall ownership into the daemon deletes the only pre-flight
validation that exists today, a check-mode parse of the rendered ruleset on
the target before it lands. Schema validation of the rendered configuration
replaces it, and covers more than the firewall.

## What changes

Inventory renders the configuration as JSON in the model's encoding. The
daemon loads that file at startup and validates it against the schema before
acting on any of it. A file that does not validate stops the daemon before it
programs anything, which is the existing failure contract.

The per-provider catalogue lives in the shared inventory group, keyed by
environment, because the rollback watchdog's probe list is rendered on the
hypervisor and that variable group cannot read the gateway's. A catalogue in
the gateway's own group would leave that list hand-maintained and free to
drift, which is how it came to omit a provider.

The environment file stays. The pinned-destination refresher and the whole
802.1X chain read it, and it hardcodes both the variable names and the
address set names, so retiring it is separate work.

## What must not change

The daemon's behavior, given equivalent input. This piece changes how
configuration is written and read, not what any value means.

Validation must remain strict in the same places it is strict today. A
missing value is a load-time failure, not a defaulted one. The model makes
that easier to enforce, since a leaf is either present or it is not.

## Acceptance

Both environments render a file that validates against the schema.

The firewall rules, routes, and policy rules the daemon programs are
unchanged, and the tree served by the read-only surface is unchanged. File
equivalence is not the test, because the file is a different format by
design. Behavioural equivalence is the test.

Rendering an invalid configuration fails in the deploy, before the file
reaches the gateway.

## Failure modes

A file that validates but means something different from its TOML
predecessor is the risk this piece carries, and the served tree is what
catches it. Compare the tree before and after on the same host.

Schema validation at deploy time and at load time must use the same schema.
Two copies of a schema is the same duplication failure in a new place.

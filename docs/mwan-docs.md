# MWAN documentation migration

MWAN documentation is moving to the
[MWAN documentation guide](https://github.com/agoodkind/mwan/blob/main/docs/README.md).
The migration includes existing architecture documents, specifications,
implementation plans, operator runbooks, testbed references, and shared
OPNsense integration documents. Maintain subsequent MWAN documentation,
goals, execution ledgers, and acceptance records in that repository.

The MWAN-341 documents now use these locations:

- Read the [firewall specification](https://github.com/agoodkind/mwan/blob/main/docs/firewall.md) for required behavior.
- Follow the [implementation plan](https://github.com/agoodkind/mwan/blob/main/docs/plans/2026-09-26-mwan-341-firewall.md) for the work sequence.
- Apply the [epic goal and standing rules](https://github.com/agoodkind/mwan/blob/main/docs/plans/2026-09-26-mwan-341-goal.md) throughout execution.
- Update the [execution ledger](https://github.com/agoodkind/mwan/blob/main/docs/plans/2026-09-26-mwan-341-ledger.md) when work advances or context changes.

The additional copied documents remain here as migration snapshots.
Make future MWAN documentation changes in the MWAN repository.
Configs continues to contain deployment code, inventory, and general
infrastructure documentation. Run Configs deployment and OpenTofu commands
from the Configs checkout.

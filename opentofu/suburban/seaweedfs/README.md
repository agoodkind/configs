# Retire QA SeaweedFS state

This root retains its existing encrypted backend and provider identities without
provisioning a guest. OpenTofu refreshes stale resources before processing removed
blocks. Disable refresh during retirement planning to avoid contacting deleted
LXC 218.

1. Choose a private absolute path for the encrypted saved plan. After approval
   for state reconciliation, run
   `./configsctl tofu suburban/seaweedfs plan -refresh=false -out=<private-plan-path>`.
2. Independently verify the guest deletion and inspect any proposed deletions on
   the surviving hypervisor. The plan does not refresh surviving assets. Require
   the removed blocks to forget deleted guest records.
3. After separate operator approval, apply that exact saved plan with
   `./configsctl tofu suburban/seaweedfs apply <private-plan-path>`.

# Retire QA SeaweedFS state

This root retains its existing encrypted backend and provider identities without
provisioning a guest. Its removed blocks forget records on deleted LXC 218
without attempting guest access. Review and apply a retirement plan only with
separate operator approval.

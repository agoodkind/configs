# Tack Disposable Runners Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` to implement this plan task by
> task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run up to two normal Tack jobs and one Docker job on disposable,
repository-scoped runners.

**Architecture:** OpenTofu runs the runner guest as a QEMU virtual machine from
a pinned Debian cloud image. Ansible installs K3s, Helm, and GitHub's Actions
Runner Controller. The controller creates one pod per job and removes its
temporary state afterward.

## Global constraints

- Use post-change verification. Do not use test-driven development.
- Keep exact versions, checksums, digests, addresses, and guest identity in
  configuration.
- Keep removal, deregistration, replacement, and deployment steps manual.
- Keep `prevent_destroy` and every existing guest allocation.
- Do not change Tack's application LXC.
- Use a QEMU virtual machine without host device or cgroup exceptions.
- Do not install Docker Engine on the guest or mount a host Docker socket.
- Do not add lifecycle scripts, cleanup scripts, webhooks, or janitors.
- Keep Tack's default branch on GitHub-hosted runners until the selector pull
  request passes live validation.
- Use the repository's `configs` command for OpenTofu and Ansible operations.
- Use Graphite for the configuration stack. Keep Task 6 as a separate Tack
  pull request.

## Stack

1. Document the runner design and implementation boundary.
2. Declare the runner virtual machine.
3. Install K3s and Helm.
4. Install the controller and standard runner scale set.
5. Add the isolated Docker runner scale set.

## Task 1: Declare the runner virtual machine

**Files:**

- [runner guest declaration](../../../opentofu/suburban/tack_gh_runner_suburban.tf)
- [Proxmox provider configuration](../../../opentofu/providers.tf)
- [runner inventory](../../../ansible/inventory/group_vars/tack_gh_runner_suburban_servers.yml)

- [ ] Pin the Debian cloud image URL and checksum.
- [ ] Install and start the QEMU guest agent through cloud-init.
- [ ] Use the existing root SSH agent access to upload cloud-init.
- [ ] Keep `prevent_destroy` and every allocation unchanged.
- [ ] Use the virtual machine's primary interface for Flannel.
- [ ] Add no host device or cgroup exception.
- [ ] Run:

```bash
go run goodkind.io/configs/cmd/configs tofu fmt -check -recursive
go run goodkind.io/configs/cmd/configs tofu validate
```

Do not apply while the existing guest remains in Proxmox.

## Task 2: Install K3s and Helm

**Files:**

- `ansible/inventory/group_vars/tack_gh_runner_suburban_servers.yml`
- `ansible/playbooks/deploy-tack-gh-runner.yml`
- `github-runner/k3s-config.yaml.j2`

- [ ] Add explicit variables for the K3s release, official installer URL, and
      checksum.
- [ ] Add explicit variables for the Helm release, official archive URL, and
      checksum.
- [ ] Add explicit variables for the node address, pod range, service range,
      cluster DNS address, interface, and configuration directories.
- [ ] Resolve releases and checksums from official sources. Keep exact values
      in group variables.
- [ ] Configure one IPv6 K3s server with SQLite, embedded containerd, secrets
      encryption, and Flannel IPv6 masquerading.
- [ ] Disable local storage, metrics server, service load balancer, and Traefik.
- [ ] Keep `prep-guests.yml`, the inventory group, and privilege escalation.
- [ ] Install only the packages needed to fetch and unpack the releases.
- [ ] Download the official K3s installer with its configured checksum.
- [ ] Run the installer only when `k3s --version` differs from the configured
      release. Set `INSTALL_K3S_VERSION`, `INSTALL_K3S_EXEC=server`, and
      `INSTALL_K3S_SKIP_START=true`.
- [ ] Render K3s configuration before enabling and starting `k3s`.
- [ ] Run `k3s check-config` and wait for the node to report `Ready`.
- [ ] Download the official Helm archive with its configured checksum.
- [ ] Install Helm only when `helm version --short` differs from the configured
      release.
- [ ] Keep probes read-only in check mode. Skip installers, service changes,
      and live cluster checks in check mode.
- [ ] Use registered results for conditions. Do not use `default()` or
      `is defined` for inputs.
- [ ] Run:

```bash
go run goodkind.io/configs/cmd/configs lint
go run goodkind.io/configs/cmd/configs syntax-check deploy-tack-gh-runner
ansible-lint ansible/playbooks/deploy-tack-gh-runner.yml
```

## Task 3: Install the controller and standard runners

**Files:**

- `ansible/inventory/group_vars/tack_gh_runner_suburban_servers.yml`
- `ansible/playbooks/deploy-tack-gh-runner.yml`
- `github-runner/arc-controller-values.yaml.j2`
- `github-runner/arc-tack-values.yaml.j2`
- Delete `github-runner/docker-compose.yml.j2`

- [ ] Add explicit variables for the chart version, official Open Container
      Initiative chart references, namespaces, release names, service account,
      repository URL, secret name, scale limit, timeout, and image digests.
- [ ] Pin the controller and runner images for the guest architecture.
- [ ] Render controller values with the pinned image and service account.
- [ ] Render a repository-scoped `tack` scale set with zero idle runners and a
      maximum of two jobs.
- [ ] Give the runner no persistent volume, host path, or Docker daemon.
- [ ] Apply the runner namespace through `k3s kubectl apply -f -` on stdin.
- [ ] Apply the repository credential through stdin. Read
      `vault_github_runner_access_token` directly and suppress task output.
- [ ] Validate rendered releases with `helm template`.
- [ ] Install the controller before the scale set. Use exact chart versions,
      atomic rollback, readiness waits, and the configured timeout.
- [ ] Verify the controller, `AutoscalingRunnerSet`, and listener readiness.
- [ ] Delete the obsolete Compose template from source.
- [ ] Remove Docker, Compose, persistent runner, and registration tasks from
      the playbook. Add no cleanup or deregistration task.
- [ ] Run:

```bash
git diff --check
go run goodkind.io/configs/cmd/configs lint
go run goodkind.io/configs/cmd/configs syntax-check deploy-tack-gh-runner
ansible-lint ansible/playbooks/deploy-tack-gh-runner.yml
```

## Task 4: Add isolated Docker runners

**Files:**

- `ansible/inventory/group_vars/tack_gh_runner_suburban_servers.yml`
- `ansible/playbooks/deploy-tack-gh-runner.yml`
- `github-runner/arc-tack-docker-values.yaml.j2`

- [ ] Pin the Docker-in-Docker image for the guest architecture.
- [ ] Copy GitHub's documented custom Docker-in-Docker pod layout into the
      scale set template. Do not set `containerMode`.
- [ ] Use the pinned runner image for the runner and externals initializer.
- [ ] Use the pinned Docker image for the privileged daemon sidecar.
- [ ] Add only `--ipv6` and
      `--default-network-opt=bridge=com.docker.network.enable_ipv6=true` to
      GitHub's daemon arguments.
- [ ] Use pod-local `emptyDir` volumes for the work directory, Docker socket,
      Docker data, and runner externals.
- [ ] Render a repository-scoped `tack-docker` scale set with zero idle runners
      and a maximum of one job.
- [ ] Validate the release with `helm template`.
- [ ] Install it after the controller. Use the exact chart version, atomic
      rollback, a readiness wait, and the configured timeout.
- [ ] Verify its `AutoscalingRunnerSet` and listener readiness.
- [ ] Run:

```bash
git diff --check
go run goodkind.io/configs/cmd/configs lint
go run goodkind.io/configs/cmd/configs syntax-check deploy-tack-gh-runner
ansible-lint ansible/playbooks/deploy-tack-gh-runner.yml
go run goodkind.io/configs/cmd/configs tofu fmt -check -recursive
go run goodkind.io/configs/cmd/configs tofu validate
```

## Task 5: Replace the runner guest and deploy

This task deletes the current runner guest and its GitHub registrations.
Obtain explicit operator approval immediately before deletion.

- [ ] Confirm the configs stack is reviewed.
- [ ] Resolve the runner guest from `service_mapping.yml` and confirm the live
      Proxmox guest matches it.
- [ ] Manually remove the guest's persistent GitHub runner registrations.
- [ ] Manually stop and delete the runner guest in Proxmox.
- [ ] Run `configs tofu plan`. Require one runner guest creation and no other
      change.
- [ ] Run `configs tofu apply`.
- [ ] Run the playbook with `--check --diff`, then deploy it.
- [ ] Confirm the QEMU guest agent and serial console work.
- [ ] Confirm the K3s node, controller, scale sets, and listeners report ready.
- [ ] Reboot the idle guest. Confirm runner service returns without repair.

## Task 6: Route Tack workflows separately

Do not include Tack changes in the configs stack. After structural validation,
use a separate selector-only pull request as the live-validation gate. Do not
merge it until the checks below pass.

- [ ] Change existing Go job selectors to `tack`.
- [ ] Change existing Docker, overlay, and integration job selectors to
      `tack-docker`.
- [ ] Keep Dependabot on `ubuntu-latest`.
- [ ] Do not add files, steps, scripts, wrappers, configuration, or workflow
      restructuring.
- [ ] Run the repository's existing workflow checks.
- [ ] Use the pull request to confirm two `tack` jobs can overlap and only one
      `tack-docker` job runs at once.
- [ ] Cancel an active Docker job. Confirm its pod disappears and the next job
      cannot see its Docker or workspace state.

## Self-review

- Exact changing values live only in configuration.
- OpenTofu and Ansible contain no transition or cleanup logic.
- The configs stack contains no Tack repository changes.
- Tack workflow scope is limited to existing runner selectors.
- Verification follows implementation.

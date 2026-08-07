# Replace Tack runners with Actions Runner Controller

## Goal

`agoodkind/tack` gets fresh self-hosted runners for every job. Two normal jobs
and one Docker job may run concurrently. Normal completion and cancellation
delete the runner pod, its work directory, and its private Docker state.

GitHub's Actions Runner Controller owns registration, job assignment, runner
replacement, and cleanup. The deployment adds no custom runner manager,
webhook, janitor, or cleanup script.

## Scope

- Keep the runner on CT 219 at `tack-gh-runner.suburban.goodkind.io`.
- Keep its 6 cores, 12 GiB memory, 80 GiB disk, hostname, MAC address, and IPv6.
- Keep Tack's separate application LXC and its memory unchanged.
- Scope both runner sets to `https://github.com/agoodkind/tack`.
- Keep Dependabot on GitHub-hosted runners.
- Replace the current persistent Compose runners and shared host Docker socket.

## Privileged LXC

OpenTofu recreates CT 219 with `unprivileged = false` and `nesting = true`.
Changing privilege replaces the container, so the existing runner disk and
registrations are discarded. They contain no durable service data.

The final resource omits `prevent_destroy`. The guest is reconstructible and
must remain replaceable as runner infrastructure. It does not enable `keyctl`.

The deployment stops if K3s needs an unconfined AppArmor profile, unrestricted
device access, or custom cgroup mounts. Those changes would exceed the approved
LXC boundary.

Proxmox recommends a virtual machine for application containers, and K3s does
not publish a supported LXC profile. The accepted privileged LXC design
therefore requires live proof. A failed K3s configuration check, privileged
Docker build, cancellation test, or pod deletion test rejects the deployment.

## Kubernetes

Ansible installs these pinned components:

- K3s `v1.36.3+k3s1` as one server with SQLite and embedded containerd.
- Helm `v3.21.3`.
- Actions Runner Controller charts `0.14.2`.

K3s uses single-stack IPv6:

```yaml
node-ip: 3d06:bad:b01:210::219
cluster-cidr: fd21:9:42::/56
service-cidr: fd21:9:43::/112
cluster-dns: fd21:9:43::a
flannel-ipv6-masq: true
secrets-encryption: true
disable:
  - local-storage
  - metrics-server
  - servicelb
  - traefik
```

CoreDNS forwards through the guest's existing DNS64 resolver. Flannel
masquerades pod traffic through the guest's routed IPv6 address. The deployment
does not install Docker Engine on the LXC or expose a host Docker socket.

## Runner sets

The controller runs in `arc-systems`. Runner pods run in `arc-runners`.

| Scale set | Minimum | Maximum | Docker |
|---|---:|---:|---|
| `tack` | 0 | 2 | None |
| `tack-docker` | 0 | 1 | Private daemon per pod |

Both sets use the repository URL and a Kubernetes Secret named
`tack-gh-runner`. Ansible creates that Secret from
`vault_github_runner_access_token` with secret output suppressed. K3s encrypts
Secrets at rest.

The implementation pins the controller, runner, and Docker images by digest in
inventory variables. No chart or image uses `latest`.

The `tack` set uses the standard ARC runner pod. It mounts no persistent job
storage and starts no Docker daemon.

The `tack-docker` set copies GitHub's documented Docker-in-Docker pod template
into one Helm values template. Its runner work directory, Docker socket, and
Docker data use pod-local `emptyDir` volumes. The private daemon adds only the
IPv6 settings required by this network:

```yaml
args:
  - dockerd
  - --host=unix:///var/run/docker.sock
  - --group=$(DOCKER_GROUP_GID)
  - --ipv6=true
  - --default-network-opt=bridge=com.docker.network.enable_ipv6=true
```

Docker allocates private IPv6 subnets. Its embedded DNS forwards through the
pod resolver. Job containers therefore use the existing DNS64 and NAT64 path.

## Job lifecycle

1. GitHub assigns a job to `tack` or `tack-docker`.
2. ARC creates one runner pod and registers its ephemeral runner.
3. The runner executes one job.
4. ARC deletes the pod after completion or cancellation.
5. Kubernetes deletes every `emptyDir`, including Docker data.

A failed Kubernetes node is a cluster failure, not stale job state. K3s and ARC
recover after the guest or service returns. No deployment path reintroduces a
shared Docker socket or persistent runner work directory.

## Workflow cutover

The Tack workflow change happens only after ARC passes deployment validation.

- Go jobs use `runs_on: tack` through the shared Go workflow.
- Docker and integration jobs use `runs-on: tack-docker`.
- Dependabot keeps `runs-on: ubuntu-latest`.
- Integration uses its normal up, test, and always-run down steps.
- The pending custom cancellation cleanup wrapper is removed.

The current Tack default branch still uses GitHub-hosted runners. A failed ARC
deployment therefore cannot strand its workflows before cutover.

## Deployment behavior

The existing `deploy-tack-gh-runner` entry point remains canonical. Ansible:

1. Runs the K3s configuration check before installing ARC.
2. Installs pinned K3s and Helm releases.
3. Applies the repository credential without writing it to logs.
4. Uses Helm upgrade with atomic rollback and readiness waits.
5. Verifies the node, controller, listener, and both runner scale sets.

The deployment fails before workflow cutover when any readiness or network
probe fails.

## Acceptance criteria

1. OpenTofu plans replacement only for CT 219. Tack's application LXC is
   unchanged.
2. K3s reports its node ready with the configured IPv6 address.
3. Pods resolve through DNS64 and reach GitHub and GHCR over IPv6 and NAT64.
4. Two `tack` jobs run concurrently. A third waits.
5. One `tack-docker` job runs. A second waits.
6. Docker builds and Compose integration tests work through the private daemon.
7. Canceling a Docker job removes its pod and Docker state without manual
   cleanup.
8. The next Docker job cannot see containers, networks, volumes, or files from
   the canceled job.
9. A runner guest reboot restores ARC and accepts new jobs.
10. GitHub shows no legacy persistent runners after cutover.

## Out of scope

- Multi-node Kubernetes or high availability.
- More than three concurrent jobs.
- Persistent local build caches or shared runner filesystems.
- External log aggregation.
- Changes to Tack's application LXC.
- Custom runner lifecycle or cancellation cleanup code.

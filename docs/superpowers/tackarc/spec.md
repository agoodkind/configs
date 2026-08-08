# Run Tack CI on disposable runners

Tack CI runs up to three isolated jobs without retaining state between jobs.

## Keep every job disposable

GitHub's Actions Runner Controller creates one Kubernetes pod for each job.
The pod runs one job and then disappears.

Runner files use temporary pod storage. Docker jobs also get a private Docker
daemon and private temporary storage. No runner mounts a host Docker socket.

Normal completion and cancellation delete the pod and its job state. The
deployment adds no custom runner manager, janitor, webhook, or cancellation
script.

## Keep the approved host boundary

OpenTofu declares the service-mapped runner guest as a QEMU virtual machine
from a pinned Debian cloud image. The resource keeps its configured compute,
storage, identity, and network allocations. Tack's application LXC remains
unchanged.

Cloud-init installs the QEMU guest agent before Ansible connects. The runner
resource keeps `prevent_destroy`.

The virtual machine provides the kernel and device boundary required by K3s
and Docker-in-Docker. OpenTofu adds no host device or cgroup exceptions.

## Preserve IPv6 networking

K3s runs one server with SQLite, embedded containerd, and single-stack IPv6.
CoreDNS forwards through the guest's DNS64 resolver. Flannel sends pod traffic
through the guest's routed IPv6 address.

The private Docker daemon enables IPv6 for its default and user-defined bridge
networks. Job containers use the same DNS64 and NAT64 path.

Inventory owns the exact network ranges, upstream releases, charts, and image
digests. No chart or image uses `latest`.

## Separate job capacity

The controller and runner pods use separate namespaces.

| Scale set | Idle runners | Maximum jobs | Docker |
|---|---:|---:|---|
| `tack` | 0 | 2 | None |
| `tack-docker` | 0 | 1 | Private daemon per pod |

Both sets are repository-scoped to Tack. Ansible creates one Kubernetes Secret
from `vault_github_runner_access_token` without logging its value. K3s encrypts
the Secret at rest.

The `tack-docker` pods follow GitHub's documented Docker-in-Docker layout. The
only behavioral extension enables IPv6 for Docker bridge networks.

## Route Tack jobs separately

After runner validation, a separate workflow change updates only existing
runner selectors. Go jobs use `tack`. Docker builds, overlay checks, and
integration jobs use `tack-docker`.

Dependabot automation uses `runs-on: ubuntu-latest`.

The workflow change adds no files, steps, scripts, wrappers, configuration, or
job restructuring.

## Stop on failed validation

Ansible stops when the K3s configuration or readiness check fails. Helm
installs use atomic rollback and readiness waits.

A failed Kubernetes node is a cluster failure. K3s and Actions Runner
Controller recover after the guest or service returns. Operators repair the
cluster, not individual job resources.

## Verify the result

1. OpenTofu keeps the runner's allocations and Tack's application LXC unchanged.
2. The runner uses a virtual machine without host device exceptions.
3. K3s reports its node ready on IPv6.
4. Pods resolve through DNS64 and reach GitHub and GHCR.
5. Two `tack` jobs run concurrently while a third waits.
6. One `tack-docker` job runs while a second waits.
7. Docker builds and integration tests use the private daemon.
8. Canceling a Docker job removes its pod and Docker state.
9. The next Docker job sees no state from the canceled job.
10. A runner guest reboot restores job service.

## Exclude unrelated scope

- Multi-node Kubernetes or high availability.
- More than three concurrent jobs.
- Persistent local caches or shared runner filesystems.
- External log aggregation.
- Changes to Tack's application LXC.
- Custom runner lifecycle or cancellation cleanup code.

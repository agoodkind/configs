# OPNsense UI testing

Use this page when a local browser needs to reach an OPNsense UI page through
an SSH forward. The forwarding values come from the current testbed inventory
and access documentation, so this page does not own hostnames, addresses, or
site-specific ports.

## Sources

Read the current access path before opening a tunnel:

- [Suburban testbed](../infra/suburban-testbed.md) defines the testbed host and
  virtual machine layout.
- [OPNsense testbed baseline](testbed-baseline.md) defines the OPNsense guest
  role and management expectations.
- [Access](../infra/access.md) defines operator access patterns.

Use the service repository for service-specific UI paths. For the Cloudflared
OPNsense plugin, the settings page path is:

```text
/ui/cloudflared/settings
```

## Inputs

Set these values from the current inventory or the service under test:

```sh
LOCAL_BIND_HOST='<local browser bind host>'
LOCAL_PORT='<unused local port>'
JUMP_HOST='<optional ssh jump host>'
TARGET_SSH='<ssh target for the OPNsense host>'
REMOTE_HOST='<host as seen from TARGET_SSH>'
REMOTE_PORT='<OPNsense UI port as seen from TARGET_SSH>'
REMOTE_PATH='<OPNsense UI path for the page under test>'
```

Leave `JUMP_HOST` empty when direct SSH access reaches `TARGET_SSH`.

## Forward

Run the direct form when the target is reachable without a jump host:

```sh
ssh -N \
    -L "${LOCAL_BIND_HOST}:${LOCAL_PORT}:${REMOTE_HOST}:${REMOTE_PORT}" \
    "${TARGET_SSH}"
```

Run the jump form when the access documentation requires a jump host:

```sh
ssh -N \
    -L "${LOCAL_BIND_HOST}:${LOCAL_PORT}:${REMOTE_HOST}:${REMOTE_PORT}" \
    -J "${JUMP_HOST}" \
    "${TARGET_SSH}"
```

Keep this terminal open while the browser test runs.

## View

Build the local browser URL from the forwarding inputs:

```sh
LOCAL_URL="https://${LOCAL_BIND_HOST}:${LOCAL_PORT}${REMOTE_PATH}"
printf '%s\n' "${LOCAL_URL}"
```

For testbed systems that use a self-signed or locally issued certificate,
expect browser automation to ignore certificate errors only for this local
test run.

## Capture

Use headless Chrome when the change needs proof that the actual forwarded page
renders:

```sh
CHROME_BIN="${CHROME_BIN:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
SCREENSHOT_PATH="${SCREENSHOT_PATH:-.context/artifacts/opnsense-ui.png}"

"${CHROME_BIN}" \
    --headless=new \
    --disable-gpu \
    --ignore-certificate-errors \
    --screenshot="${SCREENSHOT_PATH}" \
    "${LOCAL_URL}"
```

For the Cloudflared OPNsense plugin settings page, set:

```sh
REMOTE_PATH='/ui/cloudflared/settings'
SCREENSHOT_PATH='.context/artifacts/cloudflared-settings.png'
```

The screenshot proves that the browser reached the forwarded OPNsense UI page.
It does not prove that a plugin change is installed, so pair it with the
plugin repository's install, restart, and validation steps.

## Stop

Stop the forwarding command with `Control-C` after the browser test finishes.

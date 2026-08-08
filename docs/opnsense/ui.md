# OPNsense UI testing

Use this page when a local browser needs to reach an OPNsense UI page through
an SSH forward. The forwarding values come from the current testbed inventory
and access documentation, so this page does not own hostnames, addresses, or
site-specific ports.

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
TARGET_SSH='<ssh target for the OPNsense host>'
REMOTE_HOST='<host as seen from TARGET_SSH>'
REMOTE_PORT='<OPNsense UI port as seen from TARGET_SSH>'
REMOTE_PATH='<OPNsense UI path for the page under test>'
```

## Forward

OpenSSH requires brackets around IPv6 literals in forwarding specifications.
Format both address fields, then open the forward:

```bash
format_forward_host() {
    local host="$1"

    if [[ "${host}" == \[*\] ]]; then
        printf '%s\n' "${host}"
        return
    fi

    if [[ "${host}" == *:* ]]; then
        printf '[%s]\n' "${host}"
        return
    fi

    printf '%s\n' "${host}"
}

LOCAL_BIND_SPEC="$(format_forward_host "${LOCAL_BIND_HOST}")"
REMOTE_SPEC="$(format_forward_host "${REMOTE_HOST}")"

ssh -N \
    -L "${LOCAL_BIND_SPEC}:${LOCAL_PORT}:${REMOTE_SPEC}:${REMOTE_PORT}" \
    "${TARGET_SSH}"
```

Keep this terminal open while the browser test runs.

## View

Build the local browser URL from the forwarding inputs:

```sh
LOCAL_URL="https://${LOCAL_BIND_SPEC}:${LOCAL_PORT}${REMOTE_PATH}"
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

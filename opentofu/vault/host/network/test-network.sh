#!/usr/bin/env bash
set -euo pipefail

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

require_command() {
    if ! command -v "$1" >/dev/null; then
        fail "Install the required command: $1"
    fi
}

stop_process() {
    local process_id="$1"
    local process_status
    if ! kill -TERM "$process_id"; then
        return 1
    fi
    if wait "$process_id"; then
        return
    else
        process_status=$?
        if [[ "$process_status" -ne 143 ]]; then
            printf 'The test process %s ended with status %s.\n' "$process_id" "$process_status" >&2
            return 1
        fi
    fi
}

cleanup() {
    local test_status=$?
    local cleanup_failed=0
    local process_id
    local namespace_index
    local log_file
    trap - EXIT INT TERM
    set +e

    if [[ "$test_status" -ne 0 && "$WORK_DIRECTORY_CREATED" == true ]]; then
        for log_file in "$WORK_DIRECTORY"/*.log; do
            if [[ -f "$log_file" ]]; then
                printf 'Diagnostic log: %s\n' "$log_file" >&2
                cat "$log_file" >&2
            fi
        done
    fi
    for process_id in "${PROCESS_IDS[@]}"; do
        if ! stop_process "$process_id"; then
            cleanup_failed=1
        fi
    done
    if [[ -n "$SERVICE_STOP" && "$TUN_CREATED" == true ]]; then
        prepare_service_command "$SERVICE_STOP"
        if ! ip netns exec "$ROUTER_NAMESPACE" "${SERVICE_COMMAND[@]}"; then
            cleanup_failed=1
        fi
    fi
    for ((namespace_index = ${#CREATED_NAMESPACES[@]} - 1; namespace_index >= 0; namespace_index--)); do
        if ! ip netns delete "${CREATED_NAMESPACES[namespace_index]}"; then
            cleanup_failed=1
        fi
    done
    if [[ "$WORK_DIRECTORY_CREATED" == true ]]; then
        if ! rm -rf -- "$WORK_DIRECTORY"; then
            cleanup_failed=1
        fi
    fi
    if [[ "$cleanup_failed" -ne 0 && "$test_status" -eq 0 ]]; then
        test_status=1
    fi
    if [[ "$test_status" -eq 0 ]]; then
        printf 'The test removed its processes, namespaces, and temporary files.\n'
    fi
    exit "$test_status"
}

prepare_service_command() {
    local command_line="$1"
    local argument_index
    printf '%s\n' "$command_line" >"$WORK_DIRECTORY/service-command"
    read -r -a SERVICE_COMMAND <"$WORK_DIRECTORY/service-command"
    for ((argument_index = 0; argument_index < ${#SERVICE_COMMAND[@]}; argument_index++)); do
        case "${SERVICE_COMMAND[argument_index]}" in
        /usr/sbin/tayga) SERVICE_COMMAND[argument_index]="$TAYGA_BINARY" ;;
        /etc/tayga.conf) SERVICE_COMMAND[argument_index]="$WORK_DIRECTORY/tayga.conf" ;;
        esac
    done
}

prepare_tayga() {
    local packages
    if command -v tayga >"$WORK_DIRECTORY/tayga-path"; then
        read -r TAYGA_BINARY <"$WORK_DIRECTORY/tayga-path"
        return
    fi
    printf 'TAYGA is absent; the test will extract its Debian package without installing it.\n'
    require_command apt-get
    require_command dpkg-deb
    mkdir "$WORK_DIRECTORY/package"
    (
        cd "$WORK_DIRECTORY/package"
        apt-get download tayga
    )
    packages=("$WORK_DIRECTORY/package"/*.deb)
    if [[ "${#packages[@]}" -ne 1 || ! -f "${packages[0]}" ]]; then
        fail 'The TAYGA download did not produce exactly one Debian package.'
    fi
    dpkg-deb --extract "${packages[0]}" "$WORK_DIRECTORY/extracted"
    TAYGA_BINARY="$WORK_DIRECTORY/extracted/usr/sbin/tayga"
    if [[ ! -x "$TAYGA_BINARY" ]]; then
        fail 'The extracted Debian package does not contain an executable usr/sbin/tayga.'
    fi
}

wait_for_http() {
    local namespace="$1"
    local address="$2"
    local port="$3"
    local attempt
    for ((attempt = 1; attempt <= HTTP_START_ATTEMPTS; attempt++)); do
        if ip netns exec "$namespace" curl --noproxy '*' --fail --silent --show-error \
            --connect-timeout 1 --max-time 2 --output "$WORK_DIRECTORY/ready-response" \
            "http://$address:$port/before-v4"; then
            cmp "$WORK_DIRECTORY/expected-response" "$WORK_DIRECTORY/ready-response"
            return
        fi
        printf 'The HTTP server in %s was not ready on attempt %s.\n' "$namespace" "$attempt" >&2
        sleep 0.25
    done
    fail "The HTTP server in $namespace did not start."
}

probe_outbound() {
    local namespace="$1"
    local family="$2"
    local destination="$3"
    local request_path="$4"
    local expected_source="$5"
    local source_pattern="${expected_source//./\\.}"
    ip netns exec "$namespace" curl "$family" --globoff --noproxy '*' \
        --fail --silent --show-error --connect-timeout 2 --max-time 5 \
        --output "$WORK_DIRECTORY/$request_path.response" \
        "http://$destination:$UPSTREAM_PORT/$request_path"
    cmp "$WORK_DIRECTORY/expected-response" "$WORK_DIRECTORY/$request_path.response"
    if ! grep -Eq "^$source_pattern .*\"GET /$request_path " "$WORK_DIRECTORY/upstream.log"; then
        fail "The upstream did not observe $expected_source for $request_path."
    fi
    printf 'PASS: %s completed HTTP with upstream source %s.\n' "$request_path" "$expected_source"
}

expect_rejected() {
    local curl_status
    if ip netns exec "$UPSTREAM_NAMESPACE" curl --noproxy '*' --fail --silent --show-error \
        --connect-timeout 1 --max-time 2 --output "$WORK_DIRECTORY/unsolicited.response" \
        "http://$GUEST_IPV4:$GUEST_PORT/before-v4"; then
        fail 'The production firewall accepted an unsolicited upstream connection.'
    else
        curl_status=$?
        printf 'PASS: The production firewall rejected the unsolicited connection; curl returned %s.\n' "$curl_status"
    fi
}

if [[ "$#" -ne 1 ]]; then
    fail 'Usage: bash test-network.sh DIRECTORY'
fi
if [[ "$OSTYPE" != linux* ]]; then
    fail 'Run this integration test on Linux with network namespace support.'
fi
if [[ "$EUID" -ne 0 ]]; then
    fail 'Run this integration test as root to create isolated network namespaces.'
fi

readonly CONFIG_DIRECTORY="$1"
readonly TEST_ID="vault-network-$$-$RANDOM"
readonly WORK_DIRECTORY="/tmp/$TEST_ID"
readonly ROUTER_NAMESPACE="$TEST_ID-router"
readonly GUEST6_NAMESPACE="$TEST_ID-guest6"
readonly GUEST4_NAMESPACE="$TEST_ID-guest4"
readonly UPSTREAM_NAMESPACE="$TEST_ID-upstream"
readonly GUEST_IPV4="10.250.0.20"
readonly GUEST_IPV6="3d06:bad:b01::20"
readonly GATEWAY_IPV4="10.250.0.254"
readonly GATEWAY_IPV6="3d06:bad:b01::254"
readonly UPLINK_BEFORE="192.0.2.2"
readonly UPLINK_AFTER="192.0.2.3"
readonly UPSTREAM_GATEWAY="192.0.2.1"
readonly UPSTREAM_SERVICE="203.0.113.10"
readonly UPSTREAM_PORT=18080
readonly GUEST_PORT=18081
readonly OOB_PORT=18082
readonly HTTP_START_ATTEMPTS=60

WORK_DIRECTORY_CREATED=false
TUN_CREATED=false
TAYGA_BINARY=""
NAT64_PREFIX=""
SERVICE_START=""
SERVICE_STOP=""
SERVICE_COMMAND=()
SERVICE_PRE_COMMANDS=()
PROCESS_IDS=()
CREATED_NAMESPACES=()
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

main() {
    local required_file dependency request_path config_line directive value
    local service_line namespace port service_command
    local uplink_key uplink_value
    local oob_address=""
    local oob_host

    for required_file in guest-network.nft tayga.conf tayga.service forwarding.conf uplink.network; do
        if [[ ! -r "$CONFIG_DIRECTORY/$required_file" ]]; then
            fail "The production input is missing or unreadable: $CONFIG_DIRECTORY/$required_file"
        fi
    done
    for dependency in ip nft curl python3 sysctl grep cmp mkdir rm cat cp sleep; do
        require_command "$dependency"
    done
    if [[ ! -c /dev/net/tun ]]; then
        fail 'The Linux host does not provide the required /dev/net/tun device.'
    fi

    mkdir -m 0700 -- "$WORK_DIRECTORY"
    WORK_DIRECTORY_CREATED=true
    prepare_tayga
    mkdir "$WORK_DIRECTORY/tayga-state" "$WORK_DIRECTORY/http"
    printf '%s\n' 'vault-native-network-response' >"$WORK_DIRECTORY/expected-response"
    for request_path in before-v4 before-v6 after-v4 after-v6; do
        cp "$WORK_DIRECTORY/expected-response" "$WORK_DIRECTORY/http/$request_path"
    done

    while IFS= read -r config_line; do
        directive="${config_line%% *}"
        value="${config_line#* }"
        case "$directive" in
        data-dir)
            printf 'data-dir %s\n' "$WORK_DIRECTORY/tayga-state"
            ;;
        prefix)
            NAT64_PREFIX="$value"
            printf '%s\n' "$config_line"
            ;;
        *)
            printf '%s\n' "$config_line"
            ;;
        esac
    done <"$CONFIG_DIRECTORY/tayga.conf" >"$WORK_DIRECTORY/tayga.conf"
    if [[ "$NAT64_PREFIX" != *::/96 ]]; then
        fail 'The production configuration must provide a compressed /96 NAT64 prefix for this test vector.'
    fi
    readonly NAT64_DESTINATION="${NAT64_PREFIX%/96}cb00:710a"

    while IFS='=' read -r uplink_key uplink_value; do
        if [[ "$uplink_key" == "Address" ]]; then
            oob_address="$uplink_value"
        fi
    done <"$CONFIG_DIRECTORY/uplink.network"
    if [[ -z "$oob_address" ]]; then
        fail 'The production uplink configuration must declare the OOB IPv6 address.'
    fi
    oob_host="${oob_address%/*}"

    while IFS= read -r service_line; do
        case "$service_line" in
        ExecStartPre=*) SERVICE_PRE_COMMANDS+=("${service_line#ExecStartPre=}") ;;
        ExecStart=*) SERVICE_START="${service_line#ExecStart=}" ;;
        ExecStopPost=*) SERVICE_STOP="${service_line#ExecStopPost=}" ;;
        esac
    done <"$CONFIG_DIRECTORY/tayga.service"
    if [[ "${#SERVICE_PRE_COMMANDS[@]}" -eq 0 || -z "$SERVICE_START" || -z "$SERVICE_STOP" ]]; then
        fail 'The production TAYGA unit must define its setup, foreground process, and teardown commands.'
    fi

    for namespace in "$ROUTER_NAMESPACE" "$GUEST6_NAMESPACE" "$GUEST4_NAMESPACE" "$UPSTREAM_NAMESPACE"; do
        ip netns add "$namespace"
        CREATED_NAMESPACES+=("$namespace")
        ip -n "$namespace" link set lo up
    done
    ip netns exec "$GUEST4_NAMESPACE" sysctl -w net.ipv6.conf.all.disable_ipv6=1
    ip netns exec "$UPSTREAM_NAMESPACE" sysctl -w net.ipv6.conf.all.disable_ipv6=1
    ip -n "$ROUTER_NAMESPACE" link add vmbr0 type bridge
    ip -n "$ROUTER_NAMESPACE" address add "$GATEWAY_IPV4/24" dev vmbr0
    ip -n "$ROUTER_NAMESPACE" -6 address add "$GATEWAY_IPV6/64" dev vmbr0 nodad
    ip -n "$ROUTER_NAMESPACE" link set vmbr0 up
    ip -n "$ROUTER_NAMESPACE" link add guest6-port type veth peer name eth0 netns "$GUEST6_NAMESPACE"
    ip -n "$ROUTER_NAMESPACE" link add guest4-port type veth peer name eth0 netns "$GUEST4_NAMESPACE"
    for port in guest6-port guest4-port; do
        ip -n "$ROUTER_NAMESPACE" link set "$port" master vmbr0
        ip -n "$ROUTER_NAMESPACE" link set "$port" up
    done
    ip -n "$GUEST6_NAMESPACE" link set eth0 up
    ip -n "$GUEST6_NAMESPACE" -6 address add "$GUEST_IPV6/64" dev eth0 nodad
    ip -n "$GUEST6_NAMESPACE" -6 route add default via "$GATEWAY_IPV6"
    ip -n "$GUEST6_NAMESPACE" -4 -o address show dev eth0 >"$WORK_DIRECTORY/guest6-ipv4"
    if [[ -s "$WORK_DIRECTORY/guest6-ipv4" ]]; then
        fail 'The IPv6-only guest unexpectedly has an IPv4 interface address.'
    fi
    ip -n "$GUEST4_NAMESPACE" link set eth0 up
    ip -n "$GUEST4_NAMESPACE" address add "$GUEST_IPV4/24" dev eth0
    ip -n "$GUEST4_NAMESPACE" route add default via "$GATEWAY_IPV4"
    ip -n "$ROUTER_NAMESPACE" link add mbrains type veth peer name eth0 netns "$UPSTREAM_NAMESPACE"
    ip -n "$ROUTER_NAMESPACE" address add "$UPLINK_BEFORE/24" dev mbrains
    ip -n "$ROUTER_NAMESPACE" link set mbrains up
    ip -n "$ROUTER_NAMESPACE" route add default via "$UPSTREAM_GATEWAY"
    ip -n "$UPSTREAM_NAMESPACE" address add "$UPSTREAM_GATEWAY/24" dev eth0
    ip -n "$UPSTREAM_NAMESPACE" address add "$UPSTREAM_SERVICE/32" dev lo
    ip -n "$UPSTREAM_NAMESPACE" link set eth0 up
    ip netns exec "$ROUTER_NAMESPACE" sysctl -w net.ipv6.conf.mbrains.disable_ipv6=1
    ip netns exec "$ROUTER_NAMESPACE" sysctl --load "$CONFIG_DIRECTORY/forwarding.conf"
    ip -n "$ROUTER_NAMESPACE" -6 address add "$oob_address" dev mbrains nodad

    for service_command in "${SERVICE_PRE_COMMANDS[@]}"; do
        prepare_service_command "$service_command"
        ip netns exec "$ROUTER_NAMESPACE" "${SERVICE_COMMAND[@]}"
        TUN_CREATED=true
    done
    prepare_service_command "$SERVICE_START"
    ip netns exec "$ROUTER_NAMESPACE" "${SERVICE_COMMAND[@]}" >"$WORK_DIRECTORY/tayga.log" 2>&1 &
    PROCESS_IDS+=("$!")
    ip netns exec "$ROUTER_NAMESPACE" nft --file "$CONFIG_DIRECTORY/guest-network.nft"
    ip netns exec "$UPSTREAM_NAMESPACE" python3 -m http.server "$UPSTREAM_PORT" \
        --bind 0.0.0.0 --directory "$WORK_DIRECTORY/http" >"$WORK_DIRECTORY/upstream.log" 2>&1 &
    PROCESS_IDS+=("$!")
    ip netns exec "$GUEST4_NAMESPACE" python3 -m http.server "$GUEST_PORT" \
        --bind 0.0.0.0 --directory "$WORK_DIRECTORY/http" >"$WORK_DIRECTORY/guest4.log" 2>&1 &
    PROCESS_IDS+=("$!")
    ip netns exec "$ROUTER_NAMESPACE" python3 -m http.server "$OOB_PORT" \
        --bind "$oob_host" --directory "$WORK_DIRECTORY/http" >"$WORK_DIRECTORY/oob.log" 2>&1 &
    PROCESS_IDS+=("$!")
    wait_for_http "$UPSTREAM_NAMESPACE" "$UPSTREAM_SERVICE" "$UPSTREAM_PORT"
    wait_for_http "$ROUTER_NAMESPACE" "$GUEST_IPV4" "$GUEST_PORT"
    wait_for_http "$GUEST6_NAMESPACE" "[$oob_host]" "$OOB_PORT"
    printf 'PASS: The production OOB IPv6 address accepted HTTP after IPv6 recovery.\n'

    probe_outbound "$GUEST6_NAMESPACE" -6 "[$NAT64_DESTINATION]" before-v6 "$UPLINK_BEFORE"
    probe_outbound "$GUEST4_NAMESPACE" -4 "$UPSTREAM_SERVICE" before-v4 "$UPLINK_BEFORE"

    ip -n "$ROUTER_NAMESPACE" address delete "$UPLINK_BEFORE/24" dev mbrains
    ip -n "$ROUTER_NAMESPACE" address add "$UPLINK_AFTER/24" dev mbrains
    ip -n "$ROUTER_NAMESPACE" route replace default via "$UPSTREAM_GATEWAY"
    probe_outbound "$GUEST6_NAMESPACE" -6 "[$NAT64_DESTINATION]" after-v6 "$UPLINK_AFTER"
    probe_outbound "$GUEST4_NAMESPACE" -4 "$UPSTREAM_SERVICE" after-v4 "$UPLINK_AFTER"

    ip -n "$UPSTREAM_NAMESPACE" route add 10.250.0.0/24 via "$UPLINK_AFTER"
    expect_rejected
    ip netns exec "$ROUTER_NAMESPACE" nft flush chain inet vault_guests forward
    ip netns exec "$UPSTREAM_NAMESPACE" curl --noproxy '*' --fail --silent --show-error \
        --connect-timeout 1 --max-time 2 --output "$WORK_DIRECTORY/control.response" \
        "http://$GUEST_IPV4:$GUEST_PORT/before-v4"
    cmp "$WORK_DIRECTORY/expected-response" "$WORK_DIRECTORY/control.response"
    printf 'PASS: Removing only the isolated forwarding rules admitted the unsolicited control connection.\n'
    ip netns exec "$ROUTER_NAMESPACE" nft --file "$CONFIG_DIRECTORY/guest-network.nft"
    expect_rejected

    cat "$WORK_DIRECTORY/upstream.log"
    printf 'All native guest network tests passed.\n'
}

main

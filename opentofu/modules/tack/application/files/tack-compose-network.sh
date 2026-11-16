#!/usr/bin/env bash

set -euo pipefail

readonly COMPOSE_DIRECTORY=/root/tack
readonly NETWORK_NAME=tack_default
readonly ROUTED_MODE=routed
readonly LISTENER_UNIT=tack-k3s-kafka-listener.service
readonly KAFKA_WAIT_ATTEMPTS=90
readonly KAFKA_WAIT_INTERVAL=2
readonly COMPOSE_FILES=(docker-compose.yml docker-compose.override.yml docker-compose.k3s-kafka.yml)
readonly COMPOSE_COMMAND=(/usr/bin/docker compose -f docker-compose.yml -f docker-compose.override.yml -f docker-compose.k3s-kafka.yml)

wait_for_kafka() {
    local attempt
    local container_id
    local container_state

    for ((attempt = 1; attempt <= KAFKA_WAIT_ATTEMPTS; attempt++)); do
        container_id=$("${COMPOSE_COMMAND[@]}" ps --all --quiet kafka)
        if [[ -z "${container_id}" ]]; then
            printf 'Waiting for the managed Kafka listener to create its container (%s/%s).\n' "${attempt}" "${KAFKA_WAIT_ATTEMPTS}"
        else
            container_state=$(/usr/bin/docker inspect --format '{{.State.Status}} {{.State.Health.Status}}' "${container_id}")
            if [[ "${container_state}" == 'running healthy' ]]; then
                printf 'The managed Kafka container is running and healthy.\n'
                return
            fi
            printf 'Waiting for the managed Kafka container; current state: %s (%s/%s).\n' "${container_state}" "${attempt}" "${KAFKA_WAIT_ATTEMPTS}"
        fi
        /usr/bin/sleep "${KAFKA_WAIT_INTERVAL}"
    done

    printf 'The managed Kafka container did not become running and healthy after %s checks.\n' "${KAFKA_WAIT_ATTEMPTS}" >&2
    return 1
}

main() {
    local required_file
    local existing_network
    local network_mode

    cd "${COMPOSE_DIRECTORY}"
    for required_file in "${COMPOSE_FILES[@]}" .env; do
        if [[ ! -r "${required_file}" ]]; then
            printf 'Cannot read required Compose input: %s/%s\n' "${COMPOSE_DIRECTORY}" "${required_file}" >&2
            exit 1
        fi
    done

    /usr/bin/systemctl daemon-reload
    existing_network=$(/usr/bin/docker network ls --filter "name=^${NETWORK_NAME}$" --format '{{.Name}}')
    if [[ -z "${existing_network}" ]]; then
        printf 'Network %s does not exist; resume Compose creation.\n' "${NETWORK_NAME}"
        /usr/bin/systemctl stop "${LISTENER_UNIT}"
    else
        if [[ "${existing_network}" != "${NETWORK_NAME}" ]]; then
            printf 'Docker returned unexpected network names for %s: %s\n' "${NETWORK_NAME}" "${existing_network}" >&2
            exit 1
        fi
        network_mode=$(/usr/bin/docker network inspect --format '{{index .Options "com.docker.network.bridge.gateway_mode_ipv6"}}' "${NETWORK_NAME}")
        if [[ "${network_mode}" != "${ROUTED_MODE}" ]]; then
            printf 'Network %s uses IPv6 mode %s; recreate the Compose network.\n' "${NETWORK_NAME}" "${network_mode}"
            /usr/bin/systemctl stop "${LISTENER_UNIT}"
            "${COMPOSE_COMMAND[@]}" down
        else
            printf 'Network %s already uses routed IPv6; preserve the network and running Kafka.\n' "${NETWORK_NAME}"
        fi
    fi

    /usr/bin/systemctl start "${LISTENER_UNIT}"
    wait_for_kafka
    "${COMPOSE_COMMAND[@]}" up -d --no-deps --no-build app audit-consumer
}

main "$@"

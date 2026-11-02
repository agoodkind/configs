#!/usr/bin/env bash

stop_legacy_unit() {
    local unit_name="$1"
    local load_state

    load_state=$(systemctl show --property=LoadState --value "$unit_name")
    if [[ "$load_state" == "not-found" ]]; then
        return
    fi
    systemctl disable --now "$unit_name"
}

process_owned_tables() {
    local action="$1"
    local role_name="$2"
    shift 2
    local owned_tables=("$@")
    local table_listing
    local declaration
    local table_family
    local table_name
    local owned_table
    local matched

    table_listing=$(nft list tables)
    while read -r declaration table_family table_name; do
        if [[ -z "$declaration" ]]; then
            continue
        fi
        matched=false
        for owned_table in "${owned_tables[@]}"; do
            if [[ "$table_family:$table_name" == "$owned_table" ]]; then
                matched=true
                break
            fi
        done
        if [[ "$matched" == false ]]; then
            printf 'Cannot stop nftables: table %s %s does not belong to %s.\n' \
                "$table_family" "$table_name" "$role_name" >&2
            return 1
        fi
        if [[ "$action" == "delete" ]]; then
            nft delete table "$table_family" "$table_name"
        fi
    done < <(printf '%s\n' "$table_listing")
}

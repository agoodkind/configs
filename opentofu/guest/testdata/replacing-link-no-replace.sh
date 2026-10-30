# Only the mutation test uses this function. It replaces a target on purpose.
link_no_replace() {
    local candidate_path="$2"
    local target_path="$3"
    if [[ -e "${target_path}" ]]; then
        return 1
    fi
    sleep 2
    install -m 0755 "${candidate_path}" "${target_path}"
}

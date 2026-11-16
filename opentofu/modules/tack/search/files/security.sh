#!/usr/bin/env bash
set -euo pipefail
cd /opt/tack-qa/search
security_admin() {
    docker compose exec -T opensearch /usr/share/opensearch/plugins/opensearch-security/tools/securityadmin.sh -h ::1 -p 9200 -icl -nhnv -cacert config/tack/ca.pem -cert config/tack/admin.pem -key config/tack/admin-key.pem "$@"
}
for i in {1..24}; do
    if security_admin -cd /usr/share/opensearch/config/tack; then
        exit 0
    fi
    printf 'OpenSearch security initialization attempt %s failed.\n' "$i" >&2
    sleep 5
done
printf 'OpenSearch security initialization did not finish within 24 attempts.\n' >&2
exit 1

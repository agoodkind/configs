#!/usr/bin/env bash
# Mails one failed unit's output to the Tack operator. Deployed by Ansible
# (tasks/tack-backup-staleness.yml) and run only from the OnFailure= handler
# instance, which is why the failed unit's name arrives as an argument and its
# invocation id arrives in the environment: systemd passes MONITOR_* only when
# each failing unit gets its own handler instance, so the handler is a template
# and every OnFailure= names it as handler@%n.service (systemd.exec(5)).
#
# Transport is the msmtp every guest already carries: prep-guests installs the
# msmtp package and tasks/smtp.yml renders /etc/msmtprc with the SMTP2GO
# account and this guest's From address. msmtp supplies the From and Date
# headers itself (set_from_header and set_date_header both default to auto),
# so the message below carries only To and Subject.
set -euo pipefail

FAILED_UNIT="$1"
RECIPIENT="$2"

MSMTP=/usr/bin/msmtp
JOURNAL_LINES=200

# Scope the body to the run that just failed. MONITOR_INVOCATION_ID names that
# one runtime cycle and _SYSTEMD_INVOCATION_ID is the journal field carrying
# it, so the mail can never quote an earlier run's report. systemd leaves the
# variable unset when several units share one non-templated handler; this
# handler is templated so that case does not arise, and an empty value falls
# through to the unit's recent lines with the branch logged rather than mailing
# nothing.
if [[ -n "${MONITOR_INVOCATION_ID:-}" ]]; then
    journal_scope=("_SYSTEMD_INVOCATION_ID=${MONITOR_INVOCATION_ID}")
else
    echo "tack-alarm-mail: MONITOR_INVOCATION_ID is unset; mailing the last ${JOURNAL_LINES} lines of ${FAILED_UNIT}" >&2
    journal_scope=("-u" "${FAILED_UNIT}" "-n" "${JOURNAL_LINES}")
fi

# A journal that cannot be read becomes the body rather than the end of the
# run. The alarm exists to make a silent backup failure loud, so it must still
# send when it cannot quote the failure, and the reason travels with it.
if ! body="$(journalctl "${journal_scope[@]}" --no-pager --output=cat 2>&1)"; then
    body="tack-alarm-mail: could not read the journal for ${FAILED_UNIT}: ${body}"
fi

subject="[tack] ${FAILED_UNIT} failed on $(hostname -f)"

printf 'To: %s\nSubject: %s\nContent-Type: text/plain; charset=utf-8\n\n%s\n' \
    "${RECIPIENT}" "${subject}" "${body}" | "${MSMTP}" -- "${RECIPIENT}"

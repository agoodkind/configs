#!/bin/sh
# Print one word naming this guest's global authorized_keys state, and change
# nothing. The audit-ssh-keys playbook runs this inside each guest from its
# hypervisor, so it stays POSIX sh for guests without bash, OPNsense included.
#
#   no-drop-in        sshd is not pointed at the global key file
#   key-file-missing  the drop-in exists and the key file does not
#   key-file-empty    the drop-in exists and the key file is empty
#   ok                the drop-in exists and the key file has content
set -eu

DROP_IN=/etc/ssh/sshd_config.d/99-sshpiper-global-authorized-keys.conf
KEY_FILE=/etc/ssh/authorized_keys.d/authorized_keys

if [ ! -e "$DROP_IN" ]; then
    echo "no-drop-in"
elif [ ! -e "$KEY_FILE" ]; then
    echo "key-file-missing"
elif [ ! -s "$KEY_FILE" ]; then
    echo "key-file-empty"
else
    echo "ok"
fi

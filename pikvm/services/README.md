# PiKVM systemd units

Drop-in units for the PiKVM appliance at `10.240.0.57`.

## Install

PiKVM's root filesystem is read-only by default. Remount, copy, enable, remount.

```bash
scp pikvm-auto-update.{service,timer} 10.240.0.57:/tmp/
ssh 10.240.0.57 'sudo rw \
  && sudo install -m 0644 /tmp/pikvm-auto-update.service /etc/systemd/system/ \
  && sudo install -m 0644 /tmp/pikvm-auto-update.timer   /etc/systemd/system/ \
  && sudo systemctl daemon-reload \
  && sudo systemctl enable --now pikvm-auto-update.timer \
  && sudo ro'
```

## Units

- `pikvm-auto-update.timer` fires Sun 04:00 with up to 30m jitter, and `Persistent=true` so a missed run after downtime catches up.
- `pikvm-auto-update.service` runs `/usr/bin/pikvm-update`, which reboots when packages require it.

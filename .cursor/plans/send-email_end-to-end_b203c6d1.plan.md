---
name: send-email end-to-end
overview: Initialize the send-email git repo, push to GitHub, cross-compile for linux/amd64, deploy the binary to Proxmox vault, and run a live end-to-end test sending an email via the SMTP2GO HTTP API using the existing API key in /etc/mwan-watchdog/watchdog.env.
todos:
  - id: git-init-push
    content: git init, .gitignore, gh repo create --private, initial commit and push
    status: completed
  - id: cross-compile
    content: GOOS=linux GOARCH=amd64 go build, verify binary
    status: completed
  - id: deploy-vault
    content: scp binary to vault as /usr/local/bin/send-email-go
    status: completed
  - id: live-test
    content: SSH to vault, run send-email-go with --http and SMTP2GO key, verify email arrives
    status: completed
  - id: parity-check
    content: Run bash send-email with same args, compare emails side by side
    status: completed
isProject: false
---

# send-email: init, deploy, live test on vault

## Current state

- Code lives at `~/Sites/send-email/` with 16 files, compiles and passes tests locally
- Not a git repo yet, no GitHub remote
- Vault (Proxmox host, `3d06:bad:b01::254`) has:
  - `SMTP2GO_API_KEY` in `/etc/mwan-watchdog/watchdog.env`
  - `ALERT_EMAIL=alex@goodkind.io` in same file
  - Bash `send-email` at `/opt/scripts/send-email` and `/usr/local/bin/send-email`
  - No Go toolchain
  - Debian 13 (trixie), x86_64

## Steps

### 1. Create GitHub repo and push

- `git init` in `~/Sites/send-email/`
- Add `.gitignore` (binary name `send-email`, no extension)
- `gh repo create agoodkind/send-email --private --source=. --push`
- Verify remote is set and initial commit is pushed

### 2. Cross-compile for vault

- `GOOS=linux GOARCH=amd64 go build -o send-email-linux-amd64 .`
- Built locally on the Mac, produces a static-ish binary for Debian

### 3. Deploy binary to vault

- `scp send-email-linux-amd64 root@3d06:bad:b01::254:/usr/local/bin/send-email-go`
- Install as `/usr/local/bin/send-email-go` (not overwriting the bash version yet)

### 4. Live test on vault

SSH to vault and run:

```
/usr/local/bin/send-email-go \
  -t alex@goodkind.io \
  -s "send-email-go test from vault" \
  -m "End-to-end test of the Go send-email binary.\nThis was sent via SMTP2GO HTTP API." \
  -k "$(grep SMTP2GO_API_KEY /etc/mwan-watchdog/watchdog.env | cut -d= -f2 | tr -d '\"')" \
  --http
```

Verify:
- CLI exits 0 and prints "Email sent to ... via http"
- Email arrives with HTML metadata footer (uptime, load, memory, disk, IPs, ISP)

### 5. Verify parity with bash version

Run same test with the existing bash binary for comparison:

```
/opt/scripts/send-email \
  -t alex@goodkind.io \
  -s "send-email-bash test from vault" \
  -m "Baseline test from the bash send-email." \
  --http \
  -k "$(grep SMTP2GO_API_KEY /etc/mwan-watchdog/watchdog.env | cut -d= -f2 | tr -d '\"')"
```

Compare the two emails side by side in inbox for format parity.

### What this does NOT do (yet)

- Does not replace the bash binary at `/opt/scripts/send-email` (we leave both until verified)
- Does not test the msmtprc/SMTP path (vault has no msmtprc; that is a MWAN VM test)
- Does not touch the watchdog or the `mwan/go/` infra-tools code

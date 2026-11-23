# SSH Proxy Server - Release Notes

## Pre-production fixes

0. **FR-1.1/FR-1.2 CIDR and SSO-group ACL rules** — `access_control` rules now
   accept CIDR target patterns (`10.0.0.0/8` matches target host IPs in the
   range on the default port) and an optional `groups` list: the rule applies
   when `user` matches the SSH login user or the client is a member of one of
   the listed SSO groups (case-insensitive, `user` may be omitted). Groups
   come from the SSO identity after device authorization and are evaluated on
   both the `LC_SSH_SERVER` and the static-routing paths.

0. **FR-2.1 session lifecycle events enriched** — `session_start` and all
   `session_end` variants now carry `key_fingerprint` (of the client's public
   key) and `sso_result` (`success` / `failure` / `not_required`), so a single
   event per session holds the identity proof AC FR-1 requires; correlation by
   `session_id` remains available for the auth/sso event stream.

1. **FR-1.3 e2e coverage for ACL** — `e2e/acl_test.go` (host-run) covers both ACL
   enforcement points: static routing (`connectToFirstAvailableTarget`,
   proxy on `:2222`) and `LC_SSH_SERVER` env requests (`handleEnvRequest`,
   dedicated `proxy-acl` instance on `:2223` with dynamic routing). alice is
   allowed, bob (no rule, `default: deny`) is refused on both paths.
   Unit regressions for the client-vs-target user bug:
   `TestHandleEnvRequestAllowsCrossUserTargetWhenClientUserAllowed`,
   `TestHandleEnvRequestDeniesWhenClientUserNotAllowed`,
   `TestResolveTargetAddressKeepsExplicitTargetUser` (explicit target user is
   preserved; the ACL decision is always made for the SSH login user).
   Verified against live Docker infrastructure: all 9 host-run tests pass,
   plus the full in-container suite (certificate/sequential/multi-session)
   with `access_control` enabled on both proxies. Companion fixes:
   `docker-compose.yml` now waits for the CA to be healthy before starting
   the target (the target previously raced the key generation and exited),
   the missing `e2e/Dockerfile.test` was added, host-only and container-only
   test files are separated by the `e2econtainer` build tag, and a repo-root
   `.dockerignore` keeps `e2e-keys`/recordings out of image build contexts.

2. **FR-2.3 env value leak fixed** — debug logging no longer prints SendEnv
   variable values (they may carry client tokens); only variable names are
   logged (`name=<redacted>`). Regression: `TestHandleEnvRequestDoesNotLogEnvValues`.

3. **FR-3.1 config watch survives atomic saves** — `internal/configwatch` now
   watches the parent directory of each config-related file (filtered by file
   name) instead of the file inode, so vim-style save-by-rename no longer
   silently drops the watch. SIGHUP remains as a fallback. Covered by
   `TestWatchDetectsAtomicRenameReplace` and friends.

4. **FR-2.2 audit events carry `level`** — `audit.Event` has a `level` field
   (`info`/`warning`, derived from `result` when not set explicitly) so
   Loki/Alertmanager can filter/alert on severity without parsing free text.

## Security Hardening (FR-1..FR-4 + Should/Could)

### Must (production gate)

1. **FR-1 Target ACL** (`internal/access`, `access_control` config section)
   - Per-user rules `{"user": "name-or-glob", "groups": ["sso-group"], "targets": ["host[:port]", "web-*.prod", "10.0.0.0/8", "*"]}` (groups optional; CIDR ranges match target host IPs on the default port)
   - `default: allow|deny` for users without a matching rule
   - Enforced both on `LC_SSH_SERVER` parsing and on every target connection attempt (covers static routing, failover retries, and SFTP)
   - Denials are logged and audited as `target_denied`

2. **FR-2 Structured JSON audit log** (`internal/audit`)
   - One JSON object per line on stdout: `auth_success`, `auth_failure`, `target_denied`, `session_start`, `session_end` (result/exit_status/duration/recording_id), `sso_success`, `sso_failure`, `handshake_rate_limited`
   - Designed for the journald → Loki → Alertmanager pipeline

3. **FR-3 Hot reload** (`SIGHUP` + fsnotify via `internal/configwatch`)
   - Config, ACL, trusted CA certs, log level, and rate limit are rebuilt for new connections
   - Active sessions keep their snapshot and are not interrupted
   - `authorized_keys` is re-read on every authentication (and watched for logging)

4. **FR-4 Secret indirection** (`internal/secrets`)
   - `sso.client_secret` supports `{"$env": "VAR"}`, `{"$file": "/path"}`, `"$env:VAR"`, `"$file:/path"`
   - Secrets no longer need to be plaintext in the canonical git config

### Should

- **Recording retention & integrity**: `recordings.retention_days` cleanup (hourly janitor) + `recordings.integrity` SHA-256 `.sha256` sidecars
- **Ed25519 host keys** for new installs; existing RSA keys still load
- **Handshake rate limiting**: `rate_limit` — sliding window per source IP
- **Centralized `known_hosts`**: `known_hosts` config path instead of `~/.ssh/known_hosts`
- **Keycloak group check**: `sso.required_groups` validated against `groups` / `realm_access.roles` claims
- **`/healthz`**: `health` config section, standalone HTTP endpoint

### Could

- **SFTP subsystem** (gated by `sftp_enabled`): raw binary piping to the target, not recorded
- **Per-target metrics**: `ssh_proxy_proxy_sessions_by_target_total{result, target}`

### Bug fixes (found during live e2e against a real target)

1. **ACL was evaluated against the target user instead of the client user** (`internal/server/server.go`)
   - `ACL.Allowed()` received `targetUser` at both enforcement points (`LC_SSH_SERVER` env handling and static routing), so FR-1 only worked when client and target usernames happened to match; a correctly configured rule still denied legitimate cross-user hops (e.g. client `alice` → target `bob@host`)
   - Now evaluated against the authenticated client (`sessionUser`); the audit `target_denied` event also reports `user` (client) and `target_user` (target) separately

2. **Clients hung after session end; agent channel and connection leaked** (`internal/server/server.go`, `internal/types`, `internal/client`)
   - The accepted `auth-agent@openssh.com` channel was never closed by the proxy, so OpenSSH clients waited forever for the channel to close before exiting, and the client connection stayed open indefinitely
   - The agent channel is now tracked in `SessionState` (`SetAgentChannel`/`TakeAgentChannel`) and closed when the session channel ends; the cached agent client is cleared so a subsequent session on the same connection re-opens it

---

## Overview

SSH proxy server with dynamic target routing via `LC_SSH_SERVER` or static routing through a fixed server list, SSH agent-based target authentication, optional Keycloak-backed SSO second factor, session recording, clean shutdown handling, terminal resize forwarding, and config-driven startup via `-config`. Interactive terminal sessions are enabled by default; direct commands require `allow_direct_commands` in the JSON config.

## What Was Implemented

### Core Features ✅

1. **SSH Server** (`server.go`)
   - Accepts SSH client connections with public key authentication
   - Handles SSH channel requests (`shell`, `pty-req`, `window-change`, and `exec` when `allow_direct_commands` is enabled in the JSON config)
   - **NEW: Handles SSH environment variables via `env` channel requests**

2. **Dynamic and Static Routing** ✅ NEW
   - SSH client can send `LC_SSH_SERVER` using `-o "SendEnv=LC_SSH_SERVER"`
   - Proxy also supports a fixed `static_routing.servers` list in `config.json`
   - Static mode supports `failover` and `round_robin`
   - Global `connect_timeout_seconds` and retry rounds via `retries` for both dynamic and static routing
   - When static routing is enabled, `LC_SSH_SERVER` becomes optional and is ignored

3. **Session Recording** (`recording.go`)
   - Records sessions in `asciinema` v2 by default, or in plain `script` transcript format when selected
   - Unique session IDs for each recording
   - Thread-safe recording with mutex
   - Tracks both input and output

4. **Host Key Management** (`hostkey.go`)
   - Automatic 2048-bit RSA host key generation on first run
   - Key persistence and loading from disk
   - PEM format encoding

5. **Target SSH Connection** (`client.go`)
   - Establishes a real outbound SSH connection to the target host
   - Reuses the client's SSH agent (`ssh -A` / `SSH_AUTH_SOCK`)
   - Prefers the same key that authenticated to the proxy
   - Uses `known_hosts` by default, with an optional `insecure_ignore_hostkey` config override for development

6. **Prometheus Metrics** ✅ NEW
   - Optional `metrics` block in `config.json`, disabled by default
   - Exposes a scrape endpoint such as `http://127.0.0.1:9090/metrics`
   - Built using `prometheus/client_golang`
   - Prometheus project: <https://prometheus.io/>
   - Prometheus documentation: <https://prometheus.io/docs/introduction/overview/>
   - Tracks SSH connections, handshake failures, proxy session outcomes, and SSO confirmation outcomes
   - **NEW:** exposes `ssh_proxy_sso_pending_sessions` to show how many SSH sessions are currently waiting for second-factor approval
   - **NEW:** exposes `ssh_proxy_sso_errors_total` to count failed SSO/2FA attempts, including timeouts and user-match rejections

7. **Keycloak SSO Second Factor** ✅ NEW
   - Optional `sso` block in `config.json`, disabled by default
   - Prints a browser verification link directly into the SSH terminal
   - Adds a 2FA confirmation step to reduce the chance of unauthorized bastion access
   - Waits up to `sso.auth_timeout_seconds` for approval before rejecting the session
   - Supports `sso.poll_interval_seconds` and `sso.connect_timeout_seconds` for approval polling cadence and per-request Keycloak timeout control
   - Supports `sso.enforce_ssh_user_match` to bind the approved Keycloak identity to the SSH username, with an explicit opt-out switch in config
   - Uses a minimal default `scope` of `openid`, since profile/email claims are not required by the proxy
   - Tested against a Keycloak realm named `ssh-proxy-server`
   - Keycloak project: <https://www.keycloak.org/>

### Documentation

- **README.md** - Features, usage, and examples
- **QUICKSTART.md** - Get started in 5 minutes
- **SENDENV.md** - Comprehensive SendEnv guide with troubleshooting
- **IMPLEMENTATION.md** - Architecture, design, and roadmap

## Usage

### Basic Setup

```bash
# Start proxy server
./ssh-proxy-server -config ./config.json

# In another terminal, connect with target via SendEnv + agent forwarding
LC_SSH_SERVER="target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost
```

### More Examples

```bash
# One-liner connection
LC_SSH_SERVER="server:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 admin@localhost

# Development-only startup if known_hosts mismatches must be ignored temporarily
# Set "insecure_ignore_hostkey": true in config.json, then run:
./ssh-proxy-server -config ./config.json

# SSH config file
cat >> ~/.ssh/config <<'EOF'
Host my-proxy
    HostName localhost
    Port 2222
    ForwardAgent yes
    SendEnv LC_SSH_SERVER
EOF

LC_SSH_SERVER="target:22" ssh your-user@my-proxy
```

## Key Implementation Details

### SendEnv Flow

1. **Client**: `LC_SSH_SERVER="host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" ...`
2. **SSH client**: Sends `LC_SSH_SERVER` to proxy during session setup
3. **Proxy receives**: `handleEnvRequest()` in server.go parses the variable
4. **Proxy stores**: In `SessionState.EnvVars`
5. **Proxy parses**: `user`, `host`, and `port`
6. **Proxy routes**: Opens a real target SSH session using the client's agent

### Code Changes

**`internal/server/server.go`**:
- Handles `env`, `shell`, `pty-req`, and `window-change`, plus gated `exec` requests when enabled
- Parses `LC_SSH_SERVER` into target user / host / port
- Proxies stdin/stdout/stderr to the target SSH session
- Cleanly closes the client session on `Ctrl+D`
- Logs session start/end and resize events

**`internal/client/client.go`**:
- Connects to the target host via SSH
- Uses forwarded/local SSH agent signers
- Prefers the same key used to authenticate to the proxy

**`internal/recording/recording.go`**:
- Asciinema v2 format recording
- Thread-safe frame writing with timing metadata

## Files Structure

```
ssh-proxy-server/
├── cmd/
│   └── ssh-proxy-server/
│       └── main.go
├── internal/
│   ├── client/
│   │   └── client.go
│   ├── hostkey/
│   │   └── hostkey.go
│   ├── recording/
│   │   └── recording.go
│   ├── server/
│   │   └── server.go
│   └── types/
│       └── types.go
├── recordings/
├── README.md
├── QUICKSTART.md
├── SENDENV.md
├── IMPLEMENTATION.md
└── example.sh
```

## How SendEnv Works

### Standard SSH SendEnv Option

The `-o "SendEnv=VAR_NAME"` option is part of standard OpenSSH client. When you use it:

1. Client reads `VAR_NAME` from your local environment
2. Includes it in the SSH protocol during session negotiation
3. Server receives it as an `env` channel request
4. Server can read and use the value

### Our Implementation

```go
// env requests are parsed from SSH's length-prefixed payload format
func handleEnvRequest(req *ssh.Request, state *types.SessionState) {
    // Parse uint32 name_len | name | uint32 value_len | value
    // Store in state.EnvVars map
    // If the name is LC_SSH_SERVER, split it into user / host / port
}

// When shell/exec starts, the proxy uses LC_SSH_SERVER first.
targetAddr := state.EnvVars["LC_SSH_SERVER"]
if targetAddr == "" {
    targetAddr = parseTargetFromCommand(command)
}
```

## Security Notes

✅ **Secure**:
- Variable values sent over encrypted SSH connection
- No plaintext transmission

⚠️ **Consider**:
- Don't include passwords/keys in variables
- Use server-side file permissions for recordings
- Implement access control for production
- Log all connections for audit trail

## Testing

```bash
# Terminal 1: Start proxy
cp ./config.example.json ./config.json
./ssh-proxy-server -config ./config.json

# Terminal 2: Connect
LC_SSH_SERVER="target-host.example.com:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost

# Check recordings
ls -la recordings/
cat recordings/*.cast | head -5
```

## What's Next?

Possible future enhancements:
- stricter target host verification and policy controls
- port forwarding support (`-L` / `-R`)
- SOCKS5 proxy mode for general TCP tunneling
- real-time session monitoring
- external recording storage backends
- HA / multi-instance deployment

## Build Requirements

- Go 1.21+
- Linux/macOS/Windows (tested on macOS Intel & Apple Silicon)

## Compilation

```bash
go build -o ssh-proxy-server ./cmd/ssh-proxy-server
```

Binary size: ~6.1MB (statically linked)

## License

Open source - use and modify as needed

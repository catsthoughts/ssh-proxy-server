# SSH Proxy Server

A transparent SSH proxy server written in Go for bastion-style access, SSH session auditing, and controlled target routing.
It accepts SSH connections with public key authentication, can either auto-accept client keys or validate them against an `authorized_keys` file, reuses the client's SSH agent to authenticate to the destination, supports either dynamic routing via `LC_SSH_SERVER=host[:port]` or static routing through a fixed server list with failover / retries / round-robin, can require a Keycloak-based SSO second factor before the SSH session continues, and uses the authenticated SSH session user for the final target login by default. All startup settings are loaded from a JSON config file passed via `-config`.

**Core capabilities:**
- Accepts inbound SSH connections with configurable client-key policy via `auto_accept_client_keys` and `authorized_keys` in the JSON config
- Reuses the client's forwarded SSH agent to authenticate to the target host
- Routes sessions dynamically with `LC_SSH_SERVER=host[:port]` or statically through a fixed target list with failover and round-robin
- Records interactive shell sessions in either `asciinema` or plain `script` transcript format for audit and analysis
- Can optionally allow direct command execution with `"allow_direct_commands": true` in the JSON config

**→ [Quick Start Guide](QUICKSTART.md) for immediate setup**

## Features

- **Authorized Client Access**: Accept client keys automatically by default, or enforce checks against the file configured in `authorized_keys`
- **Target ACL (FR-1)**: `access_control` restricts which targets each authenticated SSH user may reach, with `host[:port]` glob patterns (`web-*.prod`, `10.0.*`, `*`), CIDR ranges (`10.0.0.0/8`), SSO-group rules, and an allow/deny default
- **SSH Agent Reuse**: Requires forwarded agent access via `ssh -A` for target authentication
- **Certificate Support**: Supports SSH certificate-based authentication with `TrustedUserCAKeys` configuration - see **[CERTIFICATES.md](CERTIFICATES.md)** for setup examples
- **Session Recording**: Records proxied terminal activity in either `asciinema` (`.cast`) or plain `script` transcript (`.log`) format with private file permissions for audit and analysis
- **Recording Retention & Integrity**: `recordings.retention_days` expires old recordings automatically; `recordings.integrity` writes a SHA-256 `.sha256` sidecar for every finished recording
- **Structured JSON Audit Log (FR-2)**: Emits one JSON object per event on stdout (`auth_success`, `auth_failure`, `target_denied`, `session_start`, `session_end`, `sso_success`, `sso_failure`, `handshake_rate_limited`) with who/where/result/recording_id fields, ready for journald → Loki → Alertmanager
- **Hot Reload (FR-3)**: `SIGHUP` or an fsnotify change to the config file reloads settings for new connections without breaking active sessions; `authorized_keys` is re-read on every authentication
- **Secret Indirection (FR-4)**: `sso.client_secret` supports `{"$env": "VAR"}` / `{"$file": "/path"}` (or the `"$env:VAR"` / `"$file:/path"` string forms) so secrets never live as plaintext in the canonical config
- **Ed25519 Host Key**: New host keys are generated as Ed25519; existing RSA host keys keep working
- **Handshake Rate Limiting**: `rate_limit` caps SSH handshakes per source IP per minute
- **Centralized known_hosts**: `known_hosts` points the proxy at a managed target host-key file instead of `~/.ssh/known_hosts`
- **Terminal-Only by Default**: Accepts interactive `shell` sessions out of the box; direct `exec` requests stay disabled unless `allow_direct_commands` is enabled in the JSON config
- **Optional SFTP Subsystem (gated)**: `sftp_enabled` allows proxying the `sftp` subsystem to the target (binary stream, not recorded)
- **Dynamic or Static Routing**: Use `LC_SSH_SERVER=host[:port]` or enable `static_routing` with failover / round-robin
- **Transparent Proxying**: Acts as an intermediate SSH server and opens a real target SSH session
- **Host Key Verification**: Requires `~/.ssh/known_hosts` (or the configured `known_hosts` file) by default, with an explicit insecure override for development only via `insecure_ignore_hostkey` in the JSON config
- **Optional SSO Second Factor**: Can print a Keycloak verification link to the SSH console and wait for approval before proxying the session, with optional `required_groups` membership check
- **Health Endpoint**: `health.enabled` exposes `GET /healthz` returning `{"status":"ok"}`
- **Prometheus Metrics**: Can expose `/metrics` using `prometheus/client_golang` for scraping by Prometheus, including per-target session counters
- **Configurable Logging**: Supports `error`, `info`, and `debug` log levels

## Requirements

- Go `1.21+` to build the project
- A reachable target SSH host
- SSH agent forwarding via `ssh -A` for target authentication
- Your public key present in the target host's `authorized_keys`
- If you set `"auto_accept_client_keys": false`, your client key must be present in the proxy host's `authorized_keys` file or in the file referenced by `authorized_keys`
- A populated `~/.ssh/known_hosts` file on the proxy host for target verification

## Build

```bash
go build -o ssh-proxy-server ./cmd/ssh-proxy-server
```

## Test

```bash
go test ./...
```

## Usage

### Start the proxy server

Copy the example file and adjust the values you want:

```bash
cp ./config.example.json ./config.json
```

Example `config.json`:

```json
{
  "listen": "localhost:2222",
  "key": "./ssh_host_key",
  "log_level": "info",
  "recordings_dir": "./recordings",
  "authorized_keys": "./authorized_keys",
  "auto_accept_client_keys": true,
  "allow_direct_commands": false,
  "insecure_ignore_hostkey": false,
  "recording_format": "asciinema",
  "retries": 0,
  "connect_timeout_seconds": 10,
  "static_routing": {
    "enabled": false,
    "servers": [
      "primary-target:22",
      "backup-target:22"
    ],
    "mode": "failover"
  },
  "metrics": {
    "enabled": false,
    "listen": "127.0.0.1:9090",
    "path": "/metrics"
  },
  "sso": {
    "enabled": false,
    "provider": "keycloak",
    "base_url": "https://localhost:8443",
    "realm": "ssh-proxy-server",
    "client_id": "ssh-proxy-server",
    "client_secret": "",
    "scope": "openid",
    "auth_timeout_seconds": 120,
    "poll_interval_seconds": 5,
    "connect_timeout_seconds": 10,
    "enforce_ssh_user_match": true,
    "insecure_skip_verify": false
  }
}
```

Start the proxy with the config file:

```bash
./ssh-proxy-server -config ./config.json
```

Available log levels: `error`, `info`, `debug`

Key JSON settings:
- `authorized_keys` — by default you can use a local path like `./authorized_keys`
- `auto_accept_client_keys` — defaults to `true`; set to `false` to enforce checking `authorized_keys`
- `allow_direct_commands` — defaults to `false`; keeps the proxy in terminal-only mode unless enabled
- `recording_format` — `asciinema` by default; set to `script` for a plain-text transcript file
- `insecure_ignore_hostkey` — defaults to `false`; enable only for temporary development use when you need to ignore `known_hosts` mismatches or missing entries
- `retries` and `connect_timeout_seconds` — global target connection retry/timeout settings for both dynamic and static routing
- `static_routing.enabled` — when `true`, the proxy ignores `LC_SSH_SERVER` and uses the configured `servers` list
- `static_routing.mode` — `failover` or `round_robin`
- `access_control.enabled` — enforces the per-user target ACL (FR-1); when disabled any authenticated client may reach any target
- `access_control.default` — `deny` (default) or `allow` for users without a matching rule
- `access_control.rules` — list of `{"user": "name-or-glob", "groups": ["sso-group"], "targets": [...]}` entries; a rule applies when its `user` pattern matches the SSH login user or the client belongs to one of `groups` (SSO groups, case-insensitive; `user` may be omitted when `groups` is set). Targets may be `host[:port]` glob patterns (`web-*.prod`, `10.0.*`, `*`) or CIDR ranges (`10.0.0.0/8`); a pattern without a port matches the default SSH port 22 only. The SSH login user is always used for the ACL decision — an explicitly requested different target user (`other@host`) does not affect it
- `known_hosts` — path to a centralized target host-key file; empty (default) falls back to `~/.ssh/known_hosts`
- `sftp_enabled` — allows proxying the `sftp` subsystem to the target (default `false`)
- `rate_limit.enabled` / `rate_limit.max_per_minute` — caps inbound SSH handshakes per source IP per minute
- `health.enabled` / `health.listen` / `health.path` — lightweight HTTP health endpoint for load balancers/orchestrators
- `recordings.retention_days` — automatically deletes recordings (and their `.sha256` sidecars) older than this many days; `0` (default) keeps everything
- `recordings.integrity` — writes a SHA-256 `.sha256` sidecar file next to every finished recording
- `metrics.enabled` — enables the Prometheus metrics endpoint
- `metrics.listen` and `metrics.path` — where to expose the scrape endpoint (for example `127.0.0.1:9090` and `/metrics`)
- `sso.enabled` — enables a second-factor confirmation step before the SSH session is proxied
- `sso.provider` — currently supports `keycloak`
- `sso.client_secret` — optional; set it when the Keycloak client is confidential. Supports secret indirection: `{"$env": "VAR_NAME"}`, `{"$file": "/path/to/secret"}`, `"$env:VAR_NAME"`, or `"$file:/path/to/secret"` so the secret is not stored in plaintext
- `sso.required_groups` — optional list of Keycloak groups (or realm roles) the confirmed identity must belong to
- `sso.auth_timeout_seconds` — how long to wait for approval in the browser before rejecting the SSH session
- `sso.poll_interval_seconds` — how often the proxy re-checks Keycloak for approval status
- `sso.connect_timeout_seconds` — per-request timeout for discovery, device authorization, and polling calls to Keycloak
- `sso.enforce_ssh_user_match` — defaults to `true`; require the confirmed Keycloak identity to match the SSH username, set to `false` to disable this check
- `sso.insecure_skip_verify` — defaults to `false`; set to `true` to skip TLS certificate verification for self-signed certificates on the Keycloak server

### Hot reload

Send `SIGHUP` to the proxy process (or save the config file — including atomic save-by-rename as done by vim — changes are picked up via fsnotify):

```bash
kill -HUP $(pidof ssh-proxy-server)
```

The config, ACL, trusted CA certificates, log level, and rate limit are rebuilt for **new** connections. Active sessions keep the settings they started with and are not interrupted. `authorized_keys` is re-read on every authentication attempt.

### Audit log

Every security-relevant event is emitted as one JSON object per line on stdout, designed for a journald → Loki → Alertmanager pipeline:

```json
{"ts":"2026-11-21T12:50:58Z","event":"session_end","level":"info","session_id":"...","user":"alice","client_addr":"10.1.2.3:51620","target_user":"alice","target_host":"localhost","target_port":"2298","recording_id":"...","result":"success","exit_status":0,"duration_s":42.5}
```

Event types: `auth_success`, `auth_failure`, `target_denied`, `session_start`, `session_end`, `sso_success`, `sso_failure`, `handshake_rate_limited`. `session_start`/`session_end` carry `key_fingerprint` (client key) and `sso_result` (`success`/`failure`/`not_required`), so each session is fully described by its lifecycle events; the `auth_*`/`sso_*` stream correlates to them via `session_id`. Every event carries a `level` field (`info`, or `warning` for `denied`/`failure` results) so Loki/Alertmanager can filter and alert on severity. Regular (non-JSON) diagnostics stay in the same stdout stream; filter lines starting with `{` for the audit stream. Environment variable values from SendEnv are never logged (they may carry client tokens).

You can store recordings in a custom directory by setting `"recordings_dir": "/path/to/recordings"` in the JSON config.

### Connect through the proxy

**Dynamic routing: Using SendEnv to specify target**

When `static_routing.enabled` is `false`, the proxy requires the target host to be specified via the `LC_SSH_SERVER` environment variable using SSH's SendEnv option.
Use `ssh -A` so the proxy can authenticate to the target with your forwarded SSH agent. The username from the SSH session to the proxy is reused for the final target login unless you still provide a legacy `user@host[:port]` route:

```bash
LC_SSH_SERVER="target-host[:port]" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost
```

If `:port` is omitted, the proxy uses the default SSH port `22`.
The proxy receives `LC_SSH_SERVER`, validates it, extracts `host` and `port`, and uses the authenticated SSH username from the proxy session as the target user when the route omits one. Suspicious values with shell metacharacters or invalid host/port parts are rejected and logged.

**Note**: In dynamic-routing mode, connecting without `LC_SSH_SERVER` will fail with `Error: LC_SSH_SERVER not provided`.

### Optional: enable static routing with failover or round-robin

If you want the proxy to choose from a fixed list of targets, enable `static_routing` in `config.json`. In this mode, `LC_SSH_SERVER` becomes optional and is ignored.

```json
{
  "retries": 1,
  "connect_timeout_seconds": 5,
  "static_routing": {
    "enabled": true,
    "servers": [
      "primary-target:22",
      "backup-target:22"
    ],
    "mode": "round_robin"
  }
}
```

Then connect normally:

```bash
ssh -A -p 2222 your-user@localhost
```

The proxy will try the configured servers in order, move to the next one when a target is unavailable, and rotate the starting target per session when `mode` is `round_robin`. The global `retries` and `connect_timeout_seconds` values apply here too.

### Optional: enable Prometheus metrics

If you want Prometheus to scrape runtime metrics from the proxy, enable `metrics` in `config.json`.

```json
{
  "metrics": {
    "enabled": true,
    "listen": "127.0.0.1:9090",
    "path": "/metrics"
  }
}
```

Then scrape the endpoint, for example:

```bash
curl http://127.0.0.1:9090/metrics
```

Useful links:
- Prometheus project: <https://prometheus.io/>
- Prometheus documentation: <https://prometheus.io/docs/introduction/overview/>

The proxy exports counters and gauges for established SSH connections, handshake failures, proxied session results, SSO confirmation results, the number of sessions currently waiting for SSO/2FA approval, and the total number of SSO/2FA errors.

Relevant SSO-related metrics include:
- `ssh_proxy_sso_confirmations_total{result="success|failure|rejected"}` — SSO confirmation outcomes
- `ssh_proxy_sso_pending_sessions` — current SSH sessions waiting for browser approval / second factor
- `ssh_proxy_sso_errors_total` — total SSO/2FA failures, including timeout / polling errors and rejected identity matches

### Optional: enable Keycloak SSO second factor

If you want users to confirm the SSH session in a browser before proxying starts, enable `sso` in `config.json`. The test setup is aimed at a Keycloak server with realm `ssh-proxy-server`.

Why this helps: **2FA reduces the risk of unauthorized SSH access** if a workstation, browser session, or SSH key is compromised. The user must both initiate the SSH session and confirm it in the identity provider. The default `scope` is only `openid`, which is sufficient for this flow.

Useful links:
- Keycloak project: <https://www.keycloak.org/>
- Keycloak documentation: <https://www.keycloak.org/documentation>

```json
{
  "sso": {
    "enabled": true,
    "provider": "keycloak",
    "base_url": "https://localhost:8443",
    "realm": "ssh-proxy-server",
    "client_id": "ssh-proxy-server",
    "client_secret": "",
    "auth_timeout_seconds": 120,
    "poll_interval_seconds": 5,
    "connect_timeout_seconds": 10,
    "enforce_ssh_user_match": true,
    "insecure_skip_verify": false
  }
}
```

When the SSH client opens a shell or direct command session, the proxy prints a verification link to the SSH console and waits up to `auth_timeout_seconds` for confirmation. The proxy re-checks approval every `poll_interval_seconds`, and each HTTP call to Keycloak is capped by `connect_timeout_seconds`. By default it also verifies that the confirmed Keycloak identity matches the SSH username; set `enforce_ssh_user_match` to `false` only if you intentionally want to disable that binding. Set `insecure_skip_verify` to `true` only when the Keycloak server uses a self-signed TLS certificate. If the user does not approve the login in time, the SSH session is rejected.

### Optional: enable direct command execution

If you want `ssh ... <command>` style execution, set `"allow_direct_commands": true` in your JSON config and restart the proxy:

```json
{
  "allow_direct_commands": true
}
```

Then connect with a trailing command:

```bash
./ssh-proxy-server -config ./config.json
LC_SSH_SERVER="target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost 'uname -a'
```

## Examples

### Example 1: Basic SendEnv usage

```bash
# Start proxy server
./ssh-proxy-server -config ./config.json

# Connect with target specified via LC_SSH_SERVER
LC_SSH_SERVER="target-host.example.com:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost
```

## Troubleshooting

### "Error: LC_SSH_SERVER not provided"

**Problem**: The proxy requires the target host to be specified via the `LC_SSH_SERVER` environment variable.

**Solution**: Always use the SendEnv option when connecting and enable agent forwarding:

```bash
LC_SSH_SERVER="target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost
```

### "Error: direct commands are disabled"

**Problem**: You connected with `ssh ... localhost 'command'`, which sends an SSH `exec` request, but the proxy is running in terminal-only mode.

**Solution**: Either connect interactively without a trailing command, or set `"allow_direct_commands": true` in the JSON config and restart the proxy.

### "SSO device authorization failed: Invalid client or Invalid client credentials"

**Problem**: the configured Keycloak client either does not exist, is confidential without a secret, or does not have the OAuth 2.0 Device Authorization Grant enabled.

**Solution**: verify `sso.client_id`, set `sso.client_secret` when the Keycloak client is confidential, and enable **OAuth 2.0 Device Authorization Grant** for that client in the `ssh-proxy-server` realm.

### "SSO confirmation timed out"

**Problem**: Keycloak approval was not completed before the configured timeout expired.

**Solution**: open the printed verification link and finish the second-factor step, or increase `sso.auth_timeout_seconds` in `config.json`.

### "knownhosts: key mismatch"

**Problem**: The target host key in `~/.ssh/known_hosts` does not match the current server key.

**Preferred fix**: remove the stale entry and reconnect:

```bash
ssh-keygen -R target-host.example.com
ssh your-user@target-host.example.com
```

**Temporary development workaround**: set `"insecure_ignore_hostkey": true` in the JSON config and restart the proxy with `-config`.

### "Permission denied" or connection issues

**Problem**: SSH key authentication failed, the client key is not authorized on the proxy, or the target host is unreachable.

**Solutions**:
- Ensure your client key is present in the proxy host's `authorized_keys` or in the file referenced by `authorized_keys` in `config.json`
- Ensure your SSH key is added to the target host's `authorized_keys`
- Make sure your key is loaded in `ssh-agent` and connect with `ssh -A`
- Verify the target host address format: `host[:port]` (or legacy `user@host[:port]`)
- Check that the target SSH server is running and accessible

### Security configuration options

For development-only scenarios, the following options are available:
- `authorized_keys` in `config.json` — use a custom allowlist path for proxy login
- `auto_accept_client_keys` in `config.json` — set to `false` to enforce the allowlist
- `insecure_ignore_hostkey` in `config.json` — bypass `known_hosts` verification for target connections
- `SSH_PROXY_INSECURE_IGNORE_HOSTKEY=1` — sets the default for `insecure_ignore_hostkey`

## Recording Format

Sessions are recorded in the directory configured by `recordings_dir` in `config.json`.
By default, the proxy uses `"recording_format": "asciinema"` and stores files in `./recordings/`:

- `asciinema` format → `<user>_<host>_<port>_<session-id>.cast`
- `script` format → `<user>_<host>_<port>_<session-id>.log`

Both formats contain terminal transcript data with private file permissions.
You can view asciinema captures with:

```bash
asciinema play recordings/<file>.cast
```

## Project Structure

```
cmd/
└── ssh-proxy-server/
    └── main.go              # Entry point, hot reload (SIGHUP/fsnotify), health & retention
internal/
├── access/
│   └── access.go            # Target ACL: per-user target patterns (FR-1)
├── appconfig/
│   ├── config.go            # JSON config loading, defaults, validation, secret resolution
│   └── config_secrets_test.go
├── audit/
│   └── audit.go             # Structured JSON audit log on stdout (FR-2)
├── client/
│   └── client.go            # Target host connection and agent forwarding
├── configwatch/
│   └── configwatch.go       # fsnotify watcher with debounce (FR-3)
├── hostkey/
│   └── hostkey.go           # Ed25519 host key generation/loading (RSA supported)
├── metrics/
│   └── metrics.go           # Prometheus metrics collector and HTTP handler
├── ratelimit/
│   └── limiter.go           # Per-source-IP SSH handshake rate limiter
├── recording/
│   ├── recording.go         # Asciinema v2 and script format recording
│   └── retention.go         # SHA-256 integrity sidecars + retention cleanup
├── secrets/
│   └── secrets.go           # $env/$file secret indirection (FR-4)
├── server/
│   ├── security.go          # Client key authorization (authorized_keys)
│   └── server.go            # SSH server, channel handling, ACL/audit wiring, SFTP
├── sso/
│   └── sso.go               # OAuth2 device flow (Keycloak SSO/2FA, group check)
└── types/
    └── types.go             # SessionState, logging, shared types
```

## Dependencies

- `golang.org/x/crypto/ssh` - SSH protocol implementation and agent/known_hosts support
- `github.com/google/uuid` - Session and recording ID generation
- `github.com/prometheus/client_golang` - Prometheus metrics export
- `github.com/fsnotify/fsnotify` - Config file watching for hot reload

## Documentation

- **[QUICKSTART.md](QUICKSTART.md)** - Get started in minutes
- **[SENDENV.md](SENDENV.md)** - Detailed SendEnv configuration and examples
- **[CERTIFICATES.md](CERTIFICATES.md)** - SSH certificate setup, generation, and usage
- **[IMPLEMENTATION.md](IMPLEMENTATION.md)** - Architecture, design, and advanced features
- **[E2E.md](E2E.md)** - End-to-end tests documentation

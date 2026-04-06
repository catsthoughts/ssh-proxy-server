# SSH Proxy Server — Implementation Guide

## Current status

The project is a working Go-based SSH proxy for bastion-style access, auditing, and session capture. It currently:

1. accepts inbound SSH connections with public key authentication
2. routes sessions using `LC_SSH_SERVER=host[:port]` and reuses the SSH session user for the target login by default
3. proxies interactive `shell` sessions by default and enables `exec` commands only when `allow_direct_commands` is set in the JSON config
4. authenticates to the target host using the client's SSH agent
5. can require a Keycloak-based second factor before the session is proxied to the target host
   - this adds an extra identity check beyond the SSH key and helps protect bastion access
6. records full session input/output in either `asciinema` v2 or plain `script` transcript format
7. forwards PTY allocation and terminal resize events
8. exposes security controls for client-key policy, direct-command policy, and target host verification

---

## End-to-end flow

```text
SSH client
  │
  │ LC_SSH_SERVER="host[:port]" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost
  ▼
SSH proxy server
  ├─ authenticates the incoming client
  ├─ parses LC_SSH_SERVER → user / host / port
  ├─ opens an outbound SSH connection to the target
  ├─ authenticates to the target using the forwarded SSH agent
  ├─ starts an interactive shell session by default, or an exec session only when `allow_direct_commands` is enabled in the JSON config
  ├─ optionally prints a Keycloak verification link and waits for second-factor approval
  ├─ proxies stdin/stdout/stderr bidirectionally
  ├─ records the session to a `.cast` file
  └─ forwards PTY resize events to the target
  ▼
Target SSH host
```

---

## Runtime configuration

### Command-line flags

The proxy is started from `cmd/ssh-proxy-server/main.go` with a single required flag:

- `-config` — path to the JSON config file that contains all runtime settings

### JSON config fields

The config file currently supports:

- `listen` — address to listen on, default `localhost:2222`
- `key` — path to the SSH host key file
- `log_level` — `error`, `info`, or `debug`
- `recordings_dir` — directory where recordings are stored
- `authorized_keys` — path to the proxy-side `authorized_keys` file, default `~/.ssh/authorized_keys`
- `auto_accept_client_keys` — whether to accept client public keys automatically, default `true`
- `allow_direct_commands` — allow SSH `exec` requests (direct command execution), default `false`
- `recording_format` — choose `asciinema` or `script` recording output, default `asciinema`
- `insecure_ignore_hostkey` — disable target `known_hosts` verification (insecure; temporary development use only), default `false`
- `sso` — optional second-factor configuration for a Keycloak realm; disabled by default
  - `auth_timeout_seconds` — maximum time to wait for browser approval
  - `poll_interval_seconds` — polling interval for approval checks
  - `connect_timeout_seconds` — per-request HTTP timeout for Keycloak discovery/device/poll calls
  - `scope` — defaults to `openid`; profile/email claims are not required by the proxy
  - based on Keycloak device authorization flow: <https://www.keycloak.org/> and <https://www.keycloak.org/documentation>

### Security-related environment variables

A few runtime overrides are still supported:

- `SSH_PROXY_AUTO_ACCEPT_CLIENT_KEYS` — sets the default value for `auto_accept_client_keys` in the JSON config
- `SSH_PROXY_ALLOW_LOCAL_AGENT_FALLBACK=1` — allow fallback to the proxy host's local `SSH_AUTH_SOCK`
- `SSH_PROXY_INSECURE_IGNORE_HOSTKEY=1` — sets the default value for `insecure_ignore_hostkey` in the JSON config

---

## Project structure

### `cmd/ssh-proxy-server/main.go`
Responsible for process startup:

- parses runtime flags
- sets the log level
- validates auth-related startup options
- loads or generates the proxy host key
- starts the TCP listener and passes accepted connections into `server.HandleConnection(...)`

### `internal/server/server.go`
Implements the proxy-side SSH server behavior:

- SSH handshake and incoming session setup
- client public-key callback and authorization check
- parsing of `env`, `shell`, `exec`, `pty-req`, and `window-change` requests
- validation of `LC_SSH_SERVER` values, with rejection/logging of suspicious injection-like input
- session routing and target selection
- creation of recording filenames and session recorder lifecycle
- graceful shutdown and exit-status propagation

### `internal/server/security.go`
Contains the lightweight authorization helpers:

- default authorized-keys path resolution
- optional enforcement against a configured `authorized_keys` file
- auto-accept behavior for development-friendly startup

### `internal/client/client.go`
Implements outbound SSH connectivity to the target host:

- resolves the SSH agent source
- prefers the same key that authenticated to the proxy when possible
- requires forwarded SSH agent access by default
- loads `~/.ssh/known_hosts` for host verification
- allows explicit insecure host-key fallback via `insecure_ignore_hostkey` in the JSON config or the `SSH_PROXY_INSECURE_IGNORE_HOSTKEY` env override

### `internal/hostkey/hostkey.go`
Manages the proxy server host key:

- loads an existing key from disk
- generates and persists a new RSA 2048-bit host key if missing
- writes the host key with private file permissions

### `internal/recording/recording.go`
Implements the supported session recording formats:

- `asciinema` → writes a JSON header and timed input/output frames
- `script` → writes a plain-text transcript similar to the `script` utility
- captures both input (`"i"`) and output (`"o"`) where applicable
- serializes writes via a mutex
- creates recording files with `0600` permissions

### `internal/types/types.go`
Holds shared state for an active session, including:

- client identity and presented key
- target user/host/port
- SSH connection and session handles
- recording handle
- PTY metadata
- environment variables received from the client

---

## Routing and target selection

### Primary routing input

The proxy expects the destination to be provided as:

```text
LC_SSH_SERVER=host[:port]
```

Example:

```bash
LC_SSH_SERVER="target-host.example.com:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost
```

### Parsing behavior

The implementation extracts:

- `user`
- `host`
- `port`

If `:port` is omitted, the proxy defaults to `22`.

### Resolution order

For a session target, the proxy currently prefers:

1. `LC_SSH_SERVER` received via SSH environment
2. target information already parsed into session state
3. an `exec` command-derived target, if applicable and direct-command mode is enabled

---

## Authentication model

### Client → proxy authentication

Incoming clients authenticate with an SSH public key.

Current policy is configurable:

- by default, `auto_accept_client_keys=true` accepts client keys automatically
- if `auto_accept_client_keys=false`, the presented key must be found in the file passed by `authorized_keys`

### Proxy → target authentication

The proxy connects to the target host using the SSH agent available to the session:

- preferred path: forwarded agent from `ssh -A`
- development-only fallback: local `SSH_AUTH_SOCK`, enabled only when `SSH_PROXY_ALLOW_LOCAL_AGENT_FALLBACK=1`

When possible, the proxy prefers the same public key that authenticated the client to the proxy.

### Host key verification

For target verification:

- default behavior uses `~/.ssh/known_hosts`
- if `known_hosts` is unavailable or the key mismatches, the connection fails closed
- insecure bypass is available via `insecure_ignore_hostkey` in the JSON config or `SSH_PROXY_INSECURE_IGNORE_HOSTKEY=1` for temporary development-only use

---

## Session handling details

### Supported SSH request types

The current server handles these session-level requests:

- `env`
- `auth-agent-req@openssh.com`
- `pty-req`
- `window-change`
- `shell`
- `exec` (rejected unless `allow_direct_commands` is enabled in the JSON config)

Unsupported or unknown request types are rejected.

### Interactive shell vs `exec`

Interactive terminal access is the default behavior:

- `shell` → starts a remote interactive shell
- `exec` → is rejected by default; when `allow_direct_commands=true` is set in the JSON config, it runs the requested remote command and returns the exit status

### PTY and resize forwarding

When the client requests a PTY:

- the terminal type and size are parsed from `pty-req`
- the target session receives `RequestPty(...)`
- later `window-change` events are forwarded via `WindowChange(...)`

### Shutdown behavior

On clean termination:

- stdin closes
- the remote session exits
- the target exit status is propagated back to the SSH client
- the client channel and connection are closed cleanly
- lifecycle logs are emitted

---

## Recording implementation

Recordings are written under the directory configured by `recordings_dir` in the JSON config.
If that setting is omitted, the default directory is `./recordings/`.

### Permissions

- recordings directory is created and secured with `0700`
- recording files are created with `0600`

### Filename format

```text
<user>_<host>_<port>_<session-id>.cast
```

Example:

```text
alice_example.com_22_550e8400-e29b-41d4-a716-446655440000.cast
```

### File contents

Each recording contains:

- an asciinema v2 header with metadata
- input frames as `"i"`
- output frames as `"o"`

This matches the project's audit-oriented goal of capturing full session activity.

---

## Logging

The proxy supports three log levels:

- `error`
- `info`
- `debug`

Example startup:

```bash
./ssh-proxy-server -config ./config.json
```

### Typical log events

- new inbound SSH connection
- accepted or rejected public key auth
- received `LC_SSH_SERVER`
- parsed target user/host/port
- SSH agent behavior and fallback decisions
- PTY allocation and terminal resize forwarding
- input/output stream lifecycle
- SSO confirmation start / success / failure with configured timeouts
- final session exit status
- recording file creation failures

---

## Current limitations

The implementation is functional and suitable for controlled environments, but several areas remain intentionally simple:

1. **Client auth defaults are permissive**
   - `auto_accept_client_keys=true` is convenient for development but not the strictest production posture

2. **Routing variable is fixed**
   - the proxy currently expects `LC_SSH_SERVER`
   - arbitrary routing variable names are not supported

3. **No per-user or per-target ACL layer**
   - there is no host allowlist/denylist or per-user access policy yet

4. **Config remains intentionally simple**
   - configuration is now JSON-driven via `-config`, but it does not yet support profiles, includes, or live reload

5. **No rate limiting or connection throttling**
   - repeated authentication attempts are not currently limited

---

## Recommended runtime usage

### Build

```bash
go build -o ssh-proxy-server ./cmd/ssh-proxy-server
```

### Run

```bash
cp ./config.example.json ./config.json
./ssh-proxy-server -config ./config.json
```

### Connect

```bash
LC_SSH_SERVER="target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost
```

---

## Summary

The current implementation provides:

- real SSH target dialing
- agent-based target authentication
- shell and `exec` proxying
- full asciinema session recording
- contextual recording filenames
- PTY and resize forwarding
- configurable client-key policy
- secure-by-default host verification behavior with explicit development overrides

That makes the current state suitable for local or controlled-environment SSH auditing and session capture workflows, with a clear path for future hardening and access-policy controls.


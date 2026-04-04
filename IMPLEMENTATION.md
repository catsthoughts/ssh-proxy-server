# SSH Proxy Server — Implementation Guide

## Current status

The project is a working Go-based SSH proxy for bastion-style access, auditing, and session capture. It currently:

1. accepts inbound SSH connections with public key authentication
2. routes sessions using `LC_SSH_SERVER=user@host[:port]`
3. proxies both interactive `shell` sessions and `exec` commands
4. authenticates to the target host using the client's SSH agent
5. records full session input/output in asciinema v2 format
6. forwards PTY allocation and terminal resize events
7. exposes security controls for client-key policy and target host verification

---

## End-to-end flow

```text
SSH client
  │
  │ LC_SSH_SERVER="user@host[:port]" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
  ▼
SSH proxy server
  ├─ authenticates the incoming client
  ├─ parses LC_SSH_SERVER → user / host / port
  ├─ opens an outbound SSH connection to the target
  ├─ authenticates to the target using the forwarded SSH agent
  ├─ starts a shell or exec session
  ├─ proxies stdin/stdout/stderr bidirectionally
  ├─ records the session to a `.cast` file
  └─ forwards PTY resize events to the target
  ▼
Target SSH host
```

---

## Runtime configuration

### Command-line flags

The proxy is started from `cmd/ssh-proxy-server/main.go` and currently supports:

- `-listen` — address to listen on, default `localhost:2222`
- `-key` — path to the SSH host key file
- `-log-level` — `error`, `info`, or `debug`
- `-recordings-dir` — directory where `.cast` recordings are stored
- `-authorized-keys` — path to the proxy-side `authorized_keys` file, default `~/.ssh/authorized_keys`
- `-auto-accept-client-keys` — whether to accept client public keys automatically, default `true`

### Security-related environment variables

A few runtime overrides are still supported:

- `SSH_PROXY_AUTO_ACCEPT_CLIENT_KEYS` — sets the default value for `-auto-accept-client-keys`
- `SSH_PROXY_ALLOW_LOCAL_AGENT_FALLBACK=1` — allow fallback to the proxy host's local `SSH_AUTH_SOCK`
- `SSH_PROXY_INSECURE_IGNORE_HOSTKEY=1` — bypass `known_hosts` verification for development-only use

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
- allows explicit insecure host-key fallback only via an override env var

### `internal/hostkey/hostkey.go`
Manages the proxy server host key:

- loads an existing key from disk
- generates and persists a new RSA 2048-bit host key if missing
- writes the host key with private file permissions

### `internal/recording/recording.go`
Implements asciinema v2 session recording:

- writes the recording header and frames
- captures both input (`"i"`) and output (`"o"`)
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
LC_SSH_SERVER=user@host[:port]
```

Example:

```bash
LC_SSH_SERVER="ubuntu@192.168.1.100:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
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
3. an `exec` command-derived target, if applicable

---

## Authentication model

### Client → proxy authentication

Incoming clients authenticate with an SSH public key.

Current policy is configurable:

- by default, `-auto-accept-client-keys=true` accepts client keys automatically
- if `-auto-accept-client-keys=false`, the presented key must be found in the file passed by `-authorized-keys`

### Proxy → target authentication

The proxy connects to the target host using the SSH agent available to the session:

- preferred path: forwarded agent from `ssh -A`
- development-only fallback: local `SSH_AUTH_SOCK`, enabled only when `SSH_PROXY_ALLOW_LOCAL_AGENT_FALLBACK=1`

When possible, the proxy prefers the same public key that authenticated the client to the proxy.

### Host key verification

For target verification:

- default behavior uses `~/.ssh/known_hosts`
- if `known_hosts` is unavailable, the connection fails closed
- insecure bypass is available only via `SSH_PROXY_INSECURE_IGNORE_HOSTKEY=1`

---

## Session handling details

### Supported SSH request types

The current server handles these session-level requests:

- `env`
- `auth-agent-req@openssh.com`
- `pty-req`
- `window-change`
- `shell`
- `exec`

Unsupported or unknown request types are rejected.

### Interactive shell vs `exec`

Both modes are supported:

- `shell` → starts a remote interactive shell
- `exec` → runs the requested remote command and returns the exit status

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

Recordings are written as asciinema v2 `.cast` files under the directory configured by `-recordings-dir`.
If the flag is omitted, the default directory is `./recordings/`.

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
./ssh-proxy-server -listen localhost:2222 -key ./ssh_host_key -log-level debug -recordings-dir ./recordings
```

### Typical log events

- new inbound SSH connection
- accepted or rejected public key auth
- received `LC_SSH_SERVER`
- parsed target user/host/port
- SSH agent behavior and fallback decisions
- PTY allocation and terminal resize forwarding
- input/output stream lifecycle
- final session exit status
- recording file creation failures

---

## Current limitations

The implementation is functional and suitable for controlled environments, but several areas remain intentionally simple:

1. **Client auth defaults are permissive**
   - `-auto-accept-client-keys=true` is convenient for development but not the strictest production posture

2. **Routing variable is fixed**
   - the proxy currently expects `LC_SSH_SERVER`
   - arbitrary routing variable names are not supported

3. **No per-user or per-target ACL layer**
   - there is no host allowlist/denylist or per-user access policy yet

4. **No external config file**
   - configuration is currently flag-driven and minimal

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
./ssh-proxy-server \
  -listen localhost:2222 \
  -key ./ssh_host_key \
  -log-level info \
  -recordings-dir ./recordings \
  -authorized-keys ~/.ssh/authorized_keys \
  -auto-accept-client-keys=true
```

### Connect

```bash
LC_SSH_SERVER="user@target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
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


# SSH Proxy Server - Implementation Guide

## Current status

The project is now a working SSH proxy that:

1. accepts SSH client connections with public key authentication
2. reads `LC_SSH_SERVER=user@host:port` from SSH `env` requests
3. connects to the target host using the client's SSH agent
4. proxies interactive `shell` and `exec` sessions
5. records input and output in asciinema v2 format
6. forwards PTY allocation and live terminal resize events
7. logs session lifecycle events with configurable log levels

---

## End-to-end flow

```text
SSH client
  │
  │ ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
  ▼
SSH proxy server
  ├─ authenticates the client
  ├─ parses LC_SSH_SERVER → user / host / port
  ├─ opens target SSH connection using the client's agent
  ├─ creates target shell or exec session
  ├─ forwards stdin/stdout/stderr
  ├─ records the session to recordings/
  └─ forwards PTY resize events to the target
  ▼
Target SSH host
```

---

## Project structure

### `cmd/ssh-proxy-server/main.go`
- application entry point
- parses flags:
  - `-listen`
  - `-key`
  - `-log-level`
  - `-recordings-dir`
- loads or generates the proxy host key
- starts the SSH listener

### `internal/server/server.go`
Handles the proxy-side SSH protocol:

- incoming SSH handshake
- public key callback
- `env` request parsing
- `shell` and `exec` handling
- `pty-req` handling
- `window-change` forwarding
- session lifecycle logging
- recording file naming
- clean session shutdown on `Ctrl+D`

### `internal/client/client.go`
Handles outbound SSH connections to the target:

- uses forwarded/local SSH agent signers
- prefers the same key that authenticated to the proxy
- loads `~/.ssh/known_hosts` when available
- falls back to insecure host-key acceptance if `known_hosts` is unavailable

### `internal/hostkey/hostkey.go`
- generates and persists the proxy host key
- uses RSA 2048-bit keys

### `internal/recording/recording.go`
- writes asciinema v2 headers and frames
- records both input (`"i"`) and output (`"o"`)
- protects writes with a mutex

### `internal/types/types.go`
Shared session state, including:
- client identity and public key
- target user / host / port
- SSH client/session handles
- PTY size and terminal name
- env vars received from the client

---

## Target selection

### Primary routing method

The current implementation expects:

```text
LC_SSH_SERVER=user@host:port
```

Example:

```bash
LC_SSH_SERVER="ubuntu@192.168.1.100:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
```

### Parsing behavior

The proxy extracts:
- `user`
- `host`
- `port`

If the port is omitted, it defaults to `22`.

### Priority order

For routing, the proxy currently checks:
1. `LC_SSH_SERVER` from the SSH environment
2. parsed target from the `exec` command, if applicable

---

## Session handling details

### Authentication to the target

The proxy authenticates to the target host using the client's SSH agent:

- preferred path: forwarded agent from `ssh -A`
- fallback: local `SSH_AUTH_SOCK` on the proxy host

The proxy tries to match the same public key that authenticated the client to the proxy. If that key is not present in the agent, it falls back to other available agent keys.

### Interactive shell and `exec`

Both interactive shells and remote commands are supported:

- `shell` → opens a full remote shell
- `exec` → runs the requested command on the target and returns the exit status

### PTY and resize handling

Supported SSH requests include:
- `env`
- `auth-agent-req@openssh.com`
- `pty-req`
- `window-change`
- `shell`
- `exec`

When the user resizes their local terminal, the proxy forwards the new dimensions to the target session with `WindowChange(...)`.

### Clean shutdown

When the client sends `Ctrl+D`:
- stdin closes cleanly
- the remote session exits
- exit status is returned to the SSH client
- the proxy closes the client channel and connection
- session-end logs are emitted

---

## Recording format

Recordings are stored in the directory provided by `-recordings-dir` as asciinema v2 `.cast` files.
If the flag is omitted, the default directory is `./recordings/`.

### Filename format

```text
<user>_<host>_<port>_<session-id>.cast
```

Example:

```text
alice_example.com_22_c0161e40-02e4-4b1b-aea9-1897699f6cfd.cast
```

### File contents

The recorder writes:
- a v2 header with metadata
- input frames as `"i"`
- output frames as `"o"`

---

## Logging

Use:

```bash
./ssh-proxy-server -listen localhost:2222 -key ./ssh_host_key -log-level debug -recordings-dir ./recordings
```

Supported levels:
- `error`
- `info`
- `debug`

### Typical log events

- new SSH connection
- received `LC_SSH_SERVER`
- parsed target user/host/port
- SSH agent usage
- PTY requests
- terminal resize forwarding
- user input closure
- final session exit status

---

## Known limitations

The current implementation is functional, but a few things are still intentionally simple:

1. **Target host verification fallback**
   - if `~/.ssh/known_hosts` is unavailable, the proxy temporarily uses insecure host key verification for that connection

2. **Routing variable is fixed**
   - the implementation expects `LC_SSH_SERVER`
   - arbitrary custom variable names are not supported in the current code

3. **No policy layer yet**
   - no ACLs, allowlists, or per-user authorization rules

4. **No external config file yet**
   - configuration is currently flag-based and runtime-driven

---

## Recommended runtime usage

Build:

```bash
go build -o ssh-proxy-server ./cmd/ssh-proxy-server
```

Run:

```bash
./ssh-proxy-server -listen localhost:2222 -key ./ssh_host_key -log-level info -recordings-dir ./recordings
```

Connect:

```bash
LC_SSH_SERVER="user@target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
```

---

## Summary

At this point, the proxy already provides:
- real SSH target dialing
- agent-based authentication to the target
- shell/exec proxying
- asciinema recording
- contextual recording filenames
- clean session termination on `Ctrl+D`
- live terminal resize forwarding

That makes the current state suitable for local or controlled-environment SSH auditing and session capture workflows.
   - Validate client keys against allowed users
   - Consider implementing ACLs for target hosts per client

3. **Key Forwarding**:
   - Implement SSH agent socket forwarding
   - Chain authentication through proxy securely

4. **Session Recording**:
   - Encrypt recordings at rest
   - Set proper file permissions (mode 0600)
   - Regular backup and archival of recordings

5. **Auditing**:
   - Log all connection attempts
   - Log target hosts accessed
   - Timestamp all recordings

6. **Network Security**:
   - Run behind firewall
   - Consider VPN for proxy server access
   - Use mutual TLS if exposed to internet

## Advanced Features (To Implement)

### ✅ Already Implemented

1. **Environment Variable Routing (SendEnv)**
   - SSH client sends environment variables (e.g., LC_SSH_SERVER) to proxy via `-o "SendEnv=VAR_NAME"`
   - Proxy receives and uses variables for target routing
   - Supports multiple custom variables
   - Priority-based target selection (SendEnv > command)

### 🔄 Planned Features

1. **SSH Agent Forwarding**
   - Forward SSH agent socket to target
   - Allows seamless multi-hop authentication

2. **Port Forwarding**
   - Support `-L` and `-R` options through proxy
   - Local and remote tunneling

3. **SOCKS Proxy**
   - Implement SOCKS5 for general TCP tunneling
   - Use with `-D` option in SSH

4. **Session Inspection**
   - Real-time session monitoring
   - Command filtering/blocking

5. **Key Rotation**
   - Automatic host key rotation
   - Deprecation of old keys

6. **High Availability**
   - Session state persistence
   - Multiple proxy servers with failover

7. **Database Recording**
   - Store recordings in database instead of files
   - Better search and indexing capabilities

## Troubleshooting

### "Failed to establish SSH connection"
- Check SSH client compatibility
- Verify proxy server is running
- Check firewall rules

### "Error connecting to target"
- Verify target host is reachable
- Check SSH credentials for target
- Ensure SSH service is running on target

### "No session recording created"
- Check `recordings/` directory exists and is writable
- Verify disk space
- Check file permissions

### "Host key verification failed"
- Delete `~/.ssh/known_hosts` entry for proxy server
- Regenerate server host key if compromised

## Testing

```bash
# Terminal 1: Start proxy server
./ssh-proxy-server -listen localhost:2222 -key ./ssh_host_key -log-level debug -recordings-dir ./recordings

# Terminal 2: Test with ssh
LC_SSH_SERVER="localhost@127.0.0.1:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost

# Terminal 3: Check recording
ls -la recordings/
cat recordings/*.cast | head -20
```

## Files Structure After Build

```
ssh-proxy-server/
├── ssh-proxy-server          (executable binary)
├── ssh_host_key              (generated on first run)
├── recordings/               (created on first connection)
│   └── a1b2c3d4.cast        (session recording)
├── go.mod
├── go.sum
├── main.go
├── server.go
├── hostkey.go
├── client.go
├── recording.go
├── README.md
└── IMPLEMENTATION.md         (this file)
```

## Next Steps

To extend this implementation:

1. Complete SSH agent forwarding in `client.go`
2. Implement actual bidirectional proxying
3. Add configuration file support
4. Implement user ACLs and target restrictions
5. Add logging system for audit trail
6. Implement TLS for proxy server itself
7. Add session metadata (client IP, auth method, etc.)
8. Implement command filtering/blocking

# SSH Proxy Server

A transparent SSH proxy server written in Go for bastion-style access, SSH session auditing, and controlled target routing.
It accepts SSH connections with public key authentication, can either auto-accept client keys or validate them against an `authorized_keys` file, reuses the client's SSH agent to authenticate to the destination, routes sessions via `LC_SSH_SERVER=user@host[:port]`, and records activity in asciinema v2 format.

**Core capabilities:**
- Accepts inbound SSH connections with configurable client-key policy via `-auto-accept-client-keys` and `-authorized-keys`
- Reuses the client's forwarded SSH agent to authenticate to the target host
- Routes sessions using `LC_SSH_SERVER=user@host[:port]` and defaults to port `22`
- Records shell and exec sessions in asciinema v2 format for audit and analysis

**→ [Quick Start Guide](QUICKSTART.md) for immediate setup**

## Features

- **Authorized Client Access**: Accept client keys automatically by default, or enforce checks against the file passed via `-authorized-keys`
- **SSH Agent Reuse**: Requires forwarded agent access via `ssh -A` by default
- **Session Recording**: Records proxied activity in asciinema format with contextual filenames and private file permissions for audit and analysis
- **Dynamic Routing via SendEnv**: Target host specified with `LC_SSH_SERVER=user@host[:port]`
- **Transparent Proxying**: Acts as an intermediate SSH server and opens a real target SSH session
- **Host Key Verification**: Requires `~/.ssh/known_hosts` by default, with an explicit insecure override for development only
- **Configurable Logging**: Supports `error`, `info`, and `debug` log levels

## Requirements

- Go `1.21+` to build the project
- A reachable target SSH host
- SSH agent forwarding via `ssh -A` for target authentication
- Your public key present in the target host's `authorized_keys`
- If you run with `-auto-accept-client-keys=false`, your client key must be present in the proxy host's `authorized_keys` file or in the file passed via `-authorized-keys`
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

```bash
./ssh-proxy-server -listen localhost:2222 -key ./ssh_host_key -log-level info -recordings-dir ./recordings -authorized-keys ~/.ssh/authorized_keys -auto-accept-client-keys=true
```

Available log levels: `error`, `info`, `debug`

The `-authorized-keys` flag defaults to `~/.ssh/authorized_keys`.
The `-auto-accept-client-keys` flag defaults to `true`; set it to `false` to enforce checking the authorized keys file.
This keeps local development simple while still allowing a stricter production mode.

You can store recordings in a custom directory with:

```bash
-recordings-dir /path/to/recordings
```

### Connect through the proxy

**Required: Using SendEnv to specify target**

The proxy requires the target host to be specified via the `LC_SSH_SERVER` environment variable using SSH's SendEnv option.
Use `ssh -A` so the proxy can authenticate to the target with your SSH agent. By default, the proxy rejects local-agent fallback and requires a forwarded agent:

```bash
LC_SSH_SERVER="user@target-host[:port]" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
```

If `:port` is omitted, the proxy uses the default SSH port `22`.
The proxy receives `LC_SSH_SERVER`, extracts `user`, `host`, and `port`, and opens the target SSH session accordingly.

**Note**: Direct connection without `LC_SSH_SERVER` will fail with "Error: LC_SSH_SERVER not provided"

## Examples

### Example 1: Basic SendEnv usage

```bash
# Start proxy server
./ssh-proxy-server -listen localhost:2222 -key ./ssh_host_key -log-level debug -recordings-dir ./recordings

# Connect with target specified via LC_SSH_SERVER
LC_SSH_SERVER="ubuntu@192.168.1.100:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
```

## Troubleshooting

### "Error: LC_SSH_SERVER not provided"

**Problem**: The proxy requires the target host to be specified via the `LC_SSH_SERVER` environment variable.

**Solution**: Always use the SendEnv option when connecting and enable agent forwarding:

```bash
LC_SSH_SERVER="user@target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
```

### "Permission denied" or connection issues

**Problem**: SSH key authentication failed, the client key is not authorized on the proxy, or the target host is unreachable.

**Solutions**:
- Ensure your client key is present in the proxy host's `authorized_keys` or in the file passed via `-authorized-keys`
- Ensure your SSH key is added to the target host's `authorized_keys`
- Make sure your key is loaded in `ssh-agent` and connect with `ssh -A`
- Verify the target host address format: `user@host[:port]`
- Check that the target SSH server is running and accessible

### Security configuration options

For development-only scenarios, the following options are available:
- `-authorized-keys /path/to/authorized_keys` — use a custom allowlist for proxy login
- `SSH_PROXY_AUTO_ACCEPT_CLIENT_KEYS=true` — set the default for `-auto-accept-client-keys`; use `-auto-accept-client-keys=false` to enforce the allowlist
- `SSH_PROXY_ALLOW_LOCAL_AGENT_FALLBACK=1` — allow fallback to the proxy host's local `SSH_AUTH_SOCK`
- `SSH_PROXY_INSECURE_IGNORE_HOSTKEY=1` — bypass `known_hosts` verification for target connections

## Recording Format

Sessions are recorded in asciinema v2 format in the directory passed with `-recordings-dir`.
By default, that directory is `./recordings/`:
- Filename: `<user>_<host>_<port>_<session-id>.cast`
- Contains complete session transcripts, including user input and command output
- Can be viewed with `asciinema play recordings/<file>.cast`

## Project Structure

```
cmd/
└── ssh-proxy-server/
    └── main.go              # Entry point and server initialization
internal/
├── client/
│   └── client.go            # Target host connection logic
├── hostkey/
│   └── hostkey.go           # Host key generation and loading
├── recording/
│   └── recording.go         # Asciinema v2 format recording
├── server/
│   └── server.go            # SSH server connection and channel handling
└── types/
    └── types.go             # Shared type definitions
```

## Dependencies

- `golang.org/x/crypto/ssh` - SSH protocol implementation and agent/known_hosts support
- `github.com/google/uuid` - Session and recording ID generation

## Documentation

- **[QUICKSTART.md](QUICKSTART.md)** - Get started in minutes
- **[SENDENV.md](SENDENV.md)** - Detailed SendEnv configuration and examples
- **[IMPLEMENTATION.md](IMPLEMENTATION.md)** - Architecture, design, and advanced features

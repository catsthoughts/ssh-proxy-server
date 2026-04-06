# SSH Proxy Server

A transparent SSH proxy server written in Go for bastion-style access, SSH session auditing, and controlled target routing.
It accepts SSH connections with public key authentication, can either auto-accept client keys or validate them against an `authorized_keys` file, reuses the client's SSH agent to authenticate to the destination, routes sessions via `LC_SSH_SERVER=user@host[:port]`, and records activity either in `asciinema` v2 or in a plain `script`-style transcript. All startup settings are loaded from a JSON config file passed via `-config`.

**Core capabilities:**
- Accepts inbound SSH connections with configurable client-key policy via `auto_accept_client_keys` and `authorized_keys` in the JSON config
- Reuses the client's forwarded SSH agent to authenticate to the target host
- Routes sessions using `LC_SSH_SERVER=user@host[:port]` and defaults to port `22`
- Records interactive shell sessions in either `asciinema` or plain `script` transcript format for audit and analysis
- Can optionally allow direct command execution with `"allow_direct_commands": true` in the JSON config

**→ [Quick Start Guide](QUICKSTART.md) for immediate setup**

## Features

- **Authorized Client Access**: Accept client keys automatically by default, or enforce checks against the file configured in `authorized_keys`
- **SSH Agent Reuse**: Requires forwarded agent access via `ssh -A` by default
- **Session Recording**: Records proxied terminal activity in either `asciinema` (`.cast`) or plain `script` transcript (`.log`) format with private file permissions for audit and analysis
- **Terminal-Only by Default**: Accepts interactive `shell` sessions out of the box; direct `exec` requests stay disabled unless `allow_direct_commands` is enabled in the JSON config
- **Dynamic Routing via SendEnv**: Target host specified with `LC_SSH_SERVER=user@host[:port]`
- **Transparent Proxying**: Acts as an intermediate SSH server and opens a real target SSH session
- **Host Key Verification**: Requires `~/.ssh/known_hosts` by default, with an explicit insecure override for development only via `insecure_ignore_hostkey` in the JSON config
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
  "recording_format": "asciinema"
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

You can store recordings in a custom directory by setting `"recordings_dir": "/path/to/recordings"` in the JSON config.

### Connect through the proxy

**Required: Using SendEnv to specify target**

The proxy requires the target host to be specified via the `LC_SSH_SERVER` environment variable using SSH's SendEnv option.
Use `ssh -A` so the proxy can authenticate to the target with your SSH agent. By default, the proxy rejects local-agent fallback and requires a forwarded agent:

```bash
LC_SSH_SERVER="user@target-host[:port]" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
```

If `:port` is omitted, the proxy uses the default SSH port `22`.
The proxy receives `LC_SSH_SERVER`, validates it, extracts `user`, `host`, and `port`, and opens the target SSH session accordingly. Suspicious values with shell metacharacters or invalid host/port parts are rejected and logged.

**Note**: Direct connection without `LC_SSH_SERVER` will fail with "Error: LC_SSH_SERVER not provided"

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
LC_SSH_SERVER="user@target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost 'uname -a'
```

## Examples

### Example 1: Basic SendEnv usage

```bash
# Start proxy server
./ssh-proxy-server -config ./config.json

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

### "Error: direct commands are disabled"

**Problem**: You connected with `ssh ... localhost 'command'`, which sends an SSH `exec` request, but the proxy is running in terminal-only mode.

**Solution**: Either connect interactively without a trailing command, or set `"allow_direct_commands": true` in the JSON config and restart the proxy.

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
- Verify the target host address format: `user@host[:port]`
- Check that the target SSH server is running and accessible

### Security configuration options

For development-only scenarios, the following options are available:
- `authorized_keys` in `config.json` — use a custom allowlist path for proxy login
- `auto_accept_client_keys` in `config.json` — set to `false` to enforce the allowlist
- `SSH_PROXY_ALLOW_LOCAL_AGENT_FALLBACK=1` — allow fallback to the proxy host's local `SSH_AUTH_SOCK`
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

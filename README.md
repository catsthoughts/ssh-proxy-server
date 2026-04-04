# SSH Proxy Server

A Go-based SSH proxy server that:
- Accepts SSH client connections with public key authentication
- Uses the client's SSH agent to authenticate to the target host
- Routes sessions using `LC_SSH_SERVER=user@host:port`
- Records all sessions in asciinema v2 format

**→ [Quick Start Guide](QUICKSTART.md) for immediate setup**

## Features

- **SSH Agent Forwarding**: Reuses the user's SSH key via `ssh -A` / `SSH_AUTH_SOCK`
- **Session Recording**: All sessions recorded in asciinema format with contextual filenames
- **Dynamic Routing via SendEnv**: Target host specified with `LC_SSH_SERVER=user@host:port`
- **Transparent Proxying**: Acts as an intermediate SSH server and opens a real target SSH session
- **Configurable Logging**: Supports `error`, `info`, and `debug` log levels

## Build

```bash
go build -o ssh-proxy-server ./cmd/ssh-proxy-server
```

## Usage

### Start the proxy server

```bash
./ssh-proxy-server -listen localhost:2222 -key ./ssh_host_key -log-level info -recordings-dir ./recordings
```

Available log levels: `error`, `info`, `debug`

You can store recordings in a custom directory with:

```bash
-recordings-dir /path/to/recordings
```

### Connect through the proxy

**Required: Using SendEnv to specify target**

The proxy requires the target host to be specified via the `LC_SSH_SERVER` environment variable using SSH's SendEnv option.
Use `ssh -A` so the proxy can authenticate to the target with your SSH agent:

```bash
LC_SSH_SERVER="user@target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
```

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

### Example 2: Multiple proxy instances

```bash
# Start multiple proxy instances on different ports
./ssh-proxy-server -listen localhost:2222 -key ./ssh_key_1 -log-level info -recordings-dir ./recordings/proxy1 &
./ssh-proxy-server -listen localhost:2223 -key ./ssh_key_2 -log-level info -recordings-dir ./recordings/proxy2 &

# Connect to different targets via SendEnv
LC_SSH_SERVER="user1@host1:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
LC_SSH_SERVER="user2@host2:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2223 localhost
```

## Troubleshooting

### "Error: LC_SSH_SERVER not provided"

**Problem**: The proxy requires the target host to be specified via the `LC_SSH_SERVER` environment variable.

**Solution**: Always use the SendEnv option when connecting and enable agent forwarding:

```bash
LC_SSH_SERVER="user@target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
```

### "Permission denied" or connection issues

**Problem**: SSH key authentication failed or target host unreachable.

**Solutions**:
- Ensure your SSH key is added to the target host's `authorized_keys`
- Make sure your key is loaded in `ssh-agent` and connect with `ssh -A`
- Verify the target host address format: `user@host:port`
- Check that the target SSH server is running and accessible

## Recording Format

Sessions are recorded in asciinema v2 format in the directory passed with `-recordings-dir`.
By default, that directory is `./recordings/`:
- Filename: `<user>_<host>_<port>_<session-id>.cast`
- Contains complete session transcripts (input and output)
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

- `golang.org/x/crypto/ssh` - SSH protocol implementation
- `golang.org/x/term` - Terminal operations
- `github.com/google/uuid` - Session ID generation

## Documentation

- **[QUICKSTART.md](QUICKSTART.md)** - Get started in minutes
- **[SENDENV.md](SENDENV.md)** - Detailed SendEnv configuration and examples
- **[IMPLEMENTATION.md](IMPLEMENTATION.md)** - Architecture, design, and advanced features

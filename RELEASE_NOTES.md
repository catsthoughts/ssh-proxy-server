# SSH Proxy Server - Release Notes

## Overview

SSH proxy server with dynamic target routing via `LC_SSH_SERVER` or static routing through a fixed server list, SSH agent-based target authentication, session recording, clean shutdown handling, terminal resize forwarding, and config-driven startup via `-config`. Interactive terminal sessions are enabled by default; direct commands require `allow_direct_commands` in the JSON config.

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

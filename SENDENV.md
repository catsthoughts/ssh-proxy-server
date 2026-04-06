# SSH `SendEnv` Guide

This document explains how to use `LC_SSH_SERVER` with SSH's `SendEnv` option to tell the proxy which target to open.

## Required format

Use:

```text
LC_SSH_SERVER=host:port
```

Examples:
- `target-host.example.com:22`
- `example.internal:2222`

The proxy will use the authenticated SSH session user for the final target login. Legacy `user@host[:port]` values are still accepted for compatibility.

If `:port` is omitted, the proxy defaults to `22`.

## Basic usage

### 1. Start the proxy

```bash
./ssh-proxy-server -config ./config.json
```

### 2. Connect through it

```bash
LC_SSH_SERVER="target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost
```

This does three things:
1. defines `LC_SSH_SERVER` locally
2. sends it to the proxy over SSH
3. lets the proxy validate and parse `host` and `port`, then reuse the SSH session user for the target login

## Important note about authentication

The proxy uses the client's SSH agent to authenticate to the target host.
For that reason, use:

```bash
ssh -A ...
```

or make sure `SSH_AUTH_SOCK` is available in the environment where the proxy runs.

## SSH config example

Add this to `~/.ssh/config`:

```ssh-config
Host my-proxy
    HostName localhost
    Port 2222
    User your-local-user
    ForwardAgent yes
    SendEnv LC_SSH_SERVER
```

Then connect like this:

```bash
LC_SSH_SERVER="target-host:22" ssh your-user@my-proxy
```

## What the proxy currently supports

- `LC_SSH_SERVER` parsing from SSH `env` requests
- interactive `shell` sessions by default
- optional `exec` requests when `allow_direct_commands` is enabled in the JSON config
- PTY allocation via `pty-req`
- live terminal resize via `window-change`
- session recording in either `asciinema` (`.cast`) or plain `script` transcript (`.log`) format via the `recording_format` config setting

## Troubleshooting

### `Error: LC_SSH_SERVER not provided`

Use the exact command form below:

```bash
LC_SSH_SERVER="target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost
```

### `Permission denied` or no usable key

Check your agent:

```bash
ssh-add -l
```

Load a key if needed:

```bash
ssh-add ~/.ssh/id_rsa
```

### Host key verification issues on the target

The proxy requires `~/.ssh/known_hosts` for target verification by default. For temporary development-only use, you can override this by setting `"insecure_ignore_hostkey": true` in the JSON config or by setting `SSH_PROXY_INSECURE_IGNORE_HOSTKEY=1`.

## Notes

- The current implementation expects the variable name **exactly** as `LC_SSH_SERVER`
- Suspicious values containing shell metacharacters, invalid host/user parts, or malformed ports are rejected and logged
- Do not put passwords or secrets inside the variable
- Recordings are saved as:

```text
recordings/<user>_<host>_<port>_<session-id>.cast
# or .log when `"recording_format": "script"` is set in the JSON config
```

## Comparison with Alternatives

### vs hardcoded target in server
| Feature | SendEnv | Hardcoded |
|---------|---------|-----------|
| Per-client targeting | ✓ | ✗ |
| No server restart | ✓ | ✗ |
| Configuration needed | Minimal | ✓ |
| Use multiple targets | ✓ | ✗ |

# SSH `SendEnv` Guide

This document explains how to use `LC_SSH_SERVER` with SSH's `SendEnv` option to tell the proxy which target to open.

## Required format

Use:

```text
LC_SSH_SERVER=user@host:port
```

Examples:
- `ubuntu@192.168.1.100:22`
- `admin@example.internal:2222`

If `:port` is omitted, the proxy defaults to `22`.

## Basic usage

### 1. Start the proxy

```bash
./ssh-proxy-server -listen localhost:2222 -key ./ssh_host_key -log-level info -recordings-dir ./recordings
```

### 2. Connect through it

```bash
LC_SSH_SERVER="user@target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
```

This does three things:
1. defines `LC_SSH_SERVER` locally
2. sends it to the proxy over SSH
3. lets the proxy parse `user`, `host`, and `port` and dial the target

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
LC_SSH_SERVER="user@target-host:22" ssh my-proxy
```

## What the proxy currently supports

- `LC_SSH_SERVER` parsing from SSH `env` requests
- interactive `shell` sessions
- `exec` requests
- PTY allocation via `pty-req`
- live terminal resize via `window-change`
- session recording to asciinema v2 files

## Troubleshooting

### `Error: LC_SSH_SERVER not provided`

Use the exact command form below:

```bash
LC_SSH_SERVER="user@target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
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

The proxy tries to use `~/.ssh/known_hosts`. If it is unavailable, it falls back to insecure host key verification for that connection and logs this fact.

## Notes

- The current implementation expects the variable name **exactly** as `LC_SSH_SERVER`
- Do not put passwords or secrets inside the variable
- Recordings are saved as:

```text
recordings/<user>_<host>_<port>_<session-id>.cast
```

## Comparison with Alternatives

### vs hardcoded target in server
| Feature | SendEnv | Hardcoded |
|---------|---------|-----------|
| Per-client targeting | ✓ | ✗ |
| No server restart | ✓ | ✗ |
| Configuration needed | Minimal | ✓ |
| Use multiple targets | ✓ | ✗ |

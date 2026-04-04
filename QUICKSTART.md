# Quick Start Guide

## 1. Build the proxy

```bash
go build -o ssh-proxy-server ./cmd/ssh-proxy-server
```

## 2. Run the proxy

```bash
./ssh-proxy-server -listen localhost:2222 -key ./ssh_host_key -log-level info -recordings-dir ./recordings
```

Available log levels:
- `error`
- `info`
- `debug`

## 3. Connect through the proxy

In another terminal:

```bash
LC_SSH_SERVER="user@target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
```

Replace:
- `user` — username on the target host
- `target-host` — hostname or IP of the target SSH server
- `22` — target SSH port; if omitted, the proxy defaults to `22`

> `ssh -A` is recommended so the proxy can authenticate to the target using your SSH agent.

## 4. Check recordings

Sessions are saved to the directory passed with `-recordings-dir`.
If you use the default value, they appear in `recordings/`:

```bash
ls -la recordings/
asciinema play recordings/<user>_<host>_<port>_<session-id>.cast
```

## 5. Troubleshooting

### SSH agent has no keys

Check that your key is loaded:

```bash
ssh-add -l
```

If needed:

```bash
ssh-add ~/.ssh/id_rsa
```

### Proxy says `LC_SSH_SERVER not provided`

Make sure you connect exactly like this:

```bash
LC_SSH_SERVER="user@target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost
```

## How it works

1. You set `LC_SSH_SERVER=user@host:port`
2. SSH sends it to the proxy via `SendEnv`
3. The proxy parses `user`, `host`, and `port`
4. The proxy authenticates to the target with your SSH agent
5. Input/output is recorded in asciinema v2 format

## More information

- [README.md](README.md) — overview and usage
- [IMPLEMENTATION.md](IMPLEMENTATION.md) — architecture and internals
- [SENDENV.md](SENDENV.md) — details about `LC_SSH_SERVER` and `SendEnv`

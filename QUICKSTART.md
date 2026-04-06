# Quick Start Guide

## 1. Build the proxy

```bash
go build -o ssh-proxy-server ./cmd/ssh-proxy-server
```

## 2. Run the proxy

Create a config file from the example:

```bash
cp ./config.example.json ./config.json
```

By default, the example uses a local `./authorized_keys` path, so make sure that file exists if you disable auto-accept.

Then start the proxy with the config file:

```bash
./ssh-proxy-server -config ./config.json
```

Available log levels:
- `error`
- `info`
- `debug`

> By default, the proxy allows **interactive terminal sessions only** and records sessions in `asciinema` format.
>
> To permit `ssh ... <command>` style execution, set `"allow_direct_commands": true` in `config.json`.
>
> If you want plain-text transcript files instead of `.cast`, set `"recording_format": "script"`.
>
> If you want the proxy to choose from a fixed target list, enable `"static_routing": { "enabled": true, ... }` in `config.json`.
>
> The top-level `"retries"` and `"connect_timeout_seconds"` settings apply to both normal `LC_SSH_SERVER` routing and static routing.
>
> If the target host key changed and you are in a temporary development scenario, set `"insecure_ignore_hostkey": true`.

## 3. Connect through the proxy

In another terminal:

```bash
LC_SSH_SERVER="target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost
```

Replace:
- `user` — username on the target host
- `target-host` — hostname or IP of the target SSH server
- `22` — target SSH port; if omitted, the proxy defaults to `22`

> `ssh -A` is recommended so the proxy can authenticate to the target using your SSH agent.

### Optional: enable direct commands

If you want one-shot command execution, set `"allow_direct_commands": true` in `config.json`, restart the proxy, and then connect with a trailing command:

```bash
./ssh-proxy-server -config ./config.json
LC_SSH_SERVER="target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost 'hostname'
```

### Optional: use static routing with failover / round-robin

Set `"static_routing": { "enabled": true, ... }` in `config.json` to route through a fixed list of servers. In this mode, `LC_SSH_SERVER` is optional and ignored:

```bash
ssh -A -p 2222 localhost
```

The proxy will try the configured targets in order, honor the configured timeout, and retry / move to the next server when a target does not respond.

### Optional: ignore target host key mismatches for development

If you hit `knownhosts: key mismatch`, set `"insecure_ignore_hostkey": true` in `config.json` and restart the proxy.

### Optional: store recordings as plain `script` transcripts

Set `"recording_format": "script"` in `config.json`.

## 4. Check recordings

Sessions are saved to the directory configured by `"recordings_dir"` in `config.json`.
If you use the default value, they appear in `recordings/`:

```bash
ls -la recordings/
asciinema play recordings/<user>_<host>_<port>_<session-id>.cast
# or inspect plain-text script transcripts when "recording_format": "script" is used
cat recordings/<user>_<host>_<port>_<session-id>.log
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
LC_SSH_SERVER="target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 your-user@localhost
```

## How it works

1. You either set `LC_SSH_SERVER=host:port` or enable `static_routing` in `config.json`
2. SSH connects to the proxy with agent forwarding as the desired remote user
3. The proxy resolves the target dynamically or from the static server list
4. The proxy authenticates to the target with your SSH agent using that SSH session user by default
5. Input/output is recorded in the configured format (`asciinema` by default, or `script` if selected)

## More information

- [README.md](README.md) — overview and usage
- [IMPLEMENTATION.md](IMPLEMENTATION.md) — architecture and internals
- [SENDENV.md](SENDENV.md) — details about `LC_SSH_SERVER` and `SendEnv`

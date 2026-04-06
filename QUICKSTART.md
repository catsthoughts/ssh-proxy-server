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
> If the target host key changed and you are in a temporary development scenario, set `"insecure_ignore_hostkey": true`.

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

### Optional: enable direct commands

If you want one-shot command execution, set `"allow_direct_commands": true` in `config.json`, restart the proxy, and then connect with a trailing command:

```bash
./ssh-proxy-server -config ./config.json
LC_SSH_SERVER="user@target-host:22" ssh -A -o "SendEnv=LC_SSH_SERVER" -p 2222 localhost 'hostname'
```

### Optional: ignore target host key mismatches for development

If you hit `knownhosts: key mismatch`, set `"insecure_ignore_hostkey": true` in `config.json` and restart the proxy.

### Optional: store recordings as plain `script` transcripts

Set `"recording_format": "script"` in `config.json`.

## 4. Check recordings

Sessions are saved to the directory passed with `-recordings-dir`.
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

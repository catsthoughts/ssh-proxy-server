# E2E Tests

E2E tests verify the proxy with real SSH connections using Docker containers.

## Prerequisites

- Docker and docker-compose
- Go 1.21+

## Quick Start

```bash
# Start infrastructure and run tests
make e2e-infra
make e2e-test
```

## Available Tests

### TestClientCertificateAuth
**Client authentication with certificate via agent forwarding**

The client authenticates to the proxy with a certificate, then uses SSH agent forwarding to authenticate to the target host. The certificate is passed through the SSH agent.

**Expected result:** SUCCESS - command executes on target through proxy using certificate-based authentication.

### TestClientKeyAuthWithoutCertificate
**Client authentication with raw key (no certificate)**

The client authenticates to the proxy with a raw key (without certificate), then uses SSH agent forwarding to authenticate to the target host.

**Expected result:** SUCCESS - command executes on target through proxy using raw key authentication.

### TestHostCertificateVerification
**Host certificate verification**

Tests that the proxy correctly verifies the target host's certificate. The client connects through the proxy to a target that has a CA-signed certificate.

**Expected result:** SUCCESS - host certificate verification works correctly.

### TestMultipleConcurrentSessions
**10 sequential sessions with different users**

Sequentially runs 10 sessions, each with a unique user (user1-user10) using their unique key and certificate.

**Expected result:** All 10 sessions SUCCEEDED.

### TestOneUserRandomConcurrentSessions
**Random concurrent sessions for 10 users**

Runs 10 users, each with a random number of concurrent sessions (1-5). Total ~30 concurrent sessions.

**Expected result:** SUCCESS - all concurrent sessions complete successfully.

### TestSequentialUsers
**Sequential testing with 1-5 users**

Tests 1, 2, 3, 4, 5 users sequentially. Each user uses a unique key and certificate.

**Expected result:** All users from 1 to 5 authenticate successfully.

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make e2e-infra` | Start CA, target, and proxy containers |
| `make e2e-test` | Run E2E tests inside Docker container |
| `make e2e-test-host` | Run E2E tests on host machine |
| `make e2e-logs` | Show proxy logs |
| `make e2e-stop` | Stop containers (keep keys) |
| `make e2e-down` | Stop and remove containers |
| `make e2e-clean` | Remove everything including keys |
| `make e2e-regen-keys` | Regenerate all keys and restart CA |
| `make e2e-rebuild` | Rebuild proxy image and restart |
| `make e2e-full` | Full cycle: clean + build + test |

## Supported Keys and Certificates

### Supported Key Types

| Key Type | Supported | Notes |
|----------|-----------|-------|
| ED25519 | ✅ Yes | Recommended type |
| ECDSA P-256 | ✅ Yes | Supported |
| RSA | ❌ No | Not supported in tests |

### Certificate Types

| Certificate Type | Agent Forwarding | Notes |
|-----------------|------------------|-------|
| ED25519-CERT | ✅ Yes | Works with TrustedUserCAKeys |
| ECDSA-CERT | ✅ Yes | Works with TrustedUserCAKeys |

### Required SSHd Configuration for Certificate Auth

To support certificate-based authentication on the target SSH server, add `TrustedUserCAKeys`:

```
TrustedUserCAKeys /path/to/ca_key.pub
```

This directive tells sshd to trust certificates signed by the specified CA key.

## Key and Certificate Generation Examples

### 1. Generate CA Key

```bash
# ED25519 CA (recommended)
ssh-keygen -t ed25519 -f ca_key -N "" -C "My CA"

# ECDSA P-256 CA
ssh-keygen -t ecdsa -b 256 -f ecdsa_ca_key -N "" -C "My ECDSA CA"
```

### 2. Generate Client Key

```bash
# ED25519 key
ssh-keygen -t ed25519 -f user_key -N "" -C "user@example.com"

# ECDSA P-256 key
ssh-keygen -t ecdsa -b 256 -f user_ecdsa_key -N "" -C "user@example.com"
```

### 3. Sign Client Certificate

```bash
# Sign with ED25519 CA
ssh-keygen -s ca_key -I "user@example.com" -n username -V +1h user_key.pub

# Sign with validity period
ssh-keygen -s ca_key -I "user@example.com" -n username -V +24h -V +25h user_key.pub

# Specify principals
ssh-keygen -s ca_key -I "user@example.com" -n username -n root,admin -V +1h user_key.pub

# ECDSA certificate with ECDSA CA
ssh-keygen -s ecdsa_ca_key -I "user@example.com" -n username -V +1h user_ecdsa_key.pub
```

**Parameters:**
- `-s ca_key` - Signing CA private key
- `-I "description"` - Key ID (appears in logs)
- `-n username` - Principal(s) - username for SSH login
- `-V +1h` - Valid for 1 hour from now
- `-h` - Make host certificate (for server keys)

### 4. Generate Host Certificate

```bash
ssh-keygen -s ca_key -I "target-host" -n localhost -h -V +1h host_key.pub
```

### 5. Extract Public Key from Certificate

```bash
ssh-keygen -y -f user_key    # Show public key from private key
ssh-keygen -y -f user_key -f user_key-cert.pub  # With certificate
```

## SSH Client Usage Examples

### Connect with Certificate

```bash
# Using certificate file directly
ssh -i user_key -o CertificateFile=user_key-cert.pub user@target-host

# Using agent with certificate
ssh-add user_key
ssh -A user@target-host  # Agent forwarding enabled
```

### Connect with SSH Agent Forwarding

```bash
# Add key and cert to agent
ssh-add user_key

# Connect with agent forwarding
ssh -A user@target-host

# Verify agent is forwarded
ssh -A user@target-host "ssh-add -l"
```

### Verify Certificate Details

```bash
# Show certificate info
ssh-keygen -L -f user_key-cert.pub

# Check fingerprint
ssh-keygen -lf user_key-cert.pub

# Compare with what SSH client uses
ssh -v -i user_key -o CertificateFile=user_key-cert.pub user@target-host 2>&1 | grep "Offering public key"
```

## Target SSHd Configuration

### Minimal /etc/ssh/sshd_config for Certificate Auth

```bash
# Basic settings
PermitRootLogin no
PubkeyAuthentication yes
PasswordAuthentication no
ChallengeResponseAuthentication no
UsePAM no

# Allow specific user
AllowUsers testuser

# Certificate authentication (REQUIRED!)
TrustedUserCAKeys /etc/ssh/trusted_user_ca.pub

# Agent forwarding
AllowAgentForwarding yes
AllowTcpForwarding yes
```

### Generate and Install CA Public Key

```bash
# On CA machine - extract public key
ssh-keygen -y -f ca_key > ca_key.pub

# On target machine - install CA public key
cp ca_key.pub /etc/ssh/trusted_user_ca.pub
chmod 644 /etc/ssh/trusted_user_ca.pub
```

### Full Setup Example

```bash
# 1. Generate CA
ssh-keygen -t ed25519 -f my_ca -N "" -C "Production CA"
chmod 600 my_ca  # Keep CA private key SECURE

# 2. Generate user key
ssh-keygen -t ed25519 -f user_key -N "" -C "developer@workstation"

# 3. Sign user certificate (valid for 8 hours)
ssh-keygen -s my_ca -I "developer@workstation" -n developer -V +8h user_key.pub

# 4. On target server - install CA public key
ssh-keygen -y -f my_ca > /etc/ssh/trusted_user_ca.pub

# 5. Add to /etc/ssh/sshd_config:
echo "TrustedUserCAKeys /etc/ssh/trusted_user_ca.pub" >> /etc/ssh/sshd_config

# 6. Restart sshd
systemctl restart sshd

# 7. Connect from client
ssh -i user_key -o CertificateFile=user_key-cert.pub developer@target-server
```

### Verifying Setup

```bash
# On target - check sshd is configured
sshd -T | grep -E "trustedusercakeys|pubkeyauthentication"

# On client - verify certificate is valid
ssh-keygen -L -f user_key-cert.pub

# Test connection with verbose output
ssh -v -i user_key -o CertificateFile=user_key-cert.pub developer@target-server 2>&1 | grep -E "Offering|Authentications|Accepted"
```

## Generated Keys Structure

```
e2e/e2e-keys/
├── ca/
│   ├── ca_key              # ED25519 CA private key (keep secure!)
│   ├── ca_key.pub          # ED25519 CA public key
│   ├── ecdsa_ca_key        # ECDSA P-256 CA private key
│   └── ecdsa_ca_key.pub    # ECDSA CA public key
├── clients/
│   ├── user1_key           # ED25519 private key
│   ├── user1_key.pub       # ED25519 public key
│   ├── user1_key_valid-cert.pub    # Valid certificate (1h, -V +1h)
│   ├── user1_key-cert.pub          # Expired certificate (for testing rejection)
│   ├── user2_key ... user10_key    # Same structure as user1_key
│   ├── user1_ecdsa_key      # ECDSA P-256 private key
│   ├── user1_ecdsa_key.pub  # ECDSA public key
│   └── user1_ecdsa_key-cert.pub    # Valid ECDSA certificate
├── hosts/
│   ├── target_key          # Host private key
│   └── target_key-cert.pub # Host certificate
├── combined_authorized_keys  # All client public keys combined
├── target_authorized_keys    # Target authorized_keys with CA entries
├── trusted_user_ca.pub      # CA for proxy config (user certs)
├── trusted_ecdsa_user_ca.pub
├── trusted_host_ca.pub      # CA for proxy config (host certs)
└── trusted_*_ca.pub        # CA public keys for proxy configuration
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `REGENERATE_KEYS` | `false` | Set to `true` to force key regeneration |

## Running Specific Tests

```bash
cd e2e && go test -v -tags=e2econtainer -count=1 -run "TestSequentialUsers"
cd e2e && go test -v -tags=e2econtainer -count=1 -run "TestOneUserRandomConcurrentSessions"
cd e2e && go test -v -tags=e2econtainer -count=1 -run "TestClientCertificateAuth"
```

## Test Infrastructure

The E2E setup consists of three Docker containers:

- **ca** - Generates CA keys, client keys/certificates, and host keys
- **target** - Alpine-based SSH server acting as the destination host
- **proxy** - The ssh-proxy-server under test

### Files

| File | Description |
|------|-------------|
| `e2e/config.json` | Proxy configuration |
| `e2e/ssh_host_key` | Proxy host key |
| `e2e/docker-compose.yml` | Container orchestration |
| `e2e/cert_test.go` | Container-side tests (run with `make e2e-test`) |
| `e2e/multi_session_test.go` | Concurrent session tests |
| `e2e/sequential_test.go` | Sequential user tests |

## Troubleshooting

### "Permission denied (publickey)" with valid certificate

1. Check that `TrustedUserCAKeys` is configured in sshd_config:
   ```bash
   grep TrustedUserCAKeys /etc/ssh/sshd_config
   ```

2. Verify CA public key is correct:
   ```bash
   ssh-keygen -lf /etc/ssh/trusted_user_ca.pub
   ```

3. Check certificate principals match username:
   ```bash
   ssh-keygen -L -f user-cert.pub | grep Principals
   ```

4. Check certificate is not expired:
   ```bash
   ssh-keygen -L -f user-cert.pub | grep Valid
   ```

### Agent forwarding not working

1. Verify `AllowAgentForwarding yes` is in sshd_config
2. Check client is using `-A` flag or `ForwardAgent yes` in ~/.ssh/config
3. Verify agent is running: `ssh-add -l`

### Keys not working after regeneration

If tests fail after `make e2e-regen-keys`, ensure all containers are restarted:
```bash
make e2e-down
make e2e-infra
make e2e-test
```

# SSH Certificate Authentication Setup

This guide explains how to configure SSH certificate-based authentication for use with the SSH Proxy Server.

## Overview

SSH certificates provide a more scalable alternative to traditional SSH authorized_keys management. Instead of distributing individual public keys to every server, you:

1. Create a Certificate Authority (CA) key
2. Sign user public keys with the CA to create certificates
3. Configure servers to trust certificates signed by your CA

## Why Use Certificates?

| Approach | Scalability | Key Rotation | Access Revocation |
|----------|-------------|-------------|-------------------|
| Traditional authorized_keys | Poor - need to update every server | Difficult | Difficult |
| Certificates | Excellent - one CA trusted by all servers | Easy - re-sign or let certs expire | Easy - let certs expire |

## Certificate Generation Examples

### 1. Generate CA Key

```bash
# ED25519 CA (recommended)
ssh-keygen -t ed25519 -f ca_key -N "" -C "My Production CA"

# ECDSA P-256 CA
ssh-keygen -t ecdsa -b 256 -f ecdsa_ca_key -N "" -C "My ECDSA CA"
```

**Important:** Keep your CA private key secure! Anyone with access to it can create valid certificates.

### 2. Generate User Key

```bash
# ED25519 user key
ssh-keygen -t ed25519 -f user_key -N "" -C "user@example.com"

# ECDSA P-256 user key
ssh-keygen -t ecdsa -b 256 -f user_ecdsa_key -N "" -C "user@example.com"
```

### 3. Sign User Certificate

```bash
# Basic certificate (valid for 8 hours)
ssh-keygen -s ca_key -I "user@example.com" -n username -V +8h user_key.pub

# Certificate with specific principals
ssh-keygen -s ca_key -I "user@example.com" -n username,admin -V +8h user_key.pub

# Long-lived certificate (7 days)
ssh-keygen -s ca_key -I "user@example.com" -n username -V +168h user_key.pub

# ECDSA certificate with ECDSA CA
ssh-keygen -s ecdsa_ca_key -I "user@example.com" -n username -V +8h user_ecdsa_key.pub
```

#### Certificate Signing Parameters

| Parameter | Description | Example |
|-----------|-------------|---------|
| `-s ca_key` | Signing CA private key | `ca_key` |
| `-I "description"` | Key ID for logging | `"user@example.com"` |
| `-n username` | Principal(s) - SSH login username | `developer`, `admin,root` |
| `-V +Nh` | Valid for N hours from now | `+8h`, `+24h` |
| `-V +Nh:+Nh` | Valid from N hours to N hours | `-V +24h:+48h` |
| `-h` | Host certificate (not user) | For server keys |

### 4. Generate Host Certificate

```bash
# Sign host key with CA
ssh-keygen -s ca_key -I "target-host" -n localhost,target -h -V +24h host_key.pub

# The -h flag makes it a host certificate
# -n specifies valid hostnames
```

### 5. Inspect Certificate

```bash
# View certificate details
ssh-keygen -L -f user_key-cert.pub

# Check fingerprint
ssh-keygen -lf user_key-cert.pub

# View in different formats
ssh-keygen -e -f user_key-cert.pub    # RFC 4716 format
ssh-keygen -e -m RFC4716 -f user_key-cert.pub
```

## OpenSSH Server Configuration

### Required: TrustedUserCAKeys

Add to `/etc/ssh/sshd_config`:

```
TrustedUserCAKeys /etc/ssh/trusted_user_ca.pub
```

This tells sshd to trust certificates signed by the specified CA.

### Full Example /etc/ssh/sshd_config

```bash
# Basic settings
PermitRootLogin no
PubkeyAuthentication yes
PasswordAuthentication no
ChallengeResponseAuthentication no
UsePAM no

# Allow specific users
AllowUsers username1 username2

# Certificate authentication (REQUIRED)
TrustedUserCAKeys /etc/ssh/trusted_user_ca.pub

# Agent forwarding (if needed)
AllowAgentForwarding yes
AllowTcpForwarding yes
```

### Install CA Public Key on Server

```bash
# Extract public key from CA
ssh-keygen -y -f ca_key > trusted_user_ca.pub

# Install on target server
sudo cp trusted_user_ca.pub /etc/ssh/trusted_user_ca.pub
sudo chmod 644 /etc/ssh/trusted_user_ca.pub

# Restart sshd
sudo systemctl restart sshd
```

## SSH Client Usage Examples

### Connect with Certificate File

```bash
# Using certificate directly
ssh -i user_key -o CertificateFile=user_key-cert.pub username@target-host

# With verbose output to verify
ssh -v -i user_key -o CertificateFile=user_key-cert.pub username@target-host 2>&1 | grep -E "Offering|Accepted"
```

### Connect with SSH Agent

```bash
# Add key and certificate to agent
ssh-add user_key

# Connect with agent forwarding
ssh -A username@target-host

# Verify certificate is being used
ssh -A username@target-host "ssh-add -l"
```

### SSH Config Example

Add to `~/.ssh/config`:

```
Host target-server
    HostName target-host.example.com
    User username
    IdentityFile ~/.ssh/user_key
    CertificateFile ~/.ssh/user_key-cert.pub
    ForwardAgent yes
```

Then simply:
```bash
ssh target-server
```

## Verifying Your Setup

### On Server: Check sshd Configuration

```bash
# Test configuration
sshd -t

# Verify TrustedUserCAKeys is set
grep TrustedUserCAKeys /etc/ssh/sshd_config

# Check with sshd -T (uppercase)
sudo sshd -T | grep trustedusercakeys
```

### On Client: Verify Certificate

```bash
# Check certificate details
ssh-keygen -L -f user_key-cert.pub

# Look for:
# - Valid principals (should match username)
# - Valid not before / not after (current time should be within range)
# - Serial number
```

### Test Connection

```bash
# With verbose output
ssh -v -i user_key -o CertificateFile=user_key-cert.pub username@target-host 2>&1

# Look for:
# "Offering public key: ... ED25519-CERT"
# "Authentications that can continue: publickey"
# "Accepted publickey"
```

## Troubleshooting

### "Permission denied (publickey)" with valid certificate

1. **Check TrustedUserCAKeys is configured:**
   ```bash
   grep TrustedUserCAKeys /etc/ssh/sshd_config
   ```

2. **Verify CA public key is correct:**
   ```bash
   # On CA machine
   ssh-keygen -lf ca_key.pub

   # On target server
   ssh-keygen -lf /etc/ssh/trusted_user_ca.pub
   ```
   These fingerprints should match.

3. **Check certificate principals:**
   ```bash
   ssh-keygen -L -f user_key-cert.pub | grep Principals
   ```
   The principal must match the username you're trying to log in with.

4. **Check certificate validity:**
   ```bash
   ssh-keygen -L -f user_key-cert.pub | grep -E "Valid|Serial"
   ```
   Current time should be within the validity period.

5. **Check sshd logs:**
   ```bash
   sudo journalctl -u sshd -n 50
   # or
   sudo tail -f /var/log/auth.log
   ```

### "Certificate is not trusted"

The CA public key isn't properly configured on the target server:

```bash
# On target, verify the CA file exists and is readable
ls -la /etc/ssh/trusted_user_ca.pub
cat /etc/ssh/trusted_user_ca.pub
```

### "Key type not supported"

Make sure the key type is supported by both the CA and the target server's sshd:

```bash
# Check key types
ssh-keygen -lf user_key-cert.pub   # Certificate type
ssh-keygen -lf user_key.pub         # Key type
```

ED25519 and ECDSA P-256 are typically well-supported.

## Certificate Renewal and Rotation

### Short-Lived Certificates (Recommended)

Create short-lived certificates that expire automatically:

```bash
# 8-hour certificate
ssh-keygen -s ca_key -I "user@example.com" -n username -V +8h user_key.pub

# Add to cron for automatic renewal
# 0 */8 * * * ssh-keygen -s ca_key -I "user@example.com" -n username -V +8h -f user_key.pub -o user_key-cert.pub
```

### Revoking Access

Options:

1. **Wait for certificate to expire** (if using short-lived certs)
2. **Remove principal from certificate and re-sign** (requires new cert)
3. **For emergencies: add user to DenyUsers or revoke key in authorized_keys**

```bash
# Emergency revocation - add to sshd_config
echo "DenyUsers username" >> /etc/ssh/sshd_config
sudo systemctl restart sshd
```

## Security Best Practices

1. **Protect your CA private key**
   - Store on a secure workstation or hardware token
   - Use passphrase: `ssh-keygen -t ed25519 -f ca_key -N "your-passphrase"`
   - Never put it on servers

2. **Use short-lived certificates**
   - 8-24 hours for production access
   - Reduces window of opportunity if certificate is compromised

3. **Use separate CAs for different environments**
   ```bash
   # Production CA
   ssh-keygen -t ed25519 -f production_ca_key -N "" -C "Production CA"

   # Development CA
   ssh-keygen -t ed25519 -f dev_ca_key -N "" -C "Development CA"
   ```

4. **Monitor certificate usage**
   - sshd logs show certificate Key ID
   - Useful for auditing who accessed what

5. **Combine with other security measures**
   - Firewall rules
   - IP allowlisting
   - Multi-factor authentication

## Quick Reference

```bash
# 1. Create CA (do once)
ssh-keygen -t ed25519 -f ca_key -N "" -C "My CA"

# 2. Create user key (per user)
ssh-keygen -t ed25519 -f user_key -N "" -C "user@example.com"

# 3. Sign certificate (per user, as needed)
ssh-keygen -s ca_key -I "user@example.com" -n username -V +8h user_key.pub

# 4. Install CA pubkey on servers (do once per server)
ssh-keygen -y -f ca_key > /etc/ssh/trusted_user_ca.pub

# 5. Connect with certificate
ssh -i user_key -o CertificateFile=user_key-cert.pub username@target
```

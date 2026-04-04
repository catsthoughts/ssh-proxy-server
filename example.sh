#!/bin/bash

# Example usage script for SSH Proxy Server

set -euo pipefail

PROXY_LISTEN_HOST="localhost"
PROXY_LISTEN_PORT="2222"
PROXY_KEY_FILE="./ssh_host_key"
LOG_LEVEL="debug"
RECORDINGS_DIR="./recordings"
AUTHORIZED_KEYS_FILE="${HOME}/.ssh/authorized_keys"
AUTO_ACCEPT_CLIENT_KEYS="true"
TARGET_EXAMPLE="ubuntu@192.168.1.100:22"

echo "SSH Proxy Server - Example Usage"
echo "================================"
echo ""
echo "1) Build the server"
echo "-------------------"
echo "  go build -o ssh-proxy-server ./cmd/ssh-proxy-server"
echo ""
echo "2) Start the proxy server"
echo "-------------------------"
echo "  ./ssh-proxy-server \\
    -listen ${PROXY_LISTEN_HOST}:${PROXY_LISTEN_PORT} \\
    -key ${PROXY_KEY_FILE} \\
    -log-level ${LOG_LEVEL} \\
    -recordings-dir ${RECORDINGS_DIR} \\
    -authorized-keys ${AUTHORIZED_KEYS_FILE} \\
    -auto-accept-client-keys=${AUTO_ACCEPT_CLIENT_KEYS}"
echo ""
echo "3) Connect through the proxy with SSH agent forwarding"
echo "------------------------------------------------------"
echo "  LC_SSH_SERVER='${TARGET_EXAMPLE}' ssh -A -o 'SendEnv=LC_SSH_SERVER' -p ${PROXY_LISTEN_PORT} ${PROXY_LISTEN_HOST}"
echo ""
echo "Notes:"
echo "  - LC_SSH_SERVER should use the format user@host[:port]"
echo "  - Use ssh -A so the proxy can authenticate to the target with your SSH agent"
echo "  - Set -auto-accept-client-keys=false to enforce the allowlist in ${AUTHORIZED_KEYS_FILE}"
echo "  - Log levels: error | info | debug"
echo ""
echo "Recordings are written to ${RECORDINGS_DIR}/"
echo "Example filename: user_host_port_<session-id>.cast"
echo "View with: asciinema play ${RECORDINGS_DIR}/<file>.cast"


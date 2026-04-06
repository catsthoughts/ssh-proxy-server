package client

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ssh-proxy-server/internal/recording"
	"ssh-proxy-server/internal/types"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	insecureIgnoreHostKeyEnv = "SSH_PROXY_INSECURE_IGNORE_HOSTKEY"
)

func envEnabled(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

// ConnectToTarget establishes an SSH connection to the target host
// using the user's SSH agent according to the SSH agent forwarding protocol.
func ConnectToTarget(state *types.SessionState, targetUser, targetHost, targetPort string) (*ssh.Client, error) {
	authMethod, err := authMethodForClient(state)
	if err != nil {
		return nil, err
	}

	hostKeyCallback, err := getHostKeyCallback(state)
	if err != nil {
		return nil, err
	}

	timeout := 10 * time.Second
	if state != nil && state.ConnectTimeout > 0 {
		timeout = state.ConnectTimeout
	}

	config := &ssh.ClientConfig{
		User:            targetUser,
		Auth:            []ssh.AuthMethod{authMethod},
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
	}

	targetNetwork := net.JoinHostPort(targetHost, targetPort)
	client, err := ssh.Dial("tcp", targetNetwork, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to target %s@%s: %w", targetUser, targetNetwork, err)
	}

	return client, nil
}

func authMethodForClient(state *types.SessionState) (ssh.AuthMethod, error) {
	agentClient, err := GetSSHAgentConn(state)
	if err != nil {
		return nil, err
	}

	signers, err := agentClient.Signers()
	if err != nil {
		return nil, fmt.Errorf("failed to list SSH agent signers: %w", err)
	}
	if len(signers) == 0 {
		return nil, fmt.Errorf("no SSH keys loaded in the SSH agent")
	}

	if state != nil && state.ClientKey != nil {
		for _, signer := range signers {
			if bytes.Equal(signer.PublicKey().Marshal(), state.ClientKey.Marshal()) {
				types.LogDebug("Using the same SSH key that authenticated to the proxy")
				return ssh.PublicKeys(signer), nil
			}
		}
		types.LogDebug("Authenticated key not found in agent, falling back to available SSH agent keys")
	}

	return ssh.PublicKeys(signers...), nil
}

func getHostKeyCallback(state *types.SessionState) (ssh.HostKeyCallback, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve home directory: %w", err)
	}

	knownHostsPath := filepath.Join(homeDir, ".ssh", "known_hosts")
	callback, err := knownhosts.New(knownHostsPath)
	if err == nil && !hostKeyVerificationDisabled(state) {
		return callback, nil
	}
	if hostKeyVerificationDisabled(state) {
		types.LogInfo("WARNING: insecure host key verification is enabled; target host keys will not be verified")
		return ssh.InsecureIgnoreHostKey(), nil
	}
	if err == nil {
		return callback, nil
	}

	return nil, fmt.Errorf("known_hosts is required at %s for target verification (set %s=1 or start the proxy with -insecure-ignore-hostkey only for temporary development use): %w", knownHostsPath, insecureIgnoreHostKeyEnv, err)
}

func hostKeyVerificationDisabled(state *types.SessionState) bool {
	return (state != nil && state.InsecureIgnoreHostKey) || envEnabled(insecureIgnoreHostKeyEnv)
}

// ProxyWithKeyForwarding handles the proxying with key information.
func ProxyWithKeyForwarding(clientChan ssh.Channel, state *types.SessionState) error {
	return fmt.Errorf("not yet implemented")
}

// BidiProxy creates a bidirectional proxy between client and server channels
// with recording support.
func BidiProxy(clientChan io.ReadWriteCloser, targetChan io.ReadWriteCloser, recorder recording.Recorder) error {
	var wg sync.WaitGroup
	var err1, err2 error

	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := targetChan.Read(buf)
			if n > 0 {
				if recorder != nil {
					recorder.Write(buf[:n])
				}
				if _, writeErr := clientChan.Write(buf[:n]); writeErr != nil {
					err1 = writeErr
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					err1 = err
				}
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := clientChan.Read(buf)
			if n > 0 {
				if recorder != nil {
					recorder.WriteInput(buf[:n])
				}
				if _, writeErr := targetChan.Write(buf[:n]); writeErr != nil {
					err2 = writeErr
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					err2 = err
				}
				return
			}
		}
	}()

	wg.Wait()
	if err1 != nil {
		return err1
	}
	return err2
}

// GetSSHAgentConn gets a connection to the forwarded SSH agent for the current client session.
func GetSSHAgentConn(state *types.SessionState) (agent.Agent, error) {
	if state != nil && state.IsAgentRequested() && state.ClientConn != nil {
		agentChannel, requests, err := state.ClientConn.OpenChannel("auth-agent@openssh.com", nil)
		if err == nil {
			go ssh.DiscardRequests(requests)
			return agent.NewClient(agentChannel), nil
		}
		return nil, fmt.Errorf("forwarded SSH agent could not be opened: %w", err)
	}

	return nil, fmt.Errorf("SSH agent forwarding is required; connect with ssh -A")
}

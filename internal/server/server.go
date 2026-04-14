package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	proxyclient "ssh-proxy-server/internal/client"
	appmetrics "ssh-proxy-server/internal/metrics"
	"ssh-proxy-server/internal/recording"
	"ssh-proxy-server/internal/sso"
	"ssh-proxy-server/internal/types"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

const (
	RoutingModeFailover          = "failover"
	RoutingModeRoundRobin        = "round_robin"
	DefaultConnectTimeoutSeconds = 10
)

var staticRouteCounter uint64
var runSSODeviceAuth = sso.AuthenticateDeviceFlow

type RoutingConfig struct {
	StaticEnabled  bool
	StaticTargets  []string
	Mode           string
	ConnectTimeout time.Duration
	Retries        int
}

type SSOConfig struct {
	Enabled            bool
	Provider           string
	BaseURL            string
	Realm              string
	ClientID           string
	ClientSecret       string
	Scope              string
	AuthTimeout        time.Duration
	PollInterval       time.Duration
	RequestTimeout     time.Duration
	EnforceUserMatch   bool
	InsecureSkipVerify bool
}

func NormalizeRoutingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", RoutingModeFailover:
		return RoutingModeFailover
	case RoutingModeRoundRobin, "roundrobin", "round-robin":
		return RoutingModeRoundRobin
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

func IsSupportedRoutingMode(mode string) bool {
	switch NormalizeRoutingMode(mode) {
	case RoutingModeFailover, RoutingModeRoundRobin:
		return true
	default:
		return false
	}
}

func ValidateTargetAddress(target string) error {
	_, _, _, err := splitTargetAddress(target)
	return err
}

func HandleConnection(conn net.Conn, hostKey ssh.Signer, recordingsDir, authorizedKeysPath string, autoAcceptClientKeys, allowDirectCommands, insecureIgnoreHostKey bool, recordingFormat string, routingConfig RoutingConfig, ssoConfig SSOConfig) {

	defer conn.Close()

	var clientKey ssh.PublicKey

	// Setup SSH server config
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if err := isAuthorizedClientKey(key, authorizedKeysPath, autoAcceptClientKeys); err != nil {
				types.LogInfo("Rejected public key for %s from %s: %v", conn.User(), conn.RemoteAddr(), err)
				return nil, err
			}

			// Store the key for forwarding to target host
			clientKey = key
			types.LogDebug("Accepted auth for %s with key %s (%s)", conn.User(), key.Type(), ssh.FingerprintSHA256(key))
			return &ssh.Permissions{}, nil
		},
		NoClientAuth: false,
	}
	config.AddHostKey(hostKey)

	// Perform SSH handshake
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		appmetrics.Default().RecordSSHHandshakeFailure()
		log.Printf("Failed to establish SSH connection: %v", err)
		return
	}
	defer sshConn.Close()
	appmetrics.Default().SSHConnectionOpened()
	defer appmetrics.Default().SSHConnectionClosed()

	types.LogInfo("New SSH connection from %s (%s)", sshConn.RemoteAddr(), sshConn.ClientVersion())

	// Create session state for this connection
	state := &types.SessionState{
		ClientUser:            sshConn.User(),
		ClientKey:             clientKey,
		ClientConn:            sshConn,
		AllowDirectCommands:   allowDirectCommands,
		InsecureIgnoreHostKey: insecureIgnoreHostKey,
		RecordingFormat:       recording.NormalizeFormat(recordingFormat),
		StaticRoutingEnabled:  routingConfig.StaticEnabled,
		StaticTargets:         append([]string(nil), routingConfig.StaticTargets...),
		StaticRoutingMode:     NormalizeRoutingMode(routingConfig.Mode),
		ConnectTimeout:        routingConfig.ConnectTimeout,
		ConnectRetries:        routingConfig.Retries,
		SSOEnabled:            ssoConfig.Enabled,
		SSOProvider:           sso.NormalizeProvider(ssoConfig.Provider),
		SSOBaseURL:            strings.TrimSpace(ssoConfig.BaseURL),
		SSORealm:              strings.TrimSpace(ssoConfig.Realm),
		SSOClientID:           strings.TrimSpace(ssoConfig.ClientID),
		SSOClientSecret:       strings.TrimSpace(ssoConfig.ClientSecret),
		SSOScope:              strings.TrimSpace(ssoConfig.Scope),
		SSOAuthTimeout:        ssoConfig.AuthTimeout,
		SSOPollInterval:       ssoConfig.PollInterval,
		SSORequestTimeout:     ssoConfig.RequestTimeout,
		SSOEnforceUserMatch:   ssoConfig.EnforceUserMatch,
		SSOInsecureSkipVerify: ssoConfig.InsecureSkipVerify,
		RecordingsDir:         recordingsDir,
		EnvVars:               make(map[string]string),
	}
	if state.ConnectTimeout <= 0 {
		state.ConnectTimeout = time.Duration(DefaultConnectTimeoutSeconds) * time.Second
	}
	if state.ConnectRetries < 0 {
		state.ConnectRetries = 0
	}
	if state.SSOAuthTimeout <= 0 {
		state.SSOAuthTimeout = time.Duration(sso.DefaultAuthTimeoutSeconds) * time.Second
	}
	if state.SSOPollInterval <= 0 {
		state.SSOPollInterval = time.Duration(sso.DefaultPollIntervalSeconds) * time.Second
	}
	if state.SSORequestTimeout <= 0 {
		state.SSORequestTimeout = time.Duration(sso.DefaultRequestTimeoutSeconds) * time.Second
	}
	if state.SSOBaseURL == "" {
		state.SSOBaseURL = sso.DefaultBaseURL
	}
	if state.SSORealm == "" {
		state.SSORealm = sso.DefaultRealm
	}
	if state.SSOClientID == "" {
		state.SSOClientID = sso.DefaultClientID
	}
	if state.SSOScope == "" {
		state.SSOScope = sso.DefaultScope
	}

	// Handle global requests (like env variables)
	go handleGlobalRequests(reqs, state)

	// Handle channels
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			log.Printf("Failed to accept channel: %v", err)
			continue
		}

		go handleChannel(channel, requests, state)
	}
}

func handleGlobalRequests(requests <-chan *ssh.Request, state *types.SessionState) {
	for req := range requests {
		switch req.Type {
		case "env":
			handleEnvRequest(req, state)
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

func handleChannel(channel ssh.Channel, requests <-chan *ssh.Request, state *types.SessionState) {
	defer channel.Close()

	var command string
	var cols, rows int
	var sessionDone chan struct{}
	sessionStarted := false

	for req := range requests {
		switch req.Type {
		case "env":
			handleEnvRequest(req, state)
			req.Reply(true, nil)

		case "auth-agent-req@openssh.com":
			state.SetAgentRequested(true)
			types.LogDebug("SSH agent forwarding requested by client")
			req.Reply(true, nil)

		case "shell":
			if sessionStarted {
				req.Reply(false, nil)
				continue
			}
			sessionStarted = true
			sessionDone = make(chan struct{})
			req.Reply(true, nil)
			go func() {
				defer close(sessionDone)
				handleShellProxy(channel, state)
			}()

		case "exec":
			if sessionStarted {
				req.Reply(false, nil)
				continue
			}
			if len(req.Payload) > 4 {
				cmdLen := binary.BigEndian.Uint32(req.Payload[:4])
				if len(req.Payload) >= 4+int(cmdLen) {
					command = string(req.Payload[4 : 4+cmdLen])
				}
			}
			sessionStarted = true
			sessionDone = make(chan struct{})
			req.Reply(true, nil)
			go func(cmd string) {
				defer close(sessionDone)
				handleExecProxy(channel, state, cmd)
			}(command)

		case "pty-req":
			term, parsedCols, parsedRows, err := parsePtyRequest(req.Payload)
			if err != nil {
				req.Reply(false, nil)
				continue
			}
			state.SetPTY(term, parsedCols, parsedRows)
			cols, rows = parsedCols, parsedRows
			types.LogDebug("PTY request: term=%s cols=%d rows=%d", term, parsedCols, parsedRows)
			req.Reply(true, nil)

		case "window-change":
			req.Reply(true, nil)
			parseWindowChange(req.Payload, &cols, &rows)
			state.SetWindowSize(cols, rows)
			types.LogDebug("Window change: cols=%d rows=%d", cols, rows)
			if targetSession := state.TargetSessionValue(); targetSession != nil {
				targetUser, targetHost, targetPort := state.Target()
				if err := targetSession.WindowChange(rows, cols); err != nil {
					types.LogDebug("Failed to forward window change to target %s: %v", formatTargetAddress(targetUser, targetHost, targetPort), err)
				} else {
					types.LogDebug("Forwarded window change to target %s: cols=%d rows=%d", formatTargetAddress(targetUser, targetHost, targetPort), cols, rows)
				}
			}

		case "subsystem":
			req.Reply(false, nil)

		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}

	if sessionDone != nil {
		<-sessionDone
	}
}

func handleShellProxy(channel ssh.Channel, state *types.SessionState) {
	if err := ensureSSOAuthentication(channel, state); err != nil {
		appmetrics.Default().RecordProxySession("failure")
		types.LogInfo("Session rejected during SSO confirmation: client=%s err=%v", state.ClientUser, err)
		fmt.Fprintf(channel, "Error: %v\n", err)
		sendExitStatus(channel, 1)
		closeClientSession(channel, state)
		return
	}
	if err := proxySession(channel, state, "", ""); err != nil {
		appmetrics.Default().RecordProxySession("failure")
		types.LogInfo("Shell proxy failed: client=%s err=%v", state.ClientUser, err)
		fmt.Fprintf(channel, "Error: %v\n", err)
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{1}))
		closeClientSession(channel, state)
		return
	}
	appmetrics.Default().RecordProxySession("success")
}

func handleExecProxy(channel ssh.Channel, state *types.SessionState, command string) {
	if !state.AllowDirectCommands {
		appmetrics.Default().RecordProxySession("rejected")
		types.LogInfo("Rejected direct command for client=%s: direct command execution is disabled", state.ClientUser)
		fmt.Fprintf(channel, "Error: direct commands are disabled; set allow_direct_commands=true in the proxy config to enable them\n")
		sendExitStatus(channel, 1)
		closeClientSession(channel, state)
		return
	}

	if err := ensureSSOAuthentication(channel, state); err != nil {
		appmetrics.Default().RecordProxySession("failure")
		types.LogInfo("Direct command rejected during SSO confirmation: client=%s err=%v", state.ClientUser, err)
		fmt.Fprintf(channel, "Error: %v\n", err)
		sendExitStatus(channel, 1)
		closeClientSession(channel, state)
		return
	}

	if err := proxySession(channel, state, "", command); err != nil {
		appmetrics.Default().RecordProxySession("failure")
		types.LogInfo("Direct command proxy failed: client=%s err=%v", state.ClientUser, err)
		fmt.Fprintf(channel, "Error: %v\n", err)
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{1}))
		closeClientSession(channel, state)
		return
	}
	appmetrics.Default().RecordProxySession("success")
}

func ensureSSOAuthentication(channel ssh.Channel, state *types.SessionState) error {
	if state == nil || !state.SSOEnabled || state.IsSSOVerified() {
		return nil
	}

	clientUser := "unknown"
	if strings.TrimSpace(state.ClientUser) != "" {
		clientUser = state.ClientUser
	}
	types.LogInfo("Starting SSO confirmation: client=%s provider=%s realm=%s timeout=%s poll_interval=%s connect_timeout=%s", clientUser, state.SSOProvider, state.SSORealm, state.SSOAuthTimeout.Round(time.Second), state.SSOPollInterval.Round(time.Second), state.SSORequestTimeout.Round(time.Second))

	metrics := appmetrics.Default()
	metrics.SSOWaitingStarted()
	defer metrics.SSOWaitingFinished()

	cfg := sso.Config{
		Enabled:            state.SSOEnabled,
		Provider:           state.SSOProvider,
		BaseURL:            state.SSOBaseURL,
		Realm:              state.SSORealm,
		ClientID:           state.SSOClientID,
		ClientSecret:       state.SSOClientSecret,
		Scope:              state.SSOScope,
		AuthTimeout:        state.SSOAuthTimeout,
		PollInterval:       state.SSOPollInterval,
		RequestTimeout:     state.SSORequestTimeout,
		InsecureSkipVerify: state.SSOInsecureSkipVerify,
	}
	identity, err := runSSODeviceAuth(context.Background(), cfg, channel)
	if err != nil {
		metrics.RecordSSOConfirmation("failure")
		metrics.RecordSSOError()
		types.LogInfo("SSO confirmation failed: client=%s provider=%s realm=%s err=%v", clientUser, cfg.Provider, cfg.Realm, err)
		return err
	}
	if state.SSOEnforceUserMatch {
		if !identity.MatchesSSHUser(clientUser) {
			metrics.RecordSSOConfirmation("failure")
			metrics.RecordSSOError()
			identifier := identity.BestIdentifier()
			if identifier == "" {
				return fmt.Errorf("SSO confirmation succeeded but did not provide an identity that can be matched to SSH user %q", clientUser)
			}
			return fmt.Errorf("SSO identity %q does not match SSH user %q", identifier, clientUser)
		}
		types.LogInfo("SSO identity matched SSH user: client=%s identity=%s", clientUser, identity.BestIdentifier())
	}
	state.SetSSOVerified(true)
	metrics.RecordSSOConfirmation("success")
	types.LogInfo("SSO confirmation successful: client=%s provider=%s realm=%s", clientUser, cfg.Provider, cfg.Realm)
	return nil
}

func handleEnvRequest(req *ssh.Request, state *types.SessionState) {
	// Parse environment variable from request payload
	// Format: uint32 name_len | name | uint32 value_len | value
	payload := req.Payload

	if len(payload) < 8 {
		req.Reply(false, nil)
		return
	}

	nameLen := binary.BigEndian.Uint32(payload[0:4])
	if int(nameLen) > len(payload)-8 {
		req.Reply(false, nil)
		return
	}
	varName := string(payload[4 : 4+nameLen])

	valueLen := binary.BigEndian.Uint32(payload[4+nameLen : 8+nameLen])
	if int(valueLen) > len(payload)-int(8+nameLen) {
		req.Reply(false, nil)
		return
	}
	varValue := string(payload[8+nameLen : 8+nameLen+valueLen])

	if varName == "LC_SSH_SERVER" {
		if state != nil && state.StaticRoutingEnabled {
			types.LogDebug("Ignoring LC_SSH_SERVER=%q because static routing is enabled", varValue)
			req.Reply(true, nil)
			return
		}

		sessionUser := ""
		if state != nil {
			sessionUser = state.ClientUser
		}
		targetUser, targetHost, targetPort, err := resolveTargetAddress(varValue, sessionUser)
		if err != nil {
			clientUser := "unknown"
			if state != nil && state.ClientUser != "" {
				clientUser = state.ClientUser
			}
			types.LogInfo("Rejected suspicious LC_SSH_SERVER value for client=%s: %q (%v)", clientUser, varValue, err)
			req.Reply(false, nil)
			return
		}

		state.SetEnvVar(varName, varValue)
		types.LogDebug("Received environment variable: %s=%s", varName, varValue)
		state.SetTarget(targetUser, targetHost, targetPort)
		types.LogInfo("Target parsed from LC_SSH_SERVER: user=%s host=%s port=%s", targetUser, targetHost, targetPort)
		req.Reply(true, nil)
		return
	}

	state.SetEnvVar(varName, varValue)
	types.LogDebug("Received environment variable: %s=%s", varName, varValue)
	req.Reply(true, nil)
}

func proxySession(channel ssh.Channel, state *types.SessionState, targetAddr, command string) error {
	targets, err := resolveTargetCandidates(state, targetAddr, command)
	if err != nil {
		return err
	}

	targetClient, targetUser, targetHost, targetPort, err := connectToFirstAvailableTarget(state, targets)
	if err != nil {
		return err
	}
	defer targetClient.Close()
	state.SetTargetClient(targetClient)
	state.SetTarget(targetUser, targetHost, targetPort)
	defer func() {
		state.SetTargetClient(nil)
	}()

	targetSession, err := targetClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create target session: %w", err)
	}
	state.SetTargetSession(targetSession)
	defer func() {
		state.SetTargetSession(nil)
		targetSession.Close()
	}()

	recordingID := generateRecordingID()
	recordingFormat := recording.NormalizeFormat(state.RecordingFormat)
	recordingFileName := buildRecordingFileName(targetUser, targetHost, targetPort, recordingID, recordingFormat)
	recordingsDir := state.RecordingsDir
	if strings.TrimSpace(recordingsDir) == "" {
		recordingsDir = "recordings"
	}
	recordingPath := filepath.Join(recordingsDir, recordingFileName)
	if err := os.MkdirAll(recordingsDir, 0o700); err != nil {
		return fmt.Errorf("failed to create recordings directory: %w", err)
	}
	if err := os.Chmod(recordingsDir, 0o700); err != nil {
		return fmt.Errorf("failed to secure recordings directory permissions: %w", err)
	}

	recorder, err := recording.NewRecorder(recordingFormat, recordingPath)
	if err != nil {
		return fmt.Errorf("failed to create %s recorder: %w", recordingFormat, err)
	}
	state.SetRecorder(recorder)
	defer func() {
		state.SetRecorder(nil)
		_ = recorder.Close()
	}()

	stdinPipe, err := targetSession.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to open target stdin pipe: %w", err)
	}
	stdoutPipe, err := targetSession.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to open target stdout pipe: %w", err)
	}
	stderrPipe, err := targetSession.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to open target stderr pipe: %w", err)
	}

	applySessionEnv(targetSession, state.EnvVarsSnapshot())

	term, cols, rows := state.PTY()
	if term != "" {
		if cols == 0 {
			cols = 80
		}
		if rows == 0 {
			rows = 24
		}
		if err := targetSession.RequestPty(term, rows, cols, ssh.TerminalModes{
			ssh.ECHO:          1,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}); err != nil {
			return fmt.Errorf("failed to request PTY on target: %w", err)
		}
	}

	go copySessionInput(channel, stdinPipe, state)
	go copySessionOutput(channel, stdoutPipe, state, "stdout")
	go copySessionOutput(channel.Stderr(), stderrPipe, state, "stderr")

	types.LogInfo("Proxy session started: client=%s target=%s", state.ClientUser, formatTargetAddress(targetUser, targetHost, targetPort))

	if command == "" {
		if err := targetSession.Shell(); err != nil {
			return fmt.Errorf("failed to start remote shell: %w", err)
		}
		return finalizeRemoteSession(channel, state, targetSession.Wait())
	}

	if err := targetSession.Start(command); err != nil {
		return fmt.Errorf("failed to start remote command: %w", err)
	}
	return finalizeRemoteSession(channel, state, targetSession.Wait())
}

func resolveTargetCandidates(state *types.SessionState, targetAddr, command string) ([]string, error) {
	if state != nil && state.StaticRoutingEnabled {
		if len(state.StaticTargets) == 0 {
			return nil, fmt.Errorf("static routing is enabled but no servers are configured")
		}
		return orderedStaticTargets(state.StaticTargets, state.StaticRoutingMode), nil
	}

	resolved := strings.TrimSpace(targetAddr)
	if resolved == "" && state != nil {
		resolved = strings.TrimSpace(state.GetEnvVar("LC_SSH_SERVER"))
	}
	if resolved == "" && strings.TrimSpace(command) != "" {
		resolved = parseTargetFromCommand(command)
	}
	if resolved == "" {
		if strings.TrimSpace(command) != "" {
			return nil, fmt.Errorf("no target host specified")
		}
		return nil, fmt.Errorf("LC_SSH_SERVER not provided")
	}
	if err := ValidateTargetAddress(resolved); err != nil {
		return nil, err
	}
	return []string{resolved}, nil
}

func orderedStaticTargets(targets []string, mode string) []string {
	ordered := append([]string(nil), targets...)
	if len(ordered) <= 1 {
		return ordered
	}
	if NormalizeRoutingMode(mode) != RoutingModeRoundRobin {
		return ordered
	}
	start := int(atomic.AddUint64(&staticRouteCounter, 1)-1) % len(ordered)
	return append(append([]string(nil), ordered[start:]...), ordered[:start]...)
}

func connectToFirstAvailableTarget(state *types.SessionState, targets []string) (*ssh.Client, string, string, string, error) {
	if len(targets) == 0 {
		return nil, "", "", "", fmt.Errorf("no targets configured")
	}

	rounds := 1
	if state != nil && state.ConnectRetries > 0 {
		rounds += state.ConnectRetries
	}
	totalAttempts := rounds * len(targets)
	timeout := time.Duration(DefaultConnectTimeoutSeconds) * time.Second
	if state != nil && state.ConnectTimeout > 0 {
		timeout = state.ConnectTimeout
	}

	failures := make([]string, 0, totalAttempts)
	attempt := 0
	for round := 0; round < rounds; round++ {
		for _, target := range targets {
			attempt++
			sessionUser := ""
			if state != nil {
				sessionUser = state.ClientUser
			}
			targetUser, targetHost, targetPort, err := resolveTargetAddress(target, sessionUser)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", target, err))
				continue
			}
			if state != nil {
				state.SetTarget(targetUser, targetHost, targetPort)
			}

			formattedTarget := formatTargetAddress(targetUser, targetHost, targetPort)
			types.LogInfo("Connecting to target %s (attempt %d/%d, timeout=%s)", formattedTarget, attempt, totalAttempts, timeout)
			client, err := proxyclient.ConnectToTarget(state, targetUser, targetHost, targetPort)
			if err == nil {
				return client, targetUser, targetHost, targetPort, nil
			}

			types.LogInfo("Target connection failed: %s (attempt %d/%d): %v", formattedTarget, attempt, totalAttempts, err)
			failures = append(failures, fmt.Sprintf("%s: %v", formattedTarget, err))
		}
	}

	return nil, "", "", "", fmt.Errorf("failed to connect to any target after %d attempt(s): %s", totalAttempts, strings.Join(failures, "; "))
}

func splitTargetAddress(targetAddr string) (string, string, string, error) {
	if strings.TrimSpace(targetAddr) == "" {
		return "", "", "", fmt.Errorf("invalid target format (expected host[:port] or user@host[:port])")
	}
	if strings.TrimSpace(targetAddr) != targetAddr {
		return "", "", "", fmt.Errorf("target contains leading or trailing whitespace")
	}
	if strings.ContainsAny(targetAddr, " \t\r\n\"'`;&|<>\\(){}") {
		return "", "", "", fmt.Errorf("target contains potentially unsafe characters")
	}

	targetUser := ""
	hostPort := targetAddr
	if parsedUser, parsedHostPort, ok := strings.Cut(targetAddr, "@"); ok {
		if parsedUser == "" || parsedHostPort == "" {
			return "", "", "", fmt.Errorf("invalid target format (expected host[:port] or user@host[:port])")
		}
		if !isSafeTargetUser(parsedUser) {
			return "", "", "", fmt.Errorf("invalid target user")
		}
		targetUser = parsedUser
		hostPort = parsedHostPort
	}

	host := hostPort
	port := "22"
	if strings.HasPrefix(hostPort, "[") {
		if parsedHost, parsedPort, err := net.SplitHostPort(hostPort); err == nil {
			host = parsedHost
			port = parsedPort
		} else {
			host = strings.TrimSuffix(strings.TrimPrefix(hostPort, "["), "]")
		}
	} else if strings.Count(hostPort, ":") == 1 {
		if parsedHost, parsedPort, err := net.SplitHostPort(hostPort); err == nil {
			host = parsedHost
			port = parsedPort
		} else if parsedHost, parsedPort, ok := strings.Cut(hostPort, ":"); ok {
			host = parsedHost
			port = parsedPort
		}
	}

	if host == "" {
		return "", "", "", fmt.Errorf("invalid target format (expected host[:port] or user@host[:port])")
	}
	if !isSafeTargetHost(host) {
		return "", "", "", fmt.Errorf("invalid target host")
	}
	if !isSafeTargetPort(port) {
		return "", "", "", fmt.Errorf("invalid target port")
	}

	return targetUser, host, port, nil
}

func resolveTargetAddress(targetAddr, sessionUser string) (string, string, string, error) {
	targetUser, host, port, err := splitTargetAddress(targetAddr)
	if err != nil {
		return "", "", "", err
	}
	if targetUser == "" {
		sessionUser = strings.TrimSpace(sessionUser)
		if !isSafeTargetUser(sessionUser) {
			return "", "", "", fmt.Errorf("invalid SSH session user")
		}
		targetUser = sessionUser
	}
	return targetUser, host, port, nil
}

func isSafeTargetUser(user string) bool {
	if user == "" {
		return false
	}
	for _, r := range user {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-+", r) {
			continue
		}
		return false
	}
	return true
}

func isSafeTargetHost(host string) bool {
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return true
	}
	for _, r := range host {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-", r) {
			continue
		}
		return false
	}
	return true
}

func isSafeTargetPort(port string) bool {
	if port == "" {
		return false
	}
	for _, r := range port {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	value, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	return value >= 1 && value <= 65535
}

func formatTargetAddress(user, host, port string) string {
	if user == "" || host == "" {
		return ""
	}
	if port == "" {
		return user + "@" + host
	}
	return user + "@" + net.JoinHostPort(host, port)
}

func applySessionEnv(session *ssh.Session, envVars map[string]string) {
	for name, value := range envVars {
		if name == "LC_SSH_SERVER" {
			continue
		}
		if err := session.Setenv(name, value); err != nil {
			types.LogDebug("Target rejected env %s: %v", name, err)
		}
	}
}

func parsePtyRequest(payload []byte) (string, int, int, error) {
	if len(payload) < 4 {
		return "", 0, 0, fmt.Errorf("invalid PTY payload")
	}

	termLen := int(binary.BigEndian.Uint32(payload[:4]))
	if len(payload) < 4+termLen+8 {
		return "", 0, 0, fmt.Errorf("invalid PTY payload")
	}

	term := string(payload[4 : 4+termLen])
	cols := int(binary.BigEndian.Uint32(payload[4+termLen : 8+termLen]))
	rows := int(binary.BigEndian.Uint32(payload[8+termLen : 12+termLen]))
	return term, cols, rows, nil
}

func finalizeRemoteSession(channel ssh.Channel, state *types.SessionState, err error) error {
	targetUser, targetHost, targetPort := state.Target()
	target := formatTargetAddress(targetUser, targetHost, targetPort)

	if err == nil {
		types.LogInfo("Session ended: client=%s target=%s exit_status=0", state.ClientUser, target)
		sendExitStatus(channel, 0)
		closeClientSession(channel, state)
		return nil
	}

	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		status := uint32(exitErr.ExitStatus())
		types.LogInfo("Session ended: client=%s target=%s exit_status=%d", state.ClientUser, target, status)
		sendExitStatus(channel, status)
		closeClientSession(channel, state)
		return nil
	}

	types.LogInfo("Session ended with error: client=%s target=%s err=%v", state.ClientUser, target, err)
	sendExitStatus(channel, 1)
	closeClientSession(channel, state)
	return err
}

func sendExitStatus(channel ssh.Channel, code uint32) {
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{code}))
}

func closeClientSession(channel ssh.Channel, state *types.SessionState) {
	_ = channel.CloseWrite()
	_ = channel.Close()
	if state != nil && state.ClientConn != nil {
		_ = state.ClientConn.Close()
	}
}

func copySessionInput(src io.Reader, dst io.WriteCloser, state *types.SessionState) {
	defer dst.Close()

	recorder := state.RecorderValue()
	_, err := io.Copy(io.MultiWriter(dst, inputRecorderWriter{recorder: recorder}), src)
	targetUser, targetHost, targetPort := state.Target()
	target := formatTargetAddress(targetUser, targetHost, targetPort)

	if err == nil || errors.Is(err, io.EOF) {
		types.LogInfo("User input closed: client=%s target=%s", state.ClientUser, target)
		return
	}

	types.LogDebug("Input stream ended with error for target %s: %v", target, err)
}

func copySessionOutput(dst io.Writer, src io.Reader, state *types.SessionState, streamName string) {
	recorder := state.RecorderValue()
	_, err := io.Copy(io.MultiWriter(dst, outputRecorderWriter{recorder: recorder}), src)
	if err != nil && !errors.Is(err, io.EOF) {
		targetUser, targetHost, targetPort := state.Target()
		types.LogDebug("Output stream %s ended with error for target %s: %v", streamName, formatTargetAddress(targetUser, targetHost, targetPort), err)
	}
}

type outputRecorderWriter struct {
	recorder recording.Recorder
}

func (w outputRecorderWriter) Write(p []byte) (int, error) {
	if w.recorder != nil {
		if err := w.recorder.Write(p); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

type inputRecorderWriter struct {
	recorder recording.Recorder
}

func (w inputRecorderWriter) Write(p []byte) (int, error) {
	if w.recorder != nil {
		if err := w.recorder.WriteInput(p); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func parseTargetFromCommand(command string) string {
	// Parse commands like: ssh -l user target-host, ssh user@target-host, or ssh target-host
	parts := strings.Fields(command)
	if len(parts) == 0 || parts[0] != "ssh" {
		return ""
	}
	for i, part := range parts {
		if (part == "-l" || part == "-u") && i+1 < len(parts) {
			if i+2 < len(parts) {
				return parts[i+1] + "@" + parts[i+2]
			}
			continue
		}
		if strings.HasPrefix(part, "-") || part == "ssh" {
			continue
		}
		return part
	}
	return ""
}

func parseWindowChange(payload []byte, cols, rows *int) {
	if len(payload) >= 8 {
		*cols = int(binary.BigEndian.Uint32(payload[:4]))
		*rows = int(binary.BigEndian.Uint32(payload[4:8]))
	}
}

func buildRecordingFileName(targetUser, targetHost, targetPort, recordingID, recordingFormat string) string {
	parts := make([]string, 0, 4)

	if user := sanitizeFileComponent(targetUser); user != "" {
		parts = append(parts, user)
	}
	if host := sanitizeFileComponent(targetHost); host != "" {
		parts = append(parts, host)
	}
	if port := sanitizeFileComponent(targetPort); port != "" {
		parts = append(parts, port)
	}

	parts = append(parts, recordingID)
	return strings.Join(parts, "_") + recording.FileExtension(recordingFormat)
}

func sanitizeFileComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	var b strings.Builder
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "._")
}

func generateRecordingID() string {
	return uuid.New().String()
}

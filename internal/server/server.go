package server

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	proxyclient "ssh-proxy-server/internal/client"
	"ssh-proxy-server/internal/recording"
	"ssh-proxy-server/internal/types"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

func HandleConnection(conn net.Conn, hostKey ssh.Signer, recordingsDir string) {
	defer conn.Close()

	var clientKey ssh.PublicKey

	// Setup SSH server config
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			// Store the key for forwarding to target host
			clientKey = key
			types.LogDebug("Auth attempt from %s with key %s", conn.User(), key.Type())
			return &ssh.Permissions{}, nil
		},
		NoClientAuth: false,
	}
	config.AddHostKey(hostKey)

	// Perform SSH handshake
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		log.Printf("Failed to establish SSH connection: %v", err)
		return
	}
	defer sshConn.Close()

	types.LogInfo("New SSH connection from %s (%s)", sshConn.RemoteAddr(), sshConn.ClientVersion())

	// Create session state for this connection
	state := &types.SessionState{
		ClientUser:    sshConn.User(),
		ClientKey:     clientKey,
		ClientConn:    sshConn,
		RecordingsDir: recordingsDir,
		EnvVars:       make(map[string]string),
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
			state.AgentRequested = true
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
			state.PtyTerm = term
			state.PtyCols = parsedCols
			state.PtyRows = parsedRows
			cols, rows = parsedCols, parsedRows
			types.LogDebug("PTY request: term=%s cols=%d rows=%d", term, parsedCols, parsedRows)
			req.Reply(true, nil)

		case "window-change":
			req.Reply(true, nil)
			parseWindowChange(req.Payload, &cols, &rows)
			state.PtyCols = cols
			state.PtyRows = rows
			types.LogDebug("Window change: cols=%d rows=%d", cols, rows)
			if state.TargetSession != nil {
				if err := state.TargetSession.WindowChange(rows, cols); err != nil {
					types.LogDebug("Failed to forward window change to target %s: %v", formatTargetAddress(state.TargetUser, state.TargetHost, state.TargetPort), err)
				} else {
					types.LogDebug("Forwarded window change to target %s: cols=%d rows=%d", formatTargetAddress(state.TargetUser, state.TargetHost, state.TargetPort), cols, rows)
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
	targetAddr := state.EnvVars["LC_SSH_SERVER"]
	if targetAddr == "" {
		targetAddr = formatTargetAddress(state.TargetUser, state.TargetHost, state.TargetPort)
	}

	if targetAddr == "" {
		fmt.Fprintf(channel, "Error: LC_SSH_SERVER not provided\n")
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{1}))
		closeClientSession(channel, state)
		return
	}

	if err := proxySession(channel, state, targetAddr, ""); err != nil {
		fmt.Fprintf(channel, "Error: %v\n", err)
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{1}))
		closeClientSession(channel, state)
	}
}

func handleExecProxy(channel ssh.Channel, state *types.SessionState, command string) {
	targetAddr := state.EnvVars["LC_SSH_SERVER"]
	if targetAddr == "" {
		targetAddr = formatTargetAddress(state.TargetUser, state.TargetHost, state.TargetPort)
	}
	if targetAddr == "" {
		targetAddr = parseTargetFromCommand(command)
	}

	if targetAddr == "" {
		fmt.Fprintf(channel, "Error: No target host specified\n")
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{1}))
		closeClientSession(channel, state)
		return
	}

	if err := proxySession(channel, state, targetAddr, command); err != nil {
		fmt.Fprintf(channel, "Error: %v\n", err)
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{1}))
		closeClientSession(channel, state)
	}
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
	state.EnvVars[varName] = varValue
	
	types.LogDebug("Received environment variable: %s=%s", varName, varValue)
	
	if varName == "LC_SSH_SERVER" {
		targetUser, targetHost, targetPort, err := splitTargetAddress(varValue)
		if err != nil {
			types.LogInfo("Invalid LC_SSH_SERVER value: %s", varValue)
		} else {
			state.TargetUser = targetUser
			state.TargetHost = targetHost
			state.TargetPort = targetPort
			types.LogInfo("Target parsed from LC_SSH_SERVER: user=%s host=%s port=%s", targetUser, targetHost, targetPort)
		}
	}
	
	req.Reply(true, nil)
}

func proxySession(channel ssh.Channel, state *types.SessionState, targetAddr, command string) error {
	targetUser, targetHost, targetPort, err := splitTargetAddress(targetAddr)
	if err != nil {
		return err
	}
	state.TargetUser = targetUser
	state.TargetHost = targetHost
	state.TargetPort = targetPort

	recordingID := generateRecordingID()
	recordingFileName := buildRecordingFileName(state.TargetUser, state.TargetHost, state.TargetPort, recordingID)
	recordingsDir := state.RecordingsDir
	if strings.TrimSpace(recordingsDir) == "" {
		recordingsDir = "recordings"
	}
	recordingPath := filepath.Join(recordingsDir, recordingFileName)
	os.MkdirAll(recordingsDir, 0755)

	state.Recorder = recording.NewAsciinemaRecorder(recordingPath)
	defer state.Recorder.Close()

	types.LogInfo("Connecting to target %s", formatTargetAddress(targetUser, targetHost, targetPort))

	targetClient, err := proxyclient.ConnectToTarget(state, targetUser, targetHost, targetPort)
	if err != nil {
		return err
	}
	defer targetClient.Close()
	state.TargetClient = targetClient

	targetSession, err := targetClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create target session: %w", err)
	}
	state.TargetSession = targetSession
	defer func() {
		state.TargetSession = nil
		targetSession.Close()
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

	applySessionEnv(targetSession, state.EnvVars)

	if state.PtyTerm != "" {
		cols := state.PtyCols
		rows := state.PtyRows
		if cols == 0 {
			cols = 80
		}
		if rows == 0 {
			rows = 24
		}
		if err := targetSession.RequestPty(state.PtyTerm, rows, cols, ssh.TerminalModes{
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

func splitTargetAddress(targetAddr string) (string, string, string, error) {
	targetUser, hostPort, ok := strings.Cut(targetAddr, "@")
	if !ok || targetUser == "" || hostPort == "" {
		return "", "", "", fmt.Errorf("invalid target format (expected user@host:port)")
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
		return "", "", "", fmt.Errorf("invalid target format (expected user@host:port)")
	}

	return targetUser, host, port, nil
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
	target := formatTargetAddress(state.TargetUser, state.TargetHost, state.TargetPort)

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

	_, err := io.Copy(io.MultiWriter(dst, inputRecorderWriter{recorder: state.Recorder}), src)
	target := formatTargetAddress(state.TargetUser, state.TargetHost, state.TargetPort)

	if err == nil || errors.Is(err, io.EOF) {
		types.LogInfo("User input closed: client=%s target=%s", state.ClientUser, target)
		return
	}

	types.LogDebug("Input stream ended with error for target %s: %v", target, err)
}

func copySessionOutput(dst io.Writer, src io.Reader, state *types.SessionState, streamName string) {
	_, err := io.Copy(io.MultiWriter(dst, outputRecorderWriter{recorder: state.Recorder}), src)
	if err != nil && !errors.Is(err, io.EOF) {
		types.LogDebug("Output stream %s ended with error for target %s: %v", streamName, formatTargetAddress(state.TargetUser, state.TargetHost, state.TargetPort), err)
	}
}

type outputRecorderWriter struct {
	recorder *recording.AsciinemaRecorder
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
	recorder *recording.AsciinemaRecorder
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
	// Parse command like: ssh -l user target-host
	// or: ssh user@target-host
	// For simplicity, look for patterns
	parts := strings.Fields(command)
	for i, part := range parts {
		if (part == "-l" || part == "-u") && i+1 < len(parts) {
			// Next part is username
			if i+2 < len(parts) {
				return parts[i+1] + "@" + parts[i+2]
			}
		} else if strings.Contains(part, "@") && !strings.HasPrefix(part, "-") {
			return part
		}
	}
	return ""
}

func parseWindowChange(payload []byte, cols, rows *int) {
	if len(payload) >= 8 {
		*cols = int(binary.BigEndian.Uint32(payload[:4]))
		*rows = int(binary.BigEndian.Uint32(payload[4:8]))
	}
}

func buildRecordingFileName(targetUser, targetHost, targetPort, recordingID string) string {
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
	return strings.Join(parts, "_") + ".cast"
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

package types

import (
	"log"
	"sync"
	"time"

	"ssh-proxy-server/internal/recording"

	"golang.org/x/crypto/ssh"
)

var currentLogLevel int

const (
	LogLevelError = iota
	LogLevelInfo
	LogLevelDebug
)

func SetLogLevel(level string) {
	switch level {
	case "error":
		currentLogLevel = LogLevelError
	case "info":
		currentLogLevel = LogLevelInfo
	case "debug":
		currentLogLevel = LogLevelDebug
	default:
		log.Fatalf("Invalid log level: %s. Use error, info, or debug", level)
	}
}

func LogInfo(format string, args ...interface{}) {
	if currentLogLevel >= LogLevelInfo {
		log.Printf(format, args...)
	}
}

func LogDebug(format string, args ...interface{}) {
	if currentLogLevel >= LogLevelDebug {
		log.Printf(format, args...)
	}
}

// SessionState holds the state of a proxied SSH session
type SessionState struct {
	mu                    sync.RWMutex
	ClientUser            string
	ClientKey             ssh.PublicKey
	ClientConn            ssh.Conn
	AgentRequested        bool
	AllowDirectCommands   bool
	InsecureIgnoreHostKey bool
	RecordingFormat       string
	StaticRoutingEnabled  bool
	StaticTargets         []string
	StaticRoutingMode     string
	ConnectTimeout        time.Duration
	ConnectRetries        int
	SSOEnabled            bool
	SSOProvider           string
	SSOBaseURL            string
	SSORealm              string
	SSOClientID           string
	SSOClientSecret       string
	SSOScope              string
	SSOAuthTimeout        time.Duration
	SSOPollInterval       time.Duration
	SSORequestTimeout     time.Duration
	SSOEnforceUserMatch   bool
	SSOVerified           bool
	TargetHost            string
	TargetPort            string
	TargetUser            string
	TargetConn            ssh.Conn
	TargetClient          *ssh.Client
	TargetSession         *ssh.Session
	Recorder              recording.Recorder
	RecordingsDir         string
	EnvVars               map[string]string // Environment variables sent by client
	PtyTerm               string
	PtyCols               int
	PtyRows               int
}

func (s *SessionState) SetAgentRequested(value bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.AgentRequested = value
	s.mu.Unlock()
}

func (s *SessionState) IsAgentRequested() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AgentRequested
}

func (s *SessionState) SetSSOVerified(value bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.SSOVerified = value
	s.mu.Unlock()
}

func (s *SessionState) IsSSOVerified() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SSOVerified
}

func (s *SessionState) SetEnvVar(name, value string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.EnvVars == nil {
		s.EnvVars = make(map[string]string)
	}
	s.EnvVars[name] = value
	s.mu.Unlock()
}

func (s *SessionState) GetEnvVar(name string) string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.EnvVars[name]
}

func (s *SessionState) EnvVarsSnapshot() map[string]string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string, len(s.EnvVars))
	for name, value := range s.EnvVars {
		result[name] = value
	}
	return result
}

func (s *SessionState) SetTarget(user, host, port string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.TargetUser = user
	s.TargetHost = host
	s.TargetPort = port
	s.mu.Unlock()
}

func (s *SessionState) Target() (string, string, string) {
	if s == nil {
		return "", "", ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.TargetUser, s.TargetHost, s.TargetPort
}

func (s *SessionState) SetTargetClient(client *ssh.Client) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.TargetClient = client
	s.mu.Unlock()
}

func (s *SessionState) TargetClientValue() *ssh.Client {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.TargetClient
}

func (s *SessionState) SetTargetSession(session *ssh.Session) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.TargetSession = session
	s.mu.Unlock()
}

func (s *SessionState) TargetSessionValue() *ssh.Session {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.TargetSession
}

func (s *SessionState) SetRecorder(recorder recording.Recorder) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.Recorder = recorder
	s.mu.Unlock()
}

func (s *SessionState) RecorderValue() recording.Recorder {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Recorder
}

func (s *SessionState) SetPTY(term string, cols, rows int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.PtyTerm = term
	s.PtyCols = cols
	s.PtyRows = rows
	s.mu.Unlock()
}

func (s *SessionState) SetWindowSize(cols, rows int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.PtyCols = cols
	s.PtyRows = rows
	s.mu.Unlock()
}

func (s *SessionState) PTY() (string, int, int) {
	if s == nil {
		return "", 0, 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PtyTerm, s.PtyCols, s.PtyRows
}

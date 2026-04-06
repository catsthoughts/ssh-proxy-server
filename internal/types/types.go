package types

import (
	"log"
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

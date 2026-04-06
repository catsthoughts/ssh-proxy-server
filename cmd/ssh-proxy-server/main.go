package main

import (
	"flag"
	"log"
	"net"
	"os"

	"ssh-proxy-server/internal/hostkey"
	"ssh-proxy-server/internal/recording"
	"ssh-proxy-server/internal/server"
	"ssh-proxy-server/internal/types"
)

func main() {
	defaultAuthorizedKeysPath := server.DefaultAuthorizedKeysPath()

	defaultAutoAcceptClientKeys := true
	if value := os.Getenv(server.AutoAcceptClientKeysEnv); value != "" {
		defaultAutoAcceptClientKeys = value == "1" || value == "true" || value == "TRUE" || value == "yes" || value == "YES"
	}

	defaultInsecureIgnoreHostKey := false
	if value := os.Getenv("SSH_PROXY_INSECURE_IGNORE_HOSTKEY"); value != "" {
		defaultInsecureIgnoreHostKey = value == "1" || value == "true" || value == "TRUE" || value == "yes" || value == "YES"
	}

	var (
		listenAddr            = flag.String("listen", "localhost:2222", "Address to listen on")
		hostKeyFile           = flag.String("key", "./ssh_host_key", "Path to SSH host key")
		logLevel              = flag.String("log-level", "info", "Log level: error, info, debug")
		recordingsDir         = flag.String("recordings-dir", "./recordings", "Directory where session recordings are stored")
		authorizedKeysPath    = flag.String("authorized-keys", defaultAuthorizedKeysPath, "Path to authorized_keys file for proxy client authentication")
		autoAcceptClientKeys  = flag.Bool("auto-accept-client-keys", defaultAutoAcceptClientKeys, "Automatically accept client public keys without checking authorized_keys")
		allowDirectCommands   = flag.Bool("allow-direct-commands", false, "Allow SSH exec requests (direct command execution); by default only interactive terminal sessions are allowed")
		insecureIgnoreHostKey = flag.Bool("insecure-ignore-hostkey", defaultInsecureIgnoreHostKey, "Disable target host key verification (insecure; for temporary development use only)")
		recordingFormat       = flag.String("recording-format", recording.FormatAsciinema, "Session recording format: asciinema or script")
	)
	flag.Parse()

	// Set log level
	types.SetLogLevel(*logLevel)

	if !*autoAcceptClientKeys && *authorizedKeysPath == "" {
		log.Fatalf("Authorized keys path is empty; provide -authorized-keys when -auto-accept-client-keys=false")
	}
	*recordingFormat = recording.NormalizeFormat(*recordingFormat)
	if !recording.IsSupportedFormat(*recordingFormat) {
		log.Fatalf("Invalid recording format: %s. Use asciinema or script", *recordingFormat)
	}
	if *autoAcceptClientKeys {
		types.LogInfo("Client public keys will be accepted automatically")
	} else {
		types.LogInfo("Using authorized keys file %s", *authorizedKeysPath)
	}

	if *allowDirectCommands {
		types.LogInfo("Direct command execution is enabled")
	} else {
		types.LogInfo("Direct command execution is disabled; interactive terminal sessions only")
	}

	if *insecureIgnoreHostKey {
		types.LogInfo("WARNING: target host key verification is disabled via -insecure-ignore-hostkey")
	}

	types.LogInfo("Recording format: %s", *recordingFormat)

	// Load or generate host key
	hostKey, err := hostkey.LoadOrGenerateHostKey(*hostKeyFile)
	if err != nil {
		log.Fatalf("Failed to load/generate host key: %v", err)
	}

	// Setup SSH server listener
	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", *listenAddr, err)
	}
	defer listener.Close()

	types.LogInfo("SSH Proxy Server listening on %s", *listenAddr)

	// Accept connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		go server.HandleConnection(conn, hostKey, *recordingsDir, *authorizedKeysPath, *autoAcceptClientKeys, *allowDirectCommands, *insecureIgnoreHostKey, *recordingFormat)
	}
}

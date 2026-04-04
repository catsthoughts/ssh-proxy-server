package main

import (
	"flag"
	"log"
	"net"
	"os"

	"ssh-proxy-server/internal/hostkey"
	"ssh-proxy-server/internal/server"
	"ssh-proxy-server/internal/types"
)

func main() {
	defaultAuthorizedKeysPath := server.DefaultAuthorizedKeysPath()

	defaultAutoAcceptClientKeys := true
	if value := os.Getenv(server.AutoAcceptClientKeysEnv); value != "" {
		defaultAutoAcceptClientKeys = value == "1" || value == "true" || value == "TRUE" || value == "yes" || value == "YES"
	}

	var (
		listenAddr           = flag.String("listen", "localhost:2222", "Address to listen on")
		hostKeyFile          = flag.String("key", "./ssh_host_key", "Path to SSH host key")
		logLevel             = flag.String("log-level", "info", "Log level: error, info, debug")
		recordingsDir        = flag.String("recordings-dir", "./recordings", "Directory where session recordings are stored")
		authorizedKeysPath   = flag.String("authorized-keys", defaultAuthorizedKeysPath, "Path to authorized_keys file for proxy client authentication")
		autoAcceptClientKeys = flag.Bool("auto-accept-client-keys", defaultAutoAcceptClientKeys, "Automatically accept client public keys without checking authorized_keys")
	)
	flag.Parse()

	// Set log level
	types.SetLogLevel(*logLevel)

	if !*autoAcceptClientKeys && *authorizedKeysPath == "" {
		log.Fatalf("Authorized keys path is empty; provide -authorized-keys when -auto-accept-client-keys=false")
	}
	if *autoAcceptClientKeys {
		types.LogInfo("Client public keys will be accepted automatically")
	} else {
		types.LogInfo("Using authorized keys file %s", *authorizedKeysPath)
	}

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

		go server.HandleConnection(conn, hostKey, *recordingsDir, *authorizedKeysPath, *autoAcceptClientKeys)
	}
}

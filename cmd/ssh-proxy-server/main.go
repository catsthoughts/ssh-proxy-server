package main

import (
	"flag"
	"log"
	"net"

	"ssh-proxy-server/internal/hostkey"
	"ssh-proxy-server/internal/server"
	"ssh-proxy-server/internal/types"
)

func main() {
	var (
		listenAddr    = flag.String("listen", "localhost:2222", "Address to listen on")
		hostKeyFile   = flag.String("key", "./ssh_host_key", "Path to SSH host key")
		logLevel      = flag.String("log-level", "info", "Log level: error, info, debug")
		recordingsDir = flag.String("recordings-dir", "./recordings", "Directory where session recordings are stored")
	)
	flag.Parse()

	// Set log level
	types.SetLogLevel(*logLevel)

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

		go server.HandleConnection(conn, hostKey, *recordingsDir)
	}
}

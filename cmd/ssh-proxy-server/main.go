package main

import (
	"flag"
	"log"
	"net"
	"strings"
	"time"

	"ssh-proxy-server/internal/appconfig"
	"ssh-proxy-server/internal/hostkey"
	"ssh-proxy-server/internal/server"
	"ssh-proxy-server/internal/types"
)

func main() {
	configPath := flag.String("config", "", "Path to JSON config file with all startup settings")
	flag.Parse()

	if strings.TrimSpace(*configPath) == "" {
		log.Fatal("Missing -config. Start the proxy with a JSON config file, for example: ./ssh-proxy-server -config ./config.example.json")
	}

	cfg, err := appconfig.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config from %s: %v", *configPath, err)
	}

	types.SetLogLevel(cfg.LogLevel)
	types.LogInfo("Loaded config from %s", *configPath)

	if cfg.AutoAcceptClientKeys {
		types.LogInfo("Client public keys will be accepted automatically")
	} else {
		types.LogInfo("Using authorized keys file %s", cfg.AuthorizedKeys)
	}

	if cfg.AllowDirectCommands {
		types.LogInfo("Direct command execution is enabled")
	} else {
		types.LogInfo("Direct command execution is disabled; interactive terminal sessions only")
	}

	if cfg.InsecureIgnoreHostKey {
		types.LogInfo("WARNING: target host key verification is disabled via config")
	}

	types.LogInfo("Recording format: %s", cfg.RecordingFormat)
	types.LogInfo("Target connection settings: retries=%d timeout=%ds", cfg.Retries, cfg.ConnectTimeoutSeconds)
	if cfg.StaticRouting.Enabled {
		types.LogInfo("Static routing enabled: mode=%s targets=%d (LC_SSH_SERVER will be ignored)", cfg.StaticRouting.Mode, len(cfg.StaticRouting.Servers))
	} else {
		types.LogInfo("Dynamic routing enabled: target is expected from LC_SSH_SERVER")
	}

	routingConfig := server.RoutingConfig{
		StaticEnabled:  cfg.StaticRouting.Enabled,
		StaticTargets:  append([]string(nil), cfg.StaticRouting.Servers...),
		Mode:           cfg.StaticRouting.Mode,
		ConnectTimeout: time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
		Retries:        cfg.Retries,
	}

	hostKey, err := hostkey.LoadOrGenerateHostKey(cfg.Key)
	if err != nil {
		log.Fatalf("Failed to load/generate host key: %v", err)
	}

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", cfg.Listen, err)
	}
	defer listener.Close()

	types.LogInfo("SSH Proxy Server listening on %s", cfg.Listen)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		go server.HandleConnection(conn, hostKey, cfg.RecordingsDir, cfg.AuthorizedKeys, cfg.AutoAcceptClientKeys, cfg.AllowDirectCommands, cfg.InsecureIgnoreHostKey, cfg.RecordingFormat, routingConfig)
	}
}

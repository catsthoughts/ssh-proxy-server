package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"ssh-proxy-server/internal/appconfig"
	"ssh-proxy-server/internal/hostkey"
	appmetrics "ssh-proxy-server/internal/metrics"
	"ssh-proxy-server/internal/server"
	"ssh-proxy-server/internal/types"

	"golang.org/x/crypto/ssh"
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
	if cfg.SSO.Enabled {
		types.LogInfo("SSO second factor enabled: provider=%s base_url=%s realm=%s timeout=%ds poll_interval=%ds connect_timeout=%ds enforce_user_match=%t", cfg.SSO.Provider, cfg.SSO.BaseURL, cfg.SSO.Realm, cfg.SSO.AuthTimeoutSeconds, cfg.SSO.PollIntervalSeconds, cfg.SSO.ConnectTimeoutSeconds, cfg.SSO.EnforceSSHUserMatch)
	} else {
		types.LogInfo("SSO second factor is disabled")
	}
	if cfg.Metrics.Enabled {
		types.LogInfo("Prometheus metrics enabled: listen=%s path=%s", cfg.Metrics.Listen, cfg.Metrics.Path)
	} else {
		types.LogInfo("Prometheus metrics are disabled")
	}

	var trustedCACerts []ssh.PublicKey
	if len(cfg.TrustedCACerts) > 0 {
		trustedCACerts, err = server.LoadTrustedCACerts(cfg.TrustedCACerts)
		if err != nil {
			log.Fatalf("Failed to load trusted CA certificates: %v", err)
		}
		types.LogInfo("Loaded %d trusted CA certificate(s) for client certificate authentication", len(trustedCACerts))
	} else {
		types.LogInfo("No trusted CA certificates configured for client certificate authentication")
	}

	var trustedHostCACerts []ssh.PublicKey
	if len(cfg.TrustedHostCACerts) > 0 {
		trustedHostCACerts, err = server.LoadTrustedCACerts(cfg.TrustedHostCACerts)
		if err != nil {
			log.Fatalf("Failed to load trusted host CA certificates: %v", err)
		}
		types.LogInfo("Loaded %d trusted host CA certificate(s) for target host certificate verification", len(trustedHostCACerts))
	} else {
		types.LogInfo("No trusted host CA certificates configured for target host certificate verification")
	}

	routingConfig := server.RoutingConfig{
		StaticEnabled:  cfg.StaticRouting.Enabled,
		StaticTargets:  append([]string(nil), cfg.StaticRouting.Servers...),
		Mode:           cfg.StaticRouting.Mode,
		ConnectTimeout: time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
		Retries:        cfg.Retries,
	}
	ssoConfig := server.SSOConfig{
		Enabled:          cfg.SSO.Enabled,
		Provider:         cfg.SSO.Provider,
		BaseURL:          cfg.SSO.BaseURL,
		Realm:            cfg.SSO.Realm,
		ClientID:         cfg.SSO.ClientID,
		ClientSecret:     cfg.SSO.ClientSecret,
		Scope:            cfg.SSO.Scope,
		AuthTimeout:      time.Duration(cfg.SSO.AuthTimeoutSeconds) * time.Second,
		PollInterval:     time.Duration(cfg.SSO.PollIntervalSeconds) * time.Second,
		RequestTimeout:   time.Duration(cfg.SSO.ConnectTimeoutSeconds) * time.Second,
		EnforceUserMatch:   cfg.SSO.EnforceSSHUserMatch,
		InsecureSkipVerify: cfg.SSO.InsecureSkipVerify,
	}

	hostKey, err := hostkey.LoadOrGenerateHostKey(cfg.Key)
	if err != nil {
		log.Fatalf("Failed to load/generate host key: %v", err)
	}

	if cfg.Metrics.Enabled {
		metricsMux := http.NewServeMux()
		metricsMux.Handle(cfg.Metrics.Path, appmetrics.Default().Handler())
		metricsListener, err := net.Listen("tcp", cfg.Metrics.Listen)
		if err != nil {
			log.Fatalf("Failed to listen for Prometheus metrics on %s: %v", cfg.Metrics.Listen, err)
		}
		defer metricsListener.Close()
		go func() {
			if err := http.Serve(metricsListener, metricsMux); err != nil {
				log.Printf("Prometheus metrics server stopped: %v", err)
			}
		}()
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

		go server.HandleConnection(conn, hostKey, cfg.RecordingsDir, cfg.AuthorizedKeys, cfg.AutoAcceptClientKeys, cfg.AllowDirectCommands, cfg.InsecureIgnoreHostKey, cfg.RecordingFormat, routingConfig, ssoConfig, trustedCACerts, trustedHostCACerts)
	}
}

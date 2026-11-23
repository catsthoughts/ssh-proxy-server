package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"ssh-proxy-server/internal/access"
	"ssh-proxy-server/internal/appconfig"
	"ssh-proxy-server/internal/audit"
	"ssh-proxy-server/internal/configwatch"
	"ssh-proxy-server/internal/hostkey"
	appmetrics "ssh-proxy-server/internal/metrics"
	"ssh-proxy-server/internal/ratelimit"
	"ssh-proxy-server/internal/recording"
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
		types.LogInfo("SSO second factor enabled: provider=%s base_url=%s realm=%s timeout=%ds poll_interval=%ds connect_timeout=%ds enforce_user_match=%t required_groups=%d", cfg.SSO.Provider, cfg.SSO.BaseURL, cfg.SSO.Realm, cfg.SSO.AuthTimeoutSeconds, cfg.SSO.PollIntervalSeconds, cfg.SSO.ConnectTimeoutSeconds, cfg.SSO.EnforceSSHUserMatch, len(cfg.SSO.RequiredGroups))
	} else {
		types.LogInfo("SSO second factor is disabled")
	}
	if cfg.AccessControl.Enabled {
		types.LogInfo("Access control enabled: default=%s rules=%d", cfg.AccessControl.Default, len(cfg.AccessControl.Rules))
	} else {
		types.LogInfo("Access control is disabled: any authenticated client may reach any target")
	}
	if cfg.KnownHosts != "" {
		types.LogInfo("Using centralized known_hosts file %s", cfg.KnownHosts)
	}
	if cfg.SFTPEnabled {
		types.LogInfo("SFTP subsystem proxying is enabled")
	}
	if cfg.RateLimit.Enabled {
		types.LogInfo("Handshake rate limiting enabled: max_per_minute=%d per source IP", cfg.RateLimit.MaxPerMinute)
	}
	if cfg.Metrics.Enabled {
		types.LogInfo("Prometheus metrics enabled: listen=%s path=%s", cfg.Metrics.Listen, cfg.Metrics.Path)
	} else {
		types.LogInfo("Prometheus metrics are disabled")
	}

	auditLogger := audit.New(os.Stdout)

	// connConfig holds the hot-reloadable per-connection settings snapshot.
	// SIGHUP or a config-file change rebuilds it; active sessions keep the
	// snapshot they started with.
	var connConfig atomic.Value
	reload := func() {
		newCfg, err := appconfig.Load(*configPath)
		if err != nil {
			log.Printf("Failed to reload config from %s, keeping current settings: %v", *configPath, err)
			return
		}
		newConnConfig, err := buildConnectionConfig(newCfg, auditLogger)
		if err != nil {
			log.Printf("Failed to apply reloaded config from %s, keeping current settings: %v", *configPath, err)
			return
		}
		if old, ok := connConfig.Load().(server.ConnectionConfig); ok && old.Limiter != nil {
			old.Limiter.Stop()
		}
		types.SetLogLevel(newCfg.LogLevel)
		connConfig.Store(newConnConfig)
		types.LogInfo("Reloaded config from %s (active sessions are unaffected)", *configPath)
	}

	initialConnConfig, err := buildConnectionConfig(cfg, auditLogger)
	if err != nil {
		log.Fatalf("Failed to build connection settings: %v", err)
	}
	connConfig.Store(initialConnConfig)

	// SIGHUP triggers a hot reload (FR-3).
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for range sighup {
			reload()
		}
	}()

	// fsnotify watches the config file (reload) and authorized_keys (picked up
	// automatically because the file is re-read for every authentication).
	watchCtx, stopWatch := context.WithCancel(context.Background())
	watcherPaths := []string{*configPath}
	if cfg.AuthorizedKeys != "" && !cfg.AutoAcceptClientKeys {
		watcherPaths = append(watcherPaths, cfg.AuthorizedKeys)
	}
	if watcher, err := configwatch.Watch(watchCtx, watcherPaths, func(changed string) {
		if abs, err := filepath.Abs(changed); err == nil {
			if cfgAbs, err2 := filepath.Abs(*configPath); err2 == nil && abs == cfgAbs {
				reload()
				return
			}
		}
		types.LogInfo("Detected change in %s; it will be used for subsequent authentications", changed)
	}); err != nil {
		log.Printf("Failed to watch config files, hot reload via SIGHUP only: %v", err)
	} else {
		defer watcher.Close()
	}
	defer stopWatch()

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

	if cfg.Health.Enabled {
		healthMux := http.NewServeMux()
		healthMux.HandleFunc(cfg.Health.Path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
		healthListener, err := net.Listen("tcp", cfg.Health.Listen)
		if err != nil {
			log.Fatalf("Failed to listen for health checks on %s: %v", cfg.Health.Listen, err)
		}
		defer healthListener.Close()
		go func() {
			if err := http.Serve(healthListener, healthMux); err != nil {
				log.Printf("Health check server stopped: %v", err)
			}
		}()
		types.LogInfo("Health endpoint enabled: listen=%s path=%s", cfg.Health.Listen, cfg.Health.Path)
	}

	// Recording retention janitor (retention_days > 0).
	startRetentionJanitor(&connConfig)

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

		go server.HandleConnection(conn, hostKey, connConfig.Load().(server.ConnectionConfig))
	}
}

// buildConnectionConfig converts the application config into the immutable
// per-connection snapshot, compiling the ACL and loading CA certificates.
func buildConnectionConfig(cfg *appconfig.Config, auditLogger *audit.Logger) (server.ConnectionConfig, error) {
	acl, err := access.Compile(cfg.AccessControl)
	if err != nil {
		return server.ConnectionConfig{}, fmt.Errorf("invalid access_control configuration: %w", err)
	}

	var trustedCACerts []ssh.PublicKey
	if len(cfg.TrustedCACerts) > 0 {
		trustedCACerts, err = server.LoadTrustedCACerts(cfg.TrustedCACerts)
		if err != nil {
			return server.ConnectionConfig{}, fmt.Errorf("failed to load trusted CA certificates: %w", err)
		}
	}

	var trustedHostCACerts []ssh.PublicKey
	if len(cfg.TrustedHostCACerts) > 0 {
		trustedHostCACerts, err = server.LoadTrustedCACerts(cfg.TrustedHostCACerts)
		if err != nil {
			return server.ConnectionConfig{}, fmt.Errorf("failed to load trusted host CA certificates: %w", err)
		}
	}

	var handshakeLimiter *ratelimit.Limiter
	if cfg.RateLimit.Enabled && cfg.RateLimit.MaxPerMinute > 0 {
		handshakeLimiter = ratelimit.New(cfg.RateLimit.MaxPerMinute)
	}

	return server.ConnectionConfig{
		RecordingsDir:         cfg.RecordingsDir,
		AuthorizedKeys:        cfg.AuthorizedKeys,
		AutoAcceptClientKeys:  cfg.AutoAcceptClientKeys,
		AllowDirectCommands:   cfg.AllowDirectCommands,
		InsecureIgnoreHostKey: cfg.InsecureIgnoreHostKey,
		RecordingFormat:       cfg.RecordingFormat,
		Routing: server.RoutingConfig{
			StaticEnabled:  cfg.StaticRouting.Enabled,
			StaticTargets:  append([]string(nil), cfg.StaticRouting.Servers...),
			Mode:           cfg.StaticRouting.Mode,
			ConnectTimeout: time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
			Retries:        cfg.Retries,
		},
		SSO: server.SSOConfig{
			Enabled:            cfg.SSO.Enabled,
			Provider:           cfg.SSO.Provider,
			BaseURL:            cfg.SSO.BaseURL,
			Realm:              cfg.SSO.Realm,
			ClientID:           cfg.SSO.ClientID,
			ClientSecret:       cfg.SSO.ClientSecret,
			Scope:              cfg.SSO.Scope,
			AuthTimeout:        time.Duration(cfg.SSO.AuthTimeoutSeconds) * time.Second,
			PollInterval:       time.Duration(cfg.SSO.PollIntervalSeconds) * time.Second,
			RequestTimeout:     time.Duration(cfg.SSO.ConnectTimeoutSeconds) * time.Second,
			EnforceUserMatch:   cfg.SSO.EnforceSSHUserMatch,
			InsecureSkipVerify: cfg.SSO.InsecureSkipVerify,
			RequiredGroups:     append([]string(nil), cfg.SSO.RequiredGroups...),
		},
		TrustedCACerts:          trustedCACerts,
		TrustedHostCACerts:      trustedHostCACerts,
		ACL:                     acl,
		Audit:                   auditLogger,
		Limiter:                 handshakeLimiter,
		KnownHosts:              cfg.KnownHosts,
		SFTPEnabled:             cfg.SFTPEnabled,
		RecordingIntegrity:      cfg.Recordings.Integrity,
		RecordingsRetentionDays: cfg.Recordings.RetentionDays,
	}, nil
}

// startRetentionJanitor periodically removes recordings older than the
// configured retention. The retention setting is read from the latest
// snapshot so hot reloads are honored.
func startRetentionJanitor(connConfig *atomic.Value) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for now := range ticker.C {
			snapshot, ok := connConfig.Load().(server.ConnectionConfig)
			if !ok {
				continue
			}
			removed, err := recording.EnforceRetention(snapshot.RecordingsDir, snapshot.RecordingsRetentionDays, now)
			if err != nil {
				log.Printf("Recording retention cleanup failed: %v", err)
				continue
			}
			for _, name := range removed {
				types.LogInfo("Removed expired recording %s", name)
			}
		}
	}()
}

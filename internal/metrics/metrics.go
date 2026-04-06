package metrics

import (
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Collector holds the Prometheus metrics used by the SSH proxy server.
type Collector struct {
	registry             *prometheus.Registry
	sshConnectionsTotal  prometheus.Counter
	sshConnectionsActive prometheus.Gauge
	sshHandshakeFailures prometheus.Counter
	proxySessionsTotal   *prometheus.CounterVec
	ssoConfirmations     *prometheus.CounterVec
	ssoPendingSessions   prometheus.Gauge
	ssoErrorsTotal       prometheus.Counter
}

var defaultCollector = NewCollector()

// Default returns the process-wide metrics collector used by the application.
func Default() *Collector {
	return defaultCollector
}

// NewCollector creates a metrics collector with its own Prometheus registry.
func NewCollector() *Collector {
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	collector := &Collector{
		registry: registry,
		sshConnectionsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "ssh_proxy",
			Name:      "ssh_connections_total",
			Help:      "Total number of SSH connections established by the proxy.",
		}),
		sshConnectionsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "ssh_proxy",
			Name:      "ssh_connections_active",
			Help:      "Current number of active SSH connections handled by the proxy.",
		}),
		sshHandshakeFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "ssh_proxy",
			Name:      "ssh_handshake_failures_total",
			Help:      "Total number of inbound SSH handshakes that failed.",
		}),
		proxySessionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "ssh_proxy",
			Name:      "proxy_sessions_total",
			Help:      "Total number of proxied shell or exec sessions by result.",
		}, []string{"result"}),
		ssoConfirmations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "ssh_proxy",
			Name:      "sso_confirmations_total",
			Help:      "Total number of SSO confirmations by result.",
		}, []string{"result"}),
		ssoPendingSessions: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "ssh_proxy",
			Name:      "sso_pending_sessions",
			Help:      "Current number of SSH sessions waiting for SSO/2FA confirmation.",
		}),
		ssoErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "ssh_proxy",
			Name:      "sso_errors_total",
			Help:      "Total number of SSO/2FA confirmation errors.",
		}),
	}

	registry.MustRegister(
		collector.sshConnectionsTotal,
		collector.sshConnectionsActive,
		collector.sshHandshakeFailures,
		collector.proxySessionsTotal,
		collector.ssoConfirmations,
		collector.ssoPendingSessions,
		collector.ssoErrorsTotal,
	)

	return collector
}

// Handler returns the Prometheus HTTP handler for this collector's registry.
func (c *Collector) Handler() http.Handler {
	if c == nil || c.registry == nil {
		return promhttp.Handler()
	}
	return promhttp.HandlerFor(c.registry, promhttp.HandlerOpts{})
}

// SSHConnectionOpened records a new established SSH connection.
func (c *Collector) SSHConnectionOpened() {
	if c == nil {
		return
	}
	c.sshConnectionsTotal.Inc()
	c.sshConnectionsActive.Inc()
}

// SSHConnectionClosed records that an active SSH connection has ended.
func (c *Collector) SSHConnectionClosed() {
	if c == nil {
		return
	}
	c.sshConnectionsActive.Dec()
}

// RecordSSHHandshakeFailure increments the inbound SSH handshake failure counter.
func (c *Collector) RecordSSHHandshakeFailure() {
	if c == nil {
		return
	}
	c.sshHandshakeFailures.Inc()
}

// RecordProxySession records a proxied shell or exec session outcome.
func (c *Collector) RecordProxySession(result string) {
	if c == nil {
		return
	}
	c.proxySessionsTotal.WithLabelValues(normalizeResult(result)).Inc()
}

// RecordSSOConfirmation records an SSO confirmation outcome.
func (c *Collector) RecordSSOConfirmation(result string) {
	if c == nil {
		return
	}
	c.ssoConfirmations.WithLabelValues(normalizeResult(result)).Inc()
}

// SSOWaitingStarted increments the number of sessions currently waiting for SSO/2FA confirmation.
func (c *Collector) SSOWaitingStarted() {
	if c == nil {
		return
	}
	c.ssoPendingSessions.Inc()
}

// SSOWaitingFinished decrements the number of sessions waiting for SSO/2FA confirmation.
func (c *Collector) SSOWaitingFinished() {
	if c == nil {
		return
	}
	c.ssoPendingSessions.Dec()
}

// RecordSSOError increments the number of SSO/2FA confirmation errors.
func (c *Collector) RecordSSOError() {
	if c == nil {
		return
	}
	c.ssoErrorsTotal.Inc()
}

func normalizeResult(result string) string {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "success", "failure", "rejected":
		return strings.ToLower(strings.TrimSpace(result))
	default:
		return "unknown"
	}
}

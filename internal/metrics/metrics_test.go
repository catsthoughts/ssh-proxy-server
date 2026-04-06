package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCollectorHandlerExportsMetrics(t *testing.T) {
	collector := NewCollector()
	collector.SSHConnectionOpened()
	collector.RecordProxySession("success")
	collector.RecordSSOConfirmation("success")
	collector.SSOWaitingStarted()
	collector.RecordSSOError()
	collector.SSOWaitingFinished()
	collector.SSHConnectionClosed()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	collector.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("metrics handler status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, expected := range []string{
		"ssh_proxy_ssh_connections_total",
		"ssh_proxy_ssh_connections_active",
		"ssh_proxy_proxy_sessions_total",
		"ssh_proxy_sso_confirmations_total",
		"ssh_proxy_sso_pending_sessions",
		"ssh_proxy_sso_errors_total",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected metrics output to contain %q, got %q", expected, body)
		}
	}
}

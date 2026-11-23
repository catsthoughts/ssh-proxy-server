package ratelimit

import (
	"testing"
	"time"
)

func TestDisabledLimiterAllowsEverything(t *testing.T) {
	limiter := New(0)
	for i := 0; i < 100; i++ {
		if !limiter.Allow("10.0.0.1:1234") {
			t.Fatal("disabled limiter should allow everything")
		}
	}
}

func TestSlidingWindowLimit(t *testing.T) {
	limiter := New(3)
	defer limiter.Stop()

	addr := "10.0.0.1:1234"
	for i := 0; i < 3; i++ {
		if !limiter.Allow(addr) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if limiter.Allow(addr) {
		t.Fatal("4th request within the window should be denied")
	}
}

func TestPerIPIsolation(t *testing.T) {
	limiter := New(1)
	defer limiter.Stop()

	if !limiter.Allow("10.0.0.1:1") {
		t.Fatal("first request from 10.0.0.1 should be allowed")
	}
	if limiter.Allow("10.0.0.1:2") {
		t.Fatal("second request from 10.0.0.1 should be denied")
	}
	if !limiter.Allow("10.0.0.2:1") {
		t.Fatal("other IP should not be affected")
	}
}

func TestWindowSlides(t *testing.T) {
	limiter := New(1)
	defer limiter.Stop()
	limiter.window = 50 * time.Millisecond

	if !limiter.Allow("10.0.0.1:1") {
		t.Fatal("first request should be allowed")
	}
	time.Sleep(60 * time.Millisecond)
	if !limiter.Allow("10.0.0.1:2") {
		t.Fatal("request after the window elapsed should be allowed again")
	}
}

func TestHostOf(t *testing.T) {
	if got := hostOf("192.168.1.1:22"); got != "192.168.1.1" {
		t.Fatalf("hostOf() = %q, want 192.168.1.1", got)
	}
	if got := hostOf("no-port"); got != "no-port" {
		t.Fatalf("hostOf() = %q, want no-port", got)
	}
}

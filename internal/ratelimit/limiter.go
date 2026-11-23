// Package ratelimit provides a simple in-memory sliding-window rate limiter
// for inbound SSH handshakes, per source IP.
package ratelimit

import (
	"net"
	"sync"
	"time"
)

// Limiter limits how many handshakes a single source IP may start within a
// sliding window. A zero-value Limiter (or one built with New(0)) allows
// everything.
type Limiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	hits     map[string][]time.Time
	stopOnce sync.Once
	stop     chan struct{}
}

// New returns a Limiter allowing at most max events per minute per IP.
// max <= 0 disables limiting (Allow always returns true) and janitor is not
// started in that case.
func New(max int) *Limiter {
	l := &Limiter{max: max, window: time.Minute, hits: make(map[string][]time.Time)}
	if max > 0 {
		l.stop = make(chan struct{})
		go l.janitor()
	}
	return l
}

// Allow reports whether an event from the given remote address is permitted.
func (l *Limiter) Allow(remoteAddr string) bool {
	if l == nil || l.max <= 0 {
		return true
	}
	ip := hostOf(remoteAddr)
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()
	times := l.hits[ip]
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[ip] = kept
		return false
	}
	l.hits[ip] = append(kept, now)
	return true
}

// Stop terminates the background janitor goroutine.
func (l *Limiter) Stop() {
	if l == nil || l.stop == nil {
		return
	}
	l.stopOnce.Do(func() { close(l.stop) })
}

func (l *Limiter) janitor() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			l.mu.Lock()
			cutoff := time.Now().Add(-l.window)
			for ip, times := range l.hits {
				kept := times[:0]
				for _, t := range times {
					if t.After(cutoff) {
						kept = append(kept, t)
					}
				}
				if len(kept) == 0 {
					delete(l.hits, ip)
				} else {
					l.hits[ip] = kept
				}
			}
			l.mu.Unlock()
		}
	}
}

func hostOf(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

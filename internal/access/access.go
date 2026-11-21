// Package access implements the per-user target ACL (FR-1): an authenticated
// client may only be routed to targets that are explicitly allowed for its
// SSH username. Without this check any authenticated client could reach any
// host through LC_SSH_SERVER or static routing.
package access

import (
	"fmt"
	"strings"
)

// Rule grants one user access to a list of target patterns.
type Rule struct {
	User    string   `json:"user"`
	Targets []string `json:"targets"`
}

// Config is the access_control section of the JSON config.
type Config struct {
	Enabled bool   `json:"enabled"`
	Default string `json:"default"` // "allow" or "deny"
	Rules   []Rule `json:"rules"`
}

// ACL is a compiled, immutable access control list.
type ACL struct {
	enabled      bool
	defaultAllow bool
	rules        []compiledRule
}

type compiledRule struct {
	user     string
	patterns []string
}

// Compile validates the configuration and builds the ACL. An empty/disabled
// configuration compiles to an ACL that allows everything, preserving the
// previous proxy behaviour.
func Compile(cfg Config) (*ACL, error) {
	acl := &ACL{}
	if !cfg.Enabled {
		return acl, nil
	}
	acl.enabled = true

	defaultValue := strings.ToLower(strings.TrimSpace(cfg.Default))
	switch defaultValue {
	case "", "deny":
		acl.defaultAllow = false
	case "allow":
		acl.defaultAllow = true
	default:
		return nil, fmt.Errorf("access_control.default must be %q or %q", "allow", "deny")
	}

	for i, rule := range cfg.Rules {
		user := strings.TrimSpace(rule.User)
		if user == "" {
			return nil, fmt.Errorf("access_control.rules[%d].user is empty", i)
		}
		if len(rule.Targets) == 0 {
			return nil, fmt.Errorf("access_control.rules[%d].targets must contain at least one pattern", i)
		}
		patterns := make([]string, 0, len(rule.Targets))
		for j, target := range rule.Targets {
			pattern := strings.TrimSpace(target)
			if pattern == "" {
				return nil, fmt.Errorf("access_control.rules[%d].targets[%d] is empty", i, j)
			}
			if strings.ContainsAny(pattern, " \t\r\n\"'`;&|<>\\(){}") {
				return nil, fmt.Errorf("access_control.rules[%d].targets[%d] %q contains unsafe characters", i, j, pattern)
			}
			patterns = append(patterns, pattern)
		}
		acl.rules = append(acl.rules, compiledRule{user: user, patterns: patterns})
	}
	return acl, nil
}

// NormalizeDefault normalizes the access_control.default value.
func NormalizeDefault(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow":
		return "allow"
	default:
		return "deny"
	}
}

// Allowed reports whether the user may connect to host:port. Patterns with an
// explicit port match exactly; patterns without a port match the host only on
// the default SSH port 22.
func (a *ACL) Allowed(user, host, port string) bool {
	if a == nil || !a.enabled {
		return true
	}
	user = strings.TrimSpace(user)
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)

	candidates := []string{host}
	if port != "" {
		candidates[0] = host + ":" + port
		if port == "22" {
			candidates = append(candidates, host)
		}
	}

	for _, rule := range a.rules {
		if !matchPattern(rule.user, user) {
			continue
		}
		for _, pattern := range rule.patterns {
			for _, candidate := range candidates {
				if matchPattern(pattern, candidate) {
					return true
				}
			}
		}
	}
	return a.defaultAllow
}

// matchPattern matches a glob-like pattern against a value. '*' matches any
// (possibly empty) sequence of characters; everything else must match exactly.
func matchPattern(pattern, value string) bool {
	if pattern == value {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return false
	}
	// Iterative two-pointer matching; patterns are tiny so no need for DP.
	p, v := 0, 0
	starP, starV := -1, 0
	for v < len(value) {
		if p < len(pattern) && pattern[p] == value[v] {
			p++
			v++
			continue
		}
		if p < len(pattern) && pattern[p] == '*' {
			starP = p
			starV = v
			p++
			continue
		}
		if starP != -1 {
			p = starP + 1
			starV++
			v = starV
			continue
		}
		return false
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

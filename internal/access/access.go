// Package access implements the per-user target ACL (FR-1): an authenticated
// client may only be routed to targets that are explicitly allowed for its
// SSH username. Without this check any authenticated client could reach any
// host through LC_SSH_SERVER or static routing.
package access

import (
	"fmt"
	"net"
	"strings"
)

// Rule grants a user or an SSO group access to a list of target patterns. A
// rule applies when its User pattern matches the SSH login user or when the
// client is a member of one of the listed Groups (matched case-insensitively).
type Rule struct {
	User    string   `json:"user"`
	Groups  []string `json:"groups"`
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
	groups   []string
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
		groups := make([]string, 0, len(rule.Groups))
		for _, group := range rule.Groups {
			group = strings.TrimSpace(group)
			if group == "" {
				return nil, fmt.Errorf("access_control.rules[%d].groups contains an empty entry", i)
			}
			groups = append(groups, group)
		}
		if user == "" && len(groups) == 0 {
			return nil, fmt.Errorf("access_control.rules[%d] must set user or groups", i)
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
			if strings.Contains(pattern, "/") {
				if _, _, err := net.ParseCIDR(pattern); err != nil {
					return nil, fmt.Errorf("access_control.rules[%d].targets[%d] %q is not a valid CIDR range: %v", i, j, pattern, err)
				}
			}
			patterns = append(patterns, pattern)
		}
		acl.rules = append(acl.rules, compiledRule{user: user, groups: groups, patterns: patterns})
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
// the default SSH port 22. CIDR patterns (e.g. "10.0.0.0/8") match any target
// host IP inside the range, on the default port. A rule applies when its user
// pattern matches the login user or when the client belongs to one of the
// rule's groups (the SSO groups of the user).
func (a *ACL) Allowed(user, host, port string, groups ...string) bool {
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
		if !matchRuleSubject(rule, user, groups) {
			continue
		}
		for _, pattern := range rule.patterns {
			for _, candidate := range candidates {
				if matchPattern(pattern, candidate) || matchCIDR(pattern, candidate) {
					return true
				}
			}
		}
	}
	return a.defaultAllow
}

// matchRuleSubject reports whether a rule applies to the login user or to one
// of the client's SSO groups.
func matchRuleSubject(rule compiledRule, user string, groups []string) bool {
	if matchPattern(rule.user, user) {
		return true
	}
	for _, group := range groups {
		for _, ruleGroup := range rule.groups {
			if strings.EqualFold(ruleGroup, group) {
				return true
			}
		}
	}
	return false
}

// matchCIDR reports whether pattern is a CIDR range containing the candidate,
// which must be a bare IP address (host candidates, not host:port).
func matchCIDR(pattern, candidate string) bool {
	_, network, err := net.ParseCIDR(pattern)
	if err != nil {
		return false
	}
	ip := net.ParseIP(candidate)
	return ip != nil && network.Contains(ip)
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

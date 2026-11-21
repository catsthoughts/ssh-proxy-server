package access

import "testing"

func compile(t *testing.T, cfg Config) *ACL {
	t.Helper()
	acl, err := Compile(cfg)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	return acl
}

func TestDisabledAllowsEverything(t *testing.T) {
	acl := compile(t, Config{Enabled: false})
	if !acl.Allowed("anyone", "evil.example.com", "22") {
		t.Fatal("disabled ACL should allow everything")
	}
}

func TestDefaultDenyWithoutMatchingRule(t *testing.T) {
	acl := compile(t, Config{
		Enabled: true,
		Default: "deny",
		Rules: []Rule{
			{User: "alice", Targets: []string{"web1.example.com"}},
		},
	})
	if acl.Allowed("bob", "web1.example.com", "22") {
		t.Fatal("bob has no rule; default deny should reject")
	}
}

func TestExactHostAndPortMatching(t *testing.T) {
	acl := compile(t, Config{
		Enabled: true,
		Default: "deny",
		Rules: []Rule{
			{User: "alice", Targets: []string{"web1.example.com", "db.example.com:5433"}},
		},
	})
	if !acl.Allowed("alice", "web1.example.com", "22") {
		t.Fatal("alice should reach web1 on the default port")
	}
	if acl.Allowed("alice", "web1.example.com", "2222") {
		t.Fatal("pattern without port should not match a non-default port")
	}
	if !acl.Allowed("alice", "db.example.com", "5433") {
		t.Fatal("alice should reach db on port 5433")
	}
	if acl.Allowed("alice", "db.example.com", "22") {
		t.Fatal("db is only allowed on 5433")
	}
}

func TestWildcardMatching(t *testing.T) {
	acl := compile(t, Config{
		Enabled: true,
		Default: "deny",
		Rules: []Rule{
			{User: "alice", Targets: []string{"web-*.prod.example.com", "10.0.*"}},
		},
	})
	for host, want := range map[string]bool{
		"web-1.prod.example.com": true,
		"web-2.prod.example.com": true,
		"web-1.dev.example.com":  false,
		"10.0.0.5":               true,
		"10.1.0.5":               false,
	} {
		if got := acl.Allowed("alice", host, "22"); got != want {
			t.Fatalf("Allowed(alice, %s) = %v, want %v", host, got, want)
		}
	}
}

func TestStarAllowsEverythingForUser(t *testing.T) {
	acl := compile(t, Config{
		Enabled: true,
		Default: "deny",
		Rules: []Rule{
			{User: "admin", Targets: []string{"*"}},
		},
	})
	if !acl.Allowed("admin", "anything.example.com", "2222") {
		t.Fatal("admin with '*' target should reach any host:port")
	}
}

func TestDefaultAllow(t *testing.T) {
	acl := compile(t, Config{
		Enabled: true,
		Default: "allow",
		Rules: []Rule{
			{User: "alice", Targets: []string{"web1.example.com"}},
		},
	})
	if !acl.Allowed("bob", "anything.example.com", "22") {
		t.Fatal("default allow should grant unmatched users access")
	}
}

func TestUserWildcardRule(t *testing.T) {
	acl := compile(t, Config{
		Enabled: true,
		Default: "deny",
		Rules: []Rule{
			{User: "svc-*", Targets: []string{"backend.example.com"}},
		},
	})
	if !acl.Allowed("svc-deploy", "backend.example.com", "22") {
		t.Fatal("svc-* rule should match svc-deploy")
	}
	if acl.Allowed("svc-deploy", "other.example.com", "22") {
		t.Fatal("svc-deploy should not reach other hosts")
	}
}

func TestCompileValidation(t *testing.T) {
	if _, err := Compile(Config{Enabled: true, Default: "maybe"}); err == nil {
		t.Fatal("expected error for invalid default")
	}
	if _, err := Compile(Config{Enabled: true, Rules: []Rule{{User: "", Targets: []string{"x"}}}}); err == nil {
		t.Fatal("expected error for empty rule user")
	}
	if _, err := Compile(Config{Enabled: true, Rules: []Rule{{User: "a", Targets: nil}}}); err == nil {
		t.Fatal("expected error for rule without targets")
	}
	if _, err := Compile(Config{Enabled: true, Rules: []Rule{{User: "a", Targets: []string{"bad host"}}}}); err == nil {
		t.Fatal("expected error for target with unsafe characters")
	}
}

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"*", "", true},
		{"*", "anything", true},
		{"a*", "a", true},
		{"a*", "ab", true},
		{"a*b", "ab", true},
		{"a*b", "axb", true},
		{"a*b", "axbc", false},
		{"*.example.com", "x.example.com", true},
		{"*.example.com", "example.com", false},
		{"web-?", "web-1", false}, // '?' is not a wildcard, must match literally
	}
	for _, tc := range cases {
		if got := matchPattern(tc.pattern, tc.value); got != tc.want {
			t.Fatalf("matchPattern(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}

package sso

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func jwtWithClaims(t *testing.T, claims map[string]interface{}) string {
	t.Helper()
	header, err := json.Marshal(map[string]interface{}{"alg": "none"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestExtractGroupsClaim(t *testing.T) {
	token := jwtWithClaims(t, map[string]interface{}{
		"sub":                "user-1",
		"preferred_username": "alice",
		"groups":             []interface{}{"ssh-users", "admins"},
	})
	identity := extractIdentityFromJWT(token)
	if identity.PreferredUsername != "alice" {
		t.Fatalf("PreferredUsername = %q, want alice", identity.PreferredUsername)
	}
	if len(identity.Groups) != 2 || identity.Groups[0] != "ssh-users" || identity.Groups[1] != "admins" {
		t.Fatalf("Groups = %v, want [ssh-users admins]", identity.Groups)
	}
}

func TestExtractRealmAccessRoles(t *testing.T) {
	token := jwtWithClaims(t, map[string]interface{}{
		"sub": "user-2",
		"realm_access": map[string]interface{}{
			"roles": []interface{}{"ssh-users"},
		},
	})
	identity := extractIdentityFromJWT(token)
	if len(identity.Groups) != 1 || identity.Groups[0] != "ssh-users" {
		t.Fatalf("Groups = %v, want [ssh-users]", identity.Groups)
	}
}

func TestMissingGroups(t *testing.T) {
	identity := Identity{Groups: []string{"ssh-users"}}
	if missing := identity.MissingGroups([]string{"ssh-users"}); len(missing) != 0 {
		t.Fatalf("MissingGroups() = %v, want empty", missing)
	}
	if missing := identity.MissingGroups([]string{"ssh-users", "admins"}); len(missing) != 1 || missing[0] != "admins" {
		t.Fatalf("MissingGroups() = %v, want [admins]", missing)
	}
	if missing := identity.MissingGroups(nil); len(missing) != 0 {
		t.Fatalf("MissingGroups(nil) = %v, want empty", missing)
	}
}

func TestInGroupCaseInsensitive(t *testing.T) {
	identity := Identity{Groups: []string{"SSH-Users"}}
	if !identity.InGroup("ssh-users") {
		t.Fatal("InGroup() should be case-insensitive")
	}
	if identity.InGroup("other") {
		t.Fatal("InGroup() = true for non-member group")
	}
}

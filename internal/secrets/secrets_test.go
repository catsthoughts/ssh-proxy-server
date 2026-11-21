package secrets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStringPlainPassthrough(t *testing.T) {
	got, err := ResolveString("  plain-secret ")
	if err != nil {
		t.Fatalf("ResolveString() error: %v", err)
	}
	if got != "plain-secret" {
		t.Fatalf("ResolveString() = %q, want %q", got, "plain-secret")
	}
}

func TestResolveStringEnvPrefix(t *testing.T) {
	t.Setenv("SSH_PROXY_TEST_SECRET", "s3cr3t")
	got, err := ResolveString("$env:SSH_PROXY_TEST_SECRET")
	if err != nil {
		t.Fatalf("ResolveString() error: %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("ResolveString() = %q, want %q", got, "s3cr3t")
	}
}

func TestResolveStringEnvPrefixUnset(t *testing.T) {
	if _, err := ResolveString("$env:SSH_PROXY_DEFINITELY_MISSING_VAR"); err == nil {
		t.Fatal("ResolveString() expected error for unset environment variable")
	}
}

func TestResolveStringFilePrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveString("$file:" + path)
	if err != nil {
		t.Fatalf("ResolveString() error: %v", err)
	}
	if got != "file-secret" {
		t.Fatalf("ResolveString() = %q, want %q", got, "file-secret")
	}
}

func TestResolveStringFilePrefixMissing(t *testing.T) {
	if _, err := ResolveString("$file:/nonexistent/secret/path"); err == nil {
		t.Fatal("ResolveString() expected error for missing secret file")
	}
}

func TestResolveJSONStringForm(t *testing.T) {
	raw := json.RawMessage(`"plain"`)
	got, ref, err := ResolveJSON(raw)
	if err != nil || ref || got != "plain" {
		t.Fatalf("ResolveJSON() = %q, %v, %v; want plain, false, nil", got, ref, err)
	}
}

func TestResolveJSONStringPrefixForm(t *testing.T) {
	t.Setenv("SSH_PROXY_TEST_SECRET", "s3cr3t")
	got, ref, err := ResolveJSON(json.RawMessage(`"$env:SSH_PROXY_TEST_SECRET"`))
	if err != nil || !ref || got != "s3cr3t" {
		t.Fatalf("ResolveJSON() = %q, %v, %v; want s3cr3t, true, nil", got, ref, err)
	}
}

func TestResolveJSONEnvObject(t *testing.T) {
	t.Setenv("SSH_PROXY_TEST_SECRET", "s3cr3t")
	got, ref, err := ResolveJSON(json.RawMessage(`{"$env": "SSH_PROXY_TEST_SECRET"}`))
	if err != nil || !ref || got != "s3cr3t" {
		t.Fatalf("ResolveJSON() = %q, %v, %v; want s3cr3t, true, nil", got, ref, err)
	}
}

func TestResolveJSONFileObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ref, err := ResolveJSON(json.RawMessage(`{"$file": "` + path + `"}`))
	if err != nil || !ref || got != "file-secret" {
		t.Fatalf("ResolveJSON() = %q, %v, %v; want file-secret, true, nil", got, ref, err)
	}
}

func TestResolveJSONEmpty(t *testing.T) {
	got, ref, err := ResolveJSON(nil)
	if err != nil || ref || got != "" {
		t.Fatalf("ResolveJSON(nil) = %q, %v, %v; want empty, false, nil", got, ref, err)
	}
}

func TestResolveJSONInvalidObject(t *testing.T) {
	if _, _, err := ResolveJSON(json.RawMessage(`{"unknown": "x"}`)); err == nil {
		t.Fatal("ResolveJSON() expected error for object without $env/$file")
	}
}

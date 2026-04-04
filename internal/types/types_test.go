package types

import (
	"bytes"
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSetLogLevelControlsOutput(t *testing.T) {
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	defer log.SetOutput(oldWriter)
	defer log.SetFlags(oldFlags)

	log.SetFlags(0)
	var buf bytes.Buffer
	log.SetOutput(&buf)

	SetLogLevel("error")
	LogInfo("info hidden")
	LogDebug("debug hidden")
	if buf.Len() != 0 {
		t.Fatalf("expected no log output at error level, got %q", buf.String())
	}

	buf.Reset()
	SetLogLevel("info")
	LogInfo("info visible")
	LogDebug("debug hidden")
	output := buf.String()
	if !strings.Contains(output, "info visible") {
		t.Fatalf("expected info message in output, got %q", output)
	}
	if strings.Contains(output, "debug hidden") {
		t.Fatalf("did not expect debug message at info level, got %q", output)
	}

	buf.Reset()
	SetLogLevel("debug")
	LogDebug("debug visible")
	if !strings.Contains(buf.String(), "debug visible") {
		t.Fatalf("expected debug message in output, got %q", buf.String())
	}
}

func TestSetLogLevelInvalidExits(t *testing.T) {
	if os.Getenv("TEST_INVALID_LOG_LEVEL") == "1" {
		SetLogLevel("verbose")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSetLogLevelInvalidExits")
	cmd.Env = append(os.Environ(), "TEST_INVALID_LOG_LEVEL=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected invalid log level to terminate the process")
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if exitErr.Success() {
		t.Fatal("expected non-zero exit status for invalid log level")
	}
}

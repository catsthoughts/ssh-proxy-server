package recording

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	FormatAsciinema = "asciinema"
	FormatScript    = "script"
)

// Recorder records terminal sessions in a supported output format.
type Recorder interface {
	Write(data []byte) error
	WriteInput(data []byte) error
	Close() error
}

func NormalizeFormat(format string) string {
	normalized := strings.ToLower(strings.TrimSpace(format))
	if normalized == "" {
		return FormatAsciinema
	}
	return normalized
}

func IsSupportedFormat(format string) bool {
	switch NormalizeFormat(format) {
	case FormatAsciinema, FormatScript:
		return true
	default:
		return false
	}
}

func FileExtension(format string) string {
	switch NormalizeFormat(format) {
	case FormatScript:
		return ".log"
	default:
		return ".cast"
	}
}

func NewRecorder(format, filePath string) (Recorder, error) {
	switch NormalizeFormat(format) {
	case FormatAsciinema:
		return NewAsciinemaRecorder(filePath), nil
	case FormatScript:
		return NewScriptRecorder(filePath), nil
	default:
		return nil, fmt.Errorf("unsupported recording format %q (use %q or %q)", format, FormatAsciinema, FormatScript)
	}
}

// AsciinemaRecorder records terminal sessions in asciinema format.
type AsciinemaRecorder struct {
	file      *os.File
	mu        sync.Mutex
	startTime time.Time
	enabled   bool
}

// ScriptRecorder records sessions as a plain-text transcript similar to `script` output.
type ScriptRecorder struct {
	file      *os.File
	mu        sync.Mutex
	startTime time.Time
	enabled   bool
}

// AsciinemaHeader represents the JSON header for asciinema v2 format.
type AsciinemaHeader struct {
	Version   int                    `json:"version"`
	Width     int                    `json:"width"`
	Height    int                    `json:"height"`
	Timestamp int64                  `json:"timestamp"`
	Command   string                 `json:"command,omitempty"`
	Title     string                 `json:"title,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// AsciinemaFrame represents a single frame in the recording.
type AsciinemaFrame struct {
	Time float64 `json:"time"`
	Type string  `json:"type"`
	Data string  `json:"data"`
}

// NewAsciinemaRecorder creates a new asciinema recorder.
func NewAsciinemaRecorder(filePath string) *AsciinemaRecorder {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		log.Printf("Failed to create recording file %s: %v", filePath, err)
		return &AsciinemaRecorder{enabled: false}
	}

	recorder := &AsciinemaRecorder{
		file:      file,
		startTime: time.Now(),
		enabled:   true,
	}

	header := AsciinemaHeader{
		Version:   2,
		Width:     120,
		Height:    40,
		Timestamp: time.Now().Unix(),
		Title:     "SSH Proxy Session",
		Metadata: map[string]interface{}{
			"session_id": uuid.New().String(),
		},
	}

	headerJSON, _ := json.Marshal(header)
	_, _ = file.Write(headerJSON)
	_, _ = file.WriteString("\n")

	return recorder
}

// NewScriptRecorder creates a new plain-text script-style recorder.
func NewScriptRecorder(filePath string) *ScriptRecorder {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		log.Printf("Failed to create recording file %s: %v", filePath, err)
		return &ScriptRecorder{enabled: false}
	}

	recorder := &ScriptRecorder{
		file:      file,
		startTime: time.Now(),
		enabled:   true,
	}

	_, _ = file.WriteString(fmt.Sprintf("Script started on %s\n", recorder.startTime.Format(time.RFC3339)))
	return recorder
}

// Write records output data.
func (r *AsciinemaRecorder) Write(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.enabled || r.file == nil {
		return nil
	}

	elapsed := time.Since(r.startTime).Seconds()
	frame := []interface{}{elapsed, "o", string(data)}

	frameJSON, _ := json.Marshal(frame)
	_, err := r.file.Write(frameJSON)
	if err != nil {
		return err
	}

	_, err = r.file.WriteString("\n")
	return err
}

// WriteInput records input data.
func (r *AsciinemaRecorder) WriteInput(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.enabled || r.file == nil {
		return nil
	}

	elapsed := time.Since(r.startTime).Seconds()
	frame := []interface{}{elapsed, "i", string(data)}

	frameJSON, _ := json.Marshal(frame)
	_, err := r.file.Write(frameJSON)
	if err != nil {
		return err
	}

	_, err = r.file.WriteString("\n")
	return err
}

// Write appends transcript data to the script-format recording.
func (r *ScriptRecorder) Write(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.enabled || r.file == nil {
		return nil
	}
	_, err := r.file.Write(data)
	return err
}

// WriteInput appends user input to the script-format recording.
func (r *ScriptRecorder) WriteInput(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.enabled || r.file == nil {
		return nil
	}
	_, err := r.file.Write(data)
	return err
}

// Close closes the recording file.
func (r *AsciinemaRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file == nil {
		return nil
	}
	file := r.file
	r.file = nil
	return file.Close()
}

// Close closes the script transcript file.
func (r *ScriptRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file == nil {
		return nil
	}

	file := r.file
	r.file = nil
	if r.enabled {
		_, _ = file.WriteString(fmt.Sprintf("\nScript done on %s\n", time.Now().Format(time.RFC3339)))
	}

	return file.Close()
}

// generateRecordingID generates a unique recording ID.
func generateRecordingID() string {
	return uuid.New().String()[:8]
}

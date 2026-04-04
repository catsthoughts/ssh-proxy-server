package recording

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AsciinemaRecorder records terminal sessions in asciinema format
type AsciinemaRecorder struct {
	file      *os.File
	mu        sync.Mutex
	startTime time.Time
	enabled   bool
}

// AsciinemaHeader represents the JSON header for asciinema v2 format
type AsciinemaHeader struct {
	Version   int                    `json:"version"`
	Width     int                    `json:"width"`
	Height    int                    `json:"height"`
	Timestamp int64                  `json:"timestamp"`
	Command   string                 `json:"command,omitempty"`
	Title     string                 `json:"title,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// AsciinemaFrame represents a single frame in the recording
type AsciinemaFrame struct {
	Time float64 `json:"time"`
	Type string  `json:"type"`
	Data string  `json:"data"`
}

// NewAsciinemaRecorder creates a new asciinema recorder
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

	// Write header
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
	file.Write(headerJSON)
	file.WriteString("\n")

	return recorder
}

// Write records output data
func (r *AsciinemaRecorder) Write(data []byte) error {
	if !r.enabled {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	elapsed := time.Since(r.startTime).Seconds()
	frame := []interface{}{
		elapsed,
		"o",
		string(data),
	}

	frameJSON, _ := json.Marshal(frame)
	_, err := r.file.Write(frameJSON)
	if err != nil {
		return err
	}

	_, err = r.file.WriteString("\n")
	return err
}

// WriteInput records input data
func (r *AsciinemaRecorder) WriteInput(data []byte) error {
	if !r.enabled {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	elapsed := time.Since(r.startTime).Seconds()
	frame := []interface{}{
		elapsed,
		"i",
		string(data),
	}

	frameJSON, _ := json.Marshal(frame)
	_, err := r.file.Write(frameJSON)
	if err != nil {
		return err
	}

	_, err = r.file.WriteString("\n")
	return err
}

// Close closes the recording file
func (r *AsciinemaRecorder) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// generateRecordingID generates a unique recording ID
func generateRecordingID() string {
	return uuid.New().String()[:8]
}

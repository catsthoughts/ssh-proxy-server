package recording

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ChecksumExtension is the file extension used for recording integrity
// checksum sidecar files.
const ChecksumExtension = ".sha256"

// WriteChecksum computes the SHA-256 checksum of the recording at path and
// stores it in a "<path>.sha256" sidecar file with private permissions.
func WriteChecksum(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open recording for checksum: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("failed to hash recording %s: %w", path, err)
	}

	checksumPath := path + ChecksumExtension
	content := hex.EncodeToString(hash.Sum(nil)) + "  " + filepath.Base(path) + "\n"
	if err := os.WriteFile(checksumPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("failed to write recording checksum %s: %w", checksumPath, err)
	}
	return nil
}

// EnforceRetention removes recordings (and their checksum sidecars) older
// than the given retention. A retention of zero or less disables cleanup.
// It returns the names of the removed files.
func EnforceRetention(dir string, retentionDays int, now time.Time) ([]string, error) {
	if retentionDays <= 0 {
		return nil, nil
	}
	cutoff := now.Add(-time.Duration(retentionDays) * 24 * time.Hour)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read recordings directory %s: %w", dir, err)
	}

	var removed []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, FileExtension(FormatAsciinema)) &&
			!strings.HasSuffix(name, FileExtension(FormatScript)) &&
			!strings.HasSuffix(name, ChecksumExtension) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, fmt.Errorf("failed to remove expired recording %s: %w", name, err)
		}
		removed = append(removed, name)
	}
	return removed, nil
}

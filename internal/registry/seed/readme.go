package seed

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// ReadmeEntry represents a README blob stored in a seed file.
type ReadmeEntry struct {
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int    `json:"size_bytes,omitempty"`
	Sha256      string `json:"sha256,omitempty"`
}

// ReadmeFile is the JSON structure persisted for README seeds. Each entry is keyed by server name and version.
type ReadmeFile map[string]ReadmeEntry

// Key builds the standard key format for README seed entries (name@version).
func Key(serverName, version string) string {
	return fmt.Sprintf("%s@%s", serverName, version)
}

// EncodeReadme produces a ReadmeEntry from raw README bytes and content type.
func EncodeReadme(content []byte, contentType string) ReadmeEntry {
	if len(content) == 0 {
		return ReadmeEntry{ContentType: contentType}
	}

	encoded := base64.StdEncoding.EncodeToString(content)
	sum := sha256.Sum256(content)

	return ReadmeEntry{
		Content:     encoded,
		ContentType: contentType,
		SizeBytes:   len(content),
		Sha256:      hex.EncodeToString(sum[:]),
	}
}

// Decode returns the README bytes and content type from a seed entry.
func (e ReadmeEntry) Decode() ([]byte, string, error) {
	if e.Content == "" {
		return nil, e.ContentType, nil
	}

	data, err := base64.StdEncoding.DecodeString(e.Content)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode README content: %w", err)
	}

	if e.SizeBytes > 0 && len(data) != e.SizeBytes {
		return nil, "", fmt.Errorf("README size mismatch: expected %d bytes, got %d", e.SizeBytes, len(data))
	}

	if e.Sha256 != "" {
		expected, err := hex.DecodeString(e.Sha256)
		if err != nil {
			return nil, "", fmt.Errorf("invalid README sha256: %w", err)
		}
		sum := sha256.Sum256(data)
		if !bytes.Equal(expected, sum[:]) {
			return nil, "", fmt.Errorf("README sha256 mismatch")
		}
	}

	return data, e.ContentType, nil
}

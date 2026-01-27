package artifacts

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Writer manages artifacts for a single test run.
type Writer struct {
	RunDir string
}

// NewWriter creates the run directory.
func NewWriter(runDir string) (*Writer, error) {
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	return &Writer{RunDir: runDir}, nil
}

// WriteJSON writes an object to a JSON file under the run directory.
func (w *Writer) WriteJSON(name string, value any) (string, error) {
	path := filepath.Join(w.RunDir, name)
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// WriteText writes a string to a file under the run directory.
func (w *Writer) WriteText(name string, data string) (string, error) {
	path := filepath.Join(w.RunDir, name)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// WriteBytes writes bytes to a file under the run directory.
func (w *Writer) WriteBytes(name string, data []byte) (string, error) {
	path := filepath.Join(w.RunDir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

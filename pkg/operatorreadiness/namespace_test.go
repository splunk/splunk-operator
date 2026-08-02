package operatorreadiness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePodNamespacePrefersExplicitValue(t *testing.T) {
	got, err := resolvePodNamespace(" operator-system ", filepath.Join(t.TempDir(), "missing"))
	if err != nil || got != "operator-system" {
		t.Fatalf("resolvePodNamespace() = (%q, %v), want (%q, nil)", got, err, "operator-system")
	}
}

func TestResolvePodNamespaceFallsBackToServiceAccountFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "namespace")
	if err := os.WriteFile(path, []byte("operator-system\n"), 0o600); err != nil {
		t.Fatalf("write namespace fixture: %v", err)
	}
	got, err := resolvePodNamespace("", path)
	if err != nil || got != "operator-system" {
		t.Fatalf("resolvePodNamespace() = (%q, %v), want (%q, nil)", got, err, "operator-system")
	}
}

func TestResolvePodNamespaceRejectsMissingOrEmptyFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := resolvePodNamespace("", missing); err == nil {
		t.Fatal("resolvePodNamespace() missing file error = nil")
	}

	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, []byte(" \n"), 0o600); err != nil {
		t.Fatalf("write empty namespace fixture: %v", err)
	}
	if _, err := resolvePodNamespace("", empty); err == nil {
		t.Fatal("resolvePodNamespace() empty file error = nil")
	}
}

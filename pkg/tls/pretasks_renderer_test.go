package tls

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

// helper to make a temporary template FS with given contents
func withTempTemplate(t *testing.T, content string, fn func()) {
	t.Helper()
	dir := t.TempDir()
	templatesDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(templatesDir, "pretasks.tmpl.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// override the package-level indirection vars
	oldFS, oldPath := preTasksFS, preTasksPath
	preTasksFS, preTasksPath = os.DirFS(dir), "templates/pretasks.tmpl.yaml"
	defer func() { preTasksFS, preTasksPath = oldFS, oldPath }()

	fn()
}

const goodTemplate = `---
- name: Ensure canonical TLS dir
  file:
    path: "[[ .CanonicalDir ]]"
    state: directory
    owner: splunk
    group: splunk
    mode: "0755"

- name: Minimal check - assert exists
  assert:
    that:
      - '[[ .SplunkHome ]]' != ''
      - '[[ .CanonicalDir ]]' != ''
    success_msg: "Baseline ok"

[[ if ne .ServerName "" ]]
- name: Optional serverName
  debug: { msg: "serverName=[[ .ServerName ]]" }
[[ end ]]

[[ if .KVEnable ]]
- name: KV enabled section marker
  debug: { msg: "KV bundle at [[ .KVBundlePath ]]" }
[[ end ]]
`

const badTemplate = `---
- name: Broken template
  debug: { msg: "[[ .CanonicalDir ]" }   # missing closing ]]
`

const missingKeyTpl = `---
- name: Reference a missing field
  debug: { msg: "[[ .DoesNotExist ]]" }
`

func TestRender_Success_WithAndWithoutKV(t *testing.T) {
	withTempTemplate(t, goodTemplate, func() {
		// Case 1: KV disabled, no serverName
		spec := &v4.TLSConfig{
			CanonicalDir: "/opt/splunk/etc/auth/tls",
			// KVEncryptedKey is nil, so KVEnable=false
		}
		yml, sum, err := Render(spec, "", "")
		if err != nil {
			t.Fatalf("Render error (KV disabled): %v", err)
		}
		if len(strings.TrimSpace(yml)) == 0 {
			t.Fatal("empty YAML output")
		}
		if len(sum) != 64 {
			t.Fatalf("sha256 hex length = %d, want 64", len(sum))
		}
		if strings.Contains(yml, "serverName=") {
			t.Errorf("did not expect serverName section when empty, got:\n%s", yml)
		}
		if strings.Contains(yml, "KV enabled section marker") {
			t.Errorf("did not expect KV section when disabled")
		}

		// Case 2: KV enabled, with serverName
		spec2 := &v4.TLSConfig{
			CanonicalDir: "/opt/splunk/etc/auth/tls",
			KVEncryptedKey: &v4.KVEncryptedKeySpec{
				Enabled:    true,
				BundleFile: "", // default path from renderer
			},
		}
		yml2, sum2, err := Render(spec2, "splunk.example.com", "/mnt/kvpass/pass")
		if err != nil {
			t.Fatalf("Render error (KV enabled): %v", err)
		}
		if !strings.Contains(yml2, "serverName=splunk.example.com") {
			t.Errorf("expected serverName stanza when provided, got:\n%s", yml2)
		}
		if !strings.Contains(yml2, "KV enabled section marker") {
			t.Errorf("expected KV marker section when KVEnable=true, got:\n%s", yml2)
		}
		if sum2 == "" || len(sum2) != 64 {
			t.Fatalf("unexpected hash: %q", sum2)
		}
	})
}

func TestRender_Failure_ParseError(t *testing.T) {
	withTempTemplate(t, badTemplate, func() {
		spec := &v4.TLSConfig{CanonicalDir: "/opt/splunk/etc/auth/tls"}
		_, _, err := Render(spec, "", "")
		if err == nil {
			t.Fatal("expected parse error, got nil")
		}
		if !strings.Contains(err.Error(), "parse pretasks template") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestRender_Failure_MissingKey(t *testing.T) {
	withTempTemplate(t, missingKeyTpl, func() {
		spec := &v4.TLSConfig{CanonicalDir: "/opt/splunk/etc/auth/tls"}
		_, _, err := Render(spec, "", "")
		if err == nil {
			t.Fatal("expected missingkey error, got nil")
		}
		// produced from Execute with Option("missingkey=error")
		if !strings.Contains(err.Error(), "execute pretasks template") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// pkg/tls/pretasks_renderer.go
package tls

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strings"
	"text/template"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

// Embed the default pretasks template. This file must exist at build time.
// Keep this path aligned with your repo: pkg/tls/templates/pretasks.tmpl.yaml

//go:embed templates/pretasks.tmpl.yaml
var embeddedPreTasksFS embed.FS

// indirection used by tests to point at a temp FS + path
var (
	preTasksFS   fs.FS  = embeddedPreTasksFS
	preTasksPath string = "templates/pretasks.tmpl.yaml"
)

// Render renders the pretasks Ansible YAML from the TLS spec and returns the
// YAML plus a sha256 hex digest of the rendered content.
func Render(spec *v4.TLSConfig, serverName, kvPasswordFile string) (string, string, error) {
	if spec == nil {
		spec = &v4.TLSConfig{}
	}

	// Resolve defaults and fill dynamic fields
	d := defaultsFor(spec)
	d.ServerName = serverName
	d.KVPasswordFile = kvPasswordFile

	// Load template from the (overridable) FS
	tplBytes, err := fs.ReadFile(preTasksFS, preTasksPath)
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", preTasksPath, err)
	}

	// IMPORTANT: change Go template delimiters so Ansible/Jinja {{ ... }} is untouched.
	// Also fail fast on missing keys to catch template/data drift in CI.
	tpl, err := template.New("pretasks").
		Delims("[[", "]]").
		Option("missingkey=error").
		Parse(string(tplBytes))
	if err != nil {
		return "", "", fmt.Errorf("parse pretasks template: %w", err)
	}

	var b strings.Builder
	if err := tpl.Execute(&b, d); err != nil {
		return "", "", fmt.Errorf("execute pretasks template: %w", err)
	}

	sum := sha256.Sum256([]byte(b.String()))
	return b.String(), hex.EncodeToString(sum[:]), nil
}

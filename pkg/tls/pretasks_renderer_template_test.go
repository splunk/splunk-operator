package tls

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	v4 "github.com/splunk/splunk-operator/api/v4"
	"gopkg.in/yaml.v3"
)

// --- helpers ---

// render renders the real embedded template with options for KV and serverName.
func render(t *testing.T, kv bool, serverName string) string {
	t.Helper()
	spec := &v4.TLSConfig{
		CanonicalDir: "/opt/splunk/etc/auth/tls",
	}
	if kv {
		spec.KVEncryptedKey = &v4.KVEncryptedKeySpec{Enabled: true}
	}
	yml, _, err := Render(spec, serverName, "/mnt/kvpass/pass")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	return yml
}

// dumpRendered writes the rendered YAML to a temp file and logs the path.
// Use `go test -v` to see the path and the YAML inline.
func dumpRendered(t *testing.T, name, yml string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(yml), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	t.Logf("\n--- RENDERED pretasks (%s) ---\n%s\n--- END RENDERED ---\n(saved to %s)", name, yml, p)
	return p
}

func requireNoJinja(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, "{{") || strings.Contains(s, "{%") || strings.Contains(s, "{#") {
		t.Fatalf("Found Jinja remnants in rendered output")
	}
}

// Optional: ensure no leftover Go actions remain.
// (Our renderer should replace everything; this is a sanity check.)
func requireNoGoDelims(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, "[[") || strings.Contains(s, "]]") {
		t.Fatalf("Found leftover Go template delimiters in output")
	}
}

// YAML shape we expect at the top level - a list of task maps.
type yamlRoot = []map[string]any

func parseYAML(t *testing.T, s string) yamlRoot {
	t.Helper()
	var root yamlRoot
	if err := yaml.Unmarshal([]byte(s), &root); err != nil {
		t.Fatalf("Rendered YAML failed to parse: %v", err)
	}
	return root
}

// Walk all tasks, including tasks in a "block:" list, and run visit on each.
func walkTasks(tasks yamlRoot, visit func(task map[string]any)) {
	var walk func(any)
	walk = func(x any) {
		switch v := x.(type) {
		case []any:
			for _, e := range v {
				walk(e)
			}
		case []map[string]any:
			for _, m := range v {
				walk(m)
			}
		case map[string]any:
			visit(v)
			// descend into "block" if present
			if b, ok := v["block"]; ok {
				walk(b)
			}
		}
	}
	for _, t := range tasks {
		walk(t)
	}
}

// Check structure for ini_file tasks:
// - If a task has "ini_file", ensure task-level "no_extra_spaces" is NOT present.
// - If "no_extra_spaces" exists under "ini_file", it must be a boolean.
func assertIniFileNoExtraSpacesPlacement(t *testing.T, tasks yamlRoot) {
	t.Helper()
	walkTasks(tasks, func(task map[string]any) {
		name, _ := task["name"].(string)
		ini, hasIni := task["ini_file"]
		if !hasIni {
			// still ensure we did not accidentally put task-level no_extra_spaces anywhere
			if _, bad := task["no_extra_spaces"]; bad {
				t.Fatalf("task %q has no_extra_spaces at task root, but has no ini_file module", name)
			}
			return
		}
		// ini_file must be a map
		iniMap, ok := ini.(map[string]any)
		if !ok {
			t.Fatalf("task %q ini_file is not a mapping", name)
		}
		// task-level no_extra_spaces is invalid when ini_file is present
		if _, bad := task["no_extra_spaces"]; bad {
			t.Fatalf("task %q has no_extra_spaces at task root, it must be under ini_file", name)
		}
		// if nested, must be boolean
		if v, present := iniMap["no_extra_spaces"]; present {
			if _, isBool := v.(bool); !isBool {
				t.Fatalf("task %q ini_file.no_extra_spaces must be boolean, got %#v", name, v)
			}
		}
	})
}

// --- tests ---

// Renders the real embedded template, ensures no Jinja, parses as YAML,
// checks ini_file.no_extra_spaces is nested correctly, and logs the final playbook.
func TestTemplate_Real_YAML_NoJinja_BalancedAndStructured(t *testing.T) {
	out := render(t, false, "host.example.com")
	dumpRendered(t, "pretasks.rendered.yaml", out)

	requireNoJinja(t, out)
	// Optional: if you want to be strict that no [[...]] remain at all:
	requireNoGoDelims(t, out)

	root := parseYAML(t, out)
	assertIniFileNoExtraSpacesPlacement(t, root)
}

// Ensures the serverName stanza is only present when serverName is non-empty.
func TestTemplate_ServerName_Guard(t *testing.T) {
	// serverName empty -> no ini_file option=serverName
	outEmpty := render(t, false, "")
	dumpRendered(t, "pretasks.servername-empty.yaml", outEmpty)
	rootEmpty := parseYAML(t, outEmpty)

	found := false
	walkTasks(rootEmpty, func(task map[string]any) {
		if ini, ok := task["ini_file"].(map[string]any); ok {
			if opt, _ := ini["option"].(string); opt == "serverName" {
				found = true
			}
		}
	})
	if found {
		t.Fatalf("serverName ini_file task present when ServerName is empty")
	}

	// serverName provided -> must exist and be set to value
	const want = "splunk.example.com"
	outWith := render(t, false, want)
	dumpRendered(t, "pretasks.servername-present.yaml", outWith)
	rootWith := parseYAML(t, outWith)

	found = false
	walkTasks(rootWith, func(task map[string]any) {
		if ini, ok := task["ini_file"].(map[string]any); ok {
			if opt, _ := ini["option"].(string); opt == "serverName" {
				if val, _ := ini["value"].(string); val == want {
					found = true
				}
			}
		}
	})
	if !found {
		t.Fatalf("serverName ini_file task missing or incorrect when ServerName is provided")
	}
}

// Ensures KV tasks appear only when KVEnable is true.
func TestTemplate_KV_Guard(t *testing.T) {
	outNoKV := render(t, false, "")
	dumpRendered(t, "pretasks.kv-disabled.yaml", outNoKV)
	if strings.Contains(outNoKV, "KV: build encrypted bundle") ||
		strings.Contains(outNoKV, "/opt/splunk/auth/kvstore.pem") ||
		strings.Contains(outNoKV, "tls_enc.key") {
		t.Fatalf("KV tasks found when KVEnable=false")
	}

	outKV := render(t, true, "")
	dumpRendered(t, "pretasks.kv-enabled.yaml", outKV)
	if !strings.Contains(outKV, "KV: build encrypted bundle") ||
		!strings.Contains(outKV, "/opt/splunk/auth/kvstore.pem") ||
		!strings.Contains(outKV, "tls_enc.key") {
		t.Fatalf("KV tasks missing when KVEnable=true")
	}
}

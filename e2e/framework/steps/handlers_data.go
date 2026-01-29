package steps

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/spec"
)

// RegisterDataHandlers registers dataset-related steps.
func RegisterDataHandlers(reg *Registry) {
	reg.Register("data.generate.log", handleGenerateLog)
}

func handleGenerateLog(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	_ = ctx
	lines := getInt(step.With, "lines", 1)
	if lines < 1 {
		return nil, fmt.Errorf("lines must be >= 1")
	}

	path := getString(step.With, "path", "")
	if path == "" {
		path = filepath.Join(exec.Artifacts.RunDir, fmt.Sprintf("generated-%s-%s.log", exec.TestName, sanitize(step.Name)))
	}

	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	level := getString(step.With, "level", "DEBUG")
	component := getString(step.With, "component", "E2E")
	message := getString(step.With, "message", "generated log line")

	timestamp := time.Now().Add(-time.Second * time.Duration(lines))
	rand.Seed(time.Now().UnixNano())
	firstLine := ""
	for i := 0; i < lines; i++ {
		line := fmt.Sprintf("%s %s %s %s randomNumber=%d\n", timestamp.Format("01-02-2006 15:04:05.000"), level, component, message, rand.Int63())
		if _, err := file.WriteString(line); err != nil {
			return nil, err
		}
		if i == 0 {
			firstLine = line
		}
		timestamp = timestamp.Add(time.Second)
	}

	exec.Vars["last_generated_path"] = path
	metadata := map[string]string{"path": path, "count": fmt.Sprintf("%d", lines)}
	if firstLine != "" {
		trimmed := strings.TrimSuffix(firstLine, "\n")
		exec.Vars["last_generated_first_line"] = trimmed
		tokens := strings.Fields(trimmed)
		if len(tokens) > 0 {
			exec.Vars["last_generated_token"] = tokens[len(tokens)-1]
		}
		metadata["first_line"] = trimmed
	}
	return metadata, nil
}

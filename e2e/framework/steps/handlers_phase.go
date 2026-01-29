package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/spec"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RegisterPhaseHandlers registers phase assertion steps.
func RegisterPhaseHandlers(reg *Registry) {
	reg.Register("assert.splunk.phase", handleAssertSplunkPhase)
}

func handleAssertSplunkPhase(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	expected := strings.TrimSpace(getString(step.With, "phase", ""))
	if expected == "" {
		return nil, fmt.Errorf("phase is required")
	}
	timeout := exec.Config.DefaultTimeout
	if raw := getString(step.With, "timeout", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	interval := 5 * time.Second
	if raw := getString(step.With, "interval", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			interval = parsed
		}
	}

	deadline := time.Now().Add(timeout)
	for {
		obj, err := getUnstructuredResource(ctx, exec, step)
		if err != nil {
			return nil, err
		}
		phase := readPhase(obj)
		if phase == expected {
			return map[string]string{"phase": phase}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("expected phase %s, got %s", expected, phase)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func readPhase(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	return phase
}

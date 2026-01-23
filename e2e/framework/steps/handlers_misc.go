package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/spec"
)

// RegisterMiscHandlers registers misc utility steps.
func RegisterMiscHandlers(reg *Registry) {
	reg.Register("sleep", handleSleep)
}

func handleSleep(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	_ = exec
	duration := getDuration(step.With, "duration", 0)
	if duration <= 0 {
		raw := getString(step.With, "duration", "")
		if raw == "" {
			return nil, fmt.Errorf("sleep duration is required")
		}
		return nil, fmt.Errorf("invalid sleep duration %q", raw)
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}

	return map[string]string{"slept": duration.String()}, nil
}

package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/splunk/splunk-operator/e2e/framework/spec"
)

// Handler executes a step and returns metadata.
type Handler func(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error)

// Registry stores handlers by action name.
type Registry struct {
	handlers map[string]Handler
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

// Register adds a handler for a step action.
func (r *Registry) Register(action string, handler Handler) {
	r.handlers[strings.ToLower(action)] = handler
}

// Has reports whether a handler exists for the action.
func (r *Registry) Has(action string) bool {
	if r == nil {
		return false
	}
	_, ok := r.handlers[strings.ToLower(strings.TrimSpace(action))]
	return ok
}

// Execute runs a handler for the step action.
func (r *Registry) Execute(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	handler, ok := r.handlers[strings.ToLower(step.Action)]
	if !ok {
		return nil, fmt.Errorf("no handler registered for action %q", step.Action)
	}
	return handler(ctx, exec, step)
}

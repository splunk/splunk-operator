package metrics

import "time"

// NoopRecorder is a no-op implementation of Recorder for use in tests.
type NoopRecorder struct{}

func (n *NoopRecorder) ObserveReconcile(string, string, time.Duration) {}
func (n *NoopRecorder) IncReconcileError(string, string)              {}
func (n *NoopRecorder) IncRequeue(string, string)                     {}
func (n *NoopRecorder) IncValidationFailure(string, string)           {}
func (n *NoopRecorder) SetClusterPhases(map[string]float64, float64)  {}
func (n *NoopRecorder) SetDatabasePhases(map[string]float64)          {}
func (n *NoopRecorder) SetManagedUsers(string, map[string]float64)    {}
func (n *NoopRecorder) IncUserAction(string, string)                  {}
func (n *NoopRecorder) SetPoolers(string, string, float64)            {}
func (n *NoopRecorder) SetPoolerInstances(string, float64)            {}
func (n *NoopRecorder) IncFinalizerOp(string, string)                 {}
func (n *NoopRecorder) IncOwnedResourceOp(string, string, string, string) {}

// Compile-time interface check.
var _ Recorder = (*NoopRecorder)(nil)

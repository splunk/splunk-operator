package prometheus

import "github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"

// NoopRecorder is a no-op implementation of Recorder for use in tests.
type NoopRecorder struct{}

func (n *NoopRecorder) IncStatusTransition(string, string, string, string) {}
func (n *NoopRecorder) SetClusterPhases(map[string]float64)                {}
func (n *NoopRecorder) SetPoolerEnabledClusters(float64)                   {}
func (n *NoopRecorder) SetDatabasePhases(map[string]float64)               {}
func (n *NoopRecorder) SetManagedUsers(string, map[string]float64)         {}

// Compile-time interface check.
var _ ports.Recorder = (*NoopRecorder)(nil)

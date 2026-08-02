package operatorreadiness

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestPrometheusTelemetryUsesBoundedReadinessLabels(t *testing.T) {
	managerReadinessStatus.Reset()
	managerReadinessTransitions.Reset()
	managerReadinessLastTransition.Set(0)

	recorder := prometheusTelemetry{}
	recorder.SetCheck(CheckCacheSynchronized, true)
	recorder.SetCheck(CheckLeaderElectionAccess, false)
	recorder.SetCheck(CheckReconciliationParticipation, false)
	recorder.RecordTransition(false, ReasonLeaseAccessDenied)

	if got := testutil.ToFloat64(managerReadinessStatus.WithLabelValues(CheckCacheSynchronized)); got != 1 {
		t.Fatalf("cache readiness metric = %v, want 1", got)
	}
	if got := testutil.ToFloat64(managerReadinessStatus.WithLabelValues(CheckLeaderElectionAccess)); got != 0 {
		t.Fatalf("Lease readiness metric = %v, want 0", got)
	}
	if got := testutil.ToFloat64(managerReadinessTransitions.WithLabelValues("not_ready", ReasonLeaseAccessDenied)); got != 1 {
		t.Fatalf("not-ready transition metric = %v, want 1", got)
	}
	if got := testutil.ToFloat64(managerReadinessLastTransition); got <= 0 {
		t.Fatalf("last transition timestamp = %v, want positive", got)
	}

	const metricChildren = 5 // three check labels, one transition, one timestamp
	if got, err := testutil.GatherAndCount(
		crmetrics.Registry,
		"splunk_operator_manager_readiness_status",
		"splunk_operator_manager_readiness_transitions_total",
		"splunk_operator_manager_readiness_last_transition_timestamp_seconds",
	); err != nil || got != metricChildren {
		t.Fatalf("GatherAndCount() = (%d, %v), want (%d, nil)", got, err, metricChildren)
	}
}

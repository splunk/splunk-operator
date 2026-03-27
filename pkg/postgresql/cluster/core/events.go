package core

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Event reason constants — stable strings for kubectl filtering.
const (
	EventClusterCreating          = "ClusterCreating"
	EventClusterUpdated           = "ClusterUpdated"
	EventClusterReady             = "ClusterReady"
	EventPoolerReady              = "PoolerReady"
	EventClusterDeleting          = "ClusterDeleting"
	EventCleanupComplete          = "CleanupComplete"
	EventClusterClassNotFound     = "ClusterClassNotFound"
	EventConfigMergeFailed        = "ConfigMergeFailed"
	EventSecretReconcileFailed    = "SecretReconcileFailed"
	EventClusterCreateFailed      = "ClusterCreateFailed"
	EventClusterUpdateFailed      = "ClusterUpdateFailed"
	EventManagedRolesFailed       = "ManagedRolesFailed"
	EventPoolerReconcileFailed    = "PoolerReconcileFailed"
	EventConfigMapReconcileFailed = "ConfigMapReconcileFailed"
	EventClusterDegraded          = "ClusterDegraded"
	EventCleanupFailed            = "CleanupFailed"
)

func (rc *ReconcileContext) emitNormal(obj client.Object, reason, message string) {
	rc.Recorder.Event(obj, corev1.EventTypeNormal, reason, message)
}

func (rc *ReconcileContext) emitWarning(obj client.Object, reason, message string) {
	rc.Recorder.Event(obj, corev1.EventTypeWarning, reason, message)
}

// emitClusterPhaseTransition emits ClusterReady or ClusterDegraded only on
// actual phase transitions. No event is emitted when the phase is unchanged.
func (rc *ReconcileContext) emitClusterPhaseTransition(obj client.Object, oldPhase, newPhase string) {
	switch {
	case oldPhase != string(readyClusterPhase) && newPhase == string(readyClusterPhase):
		rc.emitNormal(obj, EventClusterReady, "Cluster is up and running")
	case oldPhase == string(readyClusterPhase) && newPhase != string(readyClusterPhase):
		rc.emitWarning(obj, EventClusterDegraded, fmt.Sprintf("Cluster entered phase: %s", newPhase))
	}
}

// emitPoolerReadyTransition emits PoolerReady only when the condition was not
// previously True — prevents re-emission on every reconcile while already ready.
func (rc *ReconcileContext) emitPoolerReadyTransition(obj client.Object, conditions []metav1.Condition) {
	if !meta.IsStatusConditionTrue(conditions, string(poolerReady)) {
		rc.emitNormal(obj, EventPoolerReady, "Connection poolers are ready")
	}
}

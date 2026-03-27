package core

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Event reason constants — stable strings for kubectl filtering.
const (
	EventPostgresDatabaseReady     = "PostgresDatabaseReady"
	EventResourcesAdopted          = "ResourcesAdopted"
	EventClusterValidated          = "ClusterValidated"
	EventSecretsReady              = "SecretsReady"
	EventConfigMapsReady           = "ConfigMapsReady"
	EventRoleReconciliationStarted = "RoleReconciliationStarted"
	EventRolesReady                = "RolesReady"
	EventDatabasesReady            = "DatabasesReady"
	EventPrivilegesReady           = "PrivilegesReady"
	EventDatabaseDeleting          = "DatabaseDeleting"
	EventCleanupComplete           = "CleanupComplete"
	EventClusterNotFound           = "ClusterNotFound"
	EventClusterNotReady           = "ClusterNotReady"
	EventRoleConflict              = "RoleConflict"
	EventUserSecretsFailed         = "UserSecretsFailed"
	EventAccessConfigFailed        = "AccessConfigFailed"
	EventManagedRolesPatchFailed   = "ManagedRolesPatchFailed"
	EventRoleFailed                = "RoleFailed"
	EventDatabasesReconcileFailed  = "DatabasesReconcileFailed"
	EventPrivilegesGrantFailed     = "PrivilegesGrantFailed"
	EventCleanupFailed             = "CleanupFailed"
)

func (rc *ReconcileContext) emitNormal(obj client.Object, reason, message string) {
	rc.Recorder.Event(obj, corev1.EventTypeNormal, reason, message)
}

func (rc *ReconcileContext) emitWarning(obj client.Object, reason, message string) {
	rc.Recorder.Event(obj, corev1.EventTypeWarning, reason, message)
}

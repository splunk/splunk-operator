/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package core

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	EventSecretReady                      = "SecretReady"
	EventConfigMapReady                   = "ConfigMapReady"
	EventConfigMapReconciled              = "ConfigMapReconciled"
	EventClusterAdopted                   = "ClusterAdopted"
	EventClusterCreationStarted           = "ClusterCreationStarted"
	EventClusterUpdateStarted             = "ClusterUpdateStarted"
	EventClusterReady                     = "ClusterReady"
	EventPoolerCreationStarted            = "PoolerCreationStarted"
	EventPoolerReady                      = "PoolerReady"
	EventCleanupComplete                  = "CleanupComplete"
	EventClusterClassNotFound             = "ClusterClassNotFound"
	EventConfigMergeFailed                = "ConfigMergeFailed"
	EventSecretReconcileFailed            = "SecretReconcileFailed"
	EventClusterCreateFailed              = "ClusterCreateFailed"
	EventClusterUpdateFailed              = "ClusterUpdateFailed"
	EventManagedRolesFailed               = "ManagedRolesFailed"
	EventManagedRolesReady                = "ManagedRolesReady"
	EventPoolerReconcileFailed            = "PoolerReconcileFailed"
	EventConfigMapReconcileFailed         = "ConfigMapReconcileFailed"
	EventClusterDegraded                  = "ClusterDegraded"
	EventCleanupFailed                    = "CleanupFailed"
	EventBackupConfigured                 = "BackupConfigured"
	EventScheduledBackupCreated           = "ScheduledBackupCreated"
	EventScheduledBackupDeleted           = "ScheduledBackupDeleted"
	EventBackupReconcileFailed            = "BackupReconcileFailed"
	EventObjectStoreBackupReconcileFailed = "ObjectStoreBackupReconcileFailed"
	EventObjectStoreCreated               = "ObjectStoreCreated"
	EventObjectStoreDeleted               = "ObjectStoreDeleted"
	EventUnmanagedRolesSweepDone          = "UnmanagedRolesSweepDone"
	EventUnmanagedRolesSweepFailed        = "UnmanagedRolesSweepFailed"
)

func (rc *ReconcileContext) emitNormal(obj client.Object, reason, message string) {
	rc.Recorder.Event(obj, corev1.EventTypeNormal, reason, message)
}

func (rc *ReconcileContext) emitWarning(obj client.Object, reason, message string) {
	rc.Recorder.Event(obj, corev1.EventTypeWarning, reason, message)
}

// emitClusterPhaseTransition emits ClusterReady or ClusterDegraded only on
// actual phase transitions. Provisioning and Configuring are expected phases
// after our own create/update operations, so they don't emit ClusterDegraded.
func (rc *ReconcileContext) emitClusterPhaseTransition(obj client.Object, oldPhase, newPhase string) {
	switch {
	case oldPhase != string(readyClusterPhase) && newPhase == string(readyClusterPhase):
		rc.emitNormal(obj, EventClusterReady, "Cluster is up and running")
	// only when cluster degraded from ready but not to provisioning or configuring
	case oldPhase == string(readyClusterPhase) && newPhase != string(readyClusterPhase) &&
		newPhase != string(provisioningClusterPhase) && newPhase != string(configuringClusterPhase):
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

// emitBackupReadyTransition emits BackupConfigured only when the condition was not
// previously True — prevents re-emission on every reconcile while already ready.
func (rc *ReconcileContext) emitBackupReadyTransition(obj client.Object, conditions []metav1.Condition) {
	if !meta.IsStatusConditionTrue(conditions, string(backupReady)) {
		rc.emitNormal(obj, EventBackupConfigured, "Backup configuration is ready")
	}
}

// emitPoolerCreationTransition emits PoolerCreationStarted only when the
// pooler condition is not already in the creating state.
func (rc *ReconcileContext) emitPoolerCreationTransition(obj client.Object, conditions []metav1.Condition) {
	cond := meta.FindStatusCondition(conditions, string(poolerReady))
	if cond != nil && cond.Status == metav1.ConditionFalse && cond.Reason == string(reasonPoolerCreating) {
		return
	}
	rc.emitNormal(obj, EventPoolerCreationStarted, "Connection poolers created, waiting for readiness")
}

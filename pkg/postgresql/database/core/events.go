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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	EventPostgresDatabaseReady         = "PostgresDatabaseReady"
	EventResourcesAdopted              = "ResourcesAdopted"
	EventClusterValidated              = "ClusterValidated"
	EventSecretsReady                  = "SecretsReady"
	EventConfigMapsReady               = "ConfigMapsReady"
	EventRoleReconciliationStarted     = "RoleReconciliationStarted"
	EventRolesReady                    = "RolesReady"
	EventDatabaseReconciliationStarted = "DatabaseReconciliationStarted"
	EventDatabasesReady                = "DatabasesReady"
	EventPrivilegesReady               = "PrivilegesReady"
	EventCleanupComplete               = "CleanupComplete"
	EventClusterNotFound               = "ClusterNotFound"
	EventClusterNotReady               = "ClusterNotReady"
	EventRoleConflict                  = "RoleConflict"
	EventRoleSecretsFailed             = "RoleSecretsFailed"
	EventRolesSecretsDriftDetected     = "RolesSecretsDriftDetected"
	EventAccessConfigFailed            = "AccessConfigFailed"
	EventManagedRolesPatchFailed       = "ManagedRolesPatchFailed"
	EventRoleFailed                    = "RoleFailed"
	EventDatabasesReconcileFailed      = "DatabasesReconcileFailed"
	EventPrivilegesGrantFailed         = "PrivilegesGrantFailed"
	EventCleanupFailed                 = "CleanupFailed"
)

func (rc *ReconcileContext) emitNormal(obj client.Object, reason, message string) {
	rc.Recorder.Event(obj, corev1.EventTypeNormal, reason, message)
}

func (rc *ReconcileContext) emitWarning(obj client.Object, reason, message string) {
	rc.Recorder.Event(obj, corev1.EventTypeWarning, reason, message)
}

// emitOnConditionTransition emits a Normal event only when the condition is not
// already True — prevents duplicate events on repeated reconciles.
func (rc *ReconcileContext) emitOnConditionTransition(obj client.Object, conditions []metav1.Condition, condType conditionTypes, reason, message string) {
	if !meta.IsStatusConditionTrue(conditions, string(condType)) {
		rc.emitNormal(obj, reason, message)
	}
}

// emitOnceBeforeWait emits a Normal event when the condition is either absent
// or currently True — i.e. the first time we enter a wait cycle. On subsequent
// requeue polls the condition is already False, so no duplicate is emitted.
func (rc *ReconcileContext) emitOnceBeforeWait(obj client.Object, conditions []metav1.Condition, condType conditionTypes, reason, message string) {
	cond := meta.FindStatusCondition(conditions, string(condType))
	if cond == nil || cond.Status == metav1.ConditionTrue {
		rc.emitNormal(obj, reason, message)
	}
}

// emitWarnOnceBeforeWait emits a Warning event when the condition is either
// absent or currently True — i.e. the first time we enter a degraded wait
// cycle. On subsequent requeue polls the condition is already False, so no
// duplicate is emitted.
func (rc *ReconcileContext) emitWarnOnceBeforeWait(obj client.Object, conditions []metav1.Condition, condType conditionTypes, reason, message string) {
	cond := meta.FindStatusCondition(conditions, string(condType))
	if cond == nil || cond.Status == metav1.ConditionTrue {
		rc.emitWarning(obj, reason, message)
	}
}

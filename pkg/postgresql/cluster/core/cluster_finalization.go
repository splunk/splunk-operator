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
	"context"
	"errors"
	"fmt"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// handleFinalizer owns deletion progress and finalizer release for every environment.
func handleFinalizer(ctx context.Context, rc *ReconcileContext, parent *platformv1alpha1.PostgresCluster, backupBackend BackupBackend) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(parent, PostgresClusterFinalizerName) {
		return ctrl.Result{}, nil
	}
	done, err := advanceFinalization(ctx, rc, parent, backupBackend)
	fresh := &platformv1alpha1.PostgresCluster{}
	if readErr := rc.APIReader.Get(ctx, client.ObjectKeyFromObject(parent), fresh); readErr != nil {
		return ctrl.Result{}, errors.Join(err, client.IgnoreNotFound(readErr))
	}
	if fresh.UID != parent.UID || fresh.DeletionTimestamp == nil || fresh.Generation != parent.Generation {
		return ctrl.Result{}, fmt.Errorf("finalization parent changed before completion")
	}
	if done && err == nil {
		if fresh.ResourceVersion != parent.ResourceVersion {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		before := fresh.DeepCopy()
		controllerutil.RemoveFinalizer(fresh, PostgresClusterFinalizerName)
		if err := rc.Client.Patch(ctx, fresh, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		rc.emitNormal(parent, EventCleanupComplete, fmt.Sprintf("cleanup complete for PostgresCluster %s", parent.Name))
		return ctrl.Result{}, nil
	}
	before := fresh.DeepCopy()
	reason, message := "FinalizationPending", "Waiting for managed resource finalization."
	if err != nil {
		reason, message = "FinalizationBlocked", "Finalization is blocked; preserve environments and credentials and inspect operator logs for the missing proof."
	}
	meta.SetStatusCondition(&fresh.Status.Conditions, metav1.Condition{Type: "Finalization", Status: metav1.ConditionFalse, ObservedGeneration: fresh.Generation, Reason: reason, Message: message})
	var statusErr error
	if !equality.Semantic.DeepEqual(before.Status.Conditions, fresh.Status.Conditions) {
		statusErr = rc.Client.Status().Patch(ctx, fresh, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
		if statusErr == nil && err != nil {
			rc.emitWarning(fresh, "FinalizationBlocked", message)
		}
	}
	return ctrl.Result{RequeueAfter: 3 * time.Second}, errors.Join(err, statusErr)
}

// advanceFinalization selects resource effects without releasing the parent.
func advanceFinalization(ctx context.Context, rc *ReconcileContext, parent *platformv1alpha1.PostgresCluster, backupBackend BackupBackend) (bool, error) {
	if rc.Finalizer != nil && mvutypes.BlueGreenFinalizationRequired(parent) {
		return rc.Finalizer.AdvanceFinalization(ctx, parent)
	}
	authority, err := rc.resolveClusterCard(parent)
	if err == nil {
		err = finalizeManagedEnvironments(ctx, rc, parent, authority, backupBackend)
	}
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		rc.emitWarning(parent, EventCleanupFailed, fmt.Sprintf("cleanup failed for PostgresCluster %s — check operator logs", parent.Name))
		statusErr := setStatus(ctx, rc.Client, rc.Metrics, parent, parent.Status.DeepCopy(), clusterReady, metav1.ConditionFalse, reasonClusterDeleteFailed,
			fmt.Sprintf("Failed to delete resources during cleanup: %v", err), failedClusterPhase)
		return false, errors.Join(err, statusErr)
	}
	return err == nil, err
}

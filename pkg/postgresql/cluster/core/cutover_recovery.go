/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/logging"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	usecases "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

func cutoverStage(parent *platformv1alpha1.PostgresCluster) mvutypes.CutoverStage {
	if parent == nil || len(parent.Status.PostgresMajorUpgradeStatus) == 0 {
		return mvutypes.CutoverNone
	}
	return mvutypes.CutoverStageOf(parent.Status.PostgresMajorUpgradeStatus[len(parent.Status.PostgresMajorUpgradeStatus)-1])
}

// The caller explicitly selects recovery even when class/config resolution
// failed. An unavailable plan cannot authorize a fresh commit; persisted
// routing and attempt identity still permit source restore or fail-forward.
func reconcileCutoverHandoff(ctx context.Context, rc *ReconcileContext, parent *platformv1alpha1.PostgresCluster, before mvutypes.CutoverStage, class *platformv1alpha1.PostgresClusterClass, merged *MergedConfig, configErr error, newRoleSweeper ports.NewRoleSweeperFunc, customMetricsFactory CustomMetricsFactory) (ctrl.Result, error) {
	if rc.UseCaseRegistryProvider == nil {
		return ctrl.Result{}, fmt.Errorf("consumed cutover requires its use case registry")
	}
	attemptID := parent.Status.PostgresMajorUpgradeStatus[len(parent.Status.PostgresMajorUpgradeStatus)-1].BlueGreen.AttemptID
	if err := projectCutoverInterruption(ctx, rc, parent, before); err != nil {
		return ctrl.Result{}, err
	}
	// Durable recovery already owns execution and cannot be disabled by a spec predicate.
	reconciler := usecases.NewUseCaseReconciler(nil, rc.UseCaseRegistryProvider(client.ObjectKeyFromObject(parent), parent, merged))
	if err := reconciler.Schedule(ctx); err != nil {
		return ctrl.Result{}, cutoverRuntimeError(ctx, rc, parent, before, err)
	}
	report, runErr := reconciler.Reconcile(ctx)
	result := soonerRequeue(resultFromUseCaseReport(report), ctrl.Result{RequeueAfter: time.Second})
	if err := refreshCutoverParent(ctx, rc.APIReader, parent); err != nil {
		return result, errors.Join(runErr, err)
	}
	if len(parent.Status.PostgresMajorUpgradeStatus) == 0 || parent.Status.PostgresMajorUpgradeStatus[len(parent.Status.PostgresMajorUpgradeStatus)-1].BlueGreen == nil || parent.Status.PostgresMajorUpgradeStatus[len(parent.Status.PostgresMajorUpgradeStatus)-1].BlueGreen.AttemptID != attemptID {
		return result, errors.Join(runErr, fmt.Errorf("cutover attempt changed during handoff"))
	}
	stage := cutoverStage(parent)
	if stage != before {
		if err := projectCutoverInterruption(ctx, rc, parent, stage); err != nil {
			return result, errors.Join(runErr, err)
		}
	}
	// A final restoration write is status-only. Explicitly transfer to an ordinary
	// health pass without granting another interruption or declaring Ready here.
	if stage == mvutypes.CutoverRestored {
		return soonerRequeue(result, ctrl.Result{RequeueAfter: time.Second}), runErr
	}
	if runErr != nil {
		return result, cutoverRuntimeError(ctx, rc, parent, stage, runErr)
	}
	if stage != mvutypes.CutoverTargetAvailable && stage != mvutypes.CutoverHandedOff {
		return result, nil
	}
	if configErr != nil {
		statusErr := setStatus(ctx, rc.Client, rc.Metrics, parent, parent.Status.DeepCopy(), clusterReady, metav1.ConditionFalse, reasonClusterClassNotFound, fmt.Sprintf("Class unavailable after cutover: %v", configErr), configuringClusterPhase)
		return result, errors.Join(configErr, statusErr)
	}
	released, err := reconcileAvailableCutover(ctx, rc, parent, class, newRoleSweeper, customMetricsFactory, attemptID)
	return soonerRequeue(result, released), err
}

func reconcileAvailableCutover(ctx context.Context, rc *ReconcileContext, parent *platformv1alpha1.PostgresCluster, class *platformv1alpha1.PostgresClusterClass, newRoleSweeper ports.NewRoleSweeperFunc, customMetricsFactory CustomMetricsFactory, attemptID string) (ctrl.Result, error) {
	authority, err := rc.resolveClusterCard(parent)
	if err != nil {
		return ctrl.Result{}, failCutoverReadiness(ctx, rc, parent, reasonInvalidConfiguration, err)
	}
	contracts := &reconcileContracts{Authority: authority, EnvironmentNamer: rc.EnvironmentNamer}
	if err := observeAvailableUpgradeTarget(ctx, rc.APIReader, parent, contracts, attemptID); err != nil {
		return ctrl.Result{}, failCutoverReadiness(ctx, rc, parent, reasonClusterGetFailed, err)
	}
	updateHealth := func(before *platformv1alpha1.PostgresClusterStatus, health componentHealth) error {
		return setStatusFromHealth(ctx, rc.Client, rc.Metrics, parent, before, health)
	}
	beforeHealth := parent.Status.DeepCopy()
	health, healthErr := availableUpgradeTargetHealth(parent, contracts)
	statusErr := updateHealth(beforeHealth, health)
	if err := errors.Join(healthErr, statusErr); err != nil {
		return health.Result, err
	}
	if isIntermediateState(health.State) {
		return health.Result, nil
	}
	metrics, err := customMetricsFactory(class.Spec.Provisioner, monitoring.Target{Namespace: parent.Namespace, FeatureName: parent.Name, FeatureUID: string(parent.UID), ProviderName: contracts.CNPGCluster.Name})
	if err != nil {
		return ctrl.Result{}, failCutoverReadiness(ctx, rc, parent, reasonInvalidConfiguration, err)
	}
	components := []component{
		newCustomMetricsModel(metrics, rc, updateHealth, parent, contracts),
		newManagedRolesModel(rc.Client, rc.Scheme, rc, updateHealth, parent, contracts, newRoleSweeper),
	}
	result, err := runComponents(ctx, logging.FromContext(ctx), components)
	if err != nil || result != (ctrl.Result{}) {
		return result, err
	}
	return ctrl.Result{}, advanceClusterPhase(ctx, rc, parent, readyClusterPhase)
}

// Runtime verification remains part of production readiness after handoff.
// A contradictory canonical record must not leave an earlier Ready claim live.
func cutoverRuntimeError(ctx context.Context, rc *ReconcileContext, parent *platformv1alpha1.PostgresCluster, stage mvutypes.CutoverStage, err error) error {
	if stage != mvutypes.CutoverTargetAvailable && stage != mvutypes.CutoverHandedOff {
		return err
	}
	return failCutoverReadiness(ctx, rc, parent, reasonClusterGetFailed, err)
}

func failCutoverReadiness(ctx context.Context, rc *ReconcileContext, parent *platformv1alpha1.PostgresCluster, reason conditionReasons, err error) error {
	statusErr := setStatus(ctx, rc.Client, rc.Metrics, parent, parent.Status.DeepCopy(), clusterReady, metav1.ConditionFalse, reason, fmt.Sprintf("Committed green availability cannot be verified: %v", err), configuringClusterPhase)
	return errors.Join(err, statusErr)
}

// Permission and control bootstrap do not interrupt blue. Once the durable
// fencing window starts, aggregate readiness must describe production access,
// even though the underlying CNPG environment remains healthy. Ordinary health
// reconciliation restores Ready after verified blue restoration or green
// availability; neither timestamp alone can certify current provider health.
func projectCutoverInterruption(ctx context.Context, rc *ReconcileContext, parent *platformv1alpha1.PostgresCluster, stage mvutypes.CutoverStage) error {
	if stage != mvutypes.CutoverInterrupting && stage != mvutypes.CutoverCommitted {
		return nil
	}
	return setStatus(ctx, rc.Client, rc.Metrics, parent, parent.Status.DeepCopy(), clusterReady, metav1.ConditionFalse,
		"BlueGreenSwitchover", "Blue/green switchover is fencing and switching production access.", configuringClusterPhase)
}

func refreshCutoverParent(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster) error {
	fresh := new(platformv1alpha1.PostgresCluster)
	if err := reader.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
		return fmt.Errorf("reading current cutover parent: %w", err)
	}
	if fresh.DeletionTimestamp != nil || fresh.UID != parent.UID || client.ObjectKeyFromObject(fresh) != client.ObjectKeyFromObject(parent) {
		return fmt.Errorf("cutover parent identity changed")
	}
	*parent = *fresh
	return nil
}

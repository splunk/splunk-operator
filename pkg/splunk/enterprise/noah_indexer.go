// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"fmt"
	"os"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyNoahIndexerCluster reconciles the Kubernetes resources required to
// start a Noah-selected IndexerCluster. It intentionally does not implement
// Noah bootstrap, membership, readiness, rollout, or safe scale-down.
func ApplyNoahIndexerCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (result reconcile.Result, err error) {
	result = reconcile.Result{Requeue: true, RequeueAfter: 5 * time.Second}

	eventPublisher := GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "IndexerCluster"

	isPaused := cr.GetAnnotations()[enterpriseApi.IndexerClusterPausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string) {
		status := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase:      phase,
			IsPaused:   isPaused,
			Message:    message,
			Generation: cr.GetGeneration(),
		})
		cr.Status.Phase = status.Phase
		cr.Status.Conditions = status.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "")
	defer updateCRStatus(ctx, client, cr, &err)

	if cr.Spec.NoahClusterRef == nil || cr.Spec.NoahClusterRef.Name == "" {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Noah Cluster reference is required")
		return reconcile.Result{}, splcommon.NewTerminalError(
			EventReasonValidateSpecFailed,
			"Noah IndexerCluster spec validation failed",
			fmt.Errorf("noahClusterRef.name must not be empty"),
		)
	}

	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}
	if err = validateCommonSplunkSpec(ctx, client, &cr.Spec.CommonSplunkSpec, cr); err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Indexer Cluster spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(
			EventReasonValidateSpecFailed,
			"Noah IndexerCluster spec validation failed",
			err,
		)
	}

	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-indexer", cr.GetName())

	if cr.GetDeletionTimestamp() != nil {
		if cleanupErr := DeleteOwnerReferencesForResources(ctx, client, cr, SplunkIndexer); cleanupErr != nil {
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Failed to clean up owned resources")
			return result, cleanupErr
		}
		terminating, deletionErr := k8sops.CheckForDeletion(ctx, cr, client)
		if terminating && deletionErr != nil {
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource deletion is in progress")
		} else {
			result.Requeue = false
			result.RequeueAfter = 0
		}
		return result, deletionErr
	}

	statefulSet, phase, applyErr := applyNoahIndexerResources(ctx, client, cr)
	if applyErr != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply Noah IndexerCluster resources")
		return result, applyErr
	}

	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	setPhaseAndConditions(phase, "")
	if phase == enterpriseApi.PhaseReady {
		result.Requeue = false
		result.RequeueAfter = 0
	}
	return result, nil
}

func applyNoahIndexerResources(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (*appsv1.StatefulSet, enterpriseApi.Phase, error) {
	if _, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer); err != nil {
		return nil, enterpriseApi.PhaseError, fmt.Errorf("apply Splunk config: %w", err)
	}

	services := []struct {
		headless bool
		name     string
	}{
		{headless: true, name: "headless"},
		{headless: false, name: "regular"},
	}
	for _, service := range services {
		if err := k8sops.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, service.headless)); err != nil {
			return nil, enterpriseApi.PhaseError, fmt.Errorf("apply %s Service: %w", service.name, err)
		}
	}

	defaultsConfigMap, defaultsSecret, err := ensureIndexerDefaults(ctx, client, cr)
	if err != nil {
		return nil, enterpriseApi.PhaseError, fmt.Errorf("ensure indexer defaults: %w", err)
	}

	statefulSet, err := getNoahIndexerStatefulSet(
		ctx,
		client,
		cr,
		defaultsConfigMap.AsStatefulSetOption(),
		defaultsSecret.AsStatefulSetOption(),
	)
	if err != nil {
		return nil, enterpriseApi.PhaseError, fmt.Errorf("build Noah indexer StatefulSet: %w", err)
	}

	phase, err := k8sops.ApplyStatefulSet(ctx, client, statefulSet)
	if err != nil {
		return statefulSet, enterpriseApi.PhaseError, fmt.Errorf("apply Noah indexer StatefulSet: %w", err)
	}
	// Applying the object is enough for this scaffold. Do not invoke the generic
	// pod manager: it would also authorize rollout and destructive scale-down
	// before Noah lifecycle safety exists. A StatefulSet using OnDelete may have
	// ready pods from its previous revision, so replica readiness alone cannot
	// prove that the desired pod template is running.
	if phase == enterpriseApi.PhaseReady && !noahIndexerStatefulSetConverged(statefulSet, cr.Spec.Replicas) {
		phase = enterpriseApi.PhaseUpdating
	}

	configworkflow.GarbageCollectConfigMaps(ctx, client, cr, defaultsConfigMap.Name, statefulSet.Spec.Selector)
	configworkflow.GarbageCollectSecrets(ctx, client, cr, defaultsSecret.Name, statefulSet.Spec.Selector)
	return statefulSet, phase, nil
}

// noahIndexerStatefulSetConverged reports whether the StatefulSet controller
// has observed the latest template and every desired pod is running that
// revision. It deliberately does not initiate a rollout.
func noahIndexerStatefulSetConverged(statefulSet *appsv1.StatefulSet, desiredReplicas int32) bool {
	return statefulSet != nil &&
		statefulSet.Status.ObservedGeneration >= statefulSet.Generation &&
		statefulSet.Status.UpdateRevision != "" &&
		statefulSet.Status.CurrentRevision == statefulSet.Status.UpdateRevision &&
		statefulSet.Status.UpdatedReplicas == desiredReplicas &&
		statefulSet.Status.ReadyReplicas == desiredReplicas
}

// getNoahIndexerStatefulSet constructs an indexer StatefulSet with the stable
// pod identity inputs expected by a Noah-aware Splunk image. The partial Noah
// reconcile path calls it before the remaining bootstrap configuration and
// lifecycle orchestration are available.
func getNoahIndexerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, opts ...resources.StatefulSetOption) (*appsv1.StatefulSet, error) {
	return getIndexerStatefulSet(ctx, client, cr, noahIndexerStatefulSetOptions(os.Getenv(resources.ClusterDomainEnvName), opts...)...)
}

func noahIndexerStatefulSetOptions(clusterDomain string, opts ...resources.StatefulSetOption) []resources.StatefulSetOption {
	result := make([]resources.StatefulSetOption, 0, len(opts)+1)
	result = append(result, opts...)
	return append(result, resources.WithNoahPodIdentity(clusterDomain))
}

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
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplySearchHeadClusterNoah is the top-level reconciler for a
// SearchHeadCluster that selected Noah. The regular SHC reconciler remains the
// Cluster Manager path and is not entered for Noah objects. It intentionally
// does not implement app framework, Monitoring Console, or telemetry app
// support — those are Cluster-Manager-path concerns Noah does not need.
//
// This provides stable deployer/member identity only (CSPL-5267); it does not
// yet deliver Noah service configuration to Ansible (CSPL-5268) or validate
// Noah connectivity (CSPL-5269).
func ApplySearchHeadClusterNoah(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) (result reconcile.Result, err error) {
	result = reconcile.Result{Requeue: true, RequeueAfter: 5 * time.Second}
	logger := logging.FromContext(ctx).With("func", "ApplySearchHeadClusterNoah", "name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher := GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "SearchHeadCluster"

	isPaused := cr.GetAnnotations()[enterpriseApi.SearchHeadClusterPausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string) {
		status := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: phase, IsPaused: isPaused, Message: message, Generation: cr.GetGeneration(),
		})
		cr.Status.Phase = status.Phase
		cr.Status.Conditions = status.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "")
	cr.Status.DeployerPhase = enterpriseApi.PhaseError
	defer updateCRStatus(ctx, client, cr, &err)

	if cr.Spec.NoahClusterRef == nil || cr.Spec.NoahClusterRef.Name == "" {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Noah Cluster reference is required")
		return reconcile.Result{}, splcommon.NewTerminalError(
			EventReasonValidateSpecFailed,
			"Noah Search Head Cluster spec validation failed",
			fmt.Errorf("noahClusterRef.name must not be empty"),
		)
	}

	if err = validateSearchHeadClusterSpec(ctx, client, cr); err != nil {
		eventPublisher.Warning(ctx, EventReasonValidateSpecFailed, fmt.Sprintf("Spec validation failed for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Search Head Cluster spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(EventReasonValidateSpecFailed, "Search Head Cluster spec validation failed", err)
	}

	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-search-head", cr.GetName())
	if cr.Status.Members == nil {
		cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{}
	}
	// ApplyShcSecret (invoked from inside the pod manager's Update, called by
	// applySearchHeadClusterNoah below) reads and writes these fields.
	// AdminPasswordChangedSecrets is a map: leaving it nil panics with
	// "assignment to entry in nil map" the first time a SH pod's admin
	// password differs from the namespace-scoped secret.
	if cr.Status.ShcSecretChanged == nil {
		cr.Status.ShcSecretChanged = []bool{}
	}
	if cr.Status.AdminSecretChanged == nil {
		cr.Status.AdminSecretChanged = []bool{}
	}
	if cr.Status.AdminPasswordChangedSecrets == nil {
		cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
	}

	if cr.GetDeletionTimestamp() != nil {
		setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted")
		DeleteOwnerReferencesForResources(ctx, client, cr, SplunkSearchHead)
		terminating, deletionErr := k8sops.CheckForDeletion(ctx, cr, client)
		if !terminating || deletionErr == nil {
			result = reconcile.Result{}
		}
		return result, deletionErr
	}

	var searchHeadStatefulSet *appsv1.StatefulSet
	var searchHeadPhase, deployerPhase enterpriseApi.Phase
	searchHeadPhase, deployerPhase, searchHeadStatefulSet, err = applySearchHeadClusterNoah(ctx, client, cr)
	setPhaseAndConditions(searchHeadPhase, "")
	cr.Status.DeployerPhase = deployerPhase
	if searchHeadStatefulSet != nil {
		cr.Status.ReadyReplicas = searchHeadStatefulSet.Status.ReadyReplicas
	}
	logger.InfoContext(ctx, "completed Noah runtime reconcile", "searchHeadPhase", searchHeadPhase, "deployerPhase", deployerPhase)
	return result, err
}

// applySearchHeadClusterNoah creates the services, deployer, and search-head
// StatefulSets required for Noah bootstrap, with stable pod identity
// (SPLUNK_NOAH_ENABLED, headless service name, pod name/namespace, cluster
// domain) via resources.WithNoahPodIdentity.
func applySearchHeadClusterNoah(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) (enterpriseApi.Phase, enterpriseApi.Phase, *appsv1.StatefulSet, error) {
	logger := logging.FromContext(ctx).With("func", "applySearchHeadClusterNoah", "name", cr.GetName(), "namespace", cr.GetNamespace())

	ncKey := types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.NoahClusterRef.Name}
	if err := client.Get(ctx, ncKey, &enterpriseApi.NoahCluster{}); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WarnContext(ctx, "referenced NoahCluster is not available; requeueing", "noahClusterRef", cr.Spec.NoahClusterRef.Name)
			return enterpriseApi.PhasePending, enterpriseApi.PhasePending, nil, nil
		}
		return enterpriseApi.PhaseError, enterpriseApi.PhaseError, nil, fmt.Errorf("get NoahCluster %s: %w", cr.Spec.NoahClusterRef.Name, err)
	}

	services := []struct {
		instanceType InstanceType
		headless     bool
	}{
		{instanceType: SplunkSearchHead, headless: true},
		{instanceType: SplunkSearchHead, headless: false},
		{instanceType: SplunkDeployer, headless: true},
		{instanceType: SplunkDeployer, headless: false},
	}
	for _, service := range services {
		if err := k8sops.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, service.instanceType, service.headless)); err != nil {
			return enterpriseApi.PhaseError, enterpriseApi.PhaseError, nil, fmt.Errorf("apply Noah %s service (headless=%t): %w", service.instanceType, service.headless, err)
		}
	}
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkSearchHead)
	if err != nil {
		return enterpriseApi.PhaseError, enterpriseApi.PhaseError, nil, fmt.Errorf("apply Noah SearchHeadCluster Splunk config: %w", err)
	}

	identityOption := resources.WithNoahPodIdentity(os.Getenv(resources.ClusterDomainEnvName))

	deployerStatefulSet, err := getDeployerStatefulSet(ctx, client, cr, identityOption)
	if err != nil {
		return enterpriseApi.PhaseError, enterpriseApi.PhaseError, nil, fmt.Errorf("build Noah deployer StatefulSet: %w", err)
	}
	if !deployerStatefulSet.CreationTimestamp.IsZero() {
		continueReconcile, validationErr := UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, nil)
		if validationErr != nil || !continueReconcile {
			if validationErr == nil {
				return enterpriseApi.PhasePending, enterpriseApi.PhasePending, nil, nil
			}
			return enterpriseApi.PhaseError, enterpriseApi.PhaseError, nil, validationErr
		}
	}
	deployerManager := k8sops.DefaultStatefulSetPodManager{}
	deployerPhase, err := deployerManager.Update(ctx, client, deployerStatefulSet, 1)
	if err != nil {
		return enterpriseApi.PhaseError, enterpriseApi.PhaseError, nil, fmt.Errorf("apply Noah deployer StatefulSet: %w", err)
	}

	searchHeadStatefulSet, err := getSearchHeadStatefulSet(ctx, client, cr, identityOption)
	if err != nil {
		return enterpriseApi.PhaseError, deployerPhase, nil, fmt.Errorf("build Noah search-head StatefulSet: %w", err)
	}
	searchHeadManager := newSearchHeadClusterPodManager(client, cr, namespaceScopedSecret, splclient.NewSplunkClient)
	searchHeadPhase, err := searchHeadManager.Update(ctx, client, searchHeadStatefulSet, cr.Spec.Replicas)
	if err != nil {
		return enterpriseApi.PhaseError, deployerPhase, searchHeadStatefulSet, fmt.Errorf("apply Noah search-head StatefulSet: %w", err)
	}

	// Re-arm ApplyShcSecret's per-pod change tracking once the cluster is
	// healthy, mirroring the CM path's PhaseReady reset
	// (searchheadcluster.go). Without this, ShcSecretChanged/
	// AdminSecretChanged permanently skip already-flagged pod indices after
	// their first sync, so a later pass4SymmKey/admin-password rotation
	// would silently never propagate to those pods again.
	if searchHeadPhase == enterpriseApi.PhaseReady {
		cr.Status.ShcSecretChanged = []bool{}
		cr.Status.AdminSecretChanged = []bool{}
		cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
		cr.Status.NamespaceSecretResourceVersion = namespaceScopedSecret.ObjectMeta.ResourceVersion
	}

	return searchHeadPhase, deployerPhase, searchHeadStatefulSet, nil
}

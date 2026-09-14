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

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
)

// ApplySearchHeadClusterNoah is the top-level reconciler for a
// SearchHeadCluster that selected Noah. The regular SHC reconciler remains the
// Cluster Manager path and is not entered for Noah objects. It intentionally
// does not implement app framework, Monitoring Console, or telemetry app
// support — those are Cluster-Manager-path concerns Noah does not need.
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
			splcommon.EventReasonValidateSpecFailed,
			"Noah Search Head Cluster spec validation failed",
			fmt.Errorf("noahClusterRef.name must not be empty"),
		)
	}

	if err = validateSearchHeadClusterSpec(ctx, client, cr); err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonValidateSpecFailed, fmt.Sprintf("Spec validation failed for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Search Head Cluster spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, "Search Head Cluster spec validation failed", err)
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
		// A failure here must not fall through to CheckForDeletion below: that
		// call removes finalizers once its own callbacks succeed, and doing so
		// before owner references are actually cleared would orphan them with
		// no finalizer left to retry the cleanup.
		if err = DeleteOwnerReferencesForResources(ctx, client, cr, SplunkSearchHead); err != nil {
			return reconcile.Result{}, err
		}
		terminating, deletionErr := k8sops.CheckForDeletion(ctx, cr, client)
		if !terminating || deletionErr == nil {
			result = reconcile.Result{}
		}
		return result, deletionErr
	}

	runtime, err := resolveNoahDependency(ctx, client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)

	var searchHeadStatefulSet *appsv1.StatefulSet
	// deployerPhase defaults to PhaseReady, not PhaseError: no deployer is
	// ever deployed on this path (see applySearchHeadClusterNoah's doc
	// comment), including when dependency resolution fails below and
	// applySearchHeadClusterNoah — the only other place that sets this
	// phase — never even runs.
	searchHeadPhase, deployerPhase := enterpriseApi.PhaseError, enterpriseApi.PhaseReady
	if err == nil {
		searchHeadPhase, deployerPhase, searchHeadStatefulSet, err = applySearchHeadClusterNoah(ctx, client, cr, runtime)
	}
	phaseMessage := ""
	if dependencyOutcome, handled := noahDependencyOutcome(err); handled {
		searchHeadPhase = dependencyOutcome.phase
		// deployerPhase is left as applySearchHeadClusterNoah's fixed
		// PhaseReady constant: no deployer is ever deployed on this path, so
		// a Noah dependency failure on the search-head side must not make
		// status.deployerPhase falsely report Pending/Error for a resource
		// that was never even attempted.
		phaseMessage = dependencyOutcome.message
		err = dependencyOutcome.err
		if searchHeadPhase == enterpriseApi.PhasePending {
			logger.WarnContext(ctx, "Noah dependency is not available; requeueing", "message", phaseMessage)
		}
	}
	setPhaseAndConditions(searchHeadPhase, phaseMessage)
	cr.Status.DeployerPhase = deployerPhase
	if searchHeadStatefulSet != nil {
		cr.Status.ReadyReplicas = searchHeadStatefulSet.Status.ReadyReplicas
	}
	logger.InfoContext(ctx, "completed Noah runtime reconcile", "searchHeadPhase", searchHeadPhase, "deployerPhase", deployerPhase)
	return result, err
}

// applySearchHeadClusterNoah creates the services and search-head StatefulSet
// required for Noah bootstrap. It never creates a deployer: Noah delivers
// every setting a classic deployer would otherwise push to search-heads as a
// knowledge bundle (server.conf, restmap.conf) directly to each search-head
// pod via SPLUNK_DEFAULTS_URL — the same channel splunk-ansible's Noah role
// already reads; no operator-owned init container or custom volume mount is
// involved — so a Noah SearchHeadCluster has no functional need for a
// deployer at all — this is unconditional, with no spec field controlling
// it. This is scoped to the Noah reconcile path only; the
// Cluster-Manager-path reconciler (searchheadcluster.go) always deploys a
// real deployer, unaffected by any of this.
func applySearchHeadClusterNoah(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster, runtime *configworkflow.NoahRuntime) (enterpriseApi.Phase, enterpriseApi.Phase, *appsv1.StatefulSet, error) {
	const deployerPhase = enterpriseApi.PhaseReady // no deployer is ever deployed; see doc comment above.

	noahSpec := runtime.Spec()

	services := []struct {
		instanceType InstanceType
		headless     bool
	}{
		{instanceType: SplunkSearchHead, headless: true},
		{instanceType: SplunkSearchHead, headless: false},
	}
	for _, service := range services {
		if err := k8sops.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, service.instanceType, service.headless)); err != nil {
			return enterpriseApi.PhaseError, deployerPhase, nil, fmt.Errorf("apply Noah %s service (headless=%t): %w", service.instanceType, service.headless, err)
		}
	}
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkSearchHead)
	if err != nil {
		return enterpriseApi.PhaseError, deployerPhase, nil, fmt.Errorf("apply Noah SearchHeadCluster Splunk config: %w", err)
	}

	pass4SymmKey := string(runtime.Credential())

	owner := splcommon.AsOwner(cr, true)
	// Dictionary format matches the Noah indexer path (noah_indexer.go) and
	// splunk-ansible's Noah pre-auth role, which reads splunk.conf.server.
	// content.noahService as a mapping. The credential entry below targets the
	// same path; splunk-ansible's own defaults loader deep-merges the two
	// files' nested maps, so the structural and credential fields union into
	// one [noahService] stanza before Ansible ever runs.
	credentialsSecret, err := configworkflow.EnsureSecret(ctx, client, cr, splunkconfig.NoahCredentialsConf(pass4SymmKey), &owner, resources.WithDictionaryConf())
	if err != nil {
		return enterpriseApi.PhaseError, deployerPhase, nil, fmt.Errorf("ensure Noah SearchHeadCluster credentials: %w", err)
	}

	identityOption := resources.WithNoahPodIdentity(os.Getenv(resources.ClusterDomainEnvName))

	searchHeadConfigMap, err := ensureNoahSearchHeadDefaults(ctx, client, cr, noahSpec, &owner)
	if err != nil {
		return enterpriseApi.PhaseError, deployerPhase, nil, fmt.Errorf("ensure Noah search-head defaults: %w", err)
	}
	searchHeadStatefulSet, err := getSearchHeadStatefulSet(ctx, client, cr,
		identityOption,
		searchHeadConfigMap.AsStatefulSetOption(),
		credentialsSecret.AsStatefulSetOption(),
	)
	if err != nil {
		return enterpriseApi.PhaseError, deployerPhase, nil, fmt.Errorf("build Noah search-head StatefulSet: %w", err)
	}
	// TEMPORARY: getSearchHeadEnv unconditionally points SPLUNK_DEPLOYER_URL
	// at the deployer's Service; overwrite it in place to 127.0.0.1 instead.
	// Omitting it entirely leaves the search head permanently unclustered,
	// and pointing it at any Kubernetes Service (the deployer's or even the
	// search head's own) triggers a ~6.5-minute bootstrap stall and restart
	// loop (both live-verified 2026-09-10); 127.0.0.1 avoids both by
	// resolving against the already-running local splunkd. Remove this once
	// splunk-ansible no longer needs a reachable deployer_url to bootstrap
	// shcluster-config.
	for i := range searchHeadStatefulSet.Spec.Template.Spec.Containers {
		container := &searchHeadStatefulSet.Spec.Template.Spec.Containers[i]
		if container.Name != "splunk" {
			continue
		}
		for j := range container.Env {
			if container.Env[j].Name == "SPLUNK_DEPLOYER_URL" {
				container.Env[j].Value = "127.0.0.1"
			}
		}
	}
	// With no deployer, there is no deployer StatefulSet CreationTimestamp to
	// gate the classic path's upgrade-path validation, so key the same "only
	// validate an upgrade against an already-existing StatefulSet" check off
	// the search-head StatefulSet's own CreationTimestamp instead.
	if !searchHeadStatefulSet.CreationTimestamp.IsZero() {
		continueReconcile, validationErr := UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, nil)
		if validationErr != nil || !continueReconcile {
			if validationErr == nil {
				return enterpriseApi.PhasePending, deployerPhase, nil, nil
			}
			return enterpriseApi.PhaseError, deployerPhase, nil, validationErr
		}
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

	configworkflow.GarbageCollectConfigMaps(ctx, client, cr, searchHeadConfigMap.Name, searchHeadStatefulSet.Spec.Selector)
	configworkflow.GarbageCollectSecrets(ctx, client, cr, credentialsSecret.Name, nil)
	return searchHeadPhase, deployerPhase, searchHeadStatefulSet, nil
}

func ensureNoahSearchHeadDefaults(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster, noahSpec enterpriseApi.NoahClusterSpec, owner *metav1.OwnerReference) (resources.DefaultsConfigMap, error) {
	return configworkflow.EnsureConfigMap(ctx, client, cr, splunkconfig.NoahSearchHeadConf(noahSpec.Endpoint, noahSpec.Tenant), owner, resources.WithDictionaryConf())
}

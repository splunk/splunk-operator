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

package searchheadcluster

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	// TODO: Move the App Framework and common-spec helpers to allowed lower-level
	// packages once all CRs have migrated from enterprise.
	legacyenterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	// TODO: Remove this temporary dependency once all CRs have migrated from enterprise.
	reconcileutil "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	shcworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/shc"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/telapp"
	upgrade "github.com/splunk/splunk-operator/pkg/splunk/workflow/upgrade"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const pauseRetryDelay = 30 * time.Second

// apply owns the request-level SearchHeadCluster reconciliation boundary.
func apply(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, recorder record.EventRecorder) (reconcile.Result, error) {
	logger := logging.FromContext(ctx).With("controller", "SearchHeadCluster", "name", namespacedName.Name, "namespace", namespacedName.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)

	instance := &enterpriseApi.SearchHeadCluster{}
	// Fetch the SearchHeadCluster
	if err := client.Get(ctx, namespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after
			// reconcile request. Owned objects are automatically garbage
			// collected. For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("could not load search head cluster data: %w", err)
	}

	// If the reconciliation is paused, set the Paused condition and requeue
	if instance.GetAnnotations()[enterpriseApi.SearchHeadClusterPausedAnnotation] == "true" {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: instance.Status.Phase, IsPaused: true, Message: "", Generation: instance.GetGeneration(),
		})
		instance.Status.Conditions = result.Conditions
		if err := client.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update paused status", "error", err)
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true, RequeueAfter: pauseRetryDelay}, nil
	} else if condition := meta.FindStatusCondition(instance.Status.Conditions, string(enterpriseApi.ConditionPaused)); condition != nil && condition.Status == metav1.ConditionTrue {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: instance.Status.Phase, IsPaused: false, Message: "", Generation: instance.GetGeneration(),
		})
		instance.Status.Conditions = result.Conditions
		if err := client.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update unpaused status", "error", err)
			return reconcile.Result{}, err
		}
	}

	logger.InfoContext(ctx, "start", "crVersion", instance.GetResourceVersion())
	// Pass event recorder through context
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)
	result, err := ApplySearchHeadCluster(ctx, client, instance)
	if result.Requeue && result.RequeueAfter != 0 {
		logger.InfoContext(ctx, "requeued", "periodSeconds", int(result.RequeueAfter/time.Second))
	}

	fresh := &enterpriseApi.SearchHeadCluster{}
	if fetchErr := client.Get(ctx, namespacedName, fresh); fetchErr != nil {
		if apierrors.IsNotFound(fetchErr) {
			return result, nil
		}
		logger.WarnContext(ctx, "failed to refetch CR for stalled condition update", "error", fetchErr)
		return result, fetchErr
	}
	oldConditions := append([]metav1.Condition(nil), fresh.Status.Conditions...)
	if message, ok := splcommon.TerminalMessage(err); ok {
		reason, _ := splcommon.TerminalReason(err)
		fresh.Status.Conditions = splcommon.UpsertStalledCondition(fresh.Status.Conditions, reason, message, fresh.GetGeneration())
	} else {
		fresh.Status.Conditions = splcommon.ClearStalledCondition(fresh.Status.Conditions, fresh.GetGeneration())
	}
	eventPublisher, publisherErr := k8sops.NewK8EventPublisherWithRecorder(recorder, fresh)
	if publisherErr != nil {
		logger.WarnContext(ctx, "failed to create event publisher", "error", publisherErr)
		return result, publisherErr
	}
	k8sops.EmitStalledTransitionEvents(ctx, eventPublisher, fresh.GetName(), oldConditions, fresh.Status.Conditions)
	if updateErr := client.Status().Update(ctx, fresh); updateErr != nil {
		logger.WarnContext(ctx, "failed to upsert stalled condition", "error", updateErr)
		return result, updateErr
	}
	if _, ok := splcommon.TerminalMessage(err); ok {
		return reconcile.Result{}, err
	}
	return result, err
}

// Apply is the request-level entry point used by the controller.
var Apply = apply

// Keep the existing SearchHeadCluster telemetry target unchanged after moving
// the reconcile implementation out of enterprise.
const numberOfDeployerReplicas = 1

// applySearchHeadCluster reconciles the state for a Splunk Enterprise search head cluster.
func applySearchHeadCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) (reconcile.Result, error) {
	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	logger := logging.FromContext(ctx).With("func", "ApplySearchHeadCluster")

	eventPublisher := k8sops.GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "SearchHeadCluster"

	var err error
	// Initialize phase and conditions
	isPaused := cr.GetAnnotations()[enterpriseApi.SearchHeadClusterPausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string) {
		result := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: phase, IsPaused: isPaused, Message: message, Generation: cr.GetGeneration(),
		})
		cr.Status.Phase = result.Phase
		cr.Status.Conditions = result.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "")
	cr.Status.DeployerPhase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateSearchHeadClusterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonValidateSpecFailed, fmt.Sprintf("Spec validation failed for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Search Head Cluster spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, "Search Head Cluster spec validation failed", err)
	}

	// If needed, Migrate the app framework status
	err = legacyenterprise.CheckAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, false)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "App framework migration failed")
		return result, err
	}

	// create or update general config resources
	namespaceScopedSecret, err := k8sops.ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkSearchHead)
	if err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonApplySplunkConfigFailed, fmt.Sprintf("Failed to apply general config for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration")
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// If the app framework is configured then do following things -
	// 1. Initialize the S3Clients based on providers
	// 2. Check the status of apps on remote storage.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err := legacyenterprise.InitAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, splcommon.EventReasonAppFrameworkInitFailed, fmt.Sprintf("App framework initialization failed for %s — check operator logs", cr.GetName()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			setPhaseAndConditions(enterpriseApi.PhaseError, "App framework initialization failed")
			return result, err
		}
	}

	// updates status after function completes
	cr.Status.DeployerPhase = enterpriseApi.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-search-head", cr.GetName())
	if cr.Status.Members == nil {
		cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{}
	}
	if cr.Status.ShcSecretChanged == nil {
		cr.Status.ShcSecretChanged = []bool{}
	}
	if cr.Status.AdminSecretChanged == nil {
		cr.Status.AdminSecretChanged = []bool{}
	}
	if cr.Status.AdminPasswordChangedSecrets == nil {
		cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, resources.GetSearchHeadEnv(cr), false)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console env ConfigMap during deletion")
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = legacyenterprise.UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, splcommon.SplunkSearchHead)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to clean up resources during deletion")
				return result, err
			}
		}

		k8sops.DeleteOwnerReferencesForResources(ctx, client, cr, splcommon.SplunkSearchHead)

		terminating, err := k8sops.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted")
			cr.Status.DeployerPhase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		if err != nil {
			eventPublisher.Warning(ctx, splcommon.EventReasonDeleteFailed, fmt.Sprintf("Failed to delete custom resource %s — check operator logs", cr.GetName()))
		}
		return result, err
	}

	// create or update a headless search head cluster service
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkSearchHead, true))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update Search Head headless service")
		return result, err
	}

	// create or update a regular search head cluster service
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkSearchHead, false))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update Search Head service")
		return result, err
	}

	// create or update a deployer service
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkDeployer, false))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update Deployer service")
		return result, err
	}

	// create or update statefulset for the deployer
	statefulSet, err := getDeployerStatefulSet(ctx, client, cr)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update Deployer StatefulSet")
		return result, err
	}

	// CSPL-3060 - If statefulSet is not created, avoid upgrade path validation
	if !statefulSet.CreationTimestamp.IsZero() {
		continueReconcile, err := upgrade.UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, nil)
		if err != nil || !continueReconcile {
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Upgrade path validation failed")
			} else {
				// waiting on a dependency (e.g. ClusterManager recycling) is not an error,
				// so don't leave the earlier-staged PhaseError as the persisted status on
				// either the SHC phase or the deployer phase staged at function entry
				cr.Status.DeployerPhase = enterpriseApi.PhasePending
				setPhaseAndConditions(enterpriseApi.PhasePending, "Waiting for upgrade path dependency to become ready")
			}
			return result, err
		}
	}

	deployerManager := k8sops.DefaultStatefulSetPodManager{}
	phase, err := deployerManager.Update(ctx, client, statefulSet, 1)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Deployer pods")
		return result, err
	}
	cr.Status.DeployerPhase = phase

	// create or update statefulset for the search heads
	statefulSet, err = getSearchHeadStatefulSet(ctx, client, cr)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update Search Head StatefulSet")
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = k8sops.ValidateMonitoringConsoleRef(ctx, client, statefulSet, resources.GetSearchHeadEnv(cr))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to validate Monitoring Console reference")
		return result, err
	}

	mgr := shcworkflow.NewPodManager(client, cr, namespaceScopedSecret, splclient.NewSplunkClient, shcworkflow.Operations{
		ApplyStatefulSet:             k8sops.ApplyStatefulSet,
		CheckPodsForTerminalFailures: k8sops.CheckPodsForTerminalFailures,
		UpdateStatefulSetPods:        k8sops.UpdateStatefulSetPods,
		ApplySecret:                  k8sops.ApplySecret,
	})

	// handle SHC upgrade process
	phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)

	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Search Head pods")
		return result, err
	}
	setPhaseAndConditions(phase, "")

	var finalResult *reconcile.Result
	if cr.Status.DeployerPhase == enterpriseApi.PhaseReady {
		finalResult = legacyenterprise.HandleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
	}

	if cr.Spec.MonitoringConsoleRef.Name != "" {
		_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, resources.GetSearchHeadEnv(cr), true)
		if err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console env ConfigMap")
			return result, err
		}
	}

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: splutil.GetSplunkStatefulsetName(splcommon.SplunkMonitoringConsole, cr.GetNamespace())}
		err = k8sops.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			logger.ErrorContext(ctx, "error in deleting automated MonitoringConsole resource", "error", err)
		}

		// Reset secrets related status structs
		cr.Status.ShcSecretChanged = []bool{}
		cr.Status.AdminSecretChanged = []bool{}
		cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
		cr.Status.NamespaceSecretResourceVersion = namespaceScopedSecret.ObjectMeta.ResourceVersion

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			podExecClient := splutil.GetPodExecClient(client, cr, "")
			err := telapp.AddTelApp(ctx, podExecClient, numberOfDeployerReplicas, cr)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to install Telemetry app")
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
		}
		// Update the requeue result as needed by the app framework
		if finalResult != nil {
			result = *finalResult
		}
	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}

	return result, nil
}

// ApplySearchHeadCluster is the operation seam used by focused reconciliation
// tests. Noah-backed SearchHeadClusters (spec.noahClusterRef set) have no
// deployer and reconcile through a dedicated path; classic clusters are
// unaffected.
var ApplySearchHeadCluster = func(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) (reconcile.Result, error) {
	if cr.Spec.NoahEnabled() {
		return ApplySearchHeadClusterNoah(ctx, client, cr)
	}
	return applySearchHeadCluster(ctx, client, cr)
}

// getSearchHeadStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise search heads.
func getSearchHeadStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster, opts ...resources.StatefulSetOption) (*appsv1.StatefulSet, error) {

	certMounts, err := certs.ReconcileCerts(ctx, client, cr, reconcileutil.ToCertEntries(cr.Spec.Certs, certs.AutoDNSNamesSearchHeadCluster(cr.GetName(), cr.GetNamespace())))
	if err != nil {
		return nil, fmt.Errorf("reconcile certs: %w", err)
	}

	// get search head env variables with deployer
	env := resources.GetSearchHeadEnv(cr)

	// get generic statefulset for Splunk Enterprise objects
	ss, err := k8sops.GetSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkSearchHead, cr.Spec.Replicas, env, opts...)
	if err != nil {
		return nil, err
	}
	certs.InjectCertMounts(&ss.Spec.Template, certMounts)

	return ss, nil
}

// CSPL-3652 Configure deployer resources if configured
// Use default otherwise
// Make sure to set the resources ONLY for the deployer
func setDeployerConfig(ctx context.Context, cr *enterpriseApi.SearchHeadCluster, podTemplate *corev1.PodTemplateSpec) error {
	logger := logging.FromContext(ctx).With("func", "setDeployerConfig", "name", cr.GetName(), "namespace", cr.GetNamespace())

	// Break out if this is not a deployer
	if !strings.Contains("deployer", podTemplate.Labels["app.kubernetes.io/name"]) {
		return errors.New("not a deployer, skipping setting resources")
	}
	depRes := cr.Spec.DeployerResourceSpec
	for i := range podTemplate.Spec.Containers {
		if len(depRes.Requests) != 0 {
			podTemplate.Spec.Containers[i].Resources.Requests = cr.Spec.DeployerResourceSpec.Requests
			logger.InfoContext(ctx, "setting deployer resources requests", "requests", cr.Spec.DeployerResourceSpec.Requests)
		}

		if len(depRes.Limits) != 0 {
			podTemplate.Spec.Containers[i].Resources.Limits = cr.Spec.DeployerResourceSpec.Limits
			logger.InfoContext(ctx, "setting deployer resources limits", "limits", cr.Spec.DeployerResourceSpec.Limits)
		}
	}

	// Add node affinity if configured
	if cr.Spec.DeployerNodeAffinity != nil {
		podTemplate.Spec.Affinity.NodeAffinity = cr.Spec.DeployerNodeAffinity
		logger.InfoContext(ctx, "setting deployer node affinity", "nodeAffinity", cr.Spec.DeployerNodeAffinity)
	}

	return nil
}

// getDeployerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license manager.
func getDeployerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster, opts ...resources.StatefulSetOption) (*appsv1.StatefulSet, error) {
	// Uses the same SAN set as getSearchHeadStatefulSet (SH + deployer), not
	// autoDNSNamesDeployer alone: this runs first, and EnsureCertificate is
	// create-only, so whichever call creates the cert first fixes its SANs
	// for the shared secret's lifetime.
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, reconcileutil.ToCertEntries(cr.Spec.Certs, certs.AutoDNSNamesSearchHeadCluster(cr.GetName(), cr.GetNamespace())))
	if err != nil {
		return nil, fmt.Errorf("reconcile certs: %w", err)
	}
	ss, err := k8sops.GetSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkDeployer, 1, resources.GetSearchHeadExtraEnv(cr, cr.Spec.Replicas), opts...)
	if err != nil {
		return ss, err
	}
	certs.InjectCertMounts(&ss.Spec.Template, certMounts)

	// CSPL-3562 - Set deployer resources if configured
	err = setDeployerConfig(ctx, cr, &ss.Spec.Template)
	if err != nil {
		return ss, err
	}

	// Setup App framework staging volume for apps
	resources.SetupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// validateSearchHeadClusterSpec checks validity and makes default updates to a SearchHeadClusterSpec, and returns error if something is wrong.
func validateSearchHeadClusterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) error {
	if cr.Spec.Replicas < 3 {
		cr.Spec.Replicas = 3
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := legacyenterprise.ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, false, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return reconcileutil.ValidateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

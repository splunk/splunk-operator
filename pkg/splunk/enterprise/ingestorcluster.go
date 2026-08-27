// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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
	"log/slog"
	"reflect"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splunkconfig "github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/telapp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ingestorTerminationGracePeriodSeconds = int64(300)
)

// ApplyIngestorCluster reconciles the state of an IngestorCluster custom resource
func ApplyIngestorCluster(ctx context.Context, client client.Client, cr *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
	var err error

	// Default requeue interval for the rolling eviction polling loop.
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Minute,
	}

	logger := logging.FromContext(ctx).With("func", "ApplyIngestorCluster", "name", cr.GetName(), "namespace", cr.GetNamespace())

	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	eventPublisher := GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	cr.Kind = "IngestorCluster"

	// Initialize phase and conditions (must be before validation so we can set error messages)
	isPaused := cr.GetAnnotations()[enterpriseApi.IngestorClusterPausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string) {
		result := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: phase, IsPaused: isPaused, Message: message, Generation: cr.GetGeneration(),
		})
		cr.Status.Phase = result.Phase
		cr.Status.Conditions = result.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "")

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// Validate and updates defaults for CR
	err = validateIngestorClusterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "ValidateIngestorClusterSpecFailed", "Validate Ingestor Cluster spec failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Ingestor Cluster spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(EventReasonValidateSpecFailed, "Ingestor Cluster spec validation failed", err)
	}

	// Track previous ready replicas for scaling events
	previousReadyReplicas := cr.Status.ReadyReplicas
	if cr.Status.Replicas < cr.Spec.Replicas {
		logger.InfoContext(ctx, "scaling up ingestor cluster", "previousReplicas", cr.Status.Replicas, "newReplicas", cr.Spec.Replicas)
	}
	cr.Status.Replicas = cr.Spec.Replicas

	// If needed, migrate the app framework status
	err = checkAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, true)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "App framework migration failed")
		return result, err
	}

	// If app framework is configured, then do following things
	// Initialize the S3 clients based on providers
	// Check the status of apps on remote storage
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err = initAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, "AppInfoStatusInitializationFailed", "Init and check app info status failed. Check operator logs for details.")
			cr.Status.AppContext.IsDeploymentInProgress = false
			setPhaseAndConditions(enterpriseApi.PhaseError, "App framework initialization failed")
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-ingestor", cr.GetName())

	// Create or update general config resources
	_, err = ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIngestor)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplySplunkConfigFailed", "Apply of general config failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration")
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// Check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, make([]corev1.EnvVar, 0), false)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMapFailed", "Apply of monitoring console config map failed. Check operator logs for details.")
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console env ConfigMap during deletion")
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, SplunkIngestor)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to clean up resources during deletion")
				return result, err
			}
		}

		DeleteOwnerReferencesForResources(ctx, client, cr, SplunkIngestor)

		terminating, err := k8sops.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted")
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// Create or update a headless service for ingestor cluster
	err = k8sops.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestor, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyServiceFailed", "Apply of headless service failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update headless service")
		return result, err
	}

	// Create or update a regular service for ingestor cluster
	err = k8sops.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestor, false))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyServiceFailed", "Apply of service failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update regular service")
		return result, err
	}

	// Create PodDisruptionBudget for ingestor cluster if it does not already exist
	if err = ApplyIngestorPodDisruptionBudget(ctx, client, cr); err != nil {
		eventPublisher.Warning(ctx, "ApplyPodDisruptionBudgetFailed", "Apply of PodDisruptionBudget failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create PodDisruptionBudget")
		return result, err
	}

	// If we are using App Framework and are scaling up, we should re-populate the
	// config map with all the appSource entries
	// This is done so that the new pods
	// that come up now will have the complete list of all the apps and then can
	// download and install all the apps
	// If we are scaling down, just update the auxPhaseInfo list
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 && cr.Status.ReadyReplicas > 0 {
		statefulsetName := GetSplunkStatefulsetName(SplunkIngestor, cr.GetName())

		isStatefulSetScaling, err := k8sops.IsStatefulSetScalingUpOrDown(ctx, client, cr, statefulsetName, cr.Spec.Replicas)
		if err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to determine Scaling state")
			return result, err
		}

		appStatusContext := cr.Status.AppContext

		switch isStatefulSetScaling {
		case enterpriseApi.StatefulSetScalingUp:
			// If we are indeed scaling up, then mark the deploy status to Pending
			// for all the app sources so that we add all the app sources in config map
			cr.Status.AppContext.IsDeploymentInProgress = true

			for appSrc := range appStatusContext.AppsSrcDeployStatus {
				changeAppSrcDeployInfoStatus(ctx, appSrc, appStatusContext.AppsSrcDeployStatus, enterpriseApi.RepoStateActive, enterpriseApi.DeployStatusComplete, enterpriseApi.DeployStatusPending)
				changePhaseInfo(ctx, cr.Spec.Replicas, appSrc, appStatusContext.AppsSrcDeployStatus)
			}

		// If we are scaling down, just delete the state auxPhaseInfo entries
		case enterpriseApi.StatefulSetScalingDown:
			for appSrc := range appStatusContext.AppsSrcDeployStatus {
				removeStaleEntriesFromAuxPhaseInfo(ctx, cr.Spec.Replicas, appSrc, appStatusContext.AppsSrcDeployStatus)
			}
		}
	}

	// Ensure the SOK defaults resources exist: a ConfigMap for structural SmartBus
	// config and a Secret for the credentials (both mounted via SPLUNK_DEFAULTS_URL).
	defaultsConfigMap, defaultsSecret, err := ensureIngestorDefaults(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "EnsureDefaultsFailed", "Failed to ensure defaults ConfigMap/Secret. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to ensure defaults ConfigMap/Secret")
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, splcommon.NewTerminalError(EventReasonResolveQueueObjectStorageFailed, "referenced Queue, ObjectStorage CR, or credential Secret not found", err)
		}
		return result, fmt.Errorf("ensure defaults: %w", err)
	}

	// Create or update statefulset for the ingestors
	statefulSet, err := getIngestorStatefulSet(ctx, client, cr, defaultsConfigMap.AsStatefulSetOption(), defaultsSecret.AsStatefulSetOption())
	if err != nil {
		eventPublisher.Warning(ctx, "GetIngestorStatefulSetFailed", "Get stateful set failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet")
		return result, err
	}

	// Make changes to respective mc configmap when changing/removing mcRef from spec
	err = validateMonitoringConsoleRef(ctx, client, statefulSet, make([]corev1.EnvVar, 0))
	if err != nil {
		eventPublisher.Warning(ctx, "MonitoringConsoleRefValidationFailed", "Monitoring console reference validation failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to validate Monitoring Console reference")
		return result, err
	}

	mgr := k8sops.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if err != nil {
		eventPublisher.Warning(ctx, "UpdateStatefulSetFailed", "Stateful set update failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update pods")
		return result, err
	}
	configworkflow.GarbageCollectConfigMaps(ctx, client, cr, defaultsConfigMap.Name, statefulSet.Spec.Selector)
	configworkflow.GarbageCollectSecrets(ctx, client, cr, defaultsSecret.Name, statefulSet.Spec.Selector)
	setPhaseAndConditions(phase, "")

	// Emit scaling events when phase is ready and ready replicas changed to match desired
	if phase == enterpriseApi.PhaseReady {
		desiredReplicas := cr.Spec.Replicas
		if cr.Status.ReadyReplicas == desiredReplicas && previousReadyReplicas != desiredReplicas {
			if desiredReplicas > previousReadyReplicas {
				eventPublisher.Normal(ctx, "ScaledUp",
					fmt.Sprintf("Successfully scaled %s up from %d to %d replicas", cr.GetName(), previousReadyReplicas, desiredReplicas))
			} else if desiredReplicas < previousReadyReplicas {
				eventPublisher.Normal(ctx, "ScaledDown",
					fmt.Sprintf("Successfully scaled %s down from %d to %d replicas", cr.GetName(), previousReadyReplicas, desiredReplicas))
			}
		}
	}

	// No need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {

		// Upgrade from automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = k8sops.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			eventPublisher.Warning(ctx, EventReasonMonitoringConsoleCleanupFailed, fmt.Sprintf("Failed to clean up automated monitoring console for %s — check operator logs", cr.GetName()))
			logger.ErrorContext(ctx, "delete of reference to automated MC failed", "error", err.Error())
		}
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, make([]corev1.EnvVar, 0), true)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMapFailed", "Apply of monitoring console environment config map failed. Check operator logs for details.")
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console env ConfigMap")
				return result, err
			}
		}

		finalResult := handleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
		result = *finalResult

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			podExecClient := splutil.GetPodExecClient(client, cr, "")
			err = telapp.AddTelApp(ctx, podExecClient, cr.Spec.Replicas, cr)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to install Telemetry app")
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
		}
	}

	// Poll each ingestor pod for restart_required and evict pods gated by PDB.
	// Skip while app framework deployment is in progress: ansible's REST conf
	// writes transiently set restart_required on pod startup, which would
	// otherwise trigger unintended evictions during app download/install.
	var evictResult reconcile.Result
	if !cr.Status.AppContext.IsDeploymentInProgress {
		evictResult, err = RunRollingEviction(ctx, client, cr, logger)
		if err != nil {
			eventPublisher.Warning(ctx, "RollingEvictionFailed", "Failed during rolling eviction. Check operator logs for details.")
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed during rolling eviction")
		}
	}

	// Always requeue to drive the rolling eviction polling loop, capped at 1 minute.
	// Honour a shorter interval if eviction or the app-framework requested one.
	if result.RequeueAfter == 0 || result.RequeueAfter > time.Minute {
		result.RequeueAfter = time.Minute
	}
	if evictResult.RequeueAfter > 0 && evictResult.RequeueAfter < result.RequeueAfter {
		result.RequeueAfter = evictResult.RequeueAfter
	}

	return result, err
}

// validateIngestorClusterSpec checks validity and makes default updates to a IngestorClusterSpec and returns error if something is wrong
func validateIngestorClusterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IngestorCluster) error {
	// We cannot have 0 replicas in IngestorCluster spec since this refers to number of ingestion pods in the ingestor cluster
	if cr.Spec.Replicas < 1 {
		cr.Spec.Replicas = 1
	}

	// queueRef.name and objectStorageRef.name are required; empty names would cause the reconciler
	// to silently skip queue/storage configuration without any error.
	if cr.Spec.QueueRef.Name == "" {
		return fmt.Errorf("IngestorCluster spec must reference a Queue via queueRef.name")
	}
	if cr.Spec.ObjectStorageRef.Name == "" {
		return fmt.Errorf("IngestorCluster spec must reference an ObjectStorage via objectStorageRef.name")
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// ensureIngestorDefaults resolves the IngestorCluster's SmartBus queue/object-storage
// configuration once and ensures both SOK defaults resources exist:
//   - a content-addressed ConfigMap holding the structural SmartBus config, and
//   - a content-addressed Secret holding only the credentials (access_key/secret_key).
//
// Both are immutable and mounted into every container via SPLUNK_DEFAULTS_URL.
// Returns a zero-value DefaultsConfigMap when smartbus is not configured, and a zero-value
// DefaultsSecret when no static credentials were resolved (e.g. IRSA / workload identity,
// where the Queue VolList is empty).
func ensureIngestorDefaults(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IngestorCluster) (resources.DefaultsConfigMap, resources.DefaultsSecret, error) {
	if cr.Spec.QueueRef.Name == "" {
		return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, nil
	}

	qosCfg, err := configworkflow.ResolveQueueAndObjectStorage(ctx, c, cr, cr.Spec.QueueRef, cr.Spec.ObjectStorageRef)
	if err != nil {
		return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, fmt.Errorf("resolve queue config: %w", err)
	}
	builder, err := splunkconfig.NewSmartBusConfBuilder(&qosCfg.Queue, &qosCfg.OS)
	if err != nil {
		return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
	}

	owner := splcommon.AsOwner(cr, true)

	var configMap resources.DefaultsConfigMap
	if entries := splunkconfig.IngestorConf(builder); len(entries) > 0 {
		configMap, err = configworkflow.EnsureConfigMap(ctx, c, cr, entries, &owner)
		if err != nil {
			return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
		}
	}

	var secret resources.DefaultsSecret
	if entries := splunkconfig.IngestorCredentialsConf(builder, qosCfg.AccessKey, qosCfg.SecretKey); len(entries) > 0 {
		secret, err = configworkflow.EnsureSecret(ctx, c, cr, entries, &owner)
		if err != nil {
			return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
		}
	}

	return configMap, secret, nil
}

// getIngestorStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise ingestors
func getIngestorStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IngestorCluster, opts ...resources.StatefulSetOption) (*appsv1.StatefulSet, error) {
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, toCertEntries(cr.Spec.Certs, autoDNSNames(SplunkIngestor, cr.GetName(), cr.GetNamespace(), cr.Spec.Replicas)))
	if err != nil {
		return nil, fmt.Errorf("reconcile certs: %w", err)
	}
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestor, cr.Spec.Replicas, []corev1.EnvVar{}, certMounts, opts...)
	if err != nil {
		return nil, err
	}

	// Set graceful shutdown: preStop runs splunk stop before kubelet sends SIGTERM
	gracePeriod := ingestorTerminationGracePeriodSeconds
	ss.Spec.Template.Spec.TerminationGracePeriodSeconds = &gracePeriod
	for i := range ss.Spec.Template.Spec.Containers {
		ss.Spec.Template.Spec.Containers[i].Lifecycle = &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/bin/sh", "-c", "/opt/splunk/bin/splunk stop"},
				},
			},
		}
	}

	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, nil
}

type ingestorClusterPodManager struct {
	c               splcommon.ControllerClient
	log             *slog.Logger
	cr              *enterpriseApi.IngestorCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

var newIngestorClusterPodManager = func(log *slog.Logger, cr *enterpriseApi.IngestorCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc, c splcommon.ControllerClient) ingestorClusterPodManager {
	return ingestorClusterPodManager{
		log:             log,
		cr:              cr,
		secrets:         secret,
		newSplunkClient: newSplunkClient,
		c:               c,
	}
}

func (mgr *ingestorClusterPodManager) getClient(ctx context.Context, n int32) *splclient.SplunkClient {
	logger := slog.With("func", "ingestorClusterPodManager.getClient", "name", mgr.cr.GetName(), "namespace", mgr.cr.GetNamespace())

	memberName := GetSplunkStatefulsetPodName(SplunkIngestor, mgr.cr.GetName(), n)
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, splcommon.GetSplunkServiceName(SplunkIngestor, mgr.cr.GetName(), true)))

	adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, memberName, mgr.cr.GetNamespace(), "password")
	if err != nil {
		logger.WarnContext(ctx, "couldn't retrieve the admin password from pod", "error", err)
	}

	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPwd)
}

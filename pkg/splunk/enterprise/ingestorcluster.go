/*
Copyright 2025.

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

package enterprise

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyIngestorCluster reconciles the state of an IngestorCluster custom resource
func ApplyIngestorCluster(ctx context.Context, client client.Client, cr *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
	var err error

	// Unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyIngestorCluster")

	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	eventPublisher, _ := newK8EventPublisher(client, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	cr.Kind = "IngestorCluster"

	// Validate and updates defaults for CR
	err = validateIngestorClusterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateIngestorClusterSpec", fmt.Sprintf("validate ingestor cluster spec failed %s", err.Error()))
		scopedLog.Error(err, "Failed to validate ingestor cluster spec")
		return result, err
	}

	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)
	if cr.Status.Replicas < cr.Spec.Replicas {
		cr.Status.QueueBucketAccessSecretVersion = "0"
	}
	cr.Status.Replicas = cr.Spec.Replicas

	// If needed, migrate the app framework status
	err = checkAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, true)
	if err != nil {
		return result, err
	}

	// If app framework is configured, then do following things
	// Initialize the S3 clients based on providers
	// Check the status of apps on remote storage
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err = initAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, "initAndCheckAppInfoStatus", fmt.Sprintf("init and check app info status failed %s", err.Error()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-ingestor", cr.GetName())

	// Create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIngestor)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, err
	}

	// Check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, make([]corev1.EnvVar, 0), false)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMap", fmt.Sprintf("create/update monitoring console config map failed %s", err.Error()))
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, SplunkIngestor)
			if err != nil {
				return result, err
			}
		}

		DeleteOwnerReferencesForResources(ctx, client, cr, SplunkIngestor)

		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil {
			cr.Status.Phase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// Create or update a headless service for ingestor cluster
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestor, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update headless service for ingestor cluster failed %s", err.Error()))
		return result, err
	}

	// Create or update a regular service for ingestor cluster
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestor, false))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update service for ingestor cluster failed %s", err.Error()))
		return result, err
	}

	// Create or update PodDisruptionBudget for high availability during rolling restarts
	err = ApplyPodDisruptionBudget(ctx, client, cr, SplunkIngestor, cr.Spec.Replicas)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyPodDisruptionBudget", fmt.Sprintf("create/update PodDisruptionBudget failed %s", err.Error()))
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

		isStatefulSetScaling, err := splctrl.IsStatefulSetScalingUpOrDown(ctx, client, cr, statefulsetName, cr.Spec.Replicas)
		if err != nil {
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

	// Create or update statefulset for the ingestors
	statefulSet, err := getIngestorStatefulSet(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "getIngestorStatefulSet", fmt.Sprintf("get ingestor stateful set failed %s", err.Error()))
		return result, err
	}

	// Make changes to respective mc configmap when changing/removing mcRef from spec
	err = validateMonitoringConsoleRef(ctx, client, statefulSet, make([]corev1.EnvVar, 0))
	if err != nil {
		eventPublisher.Warning(ctx, "validateMonitoringConsoleRef", fmt.Sprintf("validate monitoring console reference failed %s", err.Error()))
		return result, err
	}

	mgr := splctrl.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if err != nil {
		eventPublisher.Warning(ctx, "update", fmt.Sprintf("update stateful set failed %s", err.Error()))
		return result, err
	}
	cr.Status.Phase = phase

	// No need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		// Queue
		queue := enterpriseApi.Queue{}
		if cr.Spec.QueueRef.Name != "" {
			ns := cr.GetNamespace()
			if cr.Spec.QueueRef.Namespace != "" {
				ns = cr.Spec.QueueRef.Namespace
			}
			err = client.Get(ctx, types.NamespacedName{
				Name:      cr.Spec.QueueRef.Name,
				Namespace: ns,
			}, &queue)
			if err != nil {
				return result, err
			}
		}
		if queue.Spec.Provider == "sqs" {
			if queue.Spec.SQS.Endpoint == "" && queue.Spec.SQS.AuthRegion != "" {
				queue.Spec.SQS.Endpoint = fmt.Sprintf("https://sqs.%s.amazonaws.com", queue.Spec.SQS.AuthRegion)
			}
		}

		// Object Storage
		os := enterpriseApi.ObjectStorage{}
		if cr.Spec.ObjectStorageRef.Name != "" {
			ns := cr.GetNamespace()
			if cr.Spec.ObjectStorageRef.Namespace != "" {
				ns = cr.Spec.ObjectStorageRef.Namespace
			}
			err = client.Get(ctx, types.NamespacedName{
				Name:      cr.Spec.ObjectStorageRef.Name,
				Namespace: ns,
			}, &os)
			if err != nil {
				return result, err
			}
		}
		if os.Spec.Provider == "s3" {
			if os.Spec.S3.Endpoint == "" && queue.Spec.SQS.AuthRegion != "" {
				os.Spec.S3.Endpoint = fmt.Sprintf("https://s3.%s.amazonaws.com", queue.Spec.SQS.AuthRegion)
			}
		}

		// Secret reference
		accessKey, secretKey, version := "", "", ""
		if queue.Spec.Provider == "sqs" && cr.Spec.ServiceAccount == "" {
			for _, vol := range queue.Spec.SQS.VolList {
				if vol.SecretRef != "" {
					accessKey, secretKey, version, err = GetQueueRemoteVolumeSecrets(ctx, vol, client, cr)
					if err != nil {
						scopedLog.Error(err, "Failed to get queue remote volume secrets")
						return result, err
					}
				}
			}
		}

		// Determine if configuration needs to be updated
		configNeedsUpdate := false
		updateReason := ""

		// Check for secret changes (traditional secret-based approach)
		// For IRSA: version and Status tracking is different, handled separately below
		secretChanged := false
		if cr.Spec.ServiceAccount == "" {
			// Traditional secret-based auth: check if secret version changed
			secretChanged = cr.Status.QueueBucketAccessSecretVersion != version
			if secretChanged {
				configNeedsUpdate = true
				updateReason = "Queue/ObjectStorage secret change detected"
				scopedLog.Info("Queue/ObjectStorage secrets changed", "oldVersion", cr.Status.QueueBucketAccessSecretVersion, "newVersion", version)
			}
		} else {
			// IRSA scenario: ServiceAccount is set, no secrets used
			// Check if this is first deployment (config never applied)
			if cr.Status.QueueBucketAccessSecretVersion == "" && version == "" {
				// First deployment with IRSA - configuration needs to be applied
				configNeedsUpdate = true
				updateReason = "Initial Queue/ObjectStorage configuration for IRSA"
				scopedLog.Info("Detected first deployment with IRSA, will apply Queue/ObjectStorage configuration")
			}
			// If status is "irsa-config-applied" and version is "", config was already applied
			// Do NOT trigger updates on subsequent reconciles
		}

		// If configuration needs to be updated
		if configNeedsUpdate {
			mgr := newIngestorClusterPodManager(scopedLog, cr, namespaceScopedSecret, splclient.NewSplunkClient, client)
			err = mgr.updateIngestorConfFiles(ctx, cr, &queue.Spec, &os.Spec, accessKey, secretKey, client)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyIngestorCluster", fmt.Sprintf("Failed to update conf file for Queue/Pipeline config change after pod creation: %s", err.Error()))
				scopedLog.Error(err, "Failed to update conf file for Queue/Pipeline config change after pod creation")
				return result, err
			}

			// Only trigger rolling restart for secret changes (not for IRSA initial config)
			if secretChanged {
				// Trigger rolling restart via StatefulSet annotation update
				// Kubernetes will handle the actual rolling restart automatically
				// For secret changes, pods must restart to remount updated secrets
				scopedLog.Info("Queue/ObjectStorage secrets changed, triggering rolling restart via annotation")

				err = triggerRollingRestartViaAnnotation(ctx, client, cr, updateReason)
				if err != nil {
					scopedLog.Error(err, "Failed to trigger rolling restart for secret change")
					return result, err
				}
			}

			// Update status to mark configuration as applied
			// For IRSA, set to "irsa-config-applied" to track that config was done
			if version == "" && cr.Spec.ServiceAccount != "" {
				cr.Status.QueueBucketAccessSecretVersion = "irsa-config-applied"
			} else {
				cr.Status.QueueBucketAccessSecretVersion = version
			}
		}

		// Upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = splctrl.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			eventPublisher.Warning(ctx, "DeleteReferencesToAutomatedMCIfExists", fmt.Sprintf("delete reference to automated MC if exists failed %s", err.Error()))
			scopedLog.Error(err, "Error in deleting automated monitoring console resource")
		}
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, make([]corev1.EnvVar, 0), true)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMap", fmt.Sprintf("apply monitoring console environment config map failed %s", err.Error()))
				return result, err
			}
		}

		finalResult := handleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
		result = *finalResult

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			podExecClient := splutil.GetPodExecClient(client, cr, "")
			err = addTelApp(ctx, podExecClient, cr.Spec.Replicas, cr)
			if err != nil {
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
		}

		// Handle rolling restart mechanism - IngestorCluster uses TWO approaches:
		// 1. StatefulSet RollingUpdate: For secret changes (operator-controlled)
		//    - Already handled above via triggerRollingRestartViaAnnotation()
		// 2. Pod Eviction: For restart_required signals (SOK/Cloud config changes)
		//    - Check each pod individually and evict if restart_required is set
		//    - These two mechanisms are INDEPENDENT and can run simultaneously

		// Always check for restart_required and evict if needed
		restartErr := checkAndEvictIngestorsIfNeeded(ctx, client, cr)
		if restartErr != nil {
			scopedLog.Error(restartErr, "Failed to check/evict ingestors")
			// Don't return error, just log it - we don't want to block other operations
		}

		// Monitor rolling restart progress (for secret changes)
		restartResult, restartErr := monitorRollingRestart(ctx, client, cr)
		if restartErr != nil {
			scopedLog.Error(restartErr, "Rolling restart monitoring failed")
		}
		// If restart handler wants to requeue, honor that
		if restartResult.Requeue || restartResult.RequeueAfter > 0 {
			result = restartResult
		}
	}

	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}

	return result, nil
}

// getClient for ingestorClusterPodManager returns a SplunkClient for the member n
func (mgr *ingestorClusterPodManager) getClient(ctx context.Context, n int32) *splclient.SplunkClient {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ingestorClusterPodManager.getClient").WithValues("name", mgr.cr.GetName(), "namespace", mgr.cr.GetNamespace())

	// Get Pod Name
	memberName := GetSplunkStatefulsetPodName(SplunkIngestor, mgr.cr.GetName(), n)

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, GetSplunkServiceName(SplunkIngestor, mgr.cr.GetName(), true)))

	// Retrieve admin password from Pod
	adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, memberName, mgr.cr.GetNamespace(), "password")
	if err != nil {
		scopedLog.Error(err, "Couldn't retrieve the admin password from pod")
	}

	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPwd)
}

// validateIngestorClusterSpec checks validity and makes default updates to a IngestorClusterSpec and returns error if something is wrong
func validateIngestorClusterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IngestorCluster) error {
	// We cannot have 0 replicas in IngestorCluster spec since this refers to number of ingestion pods in the ingestor cluster
	if cr.Spec.Replicas < 3 {
		cr.Spec.Replicas = 3
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// getIngestorStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise ingestors
func getIngestorStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IngestorCluster) (*appsv1.StatefulSet, error) {
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestor, cr.Spec.Replicas, []corev1.EnvVar{})
	if err != nil {
		return nil, err
	}

	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, nil
}

// updateIngestorConfFiles checks if Queue or Pipeline inputs are created for the first time and updates the conf file if so
func (mgr *ingestorClusterPodManager) updateIngestorConfFiles(ctx context.Context, newCR *enterpriseApi.IngestorCluster, queue *enterpriseApi.QueueSpec, os *enterpriseApi.ObjectStorageSpec, accessKey, secretKey string, k8s client.Client) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("updateIngestorConfFiles").WithValues("name", newCR.GetName(), "namespace", newCR.GetNamespace())

	// Only update config for pods that exist
	readyReplicas := newCR.Status.Replicas

	// List all pods for this IngestorCluster StatefulSet
	var updateErr error
	for n := 0; n < int(readyReplicas); n++ {
		memberName := GetSplunkStatefulsetPodName(SplunkIngestor, newCR.GetName(), int32(n))
		fqdnName := splcommon.GetServiceFQDN(newCR.GetNamespace(), fmt.Sprintf("%s.%s", memberName, GetSplunkServiceName(SplunkIngestor, newCR.GetName(), true)))
		adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, k8s, memberName, newCR.GetNamespace(), "password")
		if err != nil {
			return err
		}
		splunkClient := mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(adminPwd))

		queueInputs, pipelineInputs := getQueueAndPipelineInputsForIngestorConfFiles(queue, os, accessKey, secretKey)

		for _, input := range queueInputs {
			if err := splunkClient.UpdateConfFile(scopedLog, "outputs", fmt.Sprintf("remote_queue:%s", queue.SQS.Name), [][]string{input}); err != nil {
				updateErr = err
			}
		}

		for _, input := range pipelineInputs {
			if err := splunkClient.UpdateConfFile(scopedLog, "default-mode", input[0], [][]string{{input[1], input[2]}}); err != nil {
				updateErr = err
			}
		}
	}

	return updateErr
}

// getQueueAndPipelineInputsForIngestorConfFiles returns a list of queue and pipeline inputs for ingestor pods conf files
func getQueueAndPipelineInputsForIngestorConfFiles(queue *enterpriseApi.QueueSpec, os *enterpriseApi.ObjectStorageSpec, accessKey, secretKey string) (queueInputs, pipelineInputs [][]string) {
	// Queue Inputs
	queueInputs = getQueueAndObjectStorageInputsForIngestorConfFiles(queue, os, accessKey, secretKey)

	// Pipeline inputs
	pipelineInputs = getPipelineInputsForConfFile(false)

	return
}

type ingestorClusterPodManager struct {
	c               splcommon.ControllerClient
	log             logr.Logger
	cr              *enterpriseApi.IngestorCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// newIngestorClusterPodManager creates pod manager to handle unit test cases
var newIngestorClusterPodManager = func(log logr.Logger, cr *enterpriseApi.IngestorCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc, c splcommon.ControllerClient) ingestorClusterPodManager {
	return ingestorClusterPodManager{
		log:             log,
		cr:              cr,
		secrets:         secret,
		newSplunkClient: newSplunkClient,
		c:               c,
	}
}

// getPipelineInputsForConfFile returns a list of pipeline inputs for conf file
func getPipelineInputsForConfFile(isIndexer bool) (config [][]string) {
	config = append(config,
		[]string{"pipeline:remotequeueruleset", "disabled", "false"},
		[]string{"pipeline:ruleset", "disabled", "true"},
		[]string{"pipeline:remotequeuetyping", "disabled", "false"},
		[]string{"pipeline:remotequeueoutput", "disabled", "false"},
		[]string{"pipeline:typing", "disabled", "true"},
	)
	if !isIndexer {
		config = append(config, []string{"pipeline:indexerPipe", "disabled", "true"})
	}

	return
}

// getQueueAndObjectStorageInputsForConfFiles returns a list of queue and object storage inputs for conf files
func getQueueAndObjectStorageInputsForIngestorConfFiles(queue *enterpriseApi.QueueSpec, os *enterpriseApi.ObjectStorageSpec, accessKey, secretKey string) (config [][]string) {
	queueProvider := ""
	if queue.Provider == "sqs" {
		queueProvider = "sqs_smartbus"
	}
	osProvider := ""
	if os.Provider == "s3" {
		osProvider = "sqs_smartbus"
	}
	config = append(config,
		[]string{"remote_queue.type", queueProvider},
		[]string{fmt.Sprintf("remote_queue.%s.auth_region", queueProvider), queue.SQS.AuthRegion},
		[]string{fmt.Sprintf("remote_queue.%s.endpoint", queueProvider), queue.SQS.Endpoint},
		[]string{fmt.Sprintf("remote_queue.%s.large_message_store.endpoint", osProvider), os.S3.Endpoint},
		[]string{fmt.Sprintf("remote_queue.%s.large_message_store.path", osProvider), os.S3.Path},
		[]string{fmt.Sprintf("remote_queue.%s.dead_letter_queue.name", queueProvider), queue.SQS.DLQ},
		[]string{fmt.Sprintf("remote_queue.%s.encoding_format", queueProvider), "s2s"},
		[]string{fmt.Sprintf("remote_queue.%s.max_count.max_retries_per_part", queueProvider), "4"},
		[]string{fmt.Sprintf("remote_queue.%s.retry_policy", queueProvider), "max_count"},
		[]string{fmt.Sprintf("remote_queue.%s.send_interval", queueProvider), "5s"},
	)

	if accessKey != "" && secretKey != "" {
		config = append(config, []string{fmt.Sprintf("remote_queue.%s.access_key", queueProvider), accessKey})
		config = append(config, []string{fmt.Sprintf("remote_queue.%s.secret_key", queueProvider), secretKey})
	}

	return
}

// ============================================================================
// Rolling Restart Mechanism - Helper Functions
// ============================================================================

// isPodReady checks if a pod has the Ready condition set to True
func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// shouldCheckRestartRequired determines if we should check restart_required endpoint
// Rate limits checks to avoid overwhelming Splunk REST API
func shouldCheckRestartRequired(cr *enterpriseApi.IngestorCluster) bool {
	// Don't check if restart is already in progress or failed
	if cr.Status.RestartStatus.Phase == enterpriseApi.RestartPhaseInProgress ||
		cr.Status.RestartStatus.Phase == enterpriseApi.RestartPhaseFailed {
		return false
	}

	// Check every 5 minutes
	if cr.Status.RestartStatus.LastCheckTime == nil {
		return true
	}

	elapsed := time.Since(cr.Status.RestartStatus.LastCheckTime.Time)
	checkInterval := 5 * time.Minute

	return elapsed > checkInterval
}

// checkPodsRestartRequired checks if ALL pods agree that restart is required
// This ensures configuration consistency across all replicas.
//
// Returns:
//   - allPodsAgree: true only if ALL pods are ready AND ALL agree restart is needed
//   - reason: the restart reason from Splunk
//   - error: error if we can't determine state
//
// CRITICAL: This function enforces the "ALL pods must agree" policy to prevent
// configuration split-brain scenarios. If any pod is not ready, we return false
// to wait for the cluster to stabilize before triggering restart.
func checkPodsRestartRequired(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.IngestorCluster,
) (bool, string, error) {
	scopedLog := log.FromContext(ctx).WithName("checkPodsRestartRequired")

	var allPodsReady = true
	var allReadyPodsAgreeOnRestart = true
	var restartReason string
	var readyPodsChecked int32
	var readyPodsNeedingRestart int32

	// Get Splunk admin credentials
	secret := &corev1.Secret{}
	secretName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cr.Namespace}, secret)
	if err != nil {
		scopedLog.Error(err, "Failed to get splunk secret")
		return false, "", fmt.Errorf("failed to get splunk secret: %w", err)
	}
	password := string(secret.Data["password"])

	// Check ALL pods in the StatefulSet
	for i := int32(0); i < cr.Spec.Replicas; i++ {
		podName := fmt.Sprintf("splunk-%s-ingestor-%d", cr.Name, i)

		// Get pod
		pod := &corev1.Pod{}
		err := c.Get(ctx, types.NamespacedName{Name: podName, Namespace: cr.Namespace}, pod)
		if err != nil {
			scopedLog.Error(err, "Failed to get pod", "pod", podName)
			// Pod doesn't exist or can't be retrieved - cluster not stable
			allPodsReady = false
			continue
		}

		// Check if pod is ready
		if !isPodReady(pod) {
			scopedLog.Info("Pod not ready, cannot verify restart state", "pod", podName)
			// Pod not ready - cluster not stable, wait before restart
			allPodsReady = false
			continue
		}

		// Get pod IP
		if pod.Status.PodIP == "" {
			scopedLog.Info("Pod has no IP", "pod", podName)
			allPodsReady = false
			continue
		}

		// Pod is ready, check its restart_required status
		readyPodsChecked++

		// Create SplunkClient for this pod
		managementURI := fmt.Sprintf("https://%s:8089", pod.Status.PodIP)
		splunkClient := splclient.NewSplunkClient(managementURI, "admin", password)

		// Check restart required
		restartRequired, reason, err := splunkClient.CheckRestartRequired()
		if err != nil {
			scopedLog.Error(err, "Failed to check restart required", "pod", podName)
			// Can't verify this pod's state - treat as cluster not stable
			allPodsReady = false
			continue
		}

		if restartRequired {
			scopedLog.Info("Pod needs restart", "pod", podName, "reason", reason)
			readyPodsNeedingRestart++
			restartReason = reason
		} else {
			scopedLog.Info("Pod does not need restart", "pod", podName)
			// This pod doesn't need restart - not all pods agree
			allReadyPodsAgreeOnRestart = false
		}
	}

	// Log summary
	scopedLog.Info("Restart check summary",
		"totalPods", cr.Spec.Replicas,
		"readyPodsChecked", readyPodsChecked,
		"readyPodsNeedingRestart", readyPodsNeedingRestart,
		"allPodsReady", allPodsReady,
		"allReadyPodsAgreeOnRestart", allReadyPodsAgreeOnRestart)

	// CRITICAL DECISION LOGIC:
	// Only trigger restart if:
	// 1. ALL pods are ready (cluster is stable)
	// 2. ALL ready pods agree they need restart (configuration consistency)
	//
	// This prevents split-brain scenarios where some pods have new config
	// and others don't, which can happen during:
	// - Partial app deployments
	// - Network partitions
	// - Pod restarts/failures
	// - Slow config propagation

	if !allPodsReady {
		return false, "Not all pods are ready - waiting for cluster to stabilize", nil
	}

	if readyPodsChecked == 0 {
		return false, "No ready pods found to check", nil
	}

	if !allReadyPodsAgreeOnRestart {
		return false, fmt.Sprintf("Not all pods agree on restart (%d/%d need restart)",
			readyPodsNeedingRestart, readyPodsChecked), nil
	}

	// All pods are ready AND all agree on restart - safe to proceed
	return true, restartReason, nil
}

// Note: Reload functionality is handled by the app framework.
// This operator handles restart in two ways:
// 1. StatefulSet RollingUpdate for secret changes (operator-controlled)
// 2. Pod Eviction for SOK/Cloud config changes (per-pod restart_required)

// ============================================================================
// Approach 1: StatefulSet RollingUpdate (for secret changes)
// ============================================================================

// triggerRollingRestartViaAnnotation triggers a rolling restart by updating the
// StatefulSet pod template annotation. Kubernetes StatefulSet controller will
// handle the actual rolling restart automatically.
// This is used for SECRET CHANGES where all pods need coordinated restart.
func triggerRollingRestartViaAnnotation(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.IngestorCluster,
	reason string,
) error {
	scopedLog := log.FromContext(ctx).WithName("triggerRollingRestart")

	// Get current StatefulSet
	statefulSetName := fmt.Sprintf("splunk-%s-ingestor", cr.Name)
	statefulSet := &appsv1.StatefulSet{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      statefulSetName,
		Namespace: cr.Namespace,
	}, statefulSet)
	if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Update pod template with restart annotation
	// This triggers StatefulSet controller to recreate pods
	if statefulSet.Spec.Template.Annotations == nil {
		statefulSet.Spec.Template.Annotations = make(map[string]string)
	}

	now := time.Now().Format(time.RFC3339)
	statefulSet.Spec.Template.Annotations["splunk.com/restartedAt"] = now
	statefulSet.Spec.Template.Annotations["splunk.com/restartReason"] = reason

	scopedLog.Info("Triggering rolling restart via StatefulSet update",
		"reason", reason,
		"timestamp", now,
		"replicas", *statefulSet.Spec.Replicas)

	// Update StatefulSet - Kubernetes handles rolling restart automatically
	err = c.Update(ctx, statefulSet)
	if err != nil {
		return fmt.Errorf("failed to update StatefulSet: %w", err)
	}

	// Update CR status to track restart
	cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseInProgress
	cr.Status.RestartStatus.LastRestartTime = &metav1.Time{Time: time.Now()}
	cr.Status.RestartStatus.Message = fmt.Sprintf("Rolling restart triggered: %s", reason)

	return nil
}

// monitorRollingRestart monitors the progress of a rolling restart by checking
// StatefulSet status. Returns when restart is complete or if it should requeue.
func monitorRollingRestart(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.IngestorCluster,
) (reconcile.Result, error) {
	scopedLog := log.FromContext(ctx).WithName("monitorRollingRestart")

	// Only monitor if restart is in progress
	if cr.Status.RestartStatus.Phase != enterpriseApi.RestartPhaseInProgress {
		return reconcile.Result{}, nil
	}

	// Get StatefulSet
	statefulSetName := fmt.Sprintf("splunk-%s-ingestor", cr.Name)
	statefulSet := &appsv1.StatefulSet{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      statefulSetName,
		Namespace: cr.Namespace,
	}, statefulSet)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Check if rolling update is complete
	// All these conditions must be true for completion:
	// 1. UpdatedReplicas == Replicas (all pods have new template)
	// 2. ReadyReplicas == Replicas (all pods are ready)
	// 3. CurrentRevision == UpdateRevision (update is done)
	if statefulSet.Status.UpdatedReplicas == statefulSet.Status.Replicas &&
		statefulSet.Status.ReadyReplicas == statefulSet.Status.Replicas &&
		statefulSet.Status.CurrentRevision == statefulSet.Status.UpdateRevision {

		// Rolling restart complete!
		scopedLog.Info("Rolling restart completed successfully",
			"replicas", statefulSet.Status.Replicas,
			"ready", statefulSet.Status.ReadyReplicas)

		cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseCompleted
		cr.Status.RestartStatus.Message = fmt.Sprintf(
			"Rolling restart completed successfully for %d pods",
			statefulSet.Status.Replicas)

		return reconcile.Result{}, nil
	}

	// Still in progress - update status with current progress
	cr.Status.RestartStatus.Message = fmt.Sprintf(
		"Rolling restart in progress: %d/%d pods updated, %d/%d ready",
		statefulSet.Status.UpdatedReplicas,
		statefulSet.Status.Replicas,
		statefulSet.Status.ReadyReplicas,
		statefulSet.Status.Replicas)

	scopedLog.Info("Rolling restart in progress",
		"updated", statefulSet.Status.UpdatedReplicas,
		"ready", statefulSet.Status.ReadyReplicas,
		"target", statefulSet.Status.Replicas,
		"currentRevision", statefulSet.Status.CurrentRevision,
		"updateRevision", statefulSet.Status.UpdateRevision)

	// Check again in 30 seconds
	return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
}

// ============================================================================
// Approach 2: Pod Eviction (for SOK/Cloud config changes)
// ============================================================================

// checkAndEvictIngestorsIfNeeded checks each ingestor pod individually for
// restart_required and evicts pods that need restart.
// This is used for SOK/CLOUD CONFIG CHANGES where pods signal independently.
func checkAndEvictIngestorsIfNeeded(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.IngestorCluster,
) error {
	scopedLog := log.FromContext(ctx).WithName("checkAndEvictIngestorsIfNeeded")

	// Check if StatefulSet rolling update is already in progress
	// Skip pod eviction to avoid conflict with Kubernetes StatefulSet controller
	statefulSetName := fmt.Sprintf("splunk-%s-ingestor", cr.Name)
	statefulSet := &appsv1.StatefulSet{}
	err := c.Get(ctx, types.NamespacedName{Name: statefulSetName, Namespace: cr.Namespace}, statefulSet)
	if err != nil {
		scopedLog.Error(err, "Failed to get StatefulSet")
		return err
	}

	// Check if rolling update in progress
	// Special handling for partition-based updates: if partition is set,
	// UpdatedReplicas < Replicas is always true, so we check if the partitioned
	// pods are all updated
	if statefulSet.Status.UpdatedReplicas < *statefulSet.Spec.Replicas {
		// Check if partition is configured
		if statefulSet.Spec.UpdateStrategy.RollingUpdate != nil &&
			statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition != nil {

			partition := *statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition
			expectedUpdatedReplicas := *statefulSet.Spec.Replicas - partition

			// If all pods >= partition are updated, rolling update is "complete" for the partition
			// Allow eviction of pods < partition
			if statefulSet.Status.UpdatedReplicas >= expectedUpdatedReplicas {
				scopedLog.Info("Partition-based update complete, allowing eviction of non-partitioned pods",
					"partition", partition,
					"updatedReplicas", statefulSet.Status.UpdatedReplicas,
					"expectedUpdated", expectedUpdatedReplicas)
				// Fall through to eviction logic below
			} else {
				scopedLog.Info("Partition-based rolling update in progress, skipping eviction",
					"partition", partition,
					"updatedReplicas", statefulSet.Status.UpdatedReplicas,
					"expectedUpdated", expectedUpdatedReplicas)
				return nil
			}
		} else {
			// No partition - normal rolling update in progress
			scopedLog.Info("StatefulSet rolling update in progress, skipping pod eviction to avoid conflict",
				"updatedReplicas", statefulSet.Status.UpdatedReplicas,
				"desiredReplicas", *statefulSet.Spec.Replicas)
			return nil
		}
	}

	// Get admin credentials
	secret := &corev1.Secret{}
	secretName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	err = c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cr.Namespace}, secret)
	if err != nil {
		scopedLog.Error(err, "Failed to get splunk secret")
		return fmt.Errorf("failed to get splunk secret: %w", err)
	}
	password := string(secret.Data["password"])

	// Check each ingestor pod individually (NO consensus needed)
	for i := int32(0); i < cr.Spec.Replicas; i++ {
		podName := fmt.Sprintf("splunk-%s-ingestor-%d", cr.Name, i)

		// Get pod
		pod := &corev1.Pod{}
		err := c.Get(ctx, types.NamespacedName{Name: podName, Namespace: cr.Namespace}, pod)
		if err != nil {
			scopedLog.Error(err, "Failed to get pod", "pod", podName)
			continue // Skip pods that don't exist
		}

		// Only check running pods
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Check if pod is ready
		if !isPodReady(pod) {
			continue
		}

		// Get pod IP
		if pod.Status.PodIP == "" {
			continue
		}

		// Check if THIS specific pod needs restart
		managementURI := fmt.Sprintf("https://%s:8089", pod.Status.PodIP)
		splunkClient := splclient.NewSplunkClient(managementURI, "admin", password)

		restartRequired, message, err := splunkClient.CheckRestartRequired()
		if err != nil {
			scopedLog.Error(err, "Failed to check restart required", "pod", podName)
			continue
		}

		if !restartRequired {
			continue // This pod is fine
		}

		scopedLog.Info("Pod needs restart, evicting",
			"pod", podName, "message", message)

		// Evict the pod - PDB automatically protects
		err = evictPod(ctx, c, pod)
		if err != nil {
			if isPDBViolation(err) {
				scopedLog.Info("PDB blocked eviction, will retry",
					"pod", podName)
				continue
			}
			return err
		}

		scopedLog.Info("Pod eviction initiated", "pod", podName)

		// Only evict ONE pod per reconcile
		// Next reconcile (5s later) will check remaining pods
		return nil
	}

	return nil
}

// evictPod evicts a pod using Kubernetes Eviction API
// The Eviction API automatically checks PodDisruptionBudget
func evictPod(ctx context.Context, c client.Client, pod *corev1.Pod) error {
	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
	}

	// Eviction API automatically checks PDB
	// If PDB would be violated, this returns an error
	return c.SubResource("eviction").Create(ctx, pod, eviction)
}

// isPDBViolation checks if an error is due to PDB violation
func isPDBViolation(err error) bool {
	// Eviction API returns HTTP 429 Too Many Requests when PDB blocks eviction
	// This is more reliable than string matching error messages
	return k8serrors.IsTooManyRequests(err)
}

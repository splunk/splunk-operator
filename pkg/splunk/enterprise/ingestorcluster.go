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

		secretChanged := cr.Status.QueueBucketAccessSecretVersion != version

		// If queue is updated
		if secretChanged {
			mgr := newIngestorClusterPodManager(scopedLog, cr, namespaceScopedSecret, splclient.NewSplunkClient, client)
			err = mgr.updateIngestorConfFiles(ctx, cr, &queue.Spec, &os.Spec, accessKey, secretKey, client)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyIngestorCluster", fmt.Sprintf("Failed to update conf file for Queue/Pipeline config change after pod creation: %s", err.Error()))
				scopedLog.Error(err, "Failed to update conf file for Queue/Pipeline config change after pod creation")
				return result, err
			}

			// Trigger rolling restart mechanism instead of restarting all pods immediately
			// This ensures proper rolling restart with PDB respect
			scopedLog.Info("Queue/ObjectStorage secrets changed, triggering rolling restart")
			now := metav1.Now()
			cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhasePending
			cr.Status.RestartStatus.TotalPods = cr.Spec.Replicas
			cr.Status.RestartStatus.PodsNeedingRestart = cr.Spec.Replicas
			cr.Status.RestartStatus.PodsRestarted = 0
			cr.Status.RestartStatus.LastCheckTime = &now
			cr.Status.RestartStatus.Message = fmt.Sprintf("Secret change detected, %d pods need restart", cr.Spec.Replicas)

			cr.Status.QueueBucketAccessSecretVersion = version
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

		// Handle rolling restart mechanism
		// This runs after everything else is ready to check for config changes
		restartResult, restartErr := handleRollingRestart(ctx, client, cr)
		if restartErr != nil {
			scopedLog.Error(restartErr, "Rolling restart handler failed")
			// Don't return error, just log it - we don't want to block other operations
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

// checkPodsRestartRequired checks which pods need restart
// Returns count of pods needing restart, total pods checked, error
func checkPodsRestartRequired(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.IngestorCluster,
) (int32, int32, error) {
	scopedLog := log.FromContext(ctx).WithName("checkPodsRestartRequired")

	var podsNeedingRestart int32
	totalPods := cr.Spec.Replicas

	// Get Splunk admin credentials
	secret := &corev1.Secret{}
	secretName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cr.Namespace}, secret)
	if err != nil {
		scopedLog.Error(err, "Failed to get splunk secret")
		return 0, totalPods, fmt.Errorf("failed to get splunk secret: %w", err)
	}
	password := string(secret.Data["password"])

	for i := int32(0); i < cr.Spec.Replicas; i++ {
		podName := fmt.Sprintf("splunk-%s-ingestor-%d", cr.Name, i)

		// Get pod
		pod := &corev1.Pod{}
		err := c.Get(ctx, types.NamespacedName{Name: podName, Namespace: cr.Namespace}, pod)
		if err != nil {
			scopedLog.Error(err, "Failed to get pod", "pod", podName)
			continue // Skip this pod
		}

		// Check if pod is ready
		if !isPodReady(pod) {
			scopedLog.Info("Pod not ready, skipping restart check", "pod", podName)
			continue
		}

		// Get pod IP
		if pod.Status.PodIP == "" {
			scopedLog.Info("Pod has no IP, skipping", "pod", podName)
			continue
		}

		// Create SplunkClient for this pod
		managementURI := fmt.Sprintf("https://%s:8089", pod.Status.PodIP)
		splunkClient := splclient.NewSplunkClient(managementURI, "admin", password)

		// Check restart required
		restartRequired, reason, err := splunkClient.CheckRestartRequired()
		if err != nil {
			scopedLog.Error(err, "Failed to check restart required", "pod", podName)
			continue // Don't fail entire check, just skip this pod
		}

		if restartRequired {
			scopedLog.Info("Pod needs restart", "pod", podName, "reason", reason)
			podsNeedingRestart++
		}
	}

	return podsNeedingRestart, totalPods, nil
}

// tryReloadAllPods attempts to reload configuration on all pods sequentially
// Returns: count of pods still needing restart after reload, error
func tryReloadAllPods(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.IngestorCluster,
) (int32, error) {
	scopedLog := log.FromContext(ctx).WithName("tryReloadAllPods")

	// Get Splunk admin credentials
	secret := &corev1.Secret{}
	secretName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cr.Namespace}, secret)
	if err != nil {
		scopedLog.Error(err, "Failed to get splunk secret")
		return 0, fmt.Errorf("failed to get splunk secret: %w", err)
	}
	password := string(secret.Data["password"])

	var podsStillNeedingRestart int32

	// Iterate pods sequentially (one at a time)
	for i := int32(0); i < cr.Spec.Replicas; i++ {
		podName := fmt.Sprintf("splunk-%s-ingestor-%d", cr.Name, i)

		// Get pod
		pod := &corev1.Pod{}
		err := c.Get(ctx, types.NamespacedName{Name: podName, Namespace: cr.Namespace}, pod)
		if err != nil {
			scopedLog.Error(err, "Failed to get pod", "pod", podName)
			continue
		}

		// Check if pod is ready
		if !isPodReady(pod) {
			scopedLog.Info("Pod not ready, skipping reload", "pod", podName)
			continue
		}

		// Get pod IP
		if pod.Status.PodIP == "" {
			scopedLog.Info("Pod has no IP, skipping", "pod", podName)
			continue
		}

		// Create SplunkClient for this pod
		managementURI := fmt.Sprintf("https://%s:8089", pod.Status.PodIP)
		splunkClient := splclient.NewSplunkClient(managementURI, "admin", password)

		// Check if restart is required before reload
		restartRequired, reason, err := splunkClient.CheckRestartRequired()
		if err != nil {
			scopedLog.Error(err, "Failed to check restart required", "pod", podName)
			continue
		}

		if !restartRequired {
			scopedLog.Info("Pod does not need restart, skipping reload", "pod", podName)
			continue
		}

		scopedLog.Info("Pod needs restart, attempting reload", "pod", podName, "reason", reason)

		// Attempt reload
		err = splunkClient.ReloadSplunk()
		if err != nil {
			scopedLog.Error(err, "Failed to reload Splunk", "pod", podName)
			// Count as still needing restart if reload fails
			podsStillNeedingRestart++
			continue
		}

		scopedLog.Info("Successfully triggered reload on pod", "pod", podName)

		// Wait 5 seconds for reload to settle
		time.Sleep(5 * time.Second)

		// Check if restart is still required after reload
		restartRequired, reason, err = splunkClient.CheckRestartRequired()
		if err != nil {
			scopedLog.Error(err, "Failed to check restart required after reload", "pod", podName)
			// Assume still needs restart if check fails
			podsStillNeedingRestart++
			continue
		}

		if restartRequired {
			scopedLog.Info("Pod still needs restart after reload, will require full restart", "pod", podName, "reason", reason)
			podsStillNeedingRestart++
		} else {
			scopedLog.Info("Reload successful, pod no longer needs restart", "pod", podName)
		}
	}

	return podsStillNeedingRestart, nil
}

// ============================================================================
// Rolling Restart State Machine
// ============================================================================

// handleRollingRestart manages the rolling restart state machine
// Returns: result with requeue info, error
func handleRollingRestart(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.IngestorCluster,
) (reconcile.Result, error) {
	scopedLog := log.FromContext(ctx).WithName("handleRollingRestart")
	now := metav1.Now()

	// State: None or Completed - Check if restart is needed
	if cr.Status.RestartStatus.Phase == enterpriseApi.RestartPhaseNone ||
		cr.Status.RestartStatus.Phase == enterpriseApi.RestartPhaseCompleted {

		// Rate limit: only check every 5 minutes
		if !shouldCheckRestartRequired(cr) {
			return reconcile.Result{}, nil
		}

		scopedLog.Info("Checking if pods need restart")
		cr.Status.RestartStatus.LastCheckTime = &now

		podsNeedingRestart, totalPods, err := checkPodsRestartRequired(ctx, c, cr)
		if err != nil {
			cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseFailed
			cr.Status.RestartStatus.Message = fmt.Sprintf("Failed to check restart status: %v", err)
			return reconcile.Result{RequeueAfter: 1 * time.Minute}, err
		}

		if podsNeedingRestart == 0 {
			scopedLog.Info("No pods need restart")
			cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseNone
			cr.Status.RestartStatus.Message = ""
			cr.Status.RestartStatus.TotalPods = totalPods
			cr.Status.RestartStatus.PodsNeedingRestart = 0
			cr.Status.RestartStatus.PodsRestarted = 0
			return reconcile.Result{}, nil
		}

		// Transition to Pending
		scopedLog.Info("Pods need restart, transitioning to Pending", "podsNeedingRestart", podsNeedingRestart)
		cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhasePending
		cr.Status.RestartStatus.TotalPods = totalPods
		cr.Status.RestartStatus.PodsNeedingRestart = podsNeedingRestart
		cr.Status.RestartStatus.PodsRestarted = 0
		cr.Status.RestartStatus.Message = fmt.Sprintf("%d/%d pods need restart", podsNeedingRestart, totalPods)

		// Requeue immediately to start reload attempt
		return reconcile.Result{Requeue: true}, nil
	}

	// State: Pending - Try reload first
	if cr.Status.RestartStatus.Phase == enterpriseApi.RestartPhasePending {
		scopedLog.Info("Attempting sequential reload on all pods")
		cr.Status.RestartStatus.Message = fmt.Sprintf("Attempting reload on %d pods", cr.Status.RestartStatus.PodsNeedingRestart)

		podsStillNeedingRestart, err := tryReloadAllPods(ctx, c, cr)
		if err != nil {
			cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseFailed
			cr.Status.RestartStatus.Message = fmt.Sprintf("Reload failed: %v", err)
			return reconcile.Result{RequeueAfter: 1 * time.Minute}, err
		}

		if podsStillNeedingRestart == 0 {
			// Success! All pods reloaded successfully
			scopedLog.Info("All pods reloaded successfully, no restarts needed")
			cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseCompleted
			cr.Status.RestartStatus.Message = fmt.Sprintf("Configuration reloaded successfully on all %d pods, no restarts needed", cr.Status.RestartStatus.TotalPods)
			cr.Status.RestartStatus.PodsNeedingRestart = 0
			return reconcile.Result{}, nil
		}

		// Some pods still need restart, transition to InProgress
		scopedLog.Info("Reload helped but some pods still need restart", "podsStillNeedingRestart", podsStillNeedingRestart)
		cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseInProgress
		cr.Status.RestartStatus.PodsNeedingRestart = podsStillNeedingRestart
		cr.Status.RestartStatus.PodsRestarted = 0
		cr.Status.RestartStatus.LastRestartTime = &now
		cr.Status.RestartStatus.Message = fmt.Sprintf("Reloaded all pods, %d/%d still need restart", podsStillNeedingRestart, cr.Status.RestartStatus.TotalPods)

		// Requeue immediately to start restart
		return reconcile.Result{Requeue: true}, nil
	}

	// State: InProgress - Perform rolling restart one pod per reconcile
	if cr.Status.RestartStatus.Phase == enterpriseApi.RestartPhaseInProgress {
		// Timeout check: fail if stuck for more than 2 hours
		if cr.Status.RestartStatus.LastRestartTime != nil {
			elapsed := time.Since(cr.Status.RestartStatus.LastRestartTime.Time)
			if elapsed > 2*time.Hour {
				scopedLog.Error(nil, "Rolling restart timeout", "elapsed", elapsed)
				cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseFailed
				cr.Status.RestartStatus.Message = fmt.Sprintf("Restart timeout after %v", elapsed)
				return reconcile.Result{}, fmt.Errorf("rolling restart timeout")
			}
		}

		scopedLog.Info("Rolling restart in progress", "podsRestarted", cr.Status.RestartStatus.PodsRestarted, "podsNeedingRestart", cr.Status.RestartStatus.PodsNeedingRestart)

		// Find next pod that needs restart
		var podToRestart *corev1.Pod
		var podIndex int32 = -1

		for i := int32(0); i < cr.Spec.Replicas; i++ {
			podName := fmt.Sprintf("splunk-%s-ingestor-%d", cr.Name, i)
			pod := &corev1.Pod{}
			err := c.Get(ctx, types.NamespacedName{Name: podName, Namespace: cr.Namespace}, pod)
			if err != nil {
				scopedLog.Error(err, "Failed to get pod", "pod", podName)
				continue
			}

			// Check if pod is ready and has IP
			if !isPodReady(pod) || pod.Status.PodIP == "" {
				continue
			}

			// Check if this pod still needs restart
			secret := &corev1.Secret{}
			secretName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
			err = c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cr.Namespace}, secret)
			if err != nil {
				scopedLog.Error(err, "Failed to get splunk secret")
				continue
			}
			password := string(secret.Data["password"])

			managementURI := fmt.Sprintf("https://%s:8089", pod.Status.PodIP)
			splunkClient := splclient.NewSplunkClient(managementURI, "admin", password)

			restartRequired, reason, err := splunkClient.CheckRestartRequired()
			if err != nil {
				scopedLog.Error(err, "Failed to check restart required", "pod", podName)
				continue
			}

			if restartRequired {
				scopedLog.Info("Found pod needing restart", "pod", podName, "reason", reason)
				podToRestart = pod
				podIndex = i
				break
			}
		}

		// No pod found needing restart - we're done!
		if podToRestart == nil {
			scopedLog.Info("All pods restarted successfully")
			cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseCompleted
			cr.Status.RestartStatus.Message = fmt.Sprintf("Rolling restart completed successfully for %d pods", cr.Status.RestartStatus.TotalPods)
			cr.Status.RestartStatus.PodsNeedingRestart = 0
			return reconcile.Result{}, nil
		}

		// Restart this pod using eviction API
		scopedLog.Info("Restarting pod", "pod", podToRestart.Name, "index", podIndex, "progress", fmt.Sprintf("%d/%d", cr.Status.RestartStatus.PodsRestarted+1, cr.Status.RestartStatus.PodsNeedingRestart))

		err := c.Delete(ctx, podToRestart)
		if err != nil && !k8serrors.IsNotFound(err) {
			scopedLog.Error(err, "Failed to delete pod", "pod", podToRestart.Name)
			cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseFailed
			cr.Status.RestartStatus.Message = fmt.Sprintf("Failed to restart pod %s: %v", podToRestart.Name, err)
			return reconcile.Result{RequeueAfter: 1 * time.Minute}, err
		}

		// Update progress
		cr.Status.RestartStatus.PodsRestarted++
		cr.Status.RestartStatus.Message = fmt.Sprintf("Restarting pod %d (%d/%d)", podIndex, cr.Status.RestartStatus.PodsRestarted, cr.Status.RestartStatus.PodsNeedingRestart)

		// Requeue after 30 seconds to wait for pod to restart and become ready
		scopedLog.Info("Pod deleted, waiting for restart", "pod", podToRestart.Name, "requeueAfter", "30s")
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// State: Failed - Wait before retrying
	if cr.Status.RestartStatus.Phase == enterpriseApi.RestartPhaseFailed {
		scopedLog.Info("Restart operation failed, will retry", "message", cr.Status.RestartStatus.Message)
		// Reset to None after 5 minutes to allow retry
		if cr.Status.RestartStatus.LastCheckTime != nil {
			elapsed := time.Since(cr.Status.RestartStatus.LastCheckTime.Time)
			if elapsed > 5*time.Minute {
				scopedLog.Info("Resetting failed restart status for retry")
				cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseNone
				cr.Status.RestartStatus.Message = ""
			}
		}
		return reconcile.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	return reconcile.Result{}, nil
}

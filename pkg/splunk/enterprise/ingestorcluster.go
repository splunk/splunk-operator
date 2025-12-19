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

		// Can not override original queue spec due to comparison in the later code
		queueCopy := queue
		if queueCopy.Spec.Provider == "sqs" {
			if queueCopy.Spec.SQS.Endpoint == "" && queueCopy.Spec.SQS.AuthRegion != "" {
				queueCopy.Spec.SQS.Endpoint = fmt.Sprintf("https://sqs.%s.amazonaws.com", queueCopy.Spec.SQS.AuthRegion)
			}
		}

		// Large Message Store
		os := enterpriseApi.ObjectStorage{}
		if cr.Spec.ObjectStorageRef.Name != "" {
			ns := cr.GetNamespace()
			if cr.Spec.ObjectStorageRef.Namespace != "" {
				ns = cr.Spec.ObjectStorageRef.Namespace
			}
			err = client.Get(context.Background(), types.NamespacedName{
				Name:      cr.Spec.ObjectStorageRef.Name,
				Namespace: ns,
			}, &os)
			if err != nil {
				return result, err
			}
		}

		// Can not override original queue spec due to comparison in the later code
		osCopy := os
		if osCopy.Spec.Provider == "s3" {
			if osCopy.Spec.S3.Endpoint == "" && queueCopy.Spec.SQS.AuthRegion != "" {
				osCopy.Spec.S3.Endpoint = fmt.Sprintf("https://s3.%s.amazonaws.com", queue.Spec.SQS.AuthRegion)
			}
		}

		// If bus is updated
		if cr.Status.Queue == nil || cr.Status.ObjectStorage == nil || !reflect.DeepEqual(*cr.Status.Queue, queue.Spec) || !reflect.DeepEqual(*cr.Status.ObjectStorage, os.Spec) {
			mgr := newIngestorClusterPodManager(scopedLog, cr, namespaceScopedSecret, splclient.NewSplunkClient, client)
			err = mgr.handlePushQueueChange(ctx, cr, queueCopy, osCopy, client)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyIngestorCluster", fmt.Sprintf("Failed to update conf file for Queue/Pipeline config change after pod creation: %s", err.Error()))
				scopedLog.Error(err, "Failed to update conf file for Queue/Pipeline config change after pod creation")
				return result, err
			}

			for i := int32(0); i < cr.Spec.Replicas; i++ {
				ingClient := mgr.getClient(ctx, i)
				err = ingClient.RestartSplunk()
				if err != nil {
					return result, err
				}
				scopedLog.Info("Restarted splunk", "ingestor", i)
			}

			cr.Status.Queue = &queue.Spec
			cr.Status.ObjectStorage = &os.Spec
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
	// We cannot have 0 replicas in IngestorCluster spec since this refers to number of ingestion pods in an ingestor cluster
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

// Checks if only Queue or Pipeline config changed, and updates the conf file if so
func (mgr *ingestorClusterPodManager) handlePushQueueChange(ctx context.Context, newCR *enterpriseApi.IngestorCluster, queue enterpriseApi.Queue, os enterpriseApi.ObjectStorage, k8s client.Client) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("handlePushQueueChange").WithValues("name", newCR.GetName(), "namespace", newCR.GetNamespace())

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

		newCrStatusQueue := newCR.Status.Queue
		if newCrStatusQueue == nil {
			newCrStatusQueue = &enterpriseApi.QueueSpec{}
		}
		newCrStatusObjectStorage := newCR.Status.ObjectStorage
		if newCrStatusObjectStorage == nil {
			newCrStatusObjectStorage = &enterpriseApi.ObjectStorageSpec{}
		}

		afterDelete := false
		if (queue.Spec.SQS.Name != "" && newCrStatusQueue.SQS.Name != "" && queue.Spec.SQS.Name != newCrStatusQueue.SQS.Name) ||
			(queue.Spec.Provider != "" && newCrStatusQueue.Provider != "" && queue.Spec.Provider != newCrStatusQueue.Provider) {
			if err := splunkClient.DeleteConfFileProperty(scopedLog, "outputs", fmt.Sprintf("remote_queue:%s", newCrStatusQueue.SQS.Name)); err != nil {
				updateErr = err
			}
			afterDelete = true
		}

		// Secret reference
		s3AccessKey, s3SecretKey := "", ""
		if queue.Spec.Provider == "sqs" && newCR.Spec.ServiceAccount == "" {
			for _, vol := range queue.Spec.SQS.VolList {
				if vol.SecretRef != "" {
					s3AccessKey, s3SecretKey, err = GetQueueRemoteVolumeSecrets(ctx, vol, k8s, newCR)
					if err != nil {
						scopedLog.Error(err, "Failed to get queue remote volume secrets")
						return err
					}
				}
			}
		}

		queueChangedFields, pipelineChangedFields := getChangedQueueFieldsForIngestor(&queue, &os, newCrStatusQueue, newCrStatusObjectStorage,afterDelete, s3AccessKey, s3SecretKey)

		for _, pbVal := range queueChangedFields {
			if err := splunkClient.UpdateConfFile(scopedLog, "outputs", fmt.Sprintf("remote_queue:%s", queue.Spec.SQS.Name), [][]string{pbVal}); err != nil {
				updateErr = err
			}
		}

		for _, field := range pipelineChangedFields {
			if err := splunkClient.UpdateConfFile(scopedLog, "default-mode", field[0], [][]string{{field[1], field[2]}}); err != nil {
				updateErr = err
			}
		}
	}

	// Do NOT restart Splunk
	return updateErr
}

// getChangedBusFieldsForIngestor returns a list of changed bus and pipeline fields for ingestor pods
func getChangedQueueFieldsForIngestor(queue *enterpriseApi.Queue, os *enterpriseApi.ObjectStorage, queueStatus *enterpriseApi.QueueSpec, osStatus *enterpriseApi.ObjectStorageSpec, afterDelete bool, s3AccessKey, s3SecretKey string) (queueChangedFields, pipelineChangedFields [][]string) {
	// Push changed bus fields
	queueChangedFields = pushQueueChanged(queueStatus, &queue.Spec, osStatus, &os.Spec, afterDelete, s3AccessKey, s3SecretKey)

	// Always changed pipeline fields
	pipelineChangedFields = pipelineConfig(false)

	return
}

type ingestorClusterPodManager struct {
	c               splcommon.ControllerClient
	log             logr.Logger
	cr              *enterpriseApi.IngestorCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// newIngestorClusterPodManager function to create pod manager this is added to write unit test case
var newIngestorClusterPodManager = func(log logr.Logger, cr *enterpriseApi.IngestorCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc, c splcommon.ControllerClient) ingestorClusterPodManager {
	return ingestorClusterPodManager{
		log:             log,
		cr:              cr,
		secrets:         secret,
		newSplunkClient: newSplunkClient,
		c:               c,
	}
}

func pipelineConfig(isIndexer bool) (output [][]string) {
	output = append(output,
		[]string{"pipeline:remotequeueruleset", "disabled", "false"},
		[]string{"pipeline:ruleset", "disabled", "true"},
		[]string{"pipeline:remotequeuetyping", "disabled", "false"},
		[]string{"pipeline:remotequeueoutput", "disabled", "false"},
		[]string{"pipeline:typing", "disabled", "true"},
	)
	if !isIndexer {
		output = append(output, []string{"pipeline:indexerPipe", "disabled", "true"})
	}
	return output
}

func pushQueueChanged(oldQueue, newQueue *enterpriseApi.QueueSpec, oldOS, newOS *enterpriseApi.ObjectStorageSpec, afterDelete bool, s3AccessKey, s3SecretKey string) (output [][]string) {
	queueProvider := ""
	if newQueue.Provider == "sqs" {
		queueProvider = "sqs_smartbus"
	}
	osProvider := ""
	if newOS.Provider == "s3" {
		osProvider = "sqs_smartbus"
	}

	if oldQueue.Provider != newQueue.Provider || afterDelete {
		output = append(output, []string{"remote_queue.type", queueProvider})
	}
	if !reflect.DeepEqual(oldQueue.SQS.VolList, newQueue.SQS.VolList) || afterDelete {
		if s3AccessKey != "" && s3SecretKey != "" {
			output = append(output, []string{fmt.Sprintf("remote_queue.%s.access_key", queueProvider), s3AccessKey})
			output = append(output, []string{fmt.Sprintf("remote_queue.%s.secret_key", queueProvider), s3SecretKey})
		}
	}
	if oldQueue.SQS.AuthRegion != newQueue.SQS.AuthRegion || afterDelete {
		output = append(output, []string{fmt.Sprintf("remote_queue.%s.auth_region", queueProvider), newQueue.SQS.AuthRegion})
	}
	if newQueue.SQS.Endpoint != "" && (oldQueue.SQS.Endpoint != newQueue.SQS.Endpoint || afterDelete) {
		output = append(output, []string{fmt.Sprintf("remote_queue.%s.endpoint", queueProvider), newQueue.SQS.Endpoint})
	}
	if newOS.S3.Endpoint != "" && (oldOS.S3.Endpoint != newOS.S3.Endpoint || afterDelete) {
		output = append(output, []string{fmt.Sprintf("remote_queue.%s.large_message_store.endpoint", osProvider), newOS.S3.Endpoint})
	}
	if oldOS.S3.Path != newOS.S3.Path || afterDelete {
		output = append(output, []string{fmt.Sprintf("remote_queue.%s.large_message_store.path", osProvider), newOS.S3.Path})
	}
	if oldQueue.SQS.DLQ != newQueue.SQS.DLQ || afterDelete {
		output = append(output, []string{fmt.Sprintf("remote_queue.%s.dead_letter_queue.name", queueProvider), newQueue.SQS.DLQ})
	}

	output = append(output,
		[]string{fmt.Sprintf("remote_queue.%s.encoding_format", queueProvider), "s2s"},
		[]string{fmt.Sprintf("remote_queue.%s.max_count.max_retries_per_part", queueProvider), "4"},
		[]string{fmt.Sprintf("remote_queue.%s.retry_policy", queueProvider), "max_count"},
		[]string{fmt.Sprintf("remote_queue.%s.send_interval", queueProvider), "5s"})

	return output
}

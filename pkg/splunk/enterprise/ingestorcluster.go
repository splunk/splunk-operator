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
	"reflect"
	"strings"
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

	eventPublisher := GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	cr.Kind = "IngestorCluster"

	// Validate and updates defaults for CR
	err = validateIngestorClusterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "SpecValidationFailure", fmt.Sprintf("validation of ingestor cluster spec failed due to %s", err.Error()))
		scopedLog.Error(err, "Failed to validate ingestor cluster spec")
		return result, err
	}

	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError

	// Track previous replicas for scaling events
	previousReplicas := cr.Status.Replicas

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)
	if cr.Status.Replicas < cr.Spec.Replicas {
		scopedLog.Info("Scaling up ingestor cluster", "previousReplicas", previousReplicas, "newReplicas", cr.Spec.Replicas)
		cr.Status.CredentialSecretVersion = "0"
		cr.Status.ServiceAccount = ""
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
			eventPublisher.Warning(ctx, "AppInfoStatusInitializationFailure", fmt.Sprintf("init and check app info status failed due to %s", err.Error()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-ingestor", cr.GetName())

	// Create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIngestor)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplySplunkConfigFailure", fmt.Sprintf("apply of general config failed due to %s", err.Error()))
		scopedLog.Error(err, "create or update general config failed")
		return result, err
	}

	// Check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, make([]corev1.EnvVar, 0), false)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMapFailure", fmt.Sprintf("apply of monitoring console config map failed due to %s", err.Error()))
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
		eventPublisher.Warning(ctx, "ApplyServiceFailure", fmt.Sprintf("apply of headless service failed due to %s", err.Error()))
		return result, err
	}

	// Create or update a regular service for ingestor cluster
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestor, false))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyServiceFailure", fmt.Sprintf("apply of service failed due to %s", err.Error()))
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
		eventPublisher.Warning(ctx, "GetIngestorStatefulSetFailure", fmt.Sprintf("get stateful set failed due to %s", err.Error()))
		return result, err
	}

	// Make changes to respective mc configmap when changing/removing mcRef from spec
	err = validateMonitoringConsoleRef(ctx, client, statefulSet, make([]corev1.EnvVar, 0))
	if err != nil {
		eventPublisher.Warning(ctx, "MonitoringConsoleRefValidationFailure", fmt.Sprintf("monitoring console reference validation failed due to %s", err.Error()))
		return result, err
	}

	mgr := splctrl.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if err != nil {
		eventPublisher.Warning(ctx, "UpdateStatefulSetFailure", fmt.Sprintf("stateful set update failed due to %s", err.Error()))
		return result, err
	}
	cr.Status.Phase = phase

	// Emit scaling events when phase is ready
	if phase == enterpriseApi.PhaseReady {
		desiredReplicas := cr.Spec.Replicas
		if desiredReplicas > previousReplicas && cr.Status.ReadyReplicas == desiredReplicas {
			eventPublisher.Normal(ctx, "ScaledUp",
				fmt.Sprintf("Successfully scaled %s up from %d to %d replicas", cr.GetName(), previousReplicas, desiredReplicas))
		} else if desiredReplicas < previousReplicas && cr.Status.ReadyReplicas == desiredReplicas {
			eventPublisher.Normal(ctx, "ScaledDown",
				fmt.Sprintf("Successfully scaled %s down from %d to %d replicas", cr.GetName(), previousReplicas, desiredReplicas))
		}
	}

	// No need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		qosCfg, err := ResolveQueueAndObjectStorage(ctx, client, cr, cr.Spec.QueueRef, cr.Spec.ObjectStorageRef, cr.Spec.ServiceAccount)
		if err != nil {
			scopedLog.Error(err, "Failed to resolve Queue/ObjectStorage config")
			return result, err
		}
		scopedLog.Info("Resolved Queue/ObjectStorage config", "queue", qosCfg.Queue, "objectStorage", qosCfg.OS, "version", qosCfg.Version, "serviceAccount", cr.Spec.ServiceAccount)

		secretChanged := cr.Status.CredentialSecretVersion != qosCfg.Version
		serviceAccountChanged := cr.Status.ServiceAccount != cr.Spec.ServiceAccount

		scopedLog.Info("Checking for changes", "previousCredentialSecretVersion", cr.Status.CredentialSecretVersion, "previousServiceAccount", cr.Status.ServiceAccount, "secretChanged", secretChanged, "serviceAccountChanged", serviceAccountChanged)

		// If queue is updated
		if secretChanged || serviceAccountChanged {
			ingMgr := newIngestorClusterPodManager(scopedLog, cr, namespaceScopedSecret, splclient.NewSplunkClient, client)
			err = ingMgr.updateIngestorConfFiles(ctx, cr, &qosCfg.Queue, &qosCfg.OS, qosCfg.AccessKey, qosCfg.SecretKey, client)
			if err != nil {
				eventPublisher.Warning(ctx, "UpdateConfFilesFailure", fmt.Sprintf("failed to update conf file for Queue/Pipeline config due to %s", err.Error()))
				scopedLog.Error(err, "Failed to update conf file for Queue/Pipeline config")
				return result, err
			}

			eventPublisher.Normal(ctx, "QueueConfigUpdated",
				fmt.Sprintf("Queue/Pipeline configuration updated for %d ingestors", cr.Spec.Replicas))
			scopedLog.Info("Queue/Pipeline configuration updated", "readyReplicas", cr.Status.ReadyReplicas)

			for i := int32(0); i < cr.Spec.Replicas; i++ {
				ingClient := ingMgr.getClient(ctx, i)
				err = ingClient.RestartSplunk()
				if err != nil {
					return result, err
				}
				scopedLog.Info("Restarted splunk", "ingestor", i)
			}

			eventPublisher.Normal(ctx, "IngestorsRestarted",
				fmt.Sprintf("Restarted Splunk on %d ingestor pods", cr.Spec.Replicas))

			cr.Status.CredentialSecretVersion = qosCfg.Version
			cr.Status.ServiceAccount = cr.Spec.ServiceAccount

			scopedLog.Info("Updated status", "credentialSecretVersion", cr.Status.CredentialSecretVersion, "serviceAccount", cr.Status.ServiceAccount)
		}

		// Upgrade from automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = splctrl.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			eventPublisher.Warning(ctx, "MCReferencesDeletionFailure", fmt.Sprintf("reference to automated MC if exists failed due to %s", err.Error()))
			scopedLog.Error(err, "Error in deleting automated monitoring console resource")
		}
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, make([]corev1.EnvVar, 0), true)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMapFailure", fmt.Sprintf("apply of monitoring console environment config map failed due to %s", err.Error()))
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
	// We cannot have 0 replicas in IngestorCluster spec since this refers to number of ingestion pods in the ingestor cluster
	if cr.Spec.Replicas < 1 {
		cr.Spec.Replicas = 1
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
			if !strings.Contains(input[0], "access_key") && !strings.Contains(input[0], "secret_key") {
				scopedLog.Info("Updating queue input in outputs.conf", "input", input)
			}
			if err := splunkClient.UpdateConfFile(scopedLog, "outputs", fmt.Sprintf("remote_queue:%s", queue.SQS.Name), [][]string{input}); err != nil {
				updateErr = err
			}
		}

		for _, input := range pipelineInputs {
			scopedLog.Info("Updating pipeline input in default-mode.conf", "input", input)
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
	authRegion := ""
	endpoint := ""
	dlq := ""
	if queue.Provider == "sqs" {
		queueProvider = "sqs_smartbus"
		authRegion = queue.SQS.AuthRegion
		endpoint = queue.SQS.Endpoint
		dlq = queue.SQS.DLQ
	}

	path := ""
	osEndpoint := ""
	osProvider := ""
	if os.Provider == "s3" {
		osProvider = "sqs_smartbus"
		osEndpoint = os.S3.Endpoint
		path = os.S3.Path
		if !strings.HasPrefix(path, "s3://") {
			path = "s3://" + path
		}
	}

	config = append(config,
		[]string{"remote_queue.type", queueProvider},
		[]string{fmt.Sprintf("remote_queue.%s.auth_region", queueProvider), authRegion},
		[]string{fmt.Sprintf("remote_queue.%s.endpoint", queueProvider), endpoint},
		[]string{fmt.Sprintf("remote_queue.%s.large_message_store.endpoint", osProvider), osEndpoint},
		[]string{fmt.Sprintf("remote_queue.%s.large_message_store.path", osProvider), path},
		[]string{fmt.Sprintf("remote_queue.%s.dead_letter_queue.name", queueProvider), dlq},
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

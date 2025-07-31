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

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splkClient "github.com/splunk/splunk-operator/pkg/splunk/client"
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

	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// Validate and updates defaults for CR
	err = validateIngestorClusterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateIngestorClusterSpec", fmt.Sprintf("validate ingestor cluster spec failed %s", err.Error()))
		scopedLog.Error(err, "Failed to validate ingestor cluster spec")
		return result, err
	}

	cr.Status.Replicas = cr.Spec.Replicas

	// If needed, Migrate the app framework status
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

	// Mount queue configuration as defaults
	cr.Spec.Defaults = fmt.Sprintf(`
		default.yml: |
			splunk:
			conf:
				- key: outputs
				value:
					directory: /opt/splunk/etc/system/local
					content:
					"remote_queue:%s":
						remote_queue.type: %s
						remote_queue.%s.encoding_format: s2s
						remote_queue.%s.auth_region: %s
						remote_queue.%s.endpoint: %s
						remote_queue.%s.large_message_store.endpoint: %s
						remote_queue.%s.large_message_store.path: %s
						remote_queue.%s.dead_letter_queue.name: %s
						remote_queue.%s.max_count.max_retries_per_part: %d
						remote_queue.%s.retry_policy: %s
						remote_queue.%s.send_interval: %s
				- key: default-mode
				value:
					directory: /opt/splunk/etc/system/local
					content:
					"pipeline:remotequeueruleset":
						disabled: "%t"
					"pipeline:ruleset":
						disabled: "%t"
					"pipeline:remotequeuetyping":
						disabled: "%t"
					"pipeline:remotequeueoutput":
						disabled: "%t"
					"pipeline:typing":
						disabled: "%t"
					"pipeline:indexerPipe":
						disabled: "%t"`,
		cr.Spec.PushBus.SQS.QueueName,
		cr.Spec.PushBus.Type,
		cr.Spec.PushBus.Type,
		cr.Spec.PushBus.Type, cr.Spec.PushBus.SQS.AuthRegion,
		cr.Spec.PushBus.Type, cr.Spec.PushBus.SQS.Endpoint,
		cr.Spec.PushBus.Type, cr.Spec.PushBus.SQS.LargeMessageStoreEndpoint,
		cr.Spec.PushBus.Type, cr.Spec.PushBus.SQS.LargeMessageStorePath,
		cr.Spec.PushBus.Type, cr.Spec.PushBus.SQS.DeadLetterQueueName,
		cr.Spec.PushBus.Type, cr.Spec.PushBus.SQS.MaxRetriesPerPart,
		cr.Spec.PushBus.Type, cr.Spec.PushBus.SQS.RetryPolicy,
		cr.Spec.PushBus.Type, cr.Spec.PushBus.SQS.SendInterval,
		cr.Spec.PipelineConfig.RemoteQueueRuleset,
		cr.Spec.PipelineConfig.RuleSet,
		cr.Spec.PipelineConfig.RemoteQueueTyping,
		cr.Spec.PipelineConfig.RemoteQueueOutput,
		cr.Spec.PipelineConfig.Typing,
		cr.Spec.PipelineConfig.IndexerPipe)

	// Create or update general config resources
	_, err = ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIngestor)
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

	// Fetch the old IngestorCluster from the API server
	oldCR := &enterpriseApi.IngestorCluster{}
	err = client.Get(ctx, types.NamespacedName{Name: cr.GetName(), Namespace: cr.GetNamespace()}, oldCR)
	if err == nil && oldCR.ResourceVersion != "" {
		// Create a SplunkClient for this cluster (adjust as needed)
		updated, err := handlePushBusOrPipelineConfigChange(ctx, oldCR, cr, client)
		if err != nil {
			scopedLog.Error(err, "Failed to update conf file for PushBus/Pipeline config change")
			return result, err
		}
		if updated {
			// Only PushBus or Pipeline changed, config updated, skip restart logic
			return result, nil
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

// validateIngestorClusterSpec checks validity and makes default updates to a IngestorClusterSpec and returns error if something is wrong
func validateIngestorClusterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IngestorCluster) error {
	// We cannot have 0 replicas in IngestorCluster spec since this refers to number of ingestion pods in an ingestor cluster
	if cr.Spec.Replicas == 0 {
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
	return getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestor, cr.Spec.Replicas, make([]corev1.EnvVar, 0))
}

// Checks if only PushBus or Pipeline config changed, and updates the conf file if so
func handlePushBusOrPipelineConfigChange(ctx context.Context, oldCR, newCR *enterpriseApi.IngestorCluster, k8s client.Client) (bool, error) {
	pushBusChanged := !reflect.DeepEqual(oldCR.Spec.PushBus, newCR.Spec.PushBus)
	pipelineChanged := !reflect.DeepEqual(oldCR.Spec.PipelineConfig, newCR.Spec.PipelineConfig)

	// If neither changed, nothing to do
	if !pushBusChanged && !pipelineChanged {
		return false, nil
	}

	// If only PushBus or Pipeline changed (not other fields)
	oldCopy := oldCR.DeepCopy()
	newCopy := newCR.DeepCopy()
	oldCopy.Spec.PushBus = enterpriseApi.PushBusSpec{}
	newCopy.Spec.PushBus = enterpriseApi.PushBusSpec{}
	oldCopy.Spec.PipelineConfig = enterpriseApi.PipelineConfigSpec{}
	newCopy.Spec.PipelineConfig = enterpriseApi.PipelineConfigSpec{}

	if reflect.DeepEqual(oldCopy.Spec, newCopy.Spec) {
		// List all pods for this IngestorCluster StatefulSet
		var updateErr error
		readyReplicas := oldCR.Status.ReadyReplicas
		for n := 0; n < int(readyReplicas); n++ {
			memberName := GetSplunkStatefulsetPodName(SplunkIngestor, oldCR.GetName(), int32(n))
			fqdnName := splcommon.GetServiceFQDN(oldCR.GetNamespace(), fmt.Sprintf("%s.%s", memberName, GetSplunkServiceName(SplunkSearchHead, oldCR.GetName(), false)))
			adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, k8s, memberName, oldCR.GetNamespace(), "password")
			if err != nil {
				return true, err
			}
			splunkClient := splkClient.NewSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(adminPwd))

			// Only PushBus or Pipeline changed
			if pipelineChanged {
				if err := splunkClient.UpdateConfFile("default-mode"); err != nil {
					updateErr = err
				}
			}
			if pushBusChanged {
				if err := splunkClient.UpdateConfFile("outputs.conf"); err != nil {
					updateErr = err
				}
			}
		}
		// Do NOT restart Splunk
		return true, updateErr
	}

	// Other fields changed, so don't handle here
	return false, nil
}

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
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

	// Check if deletion has been requested — must be before ApplySplunkConfig to avoid
	// creating resources (e.g. manual-app-update ConfigMap) in a terminating namespace.
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

	// Create or update general config resources
	_, err = ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIngestor)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
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

	// Resolve Queue/ObjectStorage CRs and build/apply queue config ConfigMap.
	// Called unconditionally — splctrl.ApplyConfigMap skips write when content unchanged.
	// Controller watches (Queue CR, ObjectStorage CR, Secret) ensure reconcile only fires on real changes.
	qosCfg, err := ResolveQueueAndObjectStorage(ctx, client, cr, cr.Spec.QueueRef, cr.Spec.ObjectStorageRef, cr.Spec.ServiceAccount)
	if err != nil {
		eventPublisher.Warning(ctx, "ResolveQueueAndObjectStorage", fmt.Sprintf("failed to resolve queue/OS config: %s", err.Error()))
		return result, err
	}
	_, err = buildAndApplyIngestorQueueConfigMap(ctx, client, cr, qosCfg)
	if err != nil {
		eventPublisher.Warning(ctx, "buildAndApplyIngestorQueueConfigMap", fmt.Sprintf("failed to build/apply queue config ConfigMap: %s", err.Error()))
		return result, err
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

// applyQueueConfigMap is a shared helper that builds and idempotently applies a queue config
// ConfigMap with the given name, namespace, owner, and data.
// Returns (true, nil) when content changed, (false, nil) when unchanged.
// Both IngestorCluster and ClusterManager (for IndexerCluster bundle push) use this.
func applyQueueConfigMap(ctx context.Context, c splcommon.ControllerClient, configMapName, namespace string, owner splcommon.MetaObject, data map[string]string) (bool, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: data,
	}
	configMap.SetOwnerReferences(append(configMap.GetOwnerReferences(), splcommon.AsOwner(owner, true)))
	return splctrl.ApplyConfigMap(ctx, c, configMap)
}

// buildAndApplyIngestorQueueConfigMap builds and idempotently applies the ingestor queue
// config ConfigMap. Returns (true, nil) when content changed, (false, nil) when unchanged.
// Called unconditionally on every reconcile — splctrl.ApplyConfigMap is the idempotency gate.
func buildAndApplyIngestorQueueConfigMap(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IngestorCluster, qosCfg *QueueOSConfig) (bool, error) {
	outputsConf := generateIngestorOutputsConf(&qosCfg.Queue, &qosCfg.OS, qosCfg.AccessKey, qosCfg.SecretKey)
	defaultModeConf := generateIngestorDefaultModeConf()
	data := map[string]string{
		"app.conf":          generateQueueConfigAppConf("Splunk Operator Ingestor Queue Config"),
		"outputs.conf":      outputsConf,
		"default-mode.conf": defaultModeConf,
		"local.meta":        generateQueueConfigLocalMeta(),
	}
	return applyQueueConfigMap(ctx, c, GetIngestorQueueConfigMapName(cr.GetName()), cr.GetNamespace(), cr, data)
}

// setupIngestorInitContainer adds the queue config init container and ConfigMap volume to the
// StatefulSet pod template. The init container symlinks conf files and copies local.meta before
// Splunk starts, enabling zero-restart first-boot configuration.
func setupIngestorInitContainer(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IngestorCluster, ss *appsv1.StatefulSet) error {
	// Determine etc volume mount name (ephemeral vs PVC — mirrors setupInitContainer pattern)
	var etcVolMntName string
	if cr.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.EphemeralStorage {
		etcVolMntName = fmt.Sprintf(splcommon.SplunkMountNamePrefix, splcommon.EtcVolumeStorage)
	} else {
		etcVolMntName = fmt.Sprintf(splcommon.PvcNamePrefix, splcommon.EtcVolumeStorage)
	}

	// Add ConfigMap volume to pod spec. defaultMode 420 (0644) matches what Kubernetes
	// applies by default after admission — setting it explicitly prevents a spurious
	// StatefulSet diff on every reconcile (current=420 vs revised=nil).
	queueConfigVolName := "mnt-splunk-queue-config"
	defaultMode := int32(420)
	ss.Spec.Template.Spec.Volumes = append(ss.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: queueConfigVolName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: GetIngestorQueueConfigMapName(cr.GetName()),
				},
				DefaultMode: &defaultMode,
			},
		},
	})

	// Security context — same as setupInitContainer in util.go
	runAsUser := int64(41812)
	runAsNonRoot := true
	privileged := false

	initContainer := corev1.Container{
		Name:            "init-ingestor-queue-config",
		Image:           ss.Spec.Template.Spec.Containers[0].Image,
		ImagePullPolicy: ss.Spec.Template.Spec.Containers[0].ImagePullPolicy,
		Command:         []string{"bash", "-c", commandForIngestorQueueConfig},
		VolumeMounts: []corev1.VolumeMount{
			{Name: etcVolMntName, MountPath: "/opt/splk/etc"},
			{Name: queueConfigVolName, MountPath: ingestorQueueConfigMountPath},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("0.25"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                &runAsUser,
			RunAsNonRoot:             &runAsNonRoot,
			AllowPrivilegeEscalation: &[]bool{false}[0],
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
				Add:  []corev1.Capability{"NET_BIND_SERVICE"},
			},
			Privileged: &privileged,
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
	}
	ss.Spec.Template.Spec.InitContainers = append(ss.Spec.Template.Spec.InitContainers, initContainer)

	// Also mount the ConfigMap volume in the main Splunk container so that the symlinks
	// created by the init container resolve correctly at runtime.
	ss.Spec.Template.Spec.Containers[0].VolumeMounts = append(ss.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      queueConfigVolName,
		MountPath: ingestorQueueConfigMountPath,
		ReadOnly:  true,
	})

	// Set ingestorQueueConfigRev annotation to ConfigMap ResourceVersion.
	// When ConfigMap content changes the RV increments, the annotation changes,
	// and the Restart EPIC detects the pod template diff and triggers a rolling restart.
	cmRV, err := splctrl.GetConfigMapResourceVersion(ctx, c, types.NamespacedName{
		Name:      GetIngestorQueueConfigMapName(cr.GetName()),
		Namespace: cr.GetNamespace(),
	})
	if err == nil && cmRV != "" {
		if ss.Spec.Template.ObjectMeta.Annotations == nil {
			ss.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}
		ss.Spec.Template.ObjectMeta.Annotations[ingestorQueueConfigRevAnnotation] = cmRV
	}

	return nil
}

// getIngestorStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise ingestors
func getIngestorStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IngestorCluster) (*appsv1.StatefulSet, error) {
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestor, cr.Spec.Replicas, []corev1.EnvVar{})
	if err != nil {
		return nil, err
	}

	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	// Add queue config ConfigMap volume + init container + ingestorQueueConfigRev annotation
	if err := setupIngestorInitContainer(ctx, client, cr, ss); err != nil {
		return nil, err
	}

	return ss, nil
}

// computeIngestorConfChecksum returns a SHA-256 hex digest of the combined conf content.
// Stored in local.meta so Splunk detects content changes on app load.
func computeIngestorConfChecksum(outputsConf, defaultModeConf string) string {
	h := sha256.New()
	h.Write([]byte(outputsConf))
	h.Write([]byte(defaultModeConf))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// buildQueueConfStanza builds an INI stanza for a remote_queue conf file.
// stanzaName is used as the stanza header (e.g. queue.SQS.Name), kvPairs is the list of key-value pairs.
// Shared by both IngestorCluster (outputs.conf) and ClusterManager (outputs.conf + inputs.conf).
func buildQueueConfStanza(stanzaName string, kvPairs [][]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[remote_queue:%s]\n", stanzaName)
	for _, kv := range kvPairs {
		fmt.Fprintf(&b, "%s = %s\n", kv[0], kv[1])
	}
	return b.String()
}

// generateIngestorOutputsConf builds outputs.conf INI content.
// Reuses getQueueAndObjectStorageInputsForIngestorConfFiles for key-value pairs.
// Credentials embedded when non-empty (same pattern as GetSmartstoreVolumesConfig).
func generateIngestorOutputsConf(queue *enterpriseApi.QueueSpec, os *enterpriseApi.ObjectStorageSpec, accessKey, secretKey string) string {
	kvPairs := getQueueAndObjectStorageInputsForIngestorConfFiles(queue, os, accessKey, secretKey)
	return buildQueueConfStanza(queue.SQS.Name, kvPairs)
}

// generateIngestorDefaultModeConf builds default-mode.conf INI content.
// Reuses getPipelineInputsForConfFile(false) for the six pipeline stanzas.
func generateIngestorDefaultModeConf() string {
	pipelineInputs := getPipelineInputsForConfFile(false)
	var b strings.Builder
	for _, input := range pipelineInputs {
		fmt.Fprintf(&b, "[%s]\n%s = %s\n\n", input[0], input[1], input[2])
	}
	return b.String()
}

// generateQueueConfigAppConf builds app.conf INI content for a queue config Splunk app.
// label is shown in the Splunk UI (e.g. "Splunk Operator Ingestor Queue Config").
func generateQueueConfigAppConf(label string) string {
	return fmt.Sprintf(`[install]
state = enabled
allows_disable = false

[package]
check_for_updates = false

[ui]
is_visible = false
is_manageable = false
label = %s
`, label)
}

// generateQueueConfigLocalMeta builds local.meta with system-level access.
// Shared by both IngestorCluster and ClusterManager queue config apps.
func generateQueueConfigLocalMeta() string {
	return `[]
access = read : [ * ], write : [ admin ]
export = system
`
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
	} else if queue.Provider == "sqs_cp" {
		queueProvider = "sqs_smartbus_cp"
	}
	if queue.Provider == "sqs" || queue.Provider == "sqs_cp" {
		authRegion = queue.SQS.AuthRegion
		endpoint = queue.SQS.Endpoint
		dlq = queue.SQS.DLQ
	}

	path := ""
	osEndpoint := ""
	osProvider := ""
	if os.Provider == "s3" {
		if queueProvider == "sqs_smartbus" {
			osProvider = "sqs_smartbus"
		} else if queueProvider == "sqs_smartbus_cp" {
			osProvider = "sqs_smartbus_cp"
		}
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

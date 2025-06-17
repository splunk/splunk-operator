// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"reflect"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"gopkg.in/yaml.v2"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	//"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyIngestionCluster reconciles the StatefulSet for N standalone instances of Splunk Enterprise.
func ApplyIngestionCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IngestionCluster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyIngestionCluster")
	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}
	eventPublisher, _ := newK8EventPublisher(client, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "IngestionCluster"

	var err error
	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateIngestionClusterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateIngestionClusterSpec", fmt.Sprintf("validate standalone spec failed %s", err.Error()))
		scopedLog.Error(err, "Failed to validate standalone spec")
		return result, err
	}

	// updates status after function completes
	cr.Status.Replicas = cr.Spec.Replicas

	// If needed, Migrate the app framework status
	err = checkAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, true)
	if err != nil {
		return result, err
	}

	// If the app framework is configured then do following things -
	// 1. Initialize the S3Clients based on providers
	// 2. Check the status of apps on remote storage.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err := initAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, "initAndCheckAppInfoStatus", fmt.Sprintf("init and check app info status failed %s", err.Error()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-ingestion-cluster", cr.GetName())

	// create or update general config resources
	_, err = ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIngestion)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, err
	}

	err = ApplyIngestionConfig(ctx, client, cr)
	if err != nil {
		scopedLog.Error(err, "create or update ingestion config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplyIngestionConfig", fmt.Sprintf("create or update ingestion config failed with error %s", err.Error()))
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getIngestionClusterExtraEnv(cr, cr.Spec.Replicas), false)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMap", fmt.Sprintf("create/update monitoring console config map failed %s", err.Error()))
				return result, err
			}
		}
		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, SplunkIngestion)
			if err != nil {
				return result, err
			}
		}
		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)

		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update a headless service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestion, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update headless service failed %s", err.Error()))
		return result, err
	}

	// create or update a regular service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestion, false))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update regular service failed %s", err.Error()))
		return result, err
	}

	// If we are using appFramework and are scaling up, we should re-populate the
	// configMap with all the appSource entries. This is done so that the new pods
	// that come up now will have the complete list of all the apps and then can
	// download and install all the apps.
	// If, we are scaling down, just update the auxPhaseInfo list
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 && cr.Status.ReadyReplicas > 0 {

		statefulsetName := GetSplunkStatefulsetName(SplunkIngestion, cr.GetName())

		isStatefulSetScaling, err := splctrl.IsStatefulSetScalingUpOrDown(ctx, client, cr, statefulsetName, cr.Spec.Replicas)
		if err != nil {
			return result, err
		}
		appStatusContext := cr.Status.AppContext
		switch isStatefulSetScaling {
		case enterpriseApi.StatefulSetScalingUp:
			// if we are indeed scaling up, then mark the deploy status to Pending
			// for all the app sources so that we add all the app sources in configMap.
			cr.Status.AppContext.IsDeploymentInProgress = true

			for appSrc := range appStatusContext.AppsSrcDeployStatus {
				changeAppSrcDeployInfoStatus(ctx, appSrc, appStatusContext.AppsSrcDeployStatus, enterpriseApi.RepoStateActive, enterpriseApi.DeployStatusComplete, enterpriseApi.DeployStatusPending)
				changePhaseInfo(ctx, cr.Spec.Replicas, appSrc, appStatusContext.AppsSrcDeployStatus)
			}

		// if we are scaling down, just delete the state auxPhaseInfo entries
		case enterpriseApi.StatefulSetScalingDown:
			for appSrc := range appStatusContext.AppsSrcDeployStatus {
				removeStaleEntriesFromAuxPhaseInfo(ctx, cr.Spec.Replicas, appSrc, appStatusContext.AppsSrcDeployStatus)
			}
		default:
			// nothing to be done
		}
	}

	// create or update statefulset
	statefulSet, err := getIngestionClusterStatefulSet(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "getIngestionClusterStatefulSet", fmt.Sprintf("get standalone status set failed %s", err.Error()))
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = validateMonitoringConsoleRef(ctx, client, statefulSet, getIngestionClusterExtraEnv(cr, cr.Spec.Replicas))
	if err != nil {
		eventPublisher.Warning(ctx, "validateMonitoringConsoleRef", fmt.Sprintf("validate monitoring console reference failed %s", err.Error()))
		return result, err
	}

	mgr := splctrl.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if err != nil {
		eventPublisher.Warning(ctx, "validateIngestionClusterSpec", fmt.Sprintf("update stateful set failed %s", err.Error()))

		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = splctrl.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			eventPublisher.Warning(ctx, "DeleteReferencesToAutomatedMCIfExists", fmt.Sprintf("delete reference to automated MC if exists failed %s", err.Error()))
			scopedLog.Error(err, "Error in deleting automated monitoring console resource")
		}
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getIngestionClusterExtraEnv(cr, cr.Spec.Replicas), true)
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
			err := addTelApp(ctx, podExecClient, cr.Spec.Replicas, cr)
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

func ApplyIngestionConfig(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IngestionCluster) error {
	reqLogger := log.FromContext(ctx).WithName("ApplyIngestionConfig")
	scopedLog := reqLogger.WithValues("cr", cr.GetName())

	// merge bus config into defaults
	updatedDefaultsYAML, err := MergeBusAndPipelines(ctx, cr.Spec.Defaults, &cr.Spec.PushBus, "ingester")
	if err != nil {
		return fmt.Errorf("merge bus into defaults: %w", err)
	}
	cr.Spec.Defaults = updatedDefaultsYAML
	scopedLog.Info("Updated ingestion configuration successfully")
	return nil
}

// MergeBusAndPipelines parses the given defaultsYAML, injects the smartbus stanza
// (outputs or inputs) for the given role, and appends the correct default-mode.conf.
// Role must be either "ingester" or "indexer".
func MergeBusAndPipelines(ctx context.Context, defaultsYAML string, bus *enterpriseApi.BusSpec, role string) (string, error) {
	// 1) Unmarshal existing defaults
	var spec DefaultSpec
	if err := yaml.Unmarshal([]byte(defaultsYAML), &spec); err != nil {
		return "", fmt.Errorf("failed to parse defaults.yml: %w", err)
	}

	// 2) Build the provider section
	sectionName, sectionMap, err := buildSectionFromBus(ctx, bus)
	if err != nil {
		return "", err
	}

	// 3) Determine which .conf to inject into
	var confKey string
	switch role {
	case "ingester":
		confKey = "outputs" // outputs.conf
	case "indexer":
		confKey = "inputs" // inputs.conf
	default:
		return "", fmt.Errorf("unknown role %q", role)
	}

	upsertConfSection(ctx, &spec, confKey, sectionName, sectionMap)

	// 4) Inject default-mode.conf for the role
	switch role {
	case "ingester":
		upsertDefaultModeConfIngester(ctx, &spec)
	case "indexer":
		upsertDefaultModeConfIndexer(ctx, &spec)
	}

	// 5) Marshal back to YAML
	out, err := yaml.Marshal(&spec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal updated defaults: %w", err)
	}
	return string(out), nil
}

// buildSectionFromBus handles each provider in turn
func buildSectionFromBus(ctx context.Context, bus *enterpriseApi.BusSpec) (string, map[string]interface{}, error) {
	switch bus.Type {
	case enterpriseApi.BusTypeSQS:
		if bus.SQS == nil {
			return "", nil, fmt.Errorf("bus.Type is sqs but .SQS is nil")
		}
		return buildSQSSection(ctx, bus.SQS)
	case enterpriseApi.BusTypeKafka:
		if bus.Kafka == nil {
			return "", nil, fmt.Errorf("bus.Type is kafka but .Kafka is nil")
		}
		return buildKafkaSection(ctx, bus.Kafka)
	case enterpriseApi.BusTypePubSub:
		if bus.PubSub == nil {
			return "", nil, fmt.Errorf("bus.Type is pubsub but .PubSub is nil")
		}
		return buildPubSubSection(ctx, bus.PubSub)
	case enterpriseApi.BusTypeServiceBus:
		if bus.ServiceBus == nil {
			return "", nil, fmt.Errorf("bus.Type is servicebus but .ServiceBus is nil")
		}
		return buildServiceBusSection(ctx, bus.ServiceBus)
	default:
		return "", nil, fmt.Errorf("unsupported BusType %q", bus.Type)
	}
}

// ——— provider‐specific builders ———————————————————————

func buildSQSSection(ctx context.Context, s *enterpriseApi.SQSBusConfig) (string, map[string]interface{}, error) {
	name := fmt.Sprintf("remote_queue:%s", s.QueueName)
	enc := s.EncodingFormat
	if enc == "" {
		enc = "s2s"
	}
	maxRt := s.MaxRetriesPerPart
	if maxRt == 0 {
		maxRt = 3
	}
	rp := s.RetryPolicy
	if rp == "" {
		rp = "max_count"
	}
	si := s.SendInterval
	if si == "" {
		si = "4s"
	}

	return name, map[string]interface{}{
		"remote_queue.type":                                        "sqs_smartbus",
		"remote_queue.sqs_smartbus.encoding_format":                enc,
		"remote_queue.sqs_smartbus.auth_region":                    s.AuthRegion,
		"remote_queue.sqs_smartbus.endpoint":                       s.Endpoint,
		"remote_queue.sqs_smartbus.large_message_store.endpoint":   s.LargeMessageStore.Endpoint,
		"remote_queue.sqs_smartbus.large_message_store.path":       s.LargeMessageStore.Path,
		"remote_queue.sqs_smartbus.dead_letter_queue.name":         s.DeadLetterQueueName,
		"remote_queue.sqs_smartbus.max_count.max_retries_per_part": maxRt,
		"remote_queue.sqs_smartbus.retry_policy":                   rp,
		"remote_queue.sqs_smartbus.send_interval":                  si,
	}, nil
}

func buildKafkaSection(ctx context.Context, k *enterpriseApi.KafkaBusConfig) (string, map[string]interface{}, error) {
	name := fmt.Sprintf("remote_queue:%s", k.Topic)
	return name, map[string]interface{}{
		"remote_queue.type":                   "kafka_smartbus",
		"remote_queue.kafka_smartbus.brokers": strings.Join(k.Brokers, ","),
		"remote_queue.kafka_smartbus.topic":   k.Topic,
		// add TLS/SASL placeholders if needed
	}, nil
}

func buildPubSubSection(ctx context.Context, p *enterpriseApi.PubSubBusConfig) (string, map[string]interface{}, error) {
	name := fmt.Sprintf("remote_queue:%s", p.Topic)
	return name, map[string]interface{}{
		"remote_queue.type":                         "pubsub_smartbus",
		"remote_queue.pubsub_smartbus.project":      p.Project,
		"remote_queue.pubsub_smartbus.topic":        p.Topic,
		"remote_queue.pubsub_smartbus.subscription": p.Subscription,
	}, nil
}

func buildServiceBusSection(ctx context.Context, sb *enterpriseApi.ServiceBusBusConfig) (string, map[string]interface{}, error) {
	name := fmt.Sprintf("remote_queue:%s", sb.QueueName)
	tt := sb.TransportType
	if tt == "" {
		tt = "Amqp"
	}
	return name, map[string]interface{}{
		"remote_queue.type":                              "servicebus_smartbus",
		"remote_queue.servicebus_smartbus.namespace":     sb.Namespace,
		"remote_queue.servicebus_smartbus.queueName":     sb.QueueName,
		"remote_queue.servicebus_smartbus.transportType": tt,
	}, nil
}

// ——— shared upsert logic —————————————————————————————————

func upsertConfSection(ctx context.Context, spec *DefaultSpec, confKey, sectionName string, section map[string]interface{}) {
	for i, entry := range spec.Splunk.Conf {
		if entry.Key == confKey {
			if spec.Splunk.Conf[i].Value.Content == nil {
				spec.Splunk.Conf[i].Value.Content = make(map[string]map[string]interface{})
			}
			if _, exists := spec.Splunk.Conf[i].Value.Content[sectionName]; !exists {
				spec.Splunk.Conf[i].Value.Content[sectionName] = section
			}
			return
		}
	}
	spec.Splunk.Conf = append(spec.Splunk.Conf, ConfEntry{
		Key: confKey,
		Value: ConfValue{
			Directory: "/opt/splunk/etc/system/local",
			Content:   map[string]map[string]interface{}{sectionName: section},
		},
	})
}

// upsertDefaultModeConfIngester injects all six pipeline stanzas under default-mode.conf
func upsertDefaultModeConfIngester(ctx context.Context, spec *DefaultSpec) {
	// build the full content map: sectionName → settings
	content := map[string]map[string]interface{}{
		"pipeline:remotequeueruleset": {"disabled": false},
		"pipeline:ruleset":            {"disabled": true},
		"pipeline:remotequeuetyping":  {"disabled": false},
		"pipeline:remotequeueoutput":  {"disabled": false},
		"pipeline:typing":             {"disabled": true},
		"pipeline:indexerPipe":        {"disabled": true},
	}

	// look for existing key
	for i, entry := range spec.Splunk.Conf {
		if entry.Key == "default-mode" {
			spec.Splunk.Conf[i].Value = ConfValue{
				Directory: "/opt/splunk/etc/system/local",
				Content:   content,
			}
			return
		}
	}

	// not found → append
	spec.Splunk.Conf = append(spec.Splunk.Conf, ConfEntry{
		Key: "default-mode",
		Value: ConfValue{
			Directory: "/opt/splunk/etc/system/local",
			Content:   content,
		},
	})
}

// Similarly for Indexer (omitting the indexerPipe stanza):
func upsertDefaultModeConfIndexer(ctx context.Context, spec *DefaultSpec) {
	content := map[string]map[string]interface{}{
		"pipeline:remotequeueruleset": {"disabled": false},
		"pipeline:ruleset":            {"disabled": true},
		"pipeline:remotequeuetyping":  {"disabled": false},
		"pipeline:remotequeueoutput":  {"disabled": false},
		"pipeline:typing":             {"disabled": true},
	}

	for i, entry := range spec.Splunk.Conf {
		if entry.Key == "default-mode" {
			spec.Splunk.Conf[i].Value = ConfValue{
				Directory: "/opt/splunk/etc/system/local",
				Content:   content,
			}
			return
		}
	}
	spec.Splunk.Conf = append(spec.Splunk.Conf, ConfEntry{
		Key: "default-mode",
		Value: ConfValue{
			Directory: "/opt/splunk/etc/system/local",
			Content:   content,
		},
	})
}

// getIngestionClusterStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise standalone instances.
func getIngestionClusterStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IngestionCluster) (*appsv1.StatefulSet, error) {
	// get generic statefulset for Splunk Enterprise objects
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkIngestion, cr.Spec.Replicas, []corev1.EnvVar{})
	if err != nil {
		return nil, err
	}

	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, nil
}

// validateIngestionClusterSpec checks validity and makes default updates to a IngestionClusterSpec, and returns error if something is wrong.
func validateIngestionClusterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IngestionCluster) error {
	if cr.Spec.Replicas == 0 {
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

// helper function to get the list of IngestionCluster types in the current namespace
func getIngestionClusterList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.IngestionClusterList, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getIngestionClusterList")

	objectList := enterpriseApi.IngestionClusterList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		scopedLog.Error(err, "IngestionCluster types not found in namespace", "namsespace", cr.GetNamespace())
		return objectList, err
	}

	return objectList, nil
}

// upsertDefaultModeConf ensures a "default-mode" entry with the fixed pipeline stanzas
// is present in your DefaultSpec. If the key already exists, it will overwrite its content.
func upsertDefaultModeConf(spec *DefaultSpec) {
	// 1) Build the content map for default-mode.conf
	content := map[string]map[string]interface{}{
		"pipeline:remotequeueruleset": {"disabled": false},
		"pipeline:ruleset":            {"disabled": true},
		"pipeline:remotequeuetyping":  {"disabled": false},
		"pipeline:remotequeueoutput":  {"disabled": false},
		"pipeline:typing":             {"disabled": true},
		"pipeline:indexerPipe":        {"disabled": true},
	}

	// 2) Look for an existing "default-mode" entry
	for i, entry := range spec.Splunk.Conf {
		if entry.Key == "default-mode" {
			// overwrite
			spec.Splunk.Conf[i].Value = ConfValue{
				Directory: "/opt/splunk/etc/system/local",
				Content:   content,
			}
			return
		}
	}

	// 3) Not found → append a new ConfEntry
	spec.Splunk.Conf = append(spec.Splunk.Conf, ConfEntry{
		Key: "default-mode",
		Value: ConfValue{
			Directory: "/opt/splunk/etc/system/local",
			Content:   content,
		},
	})
}

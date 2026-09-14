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

package standalone

import (
	"context"
	"fmt"
	"reflect"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	// TODO: Remove this legacy dependency once all CRs have migrated from enterprise.
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/telapp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const pauseRetryDelay = time.Second * 30

// apply owns the Standalone reconcile loop for one controller-runtime request.
func apply(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, recorder record.EventRecorder) (reconcile.Result, error) {
	logger := logging.FromContext(ctx).With("controller", "Standalone", "name", namespacedName.Name, "namespace", namespacedName.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)

	instance := &enterpriseApi.Standalone{}
	err := client.Get(ctx, namespacedName, instance)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("could not load standalone data: %w", err)
	}

	if instance.GetAnnotations()[enterpriseApi.StandalonePausedAnnotation] == "true" {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: instance.Status.Phase, IsPaused: true, Message: "", Generation: instance.GetGeneration(),
		})
		instance.Status.Conditions = result.Conditions
		if err := client.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update paused status", "error", err)
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true, RequeueAfter: pauseRetryDelay}, nil
	} else if cond := meta.FindStatusCondition(instance.Status.Conditions, string(enterpriseApi.ConditionPaused)); cond != nil && cond.Status == metav1.ConditionTrue {
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
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)

	result, err := ApplyStandalone(ctx, client, instance)
	if result.Requeue && result.RequeueAfter != 0 {
		logger.InfoContext(ctx, "requeued", "periodSeconds", int(result.RequeueAfter/time.Second))
	}

	fresh := &enterpriseApi.Standalone{}
	if fetchErr := client.Get(ctx, namespacedName, fresh); fetchErr != nil {
		if k8serrors.IsNotFound(fetchErr) {
			return result, nil
		}
		logger.WarnContext(ctx, "failed to refetch CR for stalled condition update", "error", fetchErr)
		return result, fetchErr
	}
	oldConditions := append([]metav1.Condition(nil), fresh.Status.Conditions...)
	if msg, ok := splcommon.TerminalMessage(err); ok {
		reason, _ := splcommon.TerminalReason(err)
		fresh.Status.Conditions = splcommon.UpsertStalledCondition(fresh.Status.Conditions, reason, msg, fresh.GetGeneration())
	} else {
		fresh.Status.Conditions = splcommon.ClearStalledCondition(fresh.Status.Conditions, fresh.GetGeneration())
	}
	ep, epErr := k8sops.NewK8EventPublisherWithRecorder(recorder, fresh)
	if epErr != nil {
		logger.WarnContext(ctx, "failed to create event publisher", "error", epErr)
		return result, epErr
	}
	k8sops.EmitStalledTransitionEvents(ctx, ep, fresh.GetName(), oldConditions, fresh.Status.Conditions)
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
// It is a variable so controller tests can replace the reconcile boundary.
var Apply = apply

// ApplyStandalone reconciles the StatefulSet for N standalone instances of Splunk Enterprise.
// It is a variable so request-level reconciliation tests can replace the
// operation while retaining status and event handling in apply.
var ApplyStandalone = applyStandalone

func applyStandalone(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.Standalone) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	logger := logging.FromContext(ctx).With("func", "ApplyStandalone")
	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	eventPublisher := k8sops.GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "Standalone"

	var err error
	// Initialize phase and conditions
	isPaused := cr.GetAnnotations()[enterpriseApi.StandalonePausedAnnotation] == "true"
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

	// validate and updates defaults for CR
	err = ValidateStandaloneSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateStandaloneSpec", fmt.Sprintf("validate standalone spec failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Standalone spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, "Standalone spec validation failed", err)
	}

	// updates status after function completes
	cr.Status.Replicas = cr.Spec.Replicas

	// If needed, Migrate the app framework status
	err = enterprise.CheckAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, true)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "App framework migration failed")
		return result, err
	}

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) ||
		k8sops.AreRemoteVolumeKeysChanged(ctx, client, cr, splcommon.SplunkStandalone, &cr.Spec.SmartStore, cr.Status.ResourceRevMap, &err) {

		if err != nil {
			eventPublisher.Warning(ctx, "AreRemoteVolumeKeysChanged", fmt.Sprintf("check remote volume key change failed %s", err.Error()))
			setPhaseAndConditions(enterpriseApi.PhaseError, "SmartStore remote volume key validation failed")
			return result, err
		}

		_, _, err := k8sops.ApplySmartstoreConfigMap(ctx, client, cr, &cr.Spec.SmartStore)
		if err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply SmartStore ConfigMap")
			return result, err
		}

		cr.Status.SmartStore = cr.Spec.SmartStore
	}

	// If the app framework is configured then do following things -
	// 1. Initialize the S3Clients based on providers
	// 2. Check the status of apps on remote storage.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err := enterprise.InitAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, "initAndCheckAppInfoStatus", fmt.Sprintf("init and check app info status failed %s", err.Error()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			setPhaseAndConditions(enterpriseApi.PhaseError, "App framework initialization failed")
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-standalone", cr.GetName())

	// create or update general config resources
	_, err = k8sops.ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkStandalone)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration")
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// Smart Store secrets get created manually and should not be managed by the Operator
	if &cr.Spec.SmartStore != nil {
		_ = k8sops.DeleteOwnerReferencesForS3SecretObjects(ctx, client, cr, &cr.Spec.SmartStore)
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, monitoringConsoleEnv(cr, cr.Spec.Replicas), false)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMap", fmt.Sprintf("create/update monitoring console config map failed %s", err.Error()))
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console env ConfigMap during deletion")
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = enterprise.UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, splcommon.SplunkStandalone)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to clean up resources during deletion")
				return result, err
			}
		}

		_ = k8sops.DeleteOwnerReferencesForResources(ctx, client, cr, splcommon.SplunkStandalone)

		terminating, err := k8sops.CheckForDeletion(ctx, cr, client)

		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted")
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update a headless service
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkStandalone, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update headless service failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update headless service")
		return result, err
	}

	// create or update a regular service
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkStandalone, false))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update regular service failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update regular service")
		return result, err
	}

	// If we are using appFramework and are scaling up, we should re-populate the
	// configMap with all the appSource entries. This is done so that the new pods
	// that come up now will have the complete list of all the apps and then can
	// download and install all the apps.
	// If, we are scaling down, just update the auxPhaseInfo list
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 && cr.Status.ReadyReplicas > 0 {

		statefulsetName := splutil.GetSplunkStatefulsetName(splcommon.SplunkStandalone, cr.GetName())

		isStatefulSetScaling, err := k8sops.IsStatefulSetScalingUpOrDown(ctx, client, cr, statefulsetName, cr.Spec.Replicas)
		if err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to determine Scaling state")
			return result, err
		}
		appStatusContext := cr.Status.AppContext
		switch isStatefulSetScaling {
		case enterpriseApi.StatefulSetScalingUp:
			// if we are indeed scaling up, then mark the deploy status to Pending
			// for all the app sources so that we add all the app sources in configMap.
			cr.Status.AppContext.IsDeploymentInProgress = true

			for appSrc := range appStatusContext.AppsSrcDeployStatus {
				enterprise.ChangeAppSrcDeployInfoStatus(ctx, appSrc, appStatusContext.AppsSrcDeployStatus, enterpriseApi.RepoStateActive, enterpriseApi.DeployStatusComplete, enterpriseApi.DeployStatusPending)
				enterprise.ChangePhaseInfo(ctx, cr.Spec.Replicas, appSrc, appStatusContext.AppsSrcDeployStatus)
			}

		// if we are scaling down, just delete the state auxPhaseInfo entries
		case enterpriseApi.StatefulSetScalingDown:
			for appSrc := range appStatusContext.AppsSrcDeployStatus {
				enterprise.RemoveStaleEntriesFromAuxPhaseInfo(ctx, cr.Spec.Replicas, appSrc, appStatusContext.AppsSrcDeployStatus)
			}
		default:
			// nothing to be done
		}
	}

	// create or update statefulset
	statefulSet, err := GetStandaloneStatefulSet(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonStatefulSetFailed, fmt.Sprintf("get standalone status set failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet")
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = k8sops.ValidateMonitoringConsoleRef(ctx, client, statefulSet, monitoringConsoleEnv(cr, cr.Spec.Replicas))
	if err != nil {
		eventPublisher.Warning(ctx, "validateMonitoringConsoleRef", fmt.Sprintf("validate monitoring console reference failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to validate Monitoring Console reference")
		return result, err
	}

	// Track previous ready replicas for scaling events
	previousReadyReplicas := cr.Status.ReadyReplicas

	mgr := k8sops.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if err != nil {
		eventPublisher.Warning(ctx, "validateStandaloneSpec", fmt.Sprintf("update stateful set failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update pods")
		return result, err
	}
	setPhaseAndConditions(phase, "")

	// Emit scale events when phase is ready and ready replicas changed to match desired
	if phase == enterpriseApi.PhaseReady {
		desiredReplicas := cr.Spec.Replicas
		if cr.Status.ReadyReplicas == desiredReplicas && previousReadyReplicas != desiredReplicas {
			if desiredReplicas > previousReadyReplicas {
				if eventPublisher != nil {
					eventPublisher.Normal(ctx, "ScaledUp",
						fmt.Sprintf("Successfully scaled %s up from %d to %d replicas", cr.GetName(), previousReadyReplicas, desiredReplicas))
				}
			} else if desiredReplicas < previousReadyReplicas {
				if eventPublisher != nil {
					eventPublisher.Normal(ctx, "ScaledDown",
						fmt.Sprintf("Successfully scaled %s down from %d to %d replicas", cr.GetName(), previousReadyReplicas, desiredReplicas))
				}
			}
		}
	}

	if cr.Spec.MonitoringConsoleRef.Name != "" {
		_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, monitoringConsoleEnv(cr, cr.Spec.Replicas), true)
		if err != nil {
			eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMap", fmt.Sprintf("apply monitoring console environment config map failed %s", err.Error()))
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
			eventPublisher.Warning(ctx, splcommon.EventReasonMonitoringConsoleCleanupFailed, fmt.Sprintf("Failed to clean up automated monitoring console for %s — check operator logs", cr.GetName()))
			logger.ErrorContext(ctx, "error in deleting automated MonitoringConsole resource", "error", err)
		}

		finalResult := enterprise.HandleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
		result = *finalResult

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			podExecClient := splutil.GetPodExecClient(client, cr, "")
			err := telapp.AddTelApp(ctx, podExecClient, cr.Spec.Replicas, cr)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to install Telemetry app")
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

// getStandaloneStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise standalone instances.
func GetStandaloneStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.Standalone) (*appsv1.StatefulSet, error) {
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, enterprise.ToCertEntries(cr.Spec.Certs, certs.AutoDNSNames(splcommon.SplunkStandalone, cr.GetName(), cr.GetNamespace(), cr.Spec.Replicas)))
	if err != nil {
		return nil, fmt.Errorf("reconcile certs: %w", err)
	}
	// get generic statefulset for Splunk Enterprise objects
	ss, err := k8sops.GetSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkStandalone, cr.Spec.Replicas, []corev1.EnvVar{})
	if err != nil {
		return nil, err
	}
	certs.InjectCertMounts(&ss.Spec.Template, certMounts)

	smartStoreConfigMap := k8sops.GetSmartstoreConfigMap(ctx, client, cr, splcommon.SplunkStandalone)

	if smartStoreConfigMap != nil {
		resources.SetupInitContainer(&ss.Spec.Template, cr.Spec.Image, cr.Spec.ImagePullPolicy, "mkdir -p /opt/splk/etc/apps/splunk-operator/local && ln -sfn  /mnt/splunk-operator/local/indexes.conf /opt/splk/etc/apps/splunk-operator/local/indexes.conf && ln -sfn  /mnt/splunk-operator/local/server.conf /opt/splk/etc/apps/splunk-operator/local/server.conf", cr.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.EphemeralStorage)
	}

	// Setup App framework staging volume for apps
	enterprise.SetupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, nil
}

// validateStandaloneSpec checks validity and makes default updates to a StandaloneSpec, and returns error if something is wrong.
func ValidateStandaloneSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.Standalone) error {
	if cr.Spec.Replicas < 0 {
		return fmt.Errorf("replicas must be >= 0")
	}
	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) {
		err := validateSmartstoreSpec(&cr.Spec.SmartStore)
		if err != nil {
			return err
		}
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := enterprise.ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

func monitoringConsoleEnv(cr splcommon.MetaObject, replicas int32) []corev1.EnvVar {
	return []corev1.EnvVar{{Name: "SPLUNK_STANDALONE_URL", Value: splutil.GetSplunkStatefulsetUrls(cr.GetNamespace(), splcommon.SplunkStandalone, cr.GetName(), replicas, false)}}
}

// helper function to get the list of Standalone types in the current namespace
func getStandaloneList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.StandaloneList, error) {
	logger := logging.FromContext(ctx).With("func", "getStandaloneList")
	objectList := enterpriseApi.StandaloneList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		logger.ErrorContext(ctx, "Standalone types not found in namespace", "namespace", cr.GetNamespace(), "error", err)
		return objectList, err
	}

	return objectList, nil
}

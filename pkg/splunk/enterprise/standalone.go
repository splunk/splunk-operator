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
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyStandalone reconciles the StatefulSet for N standalone instances of Splunk Enterprise.
func ApplyStandalone(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.Standalone) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	logger := logging.FromContext(ctx).With("func", "ApplyStandalone")
	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	eventPublisher := GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "Standalone"

	var err error
	// Initialize phase and conditions
	isPaused := cr.GetDeletionTimestamp() == nil &&
		cr.GetAnnotations()[enterpriseApi.StandalonePausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string) {
		result := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: phase, IsPaused: isPaused, Message: message, Generation: cr.GetGeneration(),
		})
		cr.Status.Phase = result.Phase
		cr.Status.Conditions = result.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "")

	// Update the CR Status unless successful finalization has already removed
	// the finalizer and allowed the API server to delete the object.
	updateStatusOnReturn := true
	defer func() {
		if updateStatusOnReturn {
			updateCRStatus(ctx, client, cr, &err)
		}
	}()

	// Deletion finalization must run before validation, migration, remote app
	// initialization, and ApplySplunkConfig. Those normal-reconciliation steps
	// may create namespace content and cannot be prerequisites for finalization.
	if cr.GetDeletionTimestamp() != nil {
		result, err = finalizeStandaloneDeletion(
			ctx,
			client,
			cr,
			eventPublisher,
			setPhaseAndConditions,
			result,
		)
		if err == nil {
			updateStatusOnReturn = false
		}
		return result, err
	}

	// validate and updates defaults for CR
	err = validateStandaloneSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateStandaloneSpec", fmt.Sprintf("validate standalone spec failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Standalone spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(EventReasonValidateSpecFailed, "Standalone spec validation failed", err)
	}

	// updates status after function completes
	cr.Status.Replicas = cr.Spec.Replicas

	// If needed, Migrate the app framework status
	err = checkAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, true)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "App framework migration failed")
		return result, err
	}

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) ||
		AreRemoteVolumeKeysChanged(ctx, client, cr, SplunkStandalone, &cr.Spec.SmartStore, cr.Status.ResourceRevMap, &err) {

		if err != nil {
			eventPublisher.Warning(ctx, "AreRemoteVolumeKeysChanged", fmt.Sprintf("check remote volume key change failed %s", err.Error()))
			setPhaseAndConditions(enterpriseApi.PhaseError, "SmartStore remote volume key validation failed")
			return result, err
		}

		_, _, err := ApplySmartstoreConfigMap(ctx, client, cr, &cr.Spec.SmartStore)
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
		err := initAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, "initAndCheckAppInfoStatus", fmt.Sprintf("init and check app info status failed %s", err.Error()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			setPhaseAndConditions(enterpriseApi.PhaseError, "App framework initialization failed")
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-standalone", cr.GetName())

	// create or update general config resources
	_, err = ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkStandalone)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration")
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// Smart Store secrets get created manually and should not be managed by the Operator
	if &cr.Spec.SmartStore != nil {
		_ = DeleteOwnerReferencesForS3SecretObjects(ctx, client, cr, &cr.Spec.SmartStore)
	}

	// create or update a headless service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update headless service failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update headless service")
		return result, err
	}

	// create or update a regular service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, false))
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

		statefulsetName := GetSplunkStatefulsetName(SplunkStandalone, cr.GetName())

		isStatefulSetScaling, err := splctrl.IsStatefulSetScalingUpOrDown(ctx, client, cr, statefulsetName, cr.Spec.Replicas)
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
	statefulSet, err := getStandaloneStatefulSet(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "getStandaloneStatefulSet", fmt.Sprintf("get standalone status set failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet")
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = validateMonitoringConsoleRef(ctx, client, statefulSet, getStandaloneExtraEnv(cr, cr.Spec.Replicas))
	if err != nil {
		eventPublisher.Warning(ctx, "validateMonitoringConsoleRef", fmt.Sprintf("validate monitoring console reference failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to validate Monitoring Console reference")
		return result, err
	}

	// Track previous ready replicas for scaling events
	previousReadyReplicas := cr.Status.ReadyReplicas

	mgr := splctrl.DefaultStatefulSetPodManager{}
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
		_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getStandaloneExtraEnv(cr, cr.Spec.Replicas), true)
		if err != nil {
			eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMap", fmt.Sprintf("apply monitoring console environment config map failed %s", err.Error()))
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console env ConfigMap")
			return result, err
		}
	}

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = splctrl.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			eventPublisher.Warning(ctx, EventReasonMonitoringConsoleCleanupFailed, fmt.Sprintf("Failed to clean up automated monitoring console for %s — check operator logs", cr.GetName()))
			logger.ErrorContext(ctx, "error in deleting automated MonitoringConsole resource", "error", err)
		}

		finalResult := handleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
		result = *finalResult

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			podExecClient := splutil.GetPodExecClient(client, cr, "")
			err := addTelApp(ctx, podExecClient, cr.Spec.Replicas, cr)
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

// finalizeStandaloneDeletion performs only deletion-safe reads, updates,
// deletes, and finalizer work. It must not create replacement namespace
// content while Kubernetes is deleting the containing Namespace.
func finalizeStandaloneDeletion(
	ctx context.Context,
	client splcommon.ControllerClient,
	cr *enterpriseApi.Standalone,
	eventPublisher *K8EventPublisher,
	setPhaseAndConditions func(enterpriseApi.Phase, string),
	result reconcile.Result,
) (reconcile.Result, error) {
	setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted")

	if cr.Spec.MonitoringConsoleRef.Name != "" {
		if _, err := ApplyMonitoringConsoleEnvConfigMap(
			ctx,
			client,
			cr.GetNamespace(),
			cr.GetName(),
			cr.Spec.MonitoringConsoleRef.Name,
			getStandaloneExtraEnv(cr, cr.Spec.Replicas),
			false,
		); err != nil {
			eventPublisher.Warning(
				ctx,
				"ApplyMonitoringConsoleEnvConfigMap",
				fmt.Sprintf("create/update monitoring console config map failed %s", err.Error()),
			)
			setPhaseAndConditions(
				enterpriseApi.PhaseError,
				"Failed to update Monitoring Console env ConfigMap during deletion",
			)
			return result, err
		}
	}

	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		if err := UpdateOrRemoveEntryFromConfigMapLocked(
			ctx,
			client,
			cr,
			SplunkStandalone,
		); err != nil {
			setPhaseAndConditions(
				enterpriseApi.PhaseError,
				"Failed to clean up resources during deletion",
			)
			return result, err
		}
	}

	_ = DeleteOwnerReferencesForResources(ctx, client, cr, SplunkStandalone)

	_, err := splctrl.CheckForDeletion(ctx, cr, client)
	if err != nil {
		eventPublisher.Warning(
			ctx,
			EventReasonDeleteFailed,
			fmt.Sprintf("Failed to delete custom resource %s — check operator logs", cr.GetName()),
		)
		return result, err
	}

	result.Requeue = false
	return result, nil
}

// getStandaloneStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise standalone instances.
func getStandaloneStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.Standalone) (*appsv1.StatefulSet, error) {
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, toCertEntries(cr.Spec.Certs))
	if err != nil {
		return nil, err
	}
	// get generic statefulset for Splunk Enterprise objects
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, cr.Spec.Replicas, []corev1.EnvVar{}, certMounts)
	if err != nil {
		return nil, err
	}

	smartStoreConfigMap := getSmartstoreConfigMap(ctx, client, cr, SplunkStandalone)

	if smartStoreConfigMap != nil {
		setupInitContainer(&ss.Spec.Template, cr.Spec.Image, cr.Spec.ImagePullPolicy, commandForStandaloneSmartstore, cr.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.EphemeralStorage)
	}

	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, nil
}

// validateStandaloneSpec checks validity and makes default updates to a StandaloneSpec, and returns error if something is wrong.
func validateStandaloneSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.Standalone) error {
	if cr.Spec.Replicas < 0 {
		return fmt.Errorf("replicas must be >= 0")
	}
	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) {
		err := ValidateSplunkSmartstoreSpec(ctx, &cr.Spec.SmartStore)
		if err != nil {
			return err
		}
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
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

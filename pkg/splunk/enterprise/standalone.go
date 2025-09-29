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

// ApplyStandalone reconciles the StatefulSet for N standalone instances of Splunk Enterprise.
func ApplyStandalone(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.Standalone) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyStandalone")
	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}
	eventPublisher, _ := newK8EventPublisher(client, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "Standalone"

	var err error
	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateStandaloneSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateStandaloneSpec", fmt.Sprintf("validate standalone spec failed %s", err.Error()))
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

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) ||
		AreRemoteVolumeKeysChanged(ctx, client, cr, SplunkStandalone, &cr.Spec.SmartStore, cr.Status.ResourceRevMap, &err) {

		if err != nil {
			eventPublisher.Warning(ctx, "AreRemoteVolumeKeysChanged", fmt.Sprintf("check remote volume key change failed %s", err.Error()))
			return result, err
		}

		_, _, err := ApplySmartstoreConfigMap(ctx, client, cr, &cr.Spec.SmartStore)
		if err != nil {
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
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-standalone", cr.GetName())

	// create or update general config resources
	_, err = ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkStandalone)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, err
	}

	// Smart Store secrets get created manually and should not be managed by the Operator
	if &cr.Spec.SmartStore != nil {
		_ = DeleteOwnerReferencesForS3SecretObjects(ctx, client, cr, &cr.Spec.SmartStore)
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getStandaloneExtraEnv(cr, cr.Spec.Replicas), false)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMap", fmt.Sprintf("create/update monitoring console config map failed %s", err.Error()))
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, SplunkStandalone)
			if err != nil {
				return result, err
			}
		}

		DeleteOwnerReferencesForResources(ctx, client, cr, SplunkStandalone)

		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)

		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update a headless service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update headless service failed %s", err.Error()))
		return result, err
	}

	// create or update a regular service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, false))
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

		statefulsetName := GetSplunkStatefulsetName(SplunkStandalone, cr.GetName())

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
	statefulSet, err := getStandaloneStatefulSet(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "getStandaloneStatefulSet", fmt.Sprintf("get standalone status set failed %s", err.Error()))
		return result, err
	}

	// Check if a new versioned secret was created (indicating password change)
	// Reset AdminSecretChanged flags so password sync will run again
	if statefulSet != nil && len(statefulSet.Spec.Template.Spec.Volumes) > 0 {
		for _, volume := range statefulSet.Spec.Template.Spec.Volumes {
			if volume.Name == "mnt-splunk-secrets" && volume.Secret != nil {
				currentSecretName := volume.Secret.SecretName
				// Check if this is a new versioned secret (contains -v and a number)
				if strings.Contains(currentSecretName, "-secret-v") {
					// Reset flags if we haven't seen this secret before
					if cr.Status.AdminPasswordChangedSecrets == nil {
						cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
					}
					if _, exists := cr.Status.AdminPasswordChangedSecrets[currentSecretName]; !exists {
						scopedLog.Info("New versioned secret detected, resetting admin password change flags", "secret", currentSecretName)
						// Reset all AdminSecretChanged flags to false
						for i := range cr.Status.AdminSecretChanged {
							cr.Status.AdminSecretChanged[i] = false
						}
					}
				}
				break
			}
		}
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = validateMonitoringConsoleRef(ctx, client, statefulSet, getStandaloneExtraEnv(cr, cr.Spec.Replicas))
	if err != nil {
		eventPublisher.Warning(ctx, "validateMonitoringConsoleRef", fmt.Sprintf("validate monitoring console reference failed %s", err.Error()))
		return result, err
	}

	mgr := splctrl.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if err != nil {
		eventPublisher.Warning(ctx, "validateStandaloneSpec", fmt.Sprintf("update stateful set failed %s", err.Error()))

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
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getStandaloneExtraEnv(cr, cr.Spec.Replicas), true)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyMonitoringConsoleEnvConfigMap", fmt.Sprintf("apply monitoring console environment config map failed %s", err.Error()))
				return result, err
			}
		}

		// Sync admin password if changed
		err = ApplyStandaloneAdminSecret(ctx, client, cr)
		if err != nil {
			eventPublisher.Warning(ctx, "ApplyStandaloneAdminSecret", fmt.Sprintf("admin password sync failed %s", err.Error()))
			scopedLog.Error(err, "Failed to sync admin password")
			// Don't return error to avoid blocking other operations
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

// getStandaloneStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise standalone instances.
func getStandaloneStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.Standalone) (*appsv1.StatefulSet, error) {
	// get generic statefulset for Splunk Enterprise objects
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, cr.Spec.Replicas, []corev1.EnvVar{})
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
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getStandaloneList")

	objectList := enterpriseApi.StandaloneList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		scopedLog.Error(err, "Standalone types not found in namespace", "namsespace", cr.GetNamespace())
		return objectList, err
	}

	return objectList, nil
}

// ApplyStandaloneAdminSecret checks if any standalone instances have a different admin password
// from namespace scoped secret and synchronizes it
func ApplyStandaloneAdminSecret(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.Standalone) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyStandaloneAdminSecret").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// Get namespace scoped secret
	namespaceSecret, err := splutil.GetNamespaceScopedSecret(ctx, client, cr.GetNamespace())
	if err != nil {
		scopedLog.Error(err, "Failed to get namespace scoped secret")
		return err
	}

	if namespaceSecret.Data == nil || len(namespaceSecret.Data) == 0 {
		scopedLog.Info("Namespace secret has no data, skipping password sync")
		return nil
	}

	nsAdminSecret := string(namespaceSecret.Data["password"])
	if nsAdminSecret == "" {
		scopedLog.Info("No admin password found in namespace secret, skipping password sync")
		return nil
	}

	// Initialize status fields if needed
	if cr.Status.AdminSecretChanged == nil {
		cr.Status.AdminSecretChanged = make([]bool, cr.Spec.Replicas)
	}
	if cr.Status.AdminPasswordChangedSecrets == nil {
		cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
	}

	// Ensure the status arrays are the right size
	for len(cr.Status.AdminSecretChanged) < int(cr.Spec.Replicas) {
		cr.Status.AdminSecretChanged = append(cr.Status.AdminSecretChanged, false)
	}

	// Check each replica
	for i := int32(0); i < cr.Spec.Replicas; i++ {
		podName := fmt.Sprintf("%s-%d", GetSplunkStatefulsetName(SplunkStandalone, cr.GetName()), i)

		// Get current admin password from pod's secret
		podSecret, err := splutil.GetSecretFromPod(ctx, client, podName, cr.GetNamespace())
		if err != nil {
			scopedLog.Info("Could not get secret from pod, skipping", "pod", podName, "error", err.Error())
			continue // Pod might not be ready yet
		}

		adminPwd := string(podSecret.Data["password"])

		// If admin secret is different from namespace scoped secret, change it
		if adminPwd != nsAdminSecret {
			scopedLog.Info("admin password different from namespace scoped secret, changing admin password", "pod", podName)

			// Skip if already changed for this replica
			if i < int32(len(cr.Status.AdminSecretChanged)) && cr.Status.AdminSecretChanged[i] {
				scopedLog.Info("Admin password already changed for this replica, skipping", "replica", i)
				continue
			}

			// Change admin password on splunk instance
			podExecClient := splutil.GetPodExecClient(client, cr, podName)
			command := fmt.Sprintf("/opt/splunk/bin/splunk cmd splunkd rest --noauth POST /services/admin/users/admin 'password=%s'", nsAdminSecret)
			streamOptions := splutil.NewStreamOptionsObject(command)

			_, _, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
			if err != nil {
				scopedLog.Error(err, "Failed to change admin password on splunk instance", "pod", podName)
				continue
			}

			scopedLog.Info("admin password changed on the splunk instance", "pod", podName)

			// Restart Splunk to ensure consistency
			restartCommand := "/opt/splunk/bin/splunk restart"
			restartOptions := splutil.NewStreamOptionsObject(restartCommand)
			_, _, err = podExecClient.RunPodExecCommand(ctx, restartOptions, []string{"/bin/sh"})
			if err != nil {
				scopedLog.Error(err, "Failed to restart Splunk", "pod", podName)
				// Continue even if restart fails - password change might still work
			} else {
				scopedLog.Info("Splunk restarted successfully", "pod", podName)
			}

			// Mark as changed
			if i < int32(len(cr.Status.AdminSecretChanged)) {
				cr.Status.AdminSecretChanged[i] = true
			} else {
				cr.Status.AdminSecretChanged = append(cr.Status.AdminSecretChanged, true)
			}

			// Update secret change tracking
			cr.Status.AdminPasswordChangedSecrets[podSecret.GetName()] = true

			scopedLog.Info("Admin password sync completed for replica", "replica", i)
		}
	}

	return nil
}

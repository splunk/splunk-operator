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

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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

	// validate and updates defaults for CR
	err := validateStandaloneSpec(ctx, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateStandaloneSpec", fmt.Sprintf("validate standalone spec failed %s", err.Error()))
		scopedLog.Error(err, "Failed to validate standalone spec")
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas

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
	defer func() {
		client.Status().Update(ctx, cr)
		if err != nil {
			eventPublisher.Warning(ctx, "Update", fmt.Sprintf("update custom resource failed %s", err.Error()))
			scopedLog.Error(err, "Status update failed")
		}
	}()

	// create or update general config resources
	_, err = ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkStandalone)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, err
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
		DeleteOwnerReferencesForResources(ctx, client, cr, &cr.Spec.SmartStore)
		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = splcommon.PhaseTerminating
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
	// TODO: Improve this logic so that we only recycle the new pod/replica
	// and not all the existing pods.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 && cr.Status.ReadyReplicas > 0 {

		statefulsetName := GetSplunkStatefulsetName(SplunkStandalone, cr.GetName())

		isScalingUp, err := splctrl.IsStatefulSetScalingUp(ctx, client, cr, statefulsetName, cr.Spec.Replicas)
		if err != nil {
			return result, err
		} else if isScalingUp {
			// if we are indeed scaling up, then mark the deploy status to Pending
			// for all the app sources so that we add all the app sources in configMap.
			cr.Status.AppContext.IsDeploymentInProgress = true
			appStatusContext := cr.Status.AppContext
			for appSrc := range appStatusContext.AppsSrcDeployStatus {
				changeAppSrcDeployInfoStatus(ctx, appSrc, appStatusContext.AppsSrcDeployStatus, enterpriseApi.RepoStateActive, enterpriseApi.DeployStatusComplete, enterpriseApi.DeployStatusPending)
			}

			// Now apply the configMap will full app listing.
			_, _, err = ApplyAppListingConfigMap(ctx, client, cr, &cr.Spec.AppFrameworkConfig, appStatusContext.AppsSrcDeployStatus, false)
			if err != nil {
				eventPublisher.Warning(ctx, "ApplyAppListingConfigMap", fmt.Sprintf("apply app list config map failed %s", err.Error()))
				return result, err
			}
		}
	}

	// create or update statefulset
	statefulSet, err := getStandaloneStatefulSet(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "getStandaloneStatefulSet", fmt.Sprintf("get standalone status set failed %s", err.Error()))
		return result, err
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
	if cr.Status.Phase == splcommon.PhaseReady {
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
		if cr.Status.AppContext.AppsSrcDeployStatus != nil {
			markAppsStatusToComplete(ctx, client, cr, &cr.Spec.AppFrameworkConfig, cr.Status.AppContext.AppsSrcDeployStatus)
			// Schedule one more reconcile in next 5 seconds, just to cover any latest app framework config changes
			if cr.Status.AppContext.IsDeploymentInProgress {
				cr.Status.AppContext.IsDeploymentInProgress = false
				return result, nil
			}
		}
		// Requeue the reconcile after polling interval if we had set the lastAppInfoCheckTime.
		if cr.Status.AppContext.LastAppInfoCheckTime != 0 {
			result.RequeueAfter = GetNextRequeueTime(ctx, cr.Status.AppContext.AppsRepoStatusPollInterval, cr.Status.AppContext.LastAppInfoCheckTime)
		} else {
			result.Requeue = false
		}
	} /*else if cr.Status.Phase == splcommon.PhasePending {
		result.Requeue = false
	} */

	/*if !result.Requeue {
		return reconcile.Result{}, nil
	} */
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
		setupInitContainer(&ss.Spec.Template, cr.Spec.Image, cr.Spec.ImagePullPolicy, commandForStandaloneSmartstore)
	}

	// Setup App framework init containers
	setupAppInitContainers(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, nil
}

// validateStandaloneSpec checks validity and makes default updates to a StandaloneSpec, and returns error if something is wrong.
func validateStandaloneSpec(ctx context.Context, cr *enterpriseApi.Standalone) error {
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
		err := ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(&cr.Spec.CommonSplunkSpec)
}

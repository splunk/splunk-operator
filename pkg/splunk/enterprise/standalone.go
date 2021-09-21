// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
)

// ApplyStandalone reconciles the StatefulSet for N standalone instances of Splunk Enterprise.
func ApplyStandalone(client splcommon.ControllerClient, cr *enterpriseApi.Standalone) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	scopedLog := log.WithName("ApplyStandalone").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	// validate and updates defaults for CR
	err := validateStandaloneSpec(cr)
	if err != nil {
		scopedLog.Error(err, "Failed to validate standalone spec")
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) ||
		AreRemoteVolumeKeysChanged(client, cr, SplunkStandalone, &cr.Spec.SmartStore, cr.Status.ResourceRevMap, &err) {

		if err != nil {
			return result, err
		}

		_, _, err := ApplySmartstoreConfigMap(client, cr, &cr.Spec.SmartStore)
		if err != nil {
			return result, err
		}

		cr.Status.SmartStore = cr.Spec.SmartStore
	}

	// If the app framework is configured then do following things -
	// 1. Initialize the S3Clients based on providers
	// 2. Check the status of apps on remote storage.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err := initAndCheckAppInfoStatus(client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			cr.Status.AppContext.IsDeploymentInProgress = false
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-standalone", cr.GetName())
	defer func() {
		client.Status().Update(context.TODO(), cr)
		if err != nil {
			scopedLog.Error(err, "Status update failed")
		}
	}()

	// create or update general config resources
	_, err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, SplunkStandalone)
	if err != nil {
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		//update monitoring console configMap after custom resource deletion is requested
		err = ApplyMonitoringConsole(client, cr, cr.Spec.CommonSplunkSpec, getStandaloneExtraEnv(cr, cr.Spec.Replicas))
		if err != nil {
			return result, err
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMap(client, cr, SplunkStandalone)
			if err != nil {
				return result, err
			}
		}

		DeleteOwnerReferencesForResources(client, cr, &cr.Spec.SmartStore)
		terminating, err := splctrl.CheckForDeletion(cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = splcommon.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update a headless service
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, true))
	if err != nil {
		return result, err
	}

	// create or update a regular service
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, false))
	if err != nil {
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

		isScalingUp, err := splctrl.IsStatefulSetScalingUp(client, cr, statefulsetName, cr.Spec.Replicas)
		if err != nil {
			return result, err
		} else if isScalingUp {
			// if we are indeed scaling up, then mark the deploy status to Pending
			// for all the app sources so that we add all the app sources in configMap.
			cr.Status.AppContext.IsDeploymentInProgress = true
			appStatusContext := cr.Status.AppContext
			for appSrc := range appStatusContext.AppsSrcDeployStatus {
				changeAppSrcDeployInfoStatus(appSrc, appStatusContext.AppsSrcDeployStatus, enterpriseApi.RepoStateActive, enterpriseApi.DeployStatusComplete, enterpriseApi.DeployStatusPending)
			}

			// Now apply the configMap will full app listing.
			_, _, err = ApplyAppListingConfigMap(client, cr, &cr.Spec.AppFrameworkConfig, appStatusContext.AppsSrcDeployStatus, false)
			if err != nil {
				return result, err
			}
		}
	}

	// create or update statefulset
	statefulSet, err := getStandaloneStatefulSet(client, cr)
	if err != nil {
		return result, err
	}

	mgr := splctrl.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(client, statefulSet, cr.Spec.Replicas)
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == splcommon.PhaseReady {
		if cr.Status.AppContext.AppsSrcDeployStatus != nil {
			markAppsStatusToComplete(client, cr, &cr.Spec.AppFrameworkConfig, cr.Status.AppContext.AppsSrcDeployStatus)
			// Schedule one more reconcile in next 5 seconds, just to cover any latest app framework config changes
			if cr.Status.AppContext.IsDeploymentInProgress {
				cr.Status.AppContext.IsDeploymentInProgress = false
				return result, nil
			}
		}

		err = ApplyMonitoringConsole(client, cr, cr.Spec.CommonSplunkSpec, getStandaloneExtraEnv(cr, cr.Spec.Replicas))
		if err != nil {
			return result, err
		}

		// Requeue the reconcile after polling interval if we had set the lastAppInfoCheckTime.
		if cr.Status.AppContext.LastAppInfoCheckTime != 0 {
			result.RequeueAfter = GetNextRequeueTime(cr.Status.AppContext.AppsRepoStatusPollInterval, cr.Status.AppContext.LastAppInfoCheckTime)
		} else {
			result.Requeue = false
		}
	}
	return result, nil
}

// getStandaloneStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise standalone instances.
func getStandaloneStatefulSet(client splcommon.ControllerClient, cr *enterpriseApi.Standalone) (*appsv1.StatefulSet, error) {
	// get generic statefulset for Splunk Enterprise objects
	ss, err := getSplunkStatefulSet(client, cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, cr.Spec.Replicas, []corev1.EnvVar{})
	if err != nil {
		return nil, err
	}

	smartStoreConfigMap := getSmartstoreConfigMap(client, cr, SplunkStandalone)

	if smartStoreConfigMap != nil {
		setupInitContainer(&ss.Spec.Template, cr.Spec.Image, cr.Spec.ImagePullPolicy, commandForStandaloneSmartstore)
	}

	// Setup App framework init containers
	setupAppInitContainers(client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, nil
}

// validateStandaloneSpec checks validity and makes default updates to a StandaloneSpec, and returns error if something is wrong.
func validateStandaloneSpec(cr *enterpriseApi.Standalone) error {
	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) {
		err := ValidateSplunkSmartstoreSpec(&cr.Spec.SmartStore)
		if err != nil {
			return err
		}
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(&cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(&cr.Spec.CommonSplunkSpec)
}

// helper function to get the list of Standalone types in the current namespace
func getStandaloneList(c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (int, error) {
	scopedLog := log.WithName("getStandaloneList").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.StandaloneList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	numOfObjects := len(objectList.Items)

	if err != nil {
		scopedLog.Error(err, "Standalone types not found in namespace", "namsespace", cr.GetNamespace())
		return numOfObjects, err
	}

	return numOfObjects, nil
}

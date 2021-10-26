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
	"errors"
	"reflect"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	recClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
)

// ApplyLicenseManager reconciles the state for the Splunk Enterprise license manager.
func ApplyLicenseManager(client splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	scopedLog := log.WithName("ApplyLicenseManager").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	LegacyManager, err := getLicenseManagerList(client, cr)
	if LegacyManager && cr.ObjectMeta.DeletionTimestamp == nil {
		// Needs Logic to remove/delete current CR
		deleteCRManager(client, cr)
		return reconcile.Result{}, err
	} else {
		println("No License Manager, continue deployment")
	}

	// validate and updates defaults for CR
	err = validateLicenseManagerSpec(cr)
	if err != nil {
		scopedLog.Error(err, "Failed to validate license manager spec")
		return result, err
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

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	defer func() {
		client.Status().Update(context.TODO(), cr)
	}()

	// create or update general config resources
	_, err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, SplunkLicenseNewManager)
	if err != nil {
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getLicenseManagerURL(cr, &cr.Spec.CommonSplunkSpec), false)
			if err != nil {
				return result, err
			}
		}
		DeleteOwnerReferencesForResources(client, cr, nil)
		terminating, err := splctrl.CheckForDeletion(cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = splcommon.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update a service
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkLicenseNewManager, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset
	statefulSet, err := getLicenseManagerStatefulSet(client, cr)
	if err != nil {
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = validateMonitoringConsoleRef(client, statefulSet, getLicenseManagerURL(cr, &cr.Spec.CommonSplunkSpec))
	if err != nil {
		return result, err
	}

	mgr := splctrl.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(client, statefulSet, 1)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == splcommon.PhaseReady {
		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = splctrl.DeleteReferencesToAutomatedMCIfExists(client, cr, namespacedName)
		if err != nil {
			scopedLog.Error(err, "Error in deleting automated monitoring console resource")
		}
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getLicenseManagerURL(cr, &cr.Spec.CommonSplunkSpec), true)
			if err != nil {
				return result, err
			}
		}
		if cr.Status.AppContext.AppsSrcDeployStatus != nil {
			markAppsStatusToComplete(client, cr, &cr.Spec.AppFrameworkConfig, cr.Status.AppContext.AppsSrcDeployStatus)
			// Schedule one more reconcile in next 5 seconds, just to cover any latest app framework config changes
			if cr.Status.AppContext.IsDeploymentInProgress {
				cr.Status.AppContext.IsDeploymentInProgress = false
				return result, nil
			}
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

// getLicenseManagerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license manager.
func getLicenseManagerStatefulSet(client splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) (*appsv1.StatefulSet, error) {
	ss, err := getSplunkStatefulSet(client, cr, &cr.Spec.CommonSplunkSpec, SplunkLicenseNewManager, 1, []corev1.EnvVar{})
	if err != nil {
		return ss, err
	}

	// Setup App framework init containers
	setupAppInitContainers(client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// validateLicenseManagerSpec checks validity and makes default updates to a LicenseManagerSpec, and returns error if something is wrong.
func validateLicenseManagerSpec(cr *enterpriseApi.LicenseManager) error {

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(&cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(&cr.Spec.CommonSplunkSpec)
}

func deleteCRManager(c splcommon.ControllerClient, cr splcommon.MetaObject) bool {
	// scopedLog := log.WithName("RemovingLicenseManager").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// podList := &corev1.Deployment
	// opts := []recClient.ListOption{
	// 	recClient.InNamespace(cr.GetNamespace()),
	// }

	// err := c.List(context.TODO(), podList, opts...)
	// if err != nil {
	// 	scopedLog.Error(err, "LicenseManager types not found in namespace", "namespace", cr.GetNamespace())
	// 	return false, err
	// }

	// currentTime := metav1.NewTime(time.Now())
	// cr.SetDeletionTimestamp(&currentTime)
	// _, err := ApplyLicenseManager(c, cr.(*enterpriseApi.LicenseManager))
	return true
}

// helper function to get the list of LicenseMaster types in the current namespace
func getLicenseManagerList(c splcommon.ControllerClient, cr splcommon.MetaObject) (bool, error) {
	scopedLog := log.WithName("getLicenseMasterList").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	podList := &corev1.PodList{}
	opts := []recClient.ListOption{
		recClient.InNamespace(cr.GetNamespace()),
	}

	err := c.List(context.TODO(), podList, opts...)
	if err != nil {
		scopedLog.Error(err, "LicenseManager types not found in namespace", "namespace", cr.GetNamespace())
		return false, err
	}

	for i := 0; i < len(podList.Items); i++ {
		if strings.Contains(podList.Items[i].Name, "license-master") {
			return true, errors.New("Found a License Master already deployed")
		}
	}
	return false, err
}

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
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
)

// ApplyLicenseMaster reconciles the state for the Splunk Enterprise license master.
func ApplyLicenseMaster(client splcommon.ControllerClient, cr *enterprisev1.LicenseMaster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	scopedLog := log.WithName("ApplyLicenseMaster").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// validate and updates defaults for CR
	err := validateLicenseMasterSpec(&cr.Spec)
	if err != nil {
		return result, err
	}

	if cr.Spec.CommonSplunkSpec.Mock != true && !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {

		var sourceToAppsList map[string]splclient.S3Response

		for _, vol := range cr.Spec.AppFrameworkConfig.VolList {
			splclient.RegisterS3Client(vol.Provider)
		}

		sourceToAppsList, err = GetAppListFromS3Bucket(client, cr, &cr.Spec.AppFrameworkConfig)
		if err != nil {
			scopedLog.Error(err, "Unable to get apps list from remote storage")
			return result, err
		}

		for _, appSource := range cr.Spec.AppFrameworkConfig.AppSources {
			scopedLog.Info("Apps List retrieved from remote storage", "App Source", appSource.Name, "Content", sourceToAppsList[appSource.Name].Objects)
		}
		cr.Status.AppContext.AppFrameworkConfig = cr.Spec.AppFrameworkConfig
	}

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	defer func() {
		client.Status().Update(context.TODO(), cr)
	}()

	// create or update general config resources
	_, err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, SplunkLicenseMaster)
	if err != nil {
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		err = ApplyMonitoringConsole(client, cr, cr.Spec.CommonSplunkSpec, getLicenseMasterURL(cr, &cr.Spec.CommonSplunkSpec))
		if err != nil {
			return result, err
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
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkLicenseMaster, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset
	statefulSet, err := getLicenseMasterStatefulSet(client, cr)
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
		err = ApplyMonitoringConsole(client, cr, cr.Spec.CommonSplunkSpec, getLicenseMasterURL(cr, &cr.Spec.CommonSplunkSpec))
		if err != nil {
			return result, err
		}
		result.Requeue = false
	}
	return result, nil
}

// getLicenseMasterStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license master.
func getLicenseMasterStatefulSet(client splcommon.ControllerClient, cr *enterprisev1.LicenseMaster) (*appsv1.StatefulSet, error) {
	return getSplunkStatefulSet(client, cr, &cr.Spec.CommonSplunkSpec, SplunkLicenseMaster, 1, []corev1.EnvVar{})
}

// validateLicenseMasterSpec checks validity and makes default updates to a LicenseMasterSpec, and returns error if something is wrong.
func validateLicenseMasterSpec(spec *enterprisev1.LicenseMasterSpec) error {

	err := ValidateAppFrameworkSpec(&spec.AppFrameworkConfig, true)
	if err != nil {
		return err
	}

	return validateCommonSplunkSpec(&spec.CommonSplunkSpec)
}

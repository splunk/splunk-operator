// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
)

// ApplyClusterMaster reconciles the state of a Splunk Enterprise cluster master.
func ApplyClusterMaster(client splcommon.ControllerClient, cr *enterprisev1.ClusterMaster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	scopedLog := log.WithName("ApplyClusterMaster").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	// validate and updates defaults for CR
	err := validateClusterMasterSpec(cr)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-cluster-master", cr.GetName())

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) ||
		AreRemoteVolumeKeysChanged(client, cr, SplunkClusterMaster, &cr.Spec.SmartStore, cr.Status.ResourceRevMap, &err) {

		_, configMapDataChanged, err := ApplySmartstoreConfigMap(client, cr, &cr.Spec.SmartStore)
		if err != nil {
			return result, err
		} else if configMapDataChanged {
			// Do not auto populate with configMapDataChanged flag to NeedToPushMasterApps. Set it only  if
			// configMapDataChanged it true. It mush be reset, only upon initiating the bundle push REST call,
			// once the CM is in ready state otherwise, we keep retrying
			cr.Status.BundlePushTracker.NeedToPushMasterApps = true
			cr.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix()
		}

		cr.Status.SmartStore = cr.Spec.SmartStore
	}

	defer func() {
		err = client.Status().Update(context.TODO(), cr)
		if err != nil {
			scopedLog.Error(err, "Status update failed")
		}
	}()

	// create or update general config resources
	_, err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer)
	if err != nil {
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		err = ApplyMonitoringConsole(client, cr, cr.Spec.CommonSplunkSpec, getClusterMasterExtraEnv(cr, &cr.Spec.CommonSplunkSpec))
		if err != nil {
			return result, err
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

	// create or update a regular service for indexer cluster (ingestion)
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, false))
	if err != nil {
		return result, err
	}

	// create or update a regular service for the cluster master
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkClusterMaster, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset for the cluster master
	statefulSet, err := getClusterMasterStatefulSet(client, cr)
	if err != nil {
		return result, err
	}
	clusterMasterManager := splctrl.DefaultStatefulSetPodManager{}
	phase, err := clusterMasterManager.Update(client, statefulSet, 1)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == splcommon.PhaseReady {
		err = ApplyMonitoringConsole(client, cr, cr.Spec.CommonSplunkSpec, getClusterMasterExtraEnv(cr, &cr.Spec.CommonSplunkSpec))
		if err != nil {
			return result, err
		}

		// Master apps bundle push requires multiple reconcile iterations in order to reflect the configMap on the CM pod.
		// So keep PerformCmBundlePush() as the last call in this block of code, so that other functionalities are not blocked
		err = PerformCmBundlePush(client, cr)
		if err != nil {
			return result, err
		}

		if cr.Status.BundlePushTracker.NeedToPushMasterApps == false {
			result.Requeue = false
		}
	}
	return result, nil
}

// validateClusterMasterSpec checks validity and makes default updates to a ClusterMasterSpec, and returns error if something is wrong.
func validateClusterMasterSpec(cr *enterprisev1.ClusterMaster) error {
	err := ValidateSplunkSmartstoreSpec(&cr.Spec.SmartStore)
	if err != nil {
		return err
	}

	cr.Spec.SparkImage = spark.GetSparkImage(cr.Spec.SparkImage)

	return validateCommonSplunkSpec(&cr.Spec.CommonSplunkSpec)
}

// getClusterMasterStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license master.
func getClusterMasterStatefulSet(client splcommon.ControllerClient, cr *enterprisev1.ClusterMaster) (*appsv1.StatefulSet, error) {
	var extraEnvVar []corev1.EnvVar

	ss, err := getSplunkStatefulSet(client, cr, &cr.Spec.CommonSplunkSpec, SplunkClusterMaster, 1, extraEnvVar)
	if err != nil {
		return ss, err
	}
	_, exists := getSmartstoreConfigMap(client, cr, SplunkClusterMaster)

	if exists {
		setupInitContainer(&ss.Spec.Template, cr.Spec.SparkImage, cr.Spec.ImagePullPolicy, commandForCMSmartstore)
	}

	return ss, err
}

// CheckIfsmartstoreConfigMapUpdatedToPod checks if the smartstore configMap is updated on Pod or not
func CheckIfsmartstoreConfigMapUpdatedToPod(c splcommon.ControllerClient, cr *enterprisev1.ClusterMaster) error {
	scopedLog := log.WithName("CheckIfsmartstoreConfigMapUpdatedToPod").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	masterIdxcName := cr.GetName()
	cmPodName := fmt.Sprintf("splunk-%s-cluster-master-0", masterIdxcName)

	command := fmt.Sprintf("cat /mnt/splunk-operator/local/%s", configToken)
	stdOut, stdErr, err := splutil.PodExecCommand(c, cmPodName, cr.GetNamespace(), []string{"/bin/sh"}, command, false, false)
	if err != nil || stdErr != "" {
		return fmt.Errorf("Failed to check config token value on pod. stdout=%s, stderror=%s, error=%v", stdOut, stdErr, err)
	}

	configMap, exists := getSmartstoreConfigMap(c, cr, SplunkClusterMaster)
	if exists {
		tokenFromConfigMap := configMap.Data[configToken]
		if tokenFromConfigMap == stdOut {
			scopedLog.Info("Token Matched.", "on Pod=", stdOut, "from configMap=", tokenFromConfigMap)
			return nil
		}
		return fmt.Errorf("Waiting for the configMap update to the Pod. Token on Pod=%s, Token from configMap=%s", stdOut, tokenFromConfigMap)
	}

	// Somehow the configmap was deleted, ideally this should not happen
	return fmt.Errorf("Smartstore ConfigMap is missing")
}

// PerformCmBundlePush initiates the bundle push from cluster master
func PerformCmBundlePush(c splcommon.ControllerClient, cr *enterprisev1.ClusterMaster) error {
	if cr.Status.BundlePushTracker.NeedToPushMasterApps == false {
		return nil
	}

	scopedLog := log.WithName("PerformCmBundlePush").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	// Reconciler can be called for multiple reasons. If we are waiting on configMap update to happen,
	// do not increment the Retry Count unless the last check was 5 seconds ago.
	// This helps, to wait for the required time
	currentEpoch := time.Now().Unix()
	if cr.Status.BundlePushTracker.LastCheckInterval+5 > currentEpoch {
		return fmt.Errorf("Will re-attempt to push the bundle after the 5 seconds period passed from last check. LastCheckInterval=%d, current epoch=%d", cr.Status.BundlePushTracker.LastCheckInterval, currentEpoch)
	}

	scopedLog.Info("Attempting to push the bundle")
	cr.Status.BundlePushTracker.LastCheckInterval = currentEpoch

	// The amount of time it takes for the configMap update to Pod depends on
	// how often the Kubelet on the K8 node refreshes its cache with API server.
	// From our tests, the Pod can take as high as 90 seconds. So keep checking
	// for the configMap update to the Pod before proceeding for the master apps
	// bundle push.

	err := CheckIfsmartstoreConfigMapUpdatedToPod(c, cr)
	if err != nil {
		return err
	}

	err = PushMasterAppsBundle(c, cr)
	if err == nil {
		scopedLog.Info("Bundle push success")
		cr.Status.BundlePushTracker.NeedToPushMasterApps = false
	}

	return err
}

// PushMasterAppsBundle issues the REST command to for cluster master bundle push
func PushMasterAppsBundle(c splcommon.ControllerClient, cr *enterprisev1.ClusterMaster) error {
	scopedLog := log.WithName("PushMasterApps").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	defaultSecret, err := splutil.GetSecretByName(c, cr, defaultSecretObjName)
	if err != nil {
		return fmt.Errorf("Could not access default secret object to fetch admin password. Reason %v", err)
	}

	//Get the admin password from the secret object
	adminPwd, foundSecret := defaultSecret.Data["password"]
	if foundSecret == false {
		return fmt.Errorf("Could not find admin password while trying to push the master apps bundle")
	}

	scopedLog.Info("Issueing REST call to push master aps bundle")

	masterIdxcName := cr.GetName()
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), GetSplunkServiceName(SplunkClusterMaster, masterIdxcName, false))

	// Get a Splunk client to execute the REST call
	splunkClient := splclient.NewSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(adminPwd))

	return splunkClient.BundlePush(true)
}

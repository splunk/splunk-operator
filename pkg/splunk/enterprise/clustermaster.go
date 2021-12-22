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
	"strings"
	"time"

	"github.com/go-logr/logr"
	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
)

// ApplyClusterManager reconciles the state of a Splunk Enterprise cluster manager.
func ApplyClusterManager(client splcommon.ControllerClient, cr *enterpriseApi.ClusterMaster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	scopedLog := log.WithName("ApplyClusterManager").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	// validate and updates defaults for CR
	err := validateClusterManagerSpec(cr)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-%s", cr.GetName(), splcommon.ClusterManager)

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) ||
		AreRemoteVolumeKeysChanged(client, cr, SplunkClusterManager, &cr.Spec.SmartStore, cr.Status.ResourceRevMap, &err) {

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

	// This is to take care of case where AreRemoteVolumeKeysChanged returns an error if it returns false.
	if err != nil {
		return result, err
	}

	defer func() {
		err = client.Status().Update(context.TODO(), cr)
		if err != nil {
			scopedLog.Error(err, "Status update failed")
		}
	}()

	// If needed, Migrate the app framework status
	err = checkAndMigrateAppDeployStatus(client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, false)
	if err != nil {
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

	// create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer)
	if err != nil {
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			extraEnv, err := VerifyCMisMultisite(cr, namespaceScopedSecret)
			_, err = ApplyMonitoringConsoleEnvConfigMap(client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, extraEnv, false)
			if err != nil {
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMap(client, cr, SplunkClusterManager)
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

	// create or update a regular service for indexer cluster (ingestion)
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, false))
	if err != nil {
		return result, err
	}

	// create or update a regular service for the cluster manager
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkClusterManager, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset for the cluster manager
	statefulSet, err := getClusterManagerStatefulSet(client, cr)
	if err != nil {
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	extraEnv, err := VerifyCMisMultisite(cr, namespaceScopedSecret)
	err = validateMonitoringConsoleRef(client, statefulSet, extraEnv)
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
		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = splctrl.DeleteReferencesToAutomatedMCIfExists(client, cr, namespacedName)
		if err != nil {
			scopedLog.Error(err, "Error in deleting automated monitoring console resource")
		}
		//Update MC configmap
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, extraEnv, true)
			if err != nil {
				return result, err
			}
		}

		// Manager apps bundle push requires multiple reconcile iterations in order to reflect the configMap on the CM pod.
		// So keep PerformCmBundlePush() as the last call in this block of code, so that other functionalities are not blocked
		err = PerformCmBundlePush(client, cr)
		if err != nil {
			return result, err
		}

		finalResult := handleAppFrameworkActivity(client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
		result = *finalResult
	}
	return result, nil
}

// clusterMasterPodManager is used to manage the cluster manager pod
type clusterManagerPodManager struct {
	c               splcommon.ControllerClient
	log             logr.Logger
	cr              *enterpriseApi.ClusterMaster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// getClusterMasterClient for clusterMasterPodManager returns a SplunkClient for cluster manager
func (mgr *clusterManagerPodManager) getClusterManagerClient(cr *enterpriseApi.ClusterMaster) *splclient.SplunkClient {
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), GetSplunkServiceName(SplunkClusterManager, cr.GetName(), false))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// validateClusterMasterSpec checks validity and makes default updates to a ClusterMasterSpec, and returns error if something is wrong.
func validateClusterManagerSpec(cr *enterpriseApi.ClusterMaster) error {

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) {
		err := ValidateSplunkSmartstoreSpec(&cr.Spec.SmartStore)
		if err != nil {
			return err
		}
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(&cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, false)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(&cr.Spec.CommonSplunkSpec)
}

// getClusterManagerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license manager.
func getClusterManagerStatefulSet(client splcommon.ControllerClient, cr *enterpriseApi.ClusterMaster) (*appsv1.StatefulSet, error) {
	var extraEnvVar []corev1.EnvVar

	ss, err := getSplunkStatefulSet(client, cr, &cr.Spec.CommonSplunkSpec, SplunkClusterManager, 1, extraEnvVar)
	if err != nil {
		return ss, err
	}
	smartStoreConfigMap := getSmartstoreConfigMap(client, cr, SplunkClusterManager)

	if smartStoreConfigMap != nil {
		setupInitContainer(&ss.Spec.Template, cr.Spec.Image, cr.Spec.ImagePullPolicy, commandForCMSmartstore)
	}

	// Setup App framework init containers
	setupAppInitContainers(client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// CheckIfsmartstoreConfigMapUpdatedToPod checks if the smartstore configMap is updated on Pod or not
func CheckIfsmartstoreConfigMapUpdatedToPod(c splcommon.ControllerClient, cr *enterpriseApi.ClusterMaster) error {
	scopedLog := log.WithName("CheckIfsmartstoreConfigMapUpdatedToPod").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	managerIdxcName := cr.GetName()
	cmPodName := fmt.Sprintf("splunk-%s-%s-0", managerIdxcName, splcommon.ClusterManager)

	command := fmt.Sprintf("cat /mnt/splunk-operator/local/%s", configToken)
	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader(command),
	}
	stdOut, stdErr, err := splutil.PodExecCommand(c, cmPodName, cr.GetNamespace(), []string{"/bin/sh"}, streamOptions, false, false)
	if err != nil || stdErr != "" {
		return fmt.Errorf("failed to check config token value on pod. stdout=%s, stderror=%s, error=%v", stdOut, stdErr, err)
	}

	smartStoreConfigMap := getSmartstoreConfigMap(c, cr, SplunkClusterManager)
	if smartStoreConfigMap != nil {
		tokenFromConfigMap := smartStoreConfigMap.Data[configToken]
		if tokenFromConfigMap == stdOut {
			scopedLog.Info("Token Matched.", "on Pod=", stdOut, "from configMap=", tokenFromConfigMap)
			return nil
		}
		return fmt.Errorf("waiting for the configMap update to the Pod. Token on Pod=%s, Token from configMap=%s", stdOut, tokenFromConfigMap)
	}

	// Somehow the configmap was deleted, ideally this should not happen
	return fmt.Errorf("smartstore ConfigMap is missing")
}

// PerformCmBundlePush initiates the bundle push from cluster manager
func PerformCmBundlePush(c splcommon.ControllerClient, cr *enterpriseApi.ClusterMaster) error {
	if !cr.Status.BundlePushTracker.NeedToPushMasterApps {
		return nil
	}

	scopedLog := log.WithName("PerformCmBundlePush").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	// Reconciler can be called for multiple reasons. If we are waiting on configMap update to happen,
	// do not increment the Retry Count unless the last check was 5 seconds ago.
	// This helps, to wait for the required time
	currentEpoch := time.Now().Unix()
	if cr.Status.BundlePushTracker.LastCheckInterval+5 > currentEpoch {
		return fmt.Errorf("will re-attempt to push the bundle after the 5 seconds period passed from last check. LastCheckInterval=%d, current epoch=%d", cr.Status.BundlePushTracker.LastCheckInterval, currentEpoch)
	}

	scopedLog.Info("Attempting to push the bundle")
	cr.Status.BundlePushTracker.LastCheckInterval = currentEpoch

	// The amount of time it takes for the configMap update to Pod depends on
	// how often the Kubelet on the K8 node refreshes its cache with API server.
	// From our tests, the Pod can take as high as 90 seconds. So keep checking
	// for the configMap update to the Pod before proceeding for the manager apps
	// bundle push.

	err := CheckIfsmartstoreConfigMapUpdatedToPod(c, cr)
	if err != nil {
		return err
	}

	err = PushManagerAppsBundle(c, cr)
	if err == nil {
		scopedLog.Info("Bundle push success")
		cr.Status.BundlePushTracker.NeedToPushMasterApps = false
	}

	return err
}

// PushManagerAppsBundle issues the REST command to for cluster manager bundle push
func PushManagerAppsBundle(c splcommon.ControllerClient, cr *enterpriseApi.ClusterMaster) error {
	scopedLog := log.WithName("PushMasterApps").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	defaultSecret, err := splutil.GetSecretByName(c, cr, defaultSecretObjName)
	if err != nil {
		return fmt.Errorf("could not access default secret object to fetch admin password. Reason %v", err)
	}

	//Get the admin password from the secret object
	adminPwd, foundSecret := defaultSecret.Data["password"]
	if !foundSecret {
		return fmt.Errorf("could not find admin password while trying to push the manager apps bundle")
	}

	scopedLog.Info("Issuing REST call to push manager aps bundle")

	managerIdxcName := cr.GetName()
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), GetSplunkServiceName(SplunkClusterManager, managerIdxcName, false))

	// Get a Splunk client to execute the REST call
	splunkClient := splclient.NewSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(adminPwd))

	return splunkClient.BundlePush(true)
}

// helper function to get the list of ClusterManager types in the current namespace
func getClusterManagerList(c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (int, error) {
	scopedLog := log.WithName("getClusterMasterList").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.ClusterMasterList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	numOfObjects := len(objectList.Items)

	if err != nil {
		scopedLog.Error(err, "ClusterMaster types not found in namespace", "namsespace", cr.GetNamespace())
		return numOfObjects, err
	}

	return numOfObjects, nil
}

//VerifyCMisMultisite checks if its a multisite
func VerifyCMisMultisite(cr *enterpriseApi.ClusterMaster, namespaceScopedSecret *corev1.Secret) ([]corev1.EnvVar, error) {
	var err error
	scopedLog := log.WithName("Verify if Multisite Indexer Cluster").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	mgr := clusterManagerPodManager{log: scopedLog, cr: cr, secrets: namespaceScopedSecret, newSplunkClient: splclient.NewSplunkClient}
	cm := mgr.getClusterManagerClient(cr)
	clusterInfo, err := cm.GetClusterInfo(false)
	if err != nil {
		return nil, err
	}
	multiSite := clusterInfo.MultiSite
	extraEnv := getClusterMasterExtraEnv(cr, &cr.Spec.CommonSplunkSpec)
	if multiSite == "true" {
		extraEnv = append(extraEnv, corev1.EnvVar{Name: "SPLUNK_SITE", Value: "site0"}, corev1.EnvVar{Name: "SPLUNK_MULTISITE_MASTER", Value: GetSplunkServiceName(SplunkClusterManager, cr.GetName(), false)})
	}
	return extraEnv, err
}

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

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"sigs.k8s.io/controller-runtime/pkg/client"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyClusterManager reconciles the state of a Splunk Enterprise cluster manager.
func ApplyClusterManager(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyClusterManager")
	eventPublisher, _ := newK8EventPublisher(client, cr)
	cr.Kind = "ClusterManager"

	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	// validate and updates defaults for CR
	err := validateClusterManagerSpec(ctx, client, cr)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = enterpriseApi.PhaseError
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-%s", cr.GetName(), "cluster-manager")

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) ||
		AreRemoteVolumeKeysChanged(ctx, client, cr, SplunkClusterManager, &cr.Spec.SmartStore, cr.Status.ResourceRevMap, &err) {

		if err != nil {
			eventPublisher.Warning(ctx, "AreRemoteVolumeKeysChanged", fmt.Sprintf("check remote volume key change failed %s", err.Error()))
			return result, err
		}

		_, configMapDataChanged, err := ApplySmartstoreConfigMap(ctx, client, cr, &cr.Spec.SmartStore)
		if err != nil {
			return result, err
		} else if configMapDataChanged {
			// Do not auto populate with configMapDataChanged flag to NeedToPushManagerApps. Set it only  if
			// configMapDataChanged it true. It mush be reset, only upon initiating the bundle push REST call,
			// once the CM is in ready state otherwise, we keep retrying
			cr.Status.BundlePushTracker.NeedToPushManagerApps = true
			cr.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix()
		}

		cr.Status.SmartStore = cr.Spec.SmartStore
	}

	// This is to take care of case where AreRemoteVolumeKeysChanged returns an error if it returns false.
	if err != nil {
		return result, err
	}

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr)

	// If needed, Migrate the app framework status
	err = checkAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, false)
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

	// create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			extraEnv, _ := VerifyCMisMultisite(ctx, cr, namespaceScopedSecret)
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, extraEnv, false)
			if err != nil {
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, SplunkClusterManager)
			if err != nil {
				return result, err
			}
		}

		// Check if ClusterManager has any remaining references to other CRs, if so don't delete
		err = checkCmRemainingReferences(ctx, client, cr)
		if err != nil {
			return result, err
		}

		DeleteOwnerReferencesForResources(ctx, client, cr, &cr.Spec.SmartStore, SplunkClusterManager)
		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)

		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		if err != nil {
			eventPublisher.Warning(ctx, "Delete", fmt.Sprintf("delete custom resource failed %s", err.Error()))
		}
		return result, err
	}

	// create or update a regular service for the cluster manager
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkClusterManager, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset for the cluster manager
	statefulSet, err := getClusterManagerStatefulSet(ctx, client, cr)
	if err != nil {
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	extraEnv, err := VerifyCMisMultisite(ctx, cr, namespaceScopedSecret)
	err = validateMonitoringConsoleRef(ctx, client, statefulSet, extraEnv)
	if err != nil {
		return result, err
	}


	// check if the ClusterManager is ready for version upgrade, if required
	continueReconcile, err := UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, nil)
	if err != nil || !continueReconcile {
		return result, err
	}

	clusterManagerManager := splctrl.DefaultStatefulSetPodManager{}
	phase, err := clusterManagerManager.Update(ctx, client, statefulSet, 1)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = splctrl.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			scopedLog.Error(err, "Error in deleting automated monitoring console resource")
		}
		//Update MC configmap
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, extraEnv, true)
			if err != nil {
				return result, err
			}
		}

		// Create podExecClient
		podExecClient := splutil.GetPodExecClient(client, cr, "")

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			err := addTelApp(ctx, podExecClient, numberOfClusterMasterReplicas, cr)
			if err != nil {
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
		}

		// Manager apps bundle push requires multiple reconcile iterations in order to reflect the configMap on the CM pod.
		// So keep PerformCmBundlePush() as the last call in this block of code, so that other functionalities are not blocked
		err = PerformCmBundlePush(ctx, client, cr)
		if err != nil {
			return result, err
		}

		finalResult := handleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
		result = *finalResult

		// trigger MonitoringConsole reconcile by changing the splunk/image-tag annotation
		err = changeMonitoringConsoleAnnotations(ctx, client, cr)
		if err != nil {
			return result, err
		}
	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}

	return result, nil
}

// clusterManagerPodManager is used to manage the cluster manager pod
type clusterManagerPodManager struct {
	log             logr.Logger
	cr              *enterpriseApi.ClusterManager
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// getClusterManagerClient for clusterManagerPodManager returns a SplunkClient for cluster manager
func (mgr *clusterManagerPodManager) getClusterManagerClient(cr *enterpriseApi.ClusterManager) *splclient.SplunkClient {
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), GetSplunkServiceName(SplunkClusterManager, cr.GetName(), false))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// validateClusterManagerSpec checks validity and makes default updates to a ClusterManagerSpec, and returns error if something is wrong.
func validateClusterManagerSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) error {

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) {
		err := ValidateSplunkSmartstoreSpec(ctx, &cr.Spec.SmartStore)
		if err != nil {
			return err
		}
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, false, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// getClusterManagerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license manager.
func getClusterManagerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) (*appsv1.StatefulSet, error) {
	var extraEnvVar []corev1.EnvVar

	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkClusterManager, 1, extraEnvVar)
	if err != nil {
		return ss, err
	}
	smartStoreConfigMap := getSmartstoreConfigMap(ctx, client, cr, SplunkClusterManager)

	if smartStoreConfigMap != nil {
		setupInitContainer(&ss.Spec.Template, cr.Spec.Image, cr.Spec.ImagePullPolicy, commandForCMSmartstore, cr.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.EphemeralStorage)
	}
	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// CheckIfsmartstoreConfigMapUpdatedToPod checks if the smartstore configMap is updated on Pod or not
func CheckIfsmartstoreConfigMapUpdatedToPod(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager, podExecClient splutil.PodExecClientImpl) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("CheckIfsmartstoreConfigMapUpdatedToPod").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(c, cr)

	command := fmt.Sprintf("cat /mnt/splunk-operator/local/%s", configToken)
	streamOptions := splutil.NewStreamOptionsObject(command)

	stdOut, stdErr, err := podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err != nil || stdErr != "" {
		eventPublisher.Warning(ctx, "PodExecCommand", fmt.Sprintf("Failed to check config token value on pod. stdout=%s, stderror=%s, error=%v", stdOut, stdErr, err))
		return fmt.Errorf("failed to check config token value on pod. stdout=%s, stderror=%s, error=%v", stdOut, stdErr, err)
	}

	smartStoreConfigMap := getSmartstoreConfigMap(ctx, c, cr, SplunkClusterManager)
	if smartStoreConfigMap != nil {
		tokenFromConfigMap := smartStoreConfigMap.Data[configToken]
		if tokenFromConfigMap == stdOut {
			scopedLog.Info("Token Matched.", "on Pod=", stdOut, "from configMap=", tokenFromConfigMap)
			return nil
		}
		eventPublisher.Warning(ctx, "getSmartstoreConfigMap", fmt.Sprintf("waiting for the configMap update to the Pod. Token on Pod=%s, Token from configMap=%s", stdOut, tokenFromConfigMap))
		return fmt.Errorf("waiting for the configMap update to the Pod. Token on Pod=%s, Token from configMap=%s", stdOut, tokenFromConfigMap)
	}

	// Somehow the configmap was deleted, ideally this should not happen
	eventPublisher.Warning(ctx, "getSmartstoreConfigMap", "smartstore ConfigMap is missing")
	return fmt.Errorf("smartstore ConfigMap is missing")
}

// PerformCmBundlePush initiates the bundle push from cluster manager
func PerformCmBundlePush(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) error {
	if !cr.Status.BundlePushTracker.NeedToPushManagerApps {
		return nil
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("PerformCmBundlePush").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	// Reconciler can be called for multiple reasons. If we are waiting on configMap update to happen,
	// do not increment the Retry Count unless the last check was 5 seconds ago.
	// This helps, to wait for the required time
	//eventPublisher, _ := newK8EventPublisher(c, cr)

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

	cmPodName := fmt.Sprintf("splunk-%s-%s-0", cr.GetName(), "cluster-manager")
	podExecClient := splutil.GetPodExecClient(c, cr, cmPodName)
	err := CheckIfsmartstoreConfigMapUpdatedToPod(ctx, c, cr, podExecClient)
	if err != nil {
		return err
	}

	// Reset symbolic links for pod
	err = resetSymbolicLinks(ctx, c, cr, 1, podExecClient)
	if err != nil {
		return err
	}

	err = PushManagerAppsBundle(ctx, c, cr)
	if err == nil {
		scopedLog.Info("Bundle push success")
		cr.Status.BundlePushTracker.NeedToPushManagerApps = false
	}

	//eventPublisher.Warning(ctx, "BundlePush", fmt.Sprintf("Bundle push failed %s", err.Error()))
	return err
}

// PushManagerAppsBundle issues the REST command to for cluster manager bundle push
func PushManagerAppsBundle(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("PushManagerApps").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(c, cr)

	defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	defaultSecret, err := splutil.GetSecretByName(ctx, c, cr.GetNamespace(), cr.GetName(), defaultSecretObjName)
	if err != nil {
		eventPublisher.Warning(ctx, "PushManagerAppsBundle", fmt.Sprintf("Could not access default secret object to fetch admin password. Reason %v", err))
		return fmt.Errorf("could not access default secret object to fetch admin password. Reason %v", err)
	}

	//Get the admin password from the secret object
	adminPwd, foundSecret := defaultSecret.Data["password"]
	if !foundSecret {
		eventPublisher.Warning(ctx, "PushManagerAppsBundle", "could not find admin password while trying to push the manager apps bundle")
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
func getClusterManagerList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (int, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getClusterManagerList").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.ClusterManagerList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	numOfObjects := len(objectList.Items)

	if err != nil {
		scopedLog.Error(err, "ClusterManager types not found in namespace", "namsespace", cr.GetNamespace())
		return numOfObjects, err
	}

	return numOfObjects, nil
}

// VerifyCMisMultisite checks if its a multisite
func VerifyCMisMultisite(ctx context.Context, cr *enterpriseApi.ClusterManager, namespaceScopedSecret *corev1.Secret) ([]corev1.EnvVar, error) {
	var err error
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("Verify if Multisite Indexer Cluster").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	mgr := clusterManagerPodManager{log: scopedLog, cr: cr, secrets: namespaceScopedSecret, newSplunkClient: splclient.NewSplunkClient}
	cm := mgr.getClusterManagerClient(cr)
	clusterInfo, err := cm.GetClusterInfo(false)
	if err != nil {
		return nil, err
	}
	multiSite := clusterInfo.MultiSite
	extraEnv := getClusterManagerExtraEnv(cr, &cr.Spec.CommonSplunkSpec)
	if multiSite == "true" {
		extraEnv = append(extraEnv, corev1.EnvVar{Name: "SPLUNK_SITE", Value: "site0"}, corev1.EnvVar{Name: "SPLUNK_MULTISITE_MASTER", Value: GetSplunkServiceName(SplunkClusterManager, cr.GetName(), false)})
	}
	return extraEnv, err
}

// isClusterManagerReadyForUpgrade checks if ClusterManager can be upgraded if a version upgrade is in-progress
// No-operation otherwise; returns bool, err accordingly
func isClusterManagerReadyForUpgrade(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("isClusterManagerReadyForUpgrade").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(c, cr)

	// check if a LicenseManager is attached to the instance
	licenseManagerRef := cr.Spec.LicenseManagerRef
	if licenseManagerRef.Name == "" {
		return true, nil
	}

	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      GetSplunkStatefulsetName(SplunkClusterManager, cr.GetName()),
	}

	// check if the stateful set is created at this instance
	statefulSet := &appsv1.StatefulSet{}
	err := c.Get(ctx, namespacedName, statefulSet)
	if err != nil && k8serrors.IsNotFound(err) {
		return true, nil
	}

	namespacedName = types.NamespacedName{Namespace: cr.GetNamespace(), Name: licenseManagerRef.Name}
	licenseManager := &enterpriseApi.LicenseManager{}

	// get the license manager referred in cluster manager
	err = c.Get(ctx, namespacedName, licenseManager)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return true, nil
		}
		eventPublisher.Warning(ctx, "isClusterManagerReadyForUpgrade", fmt.Sprintf("Could not find the License Manager. Reason %v", err))
		scopedLog.Error(err, "Unable to get licenseManager")
		return false, err
	}

	lmImage, err := getCurrentImage(ctx, c, licenseManager, SplunkLicenseManager)
	if err != nil {
		eventPublisher.Warning(ctx, "isClusterManagerReadyForUpgrade", fmt.Sprintf("Could not get the License Manager Image. Reason %v", err))
		scopedLog.Error(err, "Unable to get licenseManager current image")
		return false, err
	}

	cmImage, err := getCurrentImage(ctx, c, cr, SplunkClusterManager)
	if err != nil {
		eventPublisher.Warning(ctx, "isClusterManagerReadyForUpgrade", fmt.Sprintf("Could not get the Cluster Manager Image. Reason %v", err))
		scopedLog.Error(err, "Unable to get clusterManager current image")
		return false, err
	}

	// check if an image upgrade is happening and whether LM has finished updating yet, return false to stop
	// further reconcile operations on CM until LM is ready
	if (cr.Spec.Image != cmImage) && (licenseManager.Status.Phase != enterpriseApi.PhaseReady || lmImage != cr.Spec.Image) {
		return false, nil
	}

	return true, nil
}

// changeClusterManagerAnnotations updates the splunk/image-tag field of the ClusterManager annotations to trigger the reconcile loop
// on update, and returns error if something is wrong
func changeClusterManagerAnnotations(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("changeClusterManagerAnnotations").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(c, cr)

	clusterManagerInstance := &enterpriseApi.ClusterManager{}
	if len(cr.Spec.ClusterManagerRef.Name) > 0 {
		// if the LicenseManager holds the ClusterManagerRef
		namespacedName := types.NamespacedName{
			Namespace: cr.GetNamespace(),
			Name:      cr.Spec.ClusterManagerRef.Name,
		}
		err := c.Get(ctx, namespacedName, clusterManagerInstance)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return err
		}
	} else {
		// List out all the ClusterManager instances in the namespace
		opts := []rclient.ListOption{
			rclient.InNamespace(cr.GetNamespace()),
		}
		objectList := enterpriseApi.ClusterManagerList{}
		err := c.List(ctx, &objectList, opts...)
		if err != nil {
			if err.Error() == "NotFound" {
				return nil
			}
			return err
		}
		if len(objectList.Items) == 0 {
			return nil
		}

		// check if instance has the required LicenseManagerRef
		for _, cm := range objectList.Items {
			if cm.Spec.LicenseManagerRef.Name == cr.GetName() {
				clusterManagerInstance = &cm
				break
			}
		}

		if len(clusterManagerInstance.GetName()) == 0 {
			return nil
		}
	}

	image, err := getCurrentImage(ctx, c, cr, SplunkLicenseManager)
	if err != nil {
		eventPublisher.Warning(ctx, "changeClusterManagerAnnotations", fmt.Sprintf("Could not get the LicenseManager Image. Reason %v", err))
		scopedLog.Error(err, "Get LicenseManager Image failed with", "error", err)
		return err
	}
	err = changeAnnotations(ctx, c, image, clusterManagerInstance)
	if err != nil {
		eventPublisher.Warning(ctx, "changeClusterManagerAnnotations", fmt.Sprintf("Could not update annotations. Reason %v", err))
		scopedLog.Error(err, "ClusterManager types update after changing annotations failed with", "error", err)
		return err
	}

	return nil
}

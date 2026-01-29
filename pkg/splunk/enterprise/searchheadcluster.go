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
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplySearchHeadCluster reconciles the state for a Splunk Enterprise search head cluster.
func ApplySearchHeadCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) (reconcile.Result, error) {
	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplySearchHeadCluster")
	eventPublisher, _ := newK8EventPublisher(client, cr)

	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "SearchHeadCluster"

	var err error
	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError
	cr.Status.DeployerPhase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateSearchHeadClusterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateSearchHeadClusterSpec", fmt.Sprintf("validate searchHeadCluster spec failed %s", err.Error()))
		scopedLog.Error(err, "Failed to validate searchHeadCluster spec")
		return result, err
	}

	// If needed, Migrate the app framework status
	err = checkAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, false)
	if err != nil {
		return result, err
	}

	// create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkSearchHead)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
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

	// updates status after function completes
	cr.Status.DeployerPhase = enterpriseApi.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-search-head", cr.GetName())
	if cr.Status.Members == nil {
		cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{}
	}
	if cr.Status.ShcSecretChanged == nil {
		cr.Status.ShcSecretChanged = []bool{}
	}
	if cr.Status.AdminSecretChanged == nil {
		cr.Status.AdminSecretChanged = []bool{}
	}
	if cr.Status.AdminPasswordChangedSecrets == nil {
		cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getSearchHeadEnv(cr), false)
			if err != nil {
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, SplunkSearchHead)
			if err != nil {
				return result, err
			}
		}

		DeleteOwnerReferencesForResources(ctx, client, cr, SplunkSearchHead)

		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterpriseApi.PhaseTerminating
			cr.Status.DeployerPhase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		if err != nil {
			eventPublisher.Warning(ctx, "Delete", fmt.Sprintf("delete custom resource failed %s", err.Error()))
		}
		return result, err
	}

	// create or update a headless search head cluster service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkSearchHead, true))
	if err != nil {
		return result, err
	}

	// create or update a regular search head cluster service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkSearchHead, false))
	if err != nil {
		return result, err
	}

	// Create or update PodDisruptionBudget for high availability during rolling restarts
	err = ApplyPodDisruptionBudget(ctx, client, cr, SplunkSearchHead, cr.Spec.Replicas)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyPodDisruptionBudget", fmt.Sprintf("create/update PodDisruptionBudget failed %s", err.Error()))
		return result, err
	}

	// create or update a deployer service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkDeployer, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset for the deployer
	statefulSet, err := getDeployerStatefulSet(ctx, client, cr)
	if err != nil {
		return result, err
	}

	// CSPL-3060 - If statefulSet is not created, avoid upgrade path validation
	if !statefulSet.CreationTimestamp.IsZero() {
		continueReconcile, err := UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, nil)
		if err != nil || !continueReconcile {
			return result, err
		}
	}

	deployerManager := splctrl.DefaultStatefulSetPodManager{}
	phase, err := deployerManager.Update(ctx, client, statefulSet, 1)
	if err != nil {
		return result, err
	}
	cr.Status.DeployerPhase = phase

	// create or update statefulset for the search heads
	statefulSet, err = getSearchHeadStatefulSet(ctx, client, cr)
	if err != nil {
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = validateMonitoringConsoleRef(ctx, client, statefulSet, getSearchHeadEnv(cr))
	if err != nil {
		return result, err
	}

	mgr := newSearchHeadClusterPodManager(client, scopedLog, cr, namespaceScopedSecret, splclient.NewSplunkClient)

	// handle SHC upgrade process
	phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)

	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	var finalResult *reconcile.Result
	if cr.Status.DeployerPhase == enterpriseApi.PhaseReady {
		finalResult = handleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
	}

	if cr.Spec.MonitoringConsoleRef.Name != "" {
		_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getSearchHeadEnv(cr), true)
		if err != nil {
			return result, err
		}
	}

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		// V3: Check if replicas have changed - if so, need to handle scale-down/up
		currentReplicas := *statefulSet.Spec.Replicas
		desiredReplicas := cr.Spec.Replicas
		if currentReplicas != desiredReplicas {
			scopedLog.Info("Replica count changed - handling scale operation",
				"current", currentReplicas,
				"desired", desiredReplicas)

			// Call Update() to handle scale-down/up with proper pod marking
			phase, err := mgr.Update(ctx, client, statefulSet, desiredReplicas)
			if err != nil {
				return result, err
			}
			cr.Status.Phase = phase

			// Update status and requeue to check completion
			return result, nil
		}

		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = splctrl.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			scopedLog.Error(err, "Error in deleting automated monitoring console resource")
		}

		// Reset secrets related status structs
		cr.Status.ShcSecretChanged = []bool{}
		cr.Status.AdminSecretChanged = []bool{}
		cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
		cr.Status.NamespaceSecretResourceVersion = namespaceScopedSecret.ObjectMeta.ResourceVersion

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			podExecClient := splutil.GetPodExecClient(client, cr, "")
			err := addTelApp(ctx, podExecClient, numberOfDeployerReplicas, cr)
			if err != nil {
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
		}

		// V3 FIX #2: PVC cleanup removed - handled by pod finalizer synchronously
		// PVCs are now deleted by the finalizer BEFORE the pod is removed

		// Handle rolling restart mechanism
		// This runs after everything else is ready to check for config changes
		restartResult, restartErr := handleSearchHeadClusterRollingRestart(ctx, client, cr)
		if restartErr != nil {
			scopedLog.Error(restartErr, "Rolling restart handler failed")
			// Don't return error, just log it - we don't want to block other operations
		}
		// If restart handler wants to requeue, honor that
		if restartResult.Requeue || restartResult.RequeueAfter > 0 {
			result = restartResult
		}

		// Update the requeue result as needed by the app framework
		if finalResult != nil {
			result = *finalResult
		}
	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}

	return result, nil
}

// ApplyShcSecret checks if any of the search heads have a different shc_secret from namespace scoped secret and changes it
func ApplyShcSecret(ctx context.Context, mgr *searchHeadClusterPodManager, replicas int32, podExecClient splutil.PodExecClientImpl) error {
	// Get namespace scoped secret
	namespaceSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, mgr.c, mgr.cr.GetNamespace())
	if err != nil {
		return err
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyShcSecret").WithValues("Desired replicas", replicas, "ShcSecretChanged", mgr.cr.Status.ShcSecretChanged, "AdminSecretChanged", mgr.cr.Status.AdminSecretChanged, "CrStatusNamespaceSecretResourceVersion", mgr.cr.Status.NamespaceSecretResourceVersion, "NamespaceSecretResourceVersion", namespaceSecret.GetObjectMeta().GetResourceVersion())

	// If namespace scoped secret revision is the same ignore
	if len(mgr.cr.Status.NamespaceSecretResourceVersion) == 0 {
		// First time, set resource version in CR
		scopedLog.Info("Setting CrStatusNamespaceSecretResourceVersion for the first time")
		mgr.cr.Status.NamespaceSecretResourceVersion = namespaceSecret.ObjectMeta.ResourceVersion
		return nil
	} else if mgr.cr.Status.NamespaceSecretResourceVersion == namespaceSecret.ObjectMeta.ResourceVersion {
		// If resource version hasn't changed don't return
		return nil
	}

	scopedLog.Info("Namespaced scoped secret revision has changed")

	// Retrieve shc_secret password from secret data
	nsShcSecret := string(namespaceSecret.Data["shc_secret"])

	// Retrieve shc_secret password from secret data
	nsAdminSecret := string(namespaceSecret.Data["password"])

	// Loop over all sh pods and get individual pod's shc_secret
	for i := int32(0); i <= replicas-1; i++ {
		// Get search head pod's name
		shPodName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), i)

		reqLogger := log.FromContext(ctx)
		scopedLog := reqLogger.WithName("ApplyShcSecretPodLoop").WithValues("Desired replicas", replicas, "ShcSecretChanged", mgr.cr.Status.ShcSecretChanged, "AdminSecretChanged", mgr.cr.Status.AdminSecretChanged, "NamespaceSecretResourceVersion", mgr.cr.Status.NamespaceSecretResourceVersion, "pod", shPodName)

		// Retrieve shc_secret password from Pod
		shcSecret, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, shPodName, mgr.cr.GetNamespace(), "shc_secret")
		if err != nil {
			return fmt.Errorf("couldn't retrieve shc_secret from secret data, error: %s", err.Error())
		}

		// set the targetPodName here
		podExecClient.SetTargetPodName(ctx, shPodName)

		var streamOptions *remotecommand.StreamOptions = &remotecommand.StreamOptions{}

		// Retrieve admin password from Pod
		adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, shPodName, mgr.cr.GetNamespace(), "password")
		if err != nil {
			return fmt.Errorf("couldn't retrieve admin password from secret data, error: %s", err.Error())
		}

		// If shc secret is different from namespace scoped secret change it
		if shcSecret != nsShcSecret {
			scopedLog.Info("shcSecret different from namespace scoped secret, changing shc secret")
			// If shc secret already changed, ignore
			if i < int32(len(mgr.cr.Status.ShcSecretChanged)) {
				if mgr.cr.Status.ShcSecretChanged[i] {
					continue
				}
			}

			// Change shc secret key
			command := fmt.Sprintf("/opt/splunk/bin/splunk edit shcluster-config -auth admin:%s -secret %s", adminPwd, nsShcSecret)
			streamOptions.Stdin = strings.NewReader(command)

			_, _, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
			if err != nil {
				return err
			}
			scopedLog.Info("shcSecret changed")

			// Note: Restart will be triggered via rolling restart mechanism after all secrets are updated
			// The handleSearchHeadClusterRollingRestart() function will detect the change and trigger
			// a zero-downtime rolling restart of all pods

			// Set the shc_secret changed flag to true
			if i < int32(len(mgr.cr.Status.ShcSecretChanged)) {
				mgr.cr.Status.ShcSecretChanged[i] = true
			} else {
				mgr.cr.Status.ShcSecretChanged = append(mgr.cr.Status.ShcSecretChanged, true)
			}
		}

		// If admin secret is different from namespace scoped secret change it
		if adminPwd != nsAdminSecret {
			scopedLog.Info("admin password different from namespace scoped secret, changing admin password")
			// If admin password already changed, ignore
			if i < int32(len(mgr.cr.Status.AdminSecretChanged)) {
				if mgr.cr.Status.AdminSecretChanged[i] {
					continue
				}
			}

			// Change admin password on splunk instance of pod
			command := fmt.Sprintf("/opt/splunk/bin/splunk cmd splunkd rest --noauth POST /services/admin/users/admin 'password=%s'", nsAdminSecret)
			streamOptions.Stdin = strings.NewReader(command)
			_, _, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
			if err != nil {
				return err
			}
			scopedLog.Info("admin password changed on the splunk instance of pod")

			// Note: Restart will be triggered via rolling restart mechanism after all secrets are updated
			// The handleSearchHeadClusterRollingRestart() function will detect the change and trigger
			// a zero-downtime rolling restart of all pods

			// Set the adminSecretChanged changed flag to true
			if i < int32(len(mgr.cr.Status.AdminSecretChanged)) {
				mgr.cr.Status.AdminSecretChanged[i] = true
			} else {
				scopedLog.Info("Appending to AdminSecretChanged")
				mgr.cr.Status.AdminSecretChanged = append(mgr.cr.Status.AdminSecretChanged, true)
			}

			// Adding to map of secrets to be synced
			podSecret, err := splutil.GetSecretFromPod(ctx, mgr.c, shPodName, mgr.cr.GetNamespace())
			if err != nil {
				return err
			}
			mgr.cr.Status.AdminPasswordChangedSecrets[podSecret.GetName()] = true
			scopedLog.Info("Secret mounted on pod(to be changed) added to map")
		}
	}

	/*
		When admin password on the secret mounted on SHC pod is different from that on the namespace scoped
		secret the operator updates the admin password on the Splunk Instance running on the Pod. At this point
		the admin password on the secret mounted on SHC pod is different from the Splunk Instance running on it.
		Since the operator utilizes the admin password retrieved from the secret mounted on a SHC pod to make
		REST API calls to the Splunk instances running on SHC Pods, it results in unsuccessful authentication.
		Update the admin password on secret mounted on SHC pod to ensure successful authentication.
	*/
	if len(mgr.cr.Status.AdminPasswordChangedSecrets) > 0 {
		for podSecretName := range mgr.cr.Status.AdminPasswordChangedSecrets {
			podSecret, err := splutil.GetSecretByName(ctx, mgr.c, mgr.cr.GetNamespace(), mgr.cr.GetName(), podSecretName)
			if err != nil {
				return fmt.Errorf("could not read secret %s, reason - %v", podSecretName, err)
			}
			podSecret.Data["password"] = []byte(nsAdminSecret)
			_, err = splctrl.ApplySecret(ctx, mgr.c, podSecret)
			if err != nil {
				return err
			}
			scopedLog.Info("admin password changed on the secret mounted on pod")
		}
	}

	return nil
}

// getSearchHeadStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise search heads.
func getSearchHeadStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) (*appsv1.StatefulSet, error) {

	// get search head env variables with deployer
	env := getSearchHeadEnv(cr)

	// get generic statefulset for Splunk Enterprise objects
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkSearchHead, cr.Spec.Replicas, env)
	if err != nil {
		return nil, err
	}

	return ss, nil
}

// CSPL-3652 Configure deployer resources if configured
// Use default otherwise
// Make sure to set the resources ONLY for the deployer
func setDeployerConfig(ctx context.Context, cr *enterpriseApi.SearchHeadCluster, podTemplate *corev1.PodTemplateSpec) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("setDeployerConfig").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// Break out if this is not a deployer
	if !strings.Contains("deployer", podTemplate.Labels["app.kubernetes.io/name"]) {
		return errors.New("not a deployer, skipping setting resources")
	}
	depRes := cr.Spec.DeployerResourceSpec
	for i := range podTemplate.Spec.Containers {
		if len(depRes.Requests) != 0 {
			podTemplate.Spec.Containers[i].Resources.Requests = cr.Spec.DeployerResourceSpec.Requests
			scopedLog.Info("Setting deployer resources requests", "requests", cr.Spec.DeployerResourceSpec.Requests)
		}

		if len(depRes.Limits) != 0 {
			podTemplate.Spec.Containers[i].Resources.Limits = cr.Spec.DeployerResourceSpec.Limits
			scopedLog.Info("Setting deployer resources limits", "limits", cr.Spec.DeployerResourceSpec.Limits)
		}
	}

	// Add node affinity if configured
	if cr.Spec.DeployerNodeAffinity != nil {
		podTemplate.Spec.Affinity.NodeAffinity = cr.Spec.DeployerNodeAffinity
		scopedLog.Info("Setting deployer node affinity", "nodeAffinity", cr.Spec.DeployerNodeAffinity)
	}

	return nil
}

// getDeployerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license manager.
func getDeployerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) (*appsv1.StatefulSet, error) {
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkDeployer, 1, getSearchHeadExtraEnv(cr, cr.Spec.Replicas))
	if err != nil {
		return ss, err
	}

	// CSPL-3562 - Set deployer resources if configured
	err = setDeployerConfig(ctx, cr, &ss.Spec.Template)
	if err != nil {
		return ss, err
	}

	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// validateSearchHeadClusterSpec checks validity and makes default updates to a SearchHeadClusterSpec, and returns error if something is wrong.
func validateSearchHeadClusterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) error {
	if cr.Spec.Replicas < 3 {
		cr.Spec.Replicas = 3
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, false, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// helper function to get the list of SearchHeadCluster types in the current namespace
func getSearchHeadClusterList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.SearchHeadClusterList, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getSearchHeadClusterList").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.SearchHeadClusterList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		scopedLog.Error(err, "SearchHeadCluster types not found in namespace", "namsespace", cr.GetNamespace())
		return objectList, err
	}

	return objectList, nil
}

// ============================================================================
// Rolling Restart Functions for SearchHeadCluster
// ============================================================================

// shouldCheckSearchHeadRestartRequired determines if we should check restart_required endpoint
// Rate limits checks to avoid overwhelming Splunk REST API
func shouldCheckSearchHeadRestartRequired(cr *enterpriseApi.SearchHeadCluster) bool {
	// Don't check if restart is already in progress or failed
	if cr.Status.RestartStatus.Phase == enterpriseApi.RestartPhaseInProgress ||
		cr.Status.RestartStatus.Phase == enterpriseApi.RestartPhaseFailed {
		return false
	}

	// Check every 5 minutes
	if cr.Status.RestartStatus.LastCheckTime == nil {
		return true
	}

	elapsed := time.Since(cr.Status.RestartStatus.LastCheckTime.Time)
	checkInterval := 5 * time.Minute

	return elapsed > checkInterval
}

// checkSearchHeadPodsRestartRequired checks if ALL search head pods agree that restart is required
func checkSearchHeadPodsRestartRequired(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.SearchHeadCluster,
) (bool, string, error) {
	scopedLog := log.FromContext(ctx).WithName("checkSearchHeadPodsRestartRequired")

	var allPodsReady = true
	var allReadyPodsAgreeOnRestart = true
	var restartReason string
	var readyPodsChecked int32
	var readyPodsNeedingRestart int32

	// Get Splunk admin credentials
	secret := &corev1.Secret{}
	secretName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cr.Namespace}, secret)
	if err != nil {
		scopedLog.Error(err, "Failed to get splunk secret")
		return false, "", fmt.Errorf("failed to get splunk secret: %w", err)
	}
	password := string(secret.Data["password"])

	// Check ALL pods in the StatefulSet
	for i := int32(0); i < cr.Spec.Replicas; i++ {
		podName := fmt.Sprintf("splunk-%s-search-head-%d", cr.Name, i)

		// Get pod
		pod := &corev1.Pod{}
		err := c.Get(ctx, types.NamespacedName{Name: podName, Namespace: cr.Namespace}, pod)
		if err != nil {
			scopedLog.Error(err, "Failed to get pod", "pod", podName)
			allPodsReady = false
			continue
		}

		// Check if pod is ready
		if !isPodReady(pod) {
			scopedLog.Info("Pod not ready, cannot verify restart state", "pod", podName)
			allPodsReady = false
			continue
		}

		// Get pod IP
		if pod.Status.PodIP == "" {
			scopedLog.Info("Pod has no IP", "pod", podName)
			allPodsReady = false
			continue
		}

		// Pod is ready, check its restart_required status
		readyPodsChecked++

		// Create SplunkClient for this pod
		managementURI := fmt.Sprintf("https://%s:8089", pod.Status.PodIP)
		splunkClient := splclient.NewSplunkClient(managementURI, "admin", password)

		// Check restart required
		restartRequired, reason, err := splunkClient.CheckRestartRequired()
		if err != nil {
			scopedLog.Error(err, "Failed to check restart required", "pod", podName)
			allPodsReady = false
			continue
		}

		if restartRequired {
			scopedLog.Info("Pod needs restart", "pod", podName, "reason", reason)
			readyPodsNeedingRestart++
			restartReason = reason
		} else {
			scopedLog.Info("Pod does not need restart", "pod", podName)
			allReadyPodsAgreeOnRestart = false
		}
	}

	// Log summary
	scopedLog.Info("Restart check summary",
		"totalPods", cr.Spec.Replicas,
		"readyPodsChecked", readyPodsChecked,
		"readyPodsNeedingRestart", readyPodsNeedingRestart,
		"allPodsReady", allPodsReady,
		"allReadyPodsAgreeOnRestart", allReadyPodsAgreeOnRestart)

	if !allPodsReady {
		return false, "Not all pods are ready - waiting for cluster to stabilize", nil
	}

	if readyPodsChecked == 0 {
		return false, "No ready pods found to check", nil
	}

	if !allReadyPodsAgreeOnRestart {
		return false, fmt.Sprintf("Not all pods agree on restart (%d/%d need restart)",
			readyPodsNeedingRestart, readyPodsChecked), nil
	}

	// All pods are ready AND all agree on restart - safe to proceed
	return true, restartReason, nil
}

// triggerSearchHeadRollingRestart triggers a rolling restart by updating the StatefulSet pod template annotation
func triggerSearchHeadRollingRestart(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.SearchHeadCluster,
	reason string,
) error {
	scopedLog := log.FromContext(ctx).WithName("triggerSearchHeadRollingRestart")

	// Get current StatefulSet
	statefulSetName := fmt.Sprintf("splunk-%s-search-head", cr.Name)
	statefulSet := &appsv1.StatefulSet{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      statefulSetName,
		Namespace: cr.Namespace,
	}, statefulSet)
	if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Update pod template with restart annotation
	if statefulSet.Spec.Template.Annotations == nil {
		statefulSet.Spec.Template.Annotations = make(map[string]string)
	}

	now := time.Now().Format(time.RFC3339)
	statefulSet.Spec.Template.Annotations["splunk.com/restartedAt"] = now
	statefulSet.Spec.Template.Annotations["splunk.com/restartReason"] = reason

	scopedLog.Info("Triggering rolling restart via StatefulSet update",
		"reason", reason,
		"timestamp", now,
		"replicas", *statefulSet.Spec.Replicas)

	// Update StatefulSet - Kubernetes handles rolling restart automatically
	err = c.Update(ctx, statefulSet)
	if err != nil {
		return fmt.Errorf("failed to update StatefulSet: %w", err)
	}

	scopedLog.Info("Successfully triggered rolling restart")
	return nil
}

// monitorSearchHeadRollingRestartProgress monitors the progress of an ongoing rolling restart
func monitorSearchHeadRollingRestartProgress(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.SearchHeadCluster,
) (reconcile.Result, error) {
	scopedLog := log.FromContext(ctx).WithName("monitorSearchHeadRollingRestartProgress")

	// Get current StatefulSet
	statefulSetName := fmt.Sprintf("splunk-%s-search-head", cr.Name)
	statefulSet := &appsv1.StatefulSet{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      statefulSetName,
		Namespace: cr.Namespace,
	}, statefulSet)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Check if rolling restart is complete
	// Complete when: currentRevision == updateRevision AND all replicas updated and ready
	if statefulSet.Status.CurrentRevision == statefulSet.Status.UpdateRevision &&
		statefulSet.Status.UpdatedReplicas == statefulSet.Status.Replicas &&
		statefulSet.Status.ReadyReplicas == statefulSet.Status.Replicas {

		scopedLog.Info("Rolling restart completed successfully",
			"revision", statefulSet.Status.CurrentRevision,
			"replicas", statefulSet.Status.Replicas)

		now := metav1.Now()
		cr.Status.RestartStatus.Phase = enterpriseApi.RestartPhaseCompleted
		cr.Status.RestartStatus.LastRestartTime = &now
		cr.Status.RestartStatus.Message = fmt.Sprintf(
			"Rolling restart completed successfully at %s. All %d pods restarted.",
			now.Format(time.RFC3339),
			statefulSet.Status.Replicas)

		return reconcile.Result{}, nil
	}

	// Still in progress - update status with current progress
	cr.Status.RestartStatus.Message = fmt.Sprintf(
		"Rolling restart in progress: %d/%d pods updated, %d/%d ready",
		statefulSet.Status.UpdatedReplicas,
		statefulSet.Status.Replicas,
		statefulSet.Status.ReadyReplicas,
		statefulSet.Status.Replicas)

	scopedLog.Info("Rolling restart in progress",
		"updated", statefulSet.Status.UpdatedReplicas,
		"ready", statefulSet.Status.ReadyReplicas,
		"target", statefulSet.Status.Replicas,
		"currentRevision", statefulSet.Status.CurrentRevision,
		"updateRevision", statefulSet.Status.UpdateRevision)

	// Check again in 30 seconds
	return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
}

// handleSearchHeadClusterRollingRestart uses per-pod eviction like IngestorCluster
// Changed from consensus-based to individual pod eviction for better responsiveness
func handleSearchHeadClusterRollingRestart(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.SearchHeadCluster,
) (reconcile.Result, error) {
	scopedLog := log.FromContext(ctx).WithName("handleSearchHeadClusterRollingRestart")

	// Always check for restart_required and evict if needed (per-pod approach)
	restartErr := checkAndEvictSearchHeadsIfNeeded(ctx, c, cr)
	if restartErr != nil {
		scopedLog.Error(restartErr, "Failed to check/evict search heads")
		// Don't return error, just log it - we don't want to block other operations
	}

	return reconcile.Result{}, nil
}

// checkAndEvictSearchHeadsIfNeeded checks each search head pod individually for
// restart_required and evicts pods that need restart.
func checkAndEvictSearchHeadsIfNeeded(
	ctx context.Context,
	c client.Client,
	cr *enterpriseApi.SearchHeadCluster,
) error {
	scopedLog := log.FromContext(ctx).WithName("checkAndEvictSearchHeadsIfNeeded")

	// Get admin credentials
	secret := &corev1.Secret{}
	secretName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cr.Namespace}, secret)
	if err != nil {
		scopedLog.Error(err, "Failed to get splunk secret")
		return fmt.Errorf("failed to get splunk secret: %w", err)
	}
	password := string(secret.Data["password"])

	// Check each search head pod individually (NO consensus needed)
	for i := int32(0); i < cr.Spec.Replicas; i++ {
		podName := fmt.Sprintf("splunk-%s-search-head-%d", cr.Name, i)

		// Get pod
		pod := &corev1.Pod{}
		err := c.Get(ctx, types.NamespacedName{Name: podName, Namespace: cr.Namespace}, pod)
		if err != nil {
			scopedLog.Error(err, "Failed to get pod", "pod", podName)
			continue // Skip pods that don't exist
		}

		// Only check running pods
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Check if pod is ready
		if !isPodReady(pod) {
			continue
		}

		// Get pod IP
		if pod.Status.PodIP == "" {
			continue
		}

		// Check if THIS specific pod needs restart
		managementURI := fmt.Sprintf("https://%s:8089", pod.Status.PodIP)
		splunkClient := splclient.NewSplunkClient(managementURI, "admin", password)

		restartRequired, message, err := splunkClient.CheckRestartRequired()
		if err != nil {
			scopedLog.Error(err, "Failed to check restart required", "pod", podName)
			continue
		}

		if !restartRequired {
			continue // This pod is fine
		}

		scopedLog.Info("Pod needs restart, evicting",
			"pod", podName, "message", message)

		// Evict the pod - PDB automatically protects
		err = evictPodSearchHead(ctx, c, pod)
		if err != nil {
			if isPDBViolationSearchHead(err) {
				scopedLog.Info("PDB blocked eviction, will retry",
					"pod", podName)
				continue
			}
			return err
		}

		scopedLog.Info("Pod eviction initiated", "pod", podName)

		// Only evict ONE pod per reconcile
		// Next reconcile (5s later) will check remaining pods
		return nil
	}

	return nil
}

// evictPodSearchHead evicts a search head pod using Kubernetes Eviction API
func evictPodSearchHead(ctx context.Context, c client.Client, pod *corev1.Pod) error {
	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
	}

	// Eviction API automatically checks PDB
	return c.SubResource("eviction").Create(ctx, pod, eviction)
}

// isPDBViolationSearchHead checks if an error is due to PDB violation
func isPDBViolationSearchHead(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Cannot evict pod")
}

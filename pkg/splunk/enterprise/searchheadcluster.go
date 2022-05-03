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

	"github.com/go-logr/logr"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

	// validate and updates defaults for CR
	err := validateSearchHeadClusterSpec(ctx, cr)
	if err != nil {
		return result, err
	}

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

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	cr.Status.DeployerPhase = splcommon.PhaseError
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
	defer func() {
		err = client.Status().Update(ctx, cr)
		if err != nil {
			scopedLog.Error(err, "Status update failed")
		}
	}()

	// create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkSearchHead)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, err
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

		DeleteOwnerReferencesForResources(ctx, client, cr, nil)
		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = splcommon.PhaseTerminating
			cr.Status.DeployerPhase = splcommon.PhaseTerminating
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

	mgr := NewSerachHeadClusterPodManager(client, scopedLog, cr, namespaceScopedSecret, splclient.NewSplunkClient)
	phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	var finalResult *reconcile.Result
	if cr.Status.DeployerPhase == splcommon.PhaseReady {
		finalResult = handleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
	}

	// no need to requeue if everything is ready
	if cr.Status.Phase == splcommon.PhaseReady {
		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = splctrl.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			scopedLog.Error(err, "Error in deleting automated monitoring console resource")
		}
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getSearchHeadEnv(cr), true)
			if err != nil {
				return result, err
			}
		}

		// Reset secrets related status structs
		cr.Status.ShcSecretChanged = []bool{}
		cr.Status.AdminSecretChanged = []bool{}
		cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
		cr.Status.NamespaceSecretResourceVersion = namespaceScopedSecret.ObjectMeta.ResourceVersion

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

// searchHeadClusterPodManager is used to manage the pods within a search head cluster
type searchHeadClusterPodManager struct {
	c               splcommon.ControllerClient
	log             logr.Logger
	cr              *enterpriseApi.SearchHeadCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// NewSerachHeadClusterPodManager function to create pod manager this is added to write unit test case
var NewSerachHeadClusterPodManager = func(client splcommon.ControllerClient, log logr.Logger, cr *enterpriseApi.SearchHeadCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc) searchHeadClusterPodManager {
	return searchHeadClusterPodManager{
		log:             log,
		cr:              cr,
		secrets:         secret,
		newSplunkClient: newSplunkClient,
		c:               client,
	}
}

// ApplyShcSecret checks if any of the search heads have a different shc_secret from namespace scoped secret and changes it
func ApplyShcSecret(ctx context.Context, mgr *searchHeadClusterPodManager, replicas int32, podExecClient splutil.PodExecClientImpl) error {
	// Get namespace scoped secret
	namespaceSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, mgr.c, mgr.cr.GetNamespace())
	if err != nil {
		return err
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyShcSecret").WithValues("Desired replicas", replicas, "ShcSecretChanged", mgr.cr.Status.ShcSecretChanged, "AdminSecretChanged", mgr.cr.Status.AdminSecretChanged, "NamespaceSecretResourceVersion", mgr.cr.Status.NamespaceSecretResourceVersion)

	// If namespace scoped secret revision is the same ignore
	if len(mgr.cr.Status.NamespaceSecretResourceVersion) == 0 {
		// First time, set resource version in CR
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
			return fmt.Errorf("couldn't retrieve shc_secret from secret data")
		}

		// set the targetPodName here
		podExecClient.SetTargetPodName(ctx, shPodName)

		var streamOptions *remotecommand.StreamOptions = &remotecommand.StreamOptions{}

		// Retrieve admin password from Pod
		adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, shPodName, mgr.cr.GetNamespace(), "password")
		if err != nil {
			return fmt.Errorf("couldn't retrieve admin password from secret data")
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

			// Get client for Pod and restart splunk instance on pod
			shClient := mgr.getClient(ctx, i)
			err = shClient.RestartSplunk()
			if err != nil {
				return err
			}
			scopedLog.Info("Restarted Splunk")

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

			// Get client for Pod and restart splunk instance on pod
			shClient := mgr.getClient(ctx, i)
			err = shClient.RestartSplunk()
			if err != nil {
				return err
			}
			scopedLog.Info("Restarted Splunk")

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
			podSecret, err := splutil.GetSecretByName(ctx, mgr.c, mgr.cr, podSecretName)
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

// Update for searchHeadClusterPodManager handles all updates for a statefulset of search heads
func (mgr *searchHeadClusterPodManager) Update(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (splcommon.Phase, error) {
	// Assign client
	if mgr.c == nil {
		mgr.c = c
	}

	// update statefulset, if necessary
	_, err := splctrl.ApplyStatefulSet(ctx, mgr.c, statefulSet)
	if err != nil {
		return splcommon.PhaseError, err
	}

	// for now pass the targetPodName as empty since we are going to fill it in ApplyShcSecret
	podExecClient := splutil.GetPodExecClient(mgr.c, mgr.cr, "")

	// Check if a recycle of shc pods is necessary(due to shc_secret mismatch with namespace scoped secret)
	err = ApplyShcSecret(ctx, mgr, desiredReplicas, podExecClient)
	if err != nil {
		return splcommon.PhaseError, err
	}

	// update CR status with SHC information
	err = mgr.updateStatus(ctx, statefulSet)
	if err != nil || mgr.cr.Status.ReadyReplicas == 0 || !mgr.cr.Status.Initialized || !mgr.cr.Status.CaptainReady {
		mgr.log.Error(err, "Search head cluster is not ready")
		return splcommon.PhasePending, nil
	}

	// manage scaling and updates
	return splctrl.UpdateStatefulSetPods(ctx, mgr.c, statefulSet, mgr, desiredReplicas)
}

// PrepareScaleDown for searchHeadClusterPodManager prepares search head pod to be removed via scale down event; it returns true when ready
func (mgr *searchHeadClusterPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	// start by quarantining the pod
	result, err := mgr.PrepareRecycle(ctx, n)
	if err != nil || !result {
		return result, err
	}

	// pod is quarantined; decommission it
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
	mgr.log.Info("Removing member from search head cluster", "memberName", memberName)
	c := mgr.getClient(ctx, n)
	err = c.RemoveSearchHeadClusterMember()
	if err != nil {
		return false, err
	}

	// all done -> ok to scale down the statefulset
	return true, nil
}

// PrepareRecycle for searchHeadClusterPodManager prepares search head pod to be recycled for updates; it returns true when ready
func (mgr *searchHeadClusterPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// Detain search head
		mgr.log.Info("Detaining search head cluster member", "memberName", memberName)
		c := mgr.getClient(ctx, n)
		return false, c.SetSearchHeadDetention(true)

	case "ManualDetention":
		// Wait until active searches have drained
		searchesComplete := mgr.cr.Status.Members[n].ActiveHistoricalSearchCount+mgr.cr.Status.Members[n].ActiveRealtimeSearchCount == 0
		if searchesComplete {
			mgr.log.Info("Detention complete", "memberName", memberName)
		} else {
			mgr.log.Info("Waiting for active searches to complete", "memberName", memberName)
		}
		return searchesComplete, nil

	case "": // this can happen after the member has already been recycled and we're just waiting for state to update
		mgr.log.Info("Member has empty Status", "memberName", memberName)
		return false, nil
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Members[n].Status)
}

// FinishRecycle for searchHeadClusterPodManager completes recycle event for search head pod; it returns true when complete
func (mgr *searchHeadClusterPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// not in detention
		return true, nil

	case "ManualDetention":
		// release from detention
		mgr.log.Info("Releasing search head cluster member from detention", "memberName", memberName)
		c := mgr.getClient(ctx, n)
		return false, c.SetSearchHeadDetention(false)
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Members[n].Status)
}

// getClient for searchHeadClusterPodManager returns a SplunkClient for the member n
func (mgr *searchHeadClusterPodManager) getClient(ctx context.Context, n int32) *splclient.SplunkClient {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("searchHeadClusterPodManager.getClient").WithValues("name", mgr.cr.GetName(), "namespace", mgr.cr.GetNamespace())

	// Get Pod Name
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, GetSplunkServiceName(SplunkSearchHead, mgr.cr.GetName(), true)))

	// Retrieve admin password from Pod
	adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, memberName, mgr.cr.GetNamespace(), "password")
	if err != nil {
		scopedLog.Error(err, "Couldn't retrieve the admin password from Pod")
	}

	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPwd)
}

// updateStatus for searchHeadClusterPodManager uses the REST API to update the status for a SearcHead custom resource
func (mgr *searchHeadClusterPodManager) updateStatus(ctx context.Context, statefulSet *appsv1.StatefulSet) error {
	// populate members status using REST API to get search head cluster member info
	mgr.cr.Status.Captain = ""
	mgr.cr.Status.CaptainReady = false
	mgr.cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if mgr.cr.Status.ReadyReplicas == 0 {
		return nil
	}
	gotCaptainInfo := false
	for n := int32(0); n < statefulSet.Status.Replicas; n++ {
		c := mgr.getClient(ctx, n)
		memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
		memberStatus := enterpriseApi.SearchHeadClusterMemberStatus{Name: memberName}
		memberInfo, err := c.GetSearchHeadClusterMemberInfo()
		if err == nil {
			memberStatus.Status = memberInfo.Status
			memberStatus.Adhoc = memberInfo.Adhoc
			memberStatus.Registered = memberInfo.Registered
			memberStatus.ActiveHistoricalSearchCount = memberInfo.ActiveHistoricalSearchCount
			memberStatus.ActiveRealtimeSearchCount = memberInfo.ActiveRealtimeSearchCount
		} else {
			mgr.log.Error(err, "Unable to retrieve search head cluster member info", "memberName", memberName)
		}

		if err == nil && !gotCaptainInfo {
			// try querying captain api; note that this should work on any node
			captainInfo, err := c.GetSearchHeadCaptainInfo()
			if err == nil {
				mgr.cr.Status.Captain = captainInfo.Label
				mgr.cr.Status.CaptainReady = captainInfo.ServiceReady
				mgr.cr.Status.Initialized = captainInfo.Initialized
				mgr.cr.Status.MinPeersJoined = captainInfo.MinPeersJoined
				mgr.cr.Status.MaintenanceMode = captainInfo.MaintenanceMode
				gotCaptainInfo = true
			} else {
				mgr.log.Error(err, "Unable to retrieve captain info", "memberName", memberName)
			}
		}

		if n < int32(len(mgr.cr.Status.Members)) {
			mgr.cr.Status.Members[n] = memberStatus
		} else {
			mgr.cr.Status.Members = append(mgr.cr.Status.Members, memberStatus)
		}
	}

	// truncate any extra members that we didn't check (leftover from scale down)
	if statefulSet.Status.Replicas < int32(len(mgr.cr.Status.Members)) {
		mgr.cr.Status.Members = mgr.cr.Status.Members[:statefulSet.Status.Replicas]
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

// getDeployerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license manager.
func getDeployerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) (*appsv1.StatefulSet, error) {
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkDeployer, 1, getSearchHeadExtraEnv(cr, cr.Spec.Replicas))
	if err != nil {
		return ss, err
	}

	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// validateSearchHeadClusterSpec checks validity and makes default updates to a SearchHeadClusterSpec, and returns error if something is wrong.
func validateSearchHeadClusterSpec(ctx context.Context, cr *enterpriseApi.SearchHeadCluster) error {
	if cr.Spec.Replicas < 3 {
		cr.Spec.Replicas = 3
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, false)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(&cr.Spec.CommonSplunkSpec)
}

// helper function to get the list of SearchHeadCluster types in the current namespace
func getSearchHeadClusterList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (int, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getSearchHeadClusterList").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.SearchHeadClusterList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	numOfObjects := len(objectList.Items)

	if err != nil {
		scopedLog.Error(err, "SearchHeadCluster types not found in namespace", "namsespace", cr.GetNamespace())
		return numOfObjects, err
	}

	return numOfObjects, nil
}

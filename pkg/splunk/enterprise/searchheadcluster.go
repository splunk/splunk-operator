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
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
)

// ApplySearchHeadCluster reconciles the state for a Splunk Enterprise search head cluster.
func ApplySearchHeadCluster(client splcommon.ControllerClient, cr *enterprisev1.SearchHeadCluster) (reconcile.Result, error) {
	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	scopedLog := log.WithName("ApplySearchHeadCluster").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// validate and updates defaults for CR
	err := validateSearchHeadClusterSpec(&cr.Spec)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	cr.Status.DeployerPhase = splcommon.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-search-head", cr.GetName())
	if cr.Status.Members == nil {
		cr.Status.Members = []enterprisev1.SearchHeadClusterMemberStatus{}
	}
	defer func() {
		err = client.Status().Update(context.TODO(), cr)
		if err != nil {
			scopedLog.Error(err, "Status update failed")
		}
	}()

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		terminating, err := splctrl.CheckForDeletion(cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = splcommon.PhaseTerminating
			cr.Status.DeployerPhase = splcommon.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, SplunkSearchHead)
	if err != nil {
		return result, err
	}

	// create or update a headless search head cluster service
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkSearchHead, true))
	if err != nil {
		return result, err
	}

	// create or update a regular search head cluster service
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkSearchHead, false))
	if err != nil {
		return result, err
	}

	// create or update a deployer service
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkDeployer, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset for the deployer
	statefulSet, err := getDeployerStatefulSet(client, cr)
	if err != nil {
		return result, err
	}
	deployerManager := splctrl.DefaultStatefulSetPodManager{}
	phase, err := deployerManager.Update(client, statefulSet, 1)
	if err != nil {
		return result, err
	}
	cr.Status.DeployerPhase = phase

	// create or update statefulset for the search heads
	statefulSet, err = getSearchHeadStatefulSet(client, cr)
	if err != nil {
		return result, err
	}
	mgr := searchHeadClusterPodManager{log: scopedLog, cr: cr, secrets: namespaceScopedSecret, newSplunkClient: splclient.NewSplunkClient}
	phase, err = mgr.Update(client, statefulSet, cr.Spec.Replicas)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == splcommon.PhaseReady {
		result.Requeue = false
	}
	return result, nil
}

// searchHeadClusterPodManager is used to manage the pods within a search head cluster
type searchHeadClusterPodManager struct {
	log             logr.Logger
	cr              *enterprisev1.SearchHeadCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// Update for searchHeadClusterPodManager handles all updates for a statefulset of search heads
func (mgr *searchHeadClusterPodManager) Update(c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (splcommon.Phase, error) {
	// update statefulset, if necessary
	_, err := splctrl.ApplyStatefulSet(c, statefulSet)
	if err != nil {
		return splcommon.PhaseError, err
	}

	// update CR status with SHC information
	err = mgr.updateStatus(statefulSet)
	if err != nil || mgr.cr.Status.ReadyReplicas == 0 || !mgr.cr.Status.Initialized || !mgr.cr.Status.CaptainReady {
		mgr.log.Error(err, "Search head cluster is not ready")
		return splcommon.PhasePending, nil
	}

	// manage scaling and updates
	return splctrl.UpdateStatefulSetPods(c, statefulSet, mgr, desiredReplicas)
}

// PrepareScaleDown for searchHeadClusterPodManager prepares search head pod to be removed via scale down event; it returns true when ready
func (mgr *searchHeadClusterPodManager) PrepareScaleDown(n int32) (bool, error) {
	// start by quarantining the pod
	result, err := mgr.PrepareRecycle(n)
	if err != nil || !result {
		return result, err
	}

	// pod is quarantined; decommission it
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
	mgr.log.Info("Removing member from search head cluster", "memberName", memberName)
	c := mgr.getClient(n)
	err = c.RemoveSearchHeadClusterMember()
	if err != nil {
		return false, err
	}

	// all done -> ok to scale down the statefulset
	return true, nil
}

// PrepareRecycle for searchHeadClusterPodManager prepares search head pod to be recycled for updates; it returns true when ready
func (mgr *searchHeadClusterPodManager) PrepareRecycle(n int32) (bool, error) {
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// Detain search head
		mgr.log.Info("Detaining search head cluster member", "memberName", memberName)
		c := mgr.getClient(n)
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
func (mgr *searchHeadClusterPodManager) FinishRecycle(n int32) (bool, error) {
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// not in detention
		return true, nil

	case "ManualDetention":
		// release from detention
		mgr.log.Info("Releasing search head cluster member from detention", "memberName", memberName)
		c := mgr.getClient(n)
		return false, c.SetSearchHeadDetention(false)
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Members[n].Status)
}

// getClient for searchHeadClusterPodManager returns a SplunkClient for the member n
func (mgr *searchHeadClusterPodManager) getClient(n int32) *splclient.SplunkClient {
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, GetSplunkServiceName(SplunkSearchHead, mgr.cr.GetName(), true)))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// updateStatus for searchHeadClusterPodManager uses the REST API to update the status for a SearcHead custom resource
func (mgr *searchHeadClusterPodManager) updateStatus(statefulSet *appsv1.StatefulSet) error {
	// populate members status using REST API to get search head cluster member info
	mgr.cr.Status.Captain = ""
	mgr.cr.Status.CaptainReady = false
	mgr.cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if mgr.cr.Status.ReadyReplicas == 0 {
		return nil
	}
	gotCaptainInfo := false
	for n := int32(0); n < statefulSet.Status.Replicas; n++ {
		c := mgr.getClient(n)
		memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
		memberStatus := enterprisev1.SearchHeadClusterMemberStatus{Name: memberName}
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
func getSearchHeadStatefulSet(client splcommon.ControllerClient, cr *enterprisev1.SearchHeadCluster) (*appsv1.StatefulSet, error) {

	// get search head env variables with deployer
	env := getSearchHeadExtraEnv(cr, cr.Spec.Replicas)
	env = append(env, corev1.EnvVar{
		Name:  "SPLUNK_DEPLOYER_URL",
		Value: GetSplunkServiceName(SplunkDeployer, cr.GetName(), false),
	})

	// get generic statefulset for Splunk Enterprise objects
	ss, err := getSplunkStatefulSet(client, cr, &cr.Spec.CommonSplunkSpec, SplunkSearchHead, cr.Spec.Replicas, env)
	if err != nil {
		return nil, err
	}

	// add spark and java mounts to search head containers
	if cr.Spec.SparkRef.Name != "" {
		addDFCToPodTemplate(&ss.Spec.Template, cr.Spec.SparkRef, cr.Spec.SparkImage, cr.Spec.ImagePullPolicy, cr.Spec.Replicas > 1)
	}

	return ss, nil
}

// getDeployerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license master.
func getDeployerStatefulSet(client splcommon.ControllerClient, cr *enterprisev1.SearchHeadCluster) (*appsv1.StatefulSet, error) {
	return getSplunkStatefulSet(client, cr, &cr.Spec.CommonSplunkSpec, SplunkDeployer, 1, getSearchHeadExtraEnv(cr, cr.Spec.Replicas))
}

// validateSearchHeadClusterSpec checks validity and makes default updates to a SearchHeadClusterSpec, and returns error if something is wrong.
func validateSearchHeadClusterSpec(spec *enterprisev1.SearchHeadClusterSpec) error {
	if spec.Replicas < 3 {
		spec.Replicas = 3
	}
	spec.SparkImage = spark.GetSparkImage(spec.SparkImage)
	return validateCommonSplunkSpec(&spec.CommonSplunkSpec)
}

// getSearchHeadExtraEnv returns extra environment variables used by search head clusters
func getSearchHeadExtraEnv(cr splcommon.MetaObject, replicas int32) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_SEARCH_HEAD_URL",
			Value: GetSplunkStatefulsetUrls(cr.GetNamespace(), SplunkSearchHead, cr.GetName(), replicas, false),
		}, {
			Name:  "SPLUNK_SEARCH_HEAD_CAPTAIN_URL",
			Value: GetSplunkStatefulsetURL(cr.GetNamespace(), SplunkSearchHead, cr.GetName(), 0, false),
		},
	}
}

// addDFCToPodTemplate modifies the podTemplateSpec object to incorporate support for DFS.
func addDFCToPodTemplate(podTemplateSpec *corev1.PodTemplateSpec, sparkRef corev1.ObjectReference, sparkImage string, imagePullPolicy string, slotsEnabled bool) {
	// create an init container in the pod, which is just used to populate the jdk and spark mount directories
	containerSpec := corev1.Container{
		Image:           sparkImage,
		ImagePullPolicy: corev1.PullPolicy(imagePullPolicy),
		Name:            "init",
		Command:         []string{"bash", "-c", "cp -r /opt/jdk /mnt && cp -r /opt/spark /mnt"},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "mnt-splunk-jdk", MountPath: "/mnt/jdk"},
			{Name: "mnt-splunk-spark", MountPath: "/mnt/spark"},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("0.25"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
	}
	podTemplateSpec.Spec.InitContainers = append(podTemplateSpec.Spec.InitContainers, containerSpec)

	// add empty jdk and spark mount directories to all of the splunk containers
	emptyVolumeSource := corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	}
	addSplunkVolumeToTemplate(podTemplateSpec, "jdk", emptyVolumeSource)
	addSplunkVolumeToTemplate(podTemplateSpec, "spark", emptyVolumeSource)

	// prepare spark master host URL
	sparkMasterHost := spark.GetSparkServiceName(spark.SparkMaster, sparkRef.Name, false)
	if sparkRef.Namespace != "" {
		sparkMasterHost = splcommon.GetServiceFQDN(sparkRef.Namespace, sparkMasterHost)
	}

	// append DFS env variables to splunk enterprise containers
	dfsEnvVar := []corev1.EnvVar{
		{Name: "SPLUNK_ENABLE_DFS", Value: "true"},
		{Name: "SPARK_MASTER_HOST", Value: sparkMasterHost},
		{Name: "SPARK_MASTER_WEBUI_PORT", Value: "8009"},
		{Name: "SPARK_HOME", Value: "/mnt/splunk-spark"},
		{Name: "JAVA_HOME", Value: "/mnt/splunk-jdk"},
		{Name: "SPLUNK_DFW_NUM_SLOTS_ENABLED", Value: "true"},
	}
	if !slotsEnabled {
		dfsEnvVar[5].Value = "false"
	}
	for idx := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].Env = append(podTemplateSpec.Spec.Containers[idx].Env, dfsEnvVar...)
	}
}

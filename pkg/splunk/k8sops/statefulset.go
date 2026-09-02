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

package k8sops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ReasonPodTerminalFailure is the machine-readable reason used when a pod is
	// stuck in a non-recoverable terminal state that requires manual remediation.
	ReasonPodTerminalFailure splcommon.EventReason = "PodTerminalFailure"
)

// DefaultStatefulSetPodManager is a simple StatefulSetPodManager that does nothing
type DefaultStatefulSetPodManager struct{}

// Update for DefaultStatefulSetPodManager handles all updates for a statefulset of standard pods
func (mgr *DefaultStatefulSetPodManager) Update(ctx context.Context, client splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	phase, err := ApplyStatefulSet(ctx, client, statefulSet)
	if err == nil && phase == enterpriseApi.PhaseReady {
		phase, err = UpdateStatefulSetPods(ctx, client, statefulSet, mgr, desiredReplicas)
	}
	return phase, err
}

// PrepareScaleDown for DefaultStatefulSetPodManager does nothing and returns true
func (mgr *DefaultStatefulSetPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	return true, nil
}

// PrepareRecycle for DefaultStatefulSetPodManager does nothing and returns true
func (mgr *DefaultStatefulSetPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	return true, nil
}

// FinishRecycle for DefaultStatefulSetPodManager does nothing and returns false
func (mgr *DefaultStatefulSetPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	return true, nil
}

func (mgr *DefaultStatefulSetPodManager) FinishUpgrade(ctx context.Context, n int32) error {
	return nil
}

// terminalPodWaitingReasons is the set of container Waiting.Reason values that
// indicate a non-recoverable pod failure. A container stuck in one of these states
// will never start without external intervention (correcting the image reference,
// creating the missing Secret/ConfigMap, etc.).
var terminalPodWaitingReasons = map[string]bool{
	"ErrImagePull":               true, // image pull failed (bad credentials, unreachable registry)
	"ImagePullBackOff":           true, // kubelet backing off after repeated pull failures
	"InvalidImageName":           true, // image reference is syntactically malformed
	"ErrInvalidImage":            true, // image reference resolves but is not a valid image
	"CreateContainerConfigError": true, // env-var or volume references a missing ConfigMap/Secret key
	"CreateContainerError":       true, // OCI runtime cannot create container (missing device, seccomp profile, etc.)
	"RunContainerError":          true, // OCI runtime cannot run container (invalid entrypoint, missing binary)
}

// checkPodsForTerminalFailures lists pods belonging to statefulSet and returns a
// descriptive error if any container or init-container is stuck in a terminal
// waiting state. Returns nil if all pods appear healthy or if the List call
// fails (treated as transient so it does not interrupt reconciliation).
func checkPodsForTerminalFailures(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet) error {
	if statefulSet.Spec.Selector == nil {
		return nil
	}
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(statefulSet.GetNamespace()),
		client.MatchingLabels(statefulSet.Spec.Selector.MatchLabels),
	}
	if err := c.List(ctx, podList, listOpts...); err != nil {
		return nil // transient API error; don't interrupt reconciliation
	}
	for i := range podList.Items {
		pod := &podList.Items[i]
		for _, cs := range pod.Status.InitContainerStatuses {
			if cs.State.Waiting != nil && terminalPodWaitingReasons[cs.State.Waiting.Reason] {
				return fmt.Errorf("pod %s init-container %q in terminal state %s: %s",
					pod.Name, cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
			}
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil && terminalPodWaitingReasons[cs.State.Waiting.Reason] {
				return fmt.Errorf("pod %s container %q in terminal state %s: %s",
					pod.Name, cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
			}
		}
	}
	return nil
}

// CheckPodsForTerminalFailures is the exported form of checkPodsForTerminalFailures.
// It returns a splcommon.TerminalError (which satisfies errors.Is(err, reconcile.TerminalError(nil)))
// if any pod belonging to statefulSet is stuck in a non-recoverable container waiting state,
// or nil otherwise. Callers only need to propagate the returned error.
func CheckPodsForTerminalFailures(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet) error {
	if termErr := checkPodsForTerminalFailures(ctx, c, statefulSet); termErr != nil {
		return splcommon.NewTerminalError(ReasonPodTerminalFailure, "Pod stuck in terminal state — manual fix required", termErr)
	}
	return nil
}

// ApplyStatefulSet creates or updates a Kubernetes StatefulSet
func ApplyStatefulSet(ctx context.Context, c splcommon.ControllerClient, revised *appsv1.StatefulSet) (enterpriseApi.Phase, error) {
	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetName()}
	var current appsv1.StatefulSet

	err := c.Get(ctx, namespacedName, &current)
	if err != nil {
		// In every reconcile, the statefulSet spec created by the operator is compared
		// against the one stored in etcd. While comparing the two specs, for the fields
		// represented by slices(ports, volume mounts etc..) the order of the elements is
		// important i.e any change in order followed by an update of statefulSet will cause
		// a change in the UpdatedRevision field in the StatefulSpec. This inturn triggers
		// a pod recycle unnecessarily. To avoid the same, sort the slices during the
		// statefulSet creation.
		// Note: During the update scenario below, MergePodUpdates takes care of sorting.
		SortPodSlices(ctx, &revised.Spec.Template.Spec, revised.GetObjectMeta().GetName())

		// no StatefulSet exists -> just create a new one
		err = splutil.CreateResource(ctx, c, revised)
		return enterpriseApi.PhasePending, err
	}

	// found an existing StatefulSet

	// check for changes in Pod template
	hasUpdates := MergePodUpdates(ctx, &current.Spec.Template, &revised.Spec.Template, current.GetObjectMeta().GetName())
	*revised = current // caller expects that object passed represents latest state

	// only update if there are material differences, as determined by comparison function
	if hasUpdates {
		// this updates the desired state template, but doesn't actually modify any pods
		// because we use an "OnUpdate" strategy https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#update-strategies
		// note also that this ignores Replicas, which is handled below by UpdateStatefulSetPods

		err = splutil.UpdateResource(ctx, c, revised)
		if err != nil {
			return enterpriseApi.PhaseUpdating, err
		}
		// always pass the latest resource back to caller
		err = c.Get(ctx, namespacedName, revised)
		return enterpriseApi.PhaseUpdating, err
	}

	// scaling and pod updates are handled by UpdateStatefulSetPods
	return enterpriseApi.PhaseReady, nil
}

// UpdateStatefulSetPods manages scaling and config updates for StatefulSets
func UpdateStatefulSetPods(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, mgr splcommon.StatefulSetPodManager, desiredReplicas int32) (enterpriseApi.Phase, error) {
	scopedLog := logging.FromContext(ctx).With("func", "UpdateStatefulSetPods",
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// Re-fetch the StatefulSet to ensure we have the latest status, especially UpdateRevision.
	// This addresses a race condition where the StatefulSet controller may not have updated
	// Status.UpdateRevision yet after a spec change was applied. Without this re-fetch,
	// we might incorrectly report PhaseReady when pods actually need to be recycled.
	namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: statefulSet.GetName()}
	err := c.Get(ctx, namespacedName, statefulSet)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to re-fetch StatefulSet for latest status", "error", err)
		return enterpriseApi.PhaseError, err
	}

	// wait for all replicas ready
	replicas := *statefulSet.Spec.Replicas
	readyReplicas := statefulSet.Status.ReadyReplicas
	if readyReplicas < replicas {
		scopedLog.InfoContext(ctx, "waiting for pods to become ready")
		// Detect terminal container states (wrong image, inaccessible registry, missing
		// ConfigMap/Secret key) that will never self-heal. Surface them as PhaseError
		// immediately rather than waiting for the full reconcile timeout.
		if termErr := checkPodsForTerminalFailures(ctx, c, statefulSet); termErr != nil {
			scopedLog.ErrorContext(ctx, "terminal pod failure detected; setting PhaseError", "error", termErr)
			return enterpriseApi.PhaseError, splcommon.NewTerminalError(ReasonPodTerminalFailure, "Pod stuck in terminal state — manual fix required", termErr)
		}
		if readyReplicas > 0 {
			return enterpriseApi.PhaseScalingUp, nil
		}
		return enterpriseApi.PhasePending, nil
	} else if readyReplicas > replicas {
		scopedLog.InfoContext(ctx, "waiting for scale down to complete")
		return enterpriseApi.PhaseScalingDown, nil
	}

	// readyReplicas == replicas

	// check for scaling up
	if readyReplicas < desiredReplicas {
		// scale up StatefulSet to match desiredReplicas
		scopedLog.InfoContext(ctx, "scaling replicas up", "replicas", desiredReplicas)
		*statefulSet.Spec.Replicas = desiredReplicas
		return enterpriseApi.PhaseScalingUp, splutil.UpdateResource(ctx, c, statefulSet)
	}

	// check for scaling down
	if readyReplicas > desiredReplicas {
		// prepare pod for removal via scale down
		n := readyReplicas - 1
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		ready, err := mgr.PrepareScaleDown(ctx, n)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to decommission Pod", "podName", podName, "error", err)
			return enterpriseApi.PhaseError, err
		}
		if !ready {
			// wait until pod quarantine has completed before deleting it
			return enterpriseApi.PhaseScalingDown, nil
		}

		// scale down statefulset to terminate pod
		scopedLog.InfoContext(ctx, "scaling replicas down", "replicas", n)
		*statefulSet.Spec.Replicas = n
		err = splutil.UpdateResource(ctx, c, statefulSet)
		if err != nil {
			scopedLog.ErrorContext(ctx, "scale down update failed for StatefulSet", "error", err)
			return enterpriseApi.PhaseError, err
		}

		// delete PVCs used by the pod so that a future scale up will have clean state
		for _, vol := range statefulSet.Spec.VolumeClaimTemplates {
			namespacedName := types.NamespacedName{
				Namespace: vol.ObjectMeta.Namespace,
				Name:      fmt.Sprintf("%s-%s", vol.ObjectMeta.Name, podName),
			}
			var pvc corev1.PersistentVolumeClaim
			err := c.Get(ctx, namespacedName, &pvc)
			if err != nil {
				scopedLog.ErrorContext(ctx, "unable to find PVC for deletion", "pvcName", pvc.ObjectMeta.Name, "error", err)
				return enterpriseApi.PhaseError, err
			}
			scopedLog.InfoContext(ctx, "deleting PVC", "pvcName", pvc.ObjectMeta.Name)
			err = c.Delete(ctx, &pvc)
			if err != nil {
				scopedLog.ErrorContext(ctx, "unable to delete PVC", "pvcName", pvc.ObjectMeta.Name, "error", err)
				return enterpriseApi.PhaseError, err
			}
		}

		return enterpriseApi.PhaseScalingDown, nil
	}

	// ready and no StatefulSet scaling is required
	// readyReplicas == desiredReplicas

	// check existing pods for desired updates
	for n := readyReplicas - 1; n >= 0; n-- {
		// get Pod
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
		var pod corev1.Pod
		err := c.Get(ctx, namespacedName, &pod)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to find Pod", "podName", podName, "error", err)
			return enterpriseApi.PhaseError, err
		}
		if pod.Status.Phase != corev1.PodRunning || len(pod.Status.ContainerStatuses) == 0 || !pod.Status.ContainerStatuses[0].Ready {
			if termErr := checkPodsForTerminalFailures(ctx, c, statefulSet); termErr != nil {
				scopedLog.ErrorContext(ctx, "terminal pod failure detected during update; setting PhaseError", "error", termErr)
				return enterpriseApi.PhaseError, splcommon.NewTerminalError(ReasonPodTerminalFailure, "Pod stuck in terminal state — manual fix required", termErr)
			}
			scopedLog.ErrorContext(ctx, "waiting for Pod to become ready", "podName", podName, "error", err)
			return enterpriseApi.PhaseUpdating, err
		}

		// terminate pod if it has pending updates; k8s will start a new one with revised template
		if statefulSet.Status.UpdateRevision != "" && statefulSet.Status.UpdateRevision != pod.GetLabels()["controller-revision-hash"] {
			// pod needs to be updated; first, prepare it to be recycled
			ready, err := mgr.PrepareRecycle(ctx, n)
			if err != nil {
				scopedLog.ErrorContext(ctx, "unable to prepare Pod for recycling", "podName", podName, "error", err)
				return enterpriseApi.PhaseError, err
			}
			if !ready {
				// wait until pod quarantine has completed before deleting it
				return enterpriseApi.PhaseUpdating, nil
			}

			// deleting pod will cause StatefulSet controller to create a new one with latest template
			scopedLog.InfoContext(ctx, "recycling Pod for updates", "podName", podName,
				"statefulSetRevision", statefulSet.Status.UpdateRevision,
				"podRevision", pod.GetLabels()["controller-revision-hash"])
			preconditions := client.Preconditions{UID: &pod.ObjectMeta.UID, ResourceVersion: &pod.ObjectMeta.ResourceVersion}
			err = c.Delete(context.Background(), &pod, preconditions)
			if err != nil {
				scopedLog.ErrorContext(ctx, "unable to delete Pod", "podName", podName, "error", err)
				return enterpriseApi.PhaseError, err
			}

			// only delete one at a time
			return enterpriseApi.PhaseUpdating, nil
		}

		// check if pod was previously prepared for recycling; if so, complete
		complete, err := mgr.FinishRecycle(ctx, n)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to complete recycling of pod", "podName", podName, "error", err)
			return enterpriseApi.PhaseError, err
		}
		if !complete {
			// return and wait until next reconcile to let things settle down
			return enterpriseApi.PhaseUpdating, nil
		}
	}

	// Remove unwanted owner references
	err = splutil.RemoveUnwantedSecrets(ctx, c, statefulSet.GetName(), statefulSet.GetNamespace())
	if err != nil {
		return enterpriseApi.PhaseReady, err
	}

	// all is good!
	scopedLog.InfoContext(ctx, "all pods are ready")

	// Finalize rolling upgrade process
	// It uses first pod to get a client
	err = mgr.FinishUpgrade(ctx, 0)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to finalize rolling upgrade process", "error", err)
		return enterpriseApi.PhaseError, err
	}

	scopedLog.InfoContext(ctx, "statefulset - Phase Ready")

	return enterpriseApi.PhaseReady, nil
}

// SetStatefulSetOwnerRef sets owner references for statefulset
func SetStatefulSetOwnerRef(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, namespacedName types.NamespacedName) error {

	statefulset, err := GetStatefulSetByName(ctx, client, namespacedName)
	if err != nil {
		return err
	}

	currentOwnerRef := statefulset.GetOwnerReferences()
	// Check if owner ref exists
	for i := 0; i < len(currentOwnerRef); i++ {
		if reflect.DeepEqual(currentOwnerRef[i].UID, cr.GetUID()) {
			return nil
		}
	}

	// Owner ref doesn't exist, update statefulset with owner references
	statefulset.SetOwnerReferences(append(statefulset.GetOwnerReferences(), splcommon.AsOwner(cr, false)))

	// Update owner reference if needed
	err = splutil.UpdateResource(ctx, client, statefulset)
	return err
}

// RemoveUnwantedOwnerRefSs removes all the unwanted owner references for statefulset except the CR it belongs to
func RemoveUnwantedOwnerRefSs(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, cr splcommon.MetaObject) error {
	scopedLog := logging.FromContext(ctx).With("func", "RemoveUnwantedOwnerRefSs", "statefulSet", namespacedName)

	scopedLog.InfoContext(ctx, "removing unwanted owner references on CR deletion")

	// Get statefulSet
	statefulset, err := GetStatefulSetByName(ctx, client, namespacedName)
	if err != nil {
		return err
	}

	// Configure statefulSet with only the CR's owner reference
	crOwnerRef := make([]metav1.OwnerReference, 0)
	statefulset.SetOwnerReferences(append(crOwnerRef, splcommon.AsOwner(cr, true)))

	// Update statefulSet
	err = splutil.UpdateResource(ctx, client, statefulset)
	if err != nil {
		return err
	}

	return err
}

// GetStatefulSetByName retrieves current statefulset
func GetStatefulSetByName(ctx context.Context, c splcommon.ControllerClient, namespacedName types.NamespacedName) (*appsv1.StatefulSet, error) {
	var statefulset appsv1.StatefulSet

	err := c.Get(ctx, namespacedName, &statefulset)
	if err != nil {
		// Didn't find it
		return nil, err
	}

	return &statefulset, nil
}

// DeleteReferencesToAutomatedMCIfExists deletes the automated MC sts. This is when customer migrates from automated MC to MC CRD
// Check if MC CR is not the owner of the MC statefulset then delete that Statefulset
func DeleteReferencesToAutomatedMCIfExists(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, namespacedName types.NamespacedName) error {
	statefulset, err := GetStatefulSetByName(ctx, client, namespacedName)
	if err != nil {
		// if MC Sts doesn't exist return nil, may have been deleted by other CR
		return nil
	}
	//2. Retrieve all the owners of the MC statefulset
	currentOwnersRef := statefulset.GetOwnerReferences()
	//3. if Multiple owners OR if current CR is the owner of the MC statefulset then delete the MC statefulset
	if len(currentOwnersRef) > 1 || (len(currentOwnersRef) == 1 && isCurrentCROwner(cr, currentOwnersRef)) {
		err := splutil.DeleteResource(ctx, client, statefulset)
		if err != nil {
			return err
		}

		//delete corresponding mc configmap
		configmap, err := GetConfigMap(ctx, client, namespacedName)
		if k8serrors.IsNotFound(err) {
			return nil
		} else if err != nil {
			return err
		}
		err = splutil.DeleteResource(ctx, client, configmap)
		return err
	}

	return nil
}

// IsStatefulSetScalingUpOrDown checks if we are currently scaling up or down
func IsStatefulSetScalingUpOrDown(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, name string, desiredReplicas int32) (enterpriseApi.StatefulSetScalingType, error) {
	scopedLog := logging.FromContext(ctx).With("func", "isScalingUp", "name", cr.GetName(), "namespace", cr.GetNamespace())

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: name}
	current, err := GetStatefulSetByName(ctx, client, namespacedName)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to get current stateful set", "name", namespacedName, "error", err)
		return enterpriseApi.StatefulSetNotScaling, err
	}

	if *current.Spec.Replicas < desiredReplicas {
		return enterpriseApi.StatefulSetScalingUp, nil
	} else if *current.Spec.Replicas > desiredReplicas {
		return enterpriseApi.StatefulSetScalingDown, nil
	}

	return enterpriseApi.StatefulSetNotScaling, nil
}

// addStorageVolumes adds storage volumes to the StatefulSet
func addStorageVolumes(ctx context.Context, cr splcommon.MetaObject, client splcommon.ControllerClient, spec *enterpriseApi.CommonSplunkSpec, statefulSet *appsv1.StatefulSet, labels map[string]string) error {

	logger := logging.FromContext(ctx).With("func", "addStorageVolumes")

	// configure storage for mount path /opt/splunk/etc
	if spec.EtcVolumeStorageConfig.EphemeralStorage {
		// add ephemeral volumes
		_ = resources.AddEphemeralVolumes(statefulSet, splcommon.EtcVolumeStorage)
	} else {
		// add PVC volumes
		err := resources.AddPVCVolumes(cr, spec, statefulSet, labels, splcommon.EtcVolumeStorage)
		if err != nil {
			return err
		}
	}

	// configure storage for mount path /opt/splunk/var
	if spec.VarVolumeStorageConfig.EphemeralStorage {
		// add ephemeral volumes
		_ = resources.AddEphemeralVolumes(statefulSet, splcommon.VarVolumeStorage)
	} else {
		// add PVC volumes
		err := resources.AddPVCVolumes(cr, spec, statefulSet, labels, splcommon.VarVolumeStorage)
		if err != nil {
			return err
		}
	}

	// Add Splunk Probe config map
	probeConfigMap, err := getProbeConfigMap(ctx, client, cr)
	if err != nil {
		logger.ErrorContext(ctx, "unable to get probeConfigMap", "error", err)
		return err
	}
	resources.AddProbeConfigMapVolume(probeConfigMap, statefulSet)
	return nil
}

func getProbeConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject) (*corev1.ConfigMap, error) {

	logger := logging.FromContext(ctx).With("func", "getProbeConfigMap")

	configMapName := splutil.GetProbeConfigMapName(cr.GetNamespace())
	configMapNamespace := cr.GetNamespace()
	namespacedName := types.NamespacedName{Namespace: configMapNamespace, Name: configMapName}

	// Check if the config map already exists
	logger.DebugContext(ctx, "checking for existing config map", "configMapName", configMapName, "configMapNamespace", configMapNamespace)
	var configMap corev1.ConfigMap
	err := client.Get(ctx, namespacedName, &configMap)

	if err == nil {
		logger.DebugContext(ctx, "retrieved existing config map", "configMapName", configMapName, "configMapNamespace", configMapNamespace)
		return &configMap, nil
	} else if !k8serrors.IsNotFound(err) {
		logger.ErrorContext(ctx, "error retrieving config map", "configMapName", configMapName, "configMapNamespace", configMapNamespace, "error", err)
		return nil, err
	}

	// Existing config map not found, create one for the probes
	logger.InfoContext(ctx, "creating new config map", "configMapName", configMapName, "configMapNamespace", configMapNamespace)
	configMap = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: configMapNamespace,
		},
	}

	// Add readiness script to config map
	data, err := splutil.ReadFile(ctx, splutil.GetReadinessScriptLocation())
	if err != nil {
		return &configMap, err
	}
	configMap.Data = map[string]string{splutil.GetReadinessScriptName(): data}
	// Add liveness script to config map
	livenessScriptLocation, _ := filepath.Abs(splutil.GetLivenessScriptLocation())
	data, err = splutil.ReadFile(ctx, livenessScriptLocation)
	if err != nil {
		return &configMap, err
	}
	configMap.Data[splutil.GetLivenessScriptName()] = data
	// Add startup script to config map
	startupScriptLocation, _ := filepath.Abs(splutil.GetStartupScriptLocation())
	data, err = splutil.ReadFile(ctx, startupScriptLocation)
	if err != nil {
		return &configMap, err
	}
	configMap.Data[splutil.GetStartupScriptName()] = data

	// Apply the configured config map
	_, err = ApplyConfigMap(ctx, client, &configMap)
	if err != nil {
		return &configMap, err
	}
	return &configMap, nil
}

// getSplunkStatefulSet returns a Kubernetes StatefulSet object for Splunk instances configured for a Splunk Enterprise resource.
func GetSplunkStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, instanceType InstanceType, replicas int32, extraEnv []corev1.EnvVar, opts ...resources.StatefulSetOption) (*appsv1.StatefulSet, error) {

	// prepare misc values
	ports := splcommon.SortContainerPorts(resources.GetSplunkContainerPorts(instanceType)) // note that port order is important for tests
	annotations := splcommon.GetIstioAnnotations(ports)
	selectLabels := resources.GetSplunkLabels(cr.GetName(), instanceType, spec.ClusterMasterRef.Name)
	if len(spec.ClusterManagerRef.Name) > 0 && len(spec.ClusterMasterRef.Name) == 0 {
		selectLabels = resources.GetSplunkLabels(cr.GetName(), instanceType, spec.ClusterManagerRef.Name)
	}
	affinity := splcommon.AppendPodAntiAffinity(&spec.Affinity, cr.GetName(), instanceType.ToString())

	// start with same labels as selector; note that this object gets modified by splcommon.AppendParentMeta()
	labels := make(map[string]string)
	for k, v := range selectLabels {
		labels[k] = v
	}

	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      splutil.GetSplunkStatefulsetName(instanceType, cr.GetName()),
	}
	statefulSet := &appsv1.StatefulSet{}
	err := client.Get(ctx, namespacedName, statefulSet)
	if err != nil && !k8serrors.IsNotFound(err) {
		return nil, err
	}

	if k8serrors.IsNotFound(err) {
		// create statefulset configuration
		statefulSet = &appsv1.StatefulSet{
			TypeMeta: metav1.TypeMeta{
				Kind:       "StatefulSet",
				APIVersion: "apps/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      splutil.GetSplunkStatefulsetName(instanceType, cr.GetName()),
				Namespace: cr.GetNamespace(),
				Labels:    labels,
			},
		}
	}

	statefulSet.Spec = appsv1.StatefulSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: selectLabels,
		},
		ServiceName:         splcommon.GetSplunkServiceName(instanceType, cr.GetName(), true),
		Replicas:            &replicas,
		PodManagementPolicy: appsv1.ParallelPodManagement,
		UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
			Type: appsv1.OnDeleteStatefulSetStrategyType,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.PodSpec{
				Affinity:                  affinity,
				Tolerations:               spec.Tolerations,
				TopologySpreadConstraints: spec.TopologySpreadConstraints,
				SchedulerName:             spec.SchedulerName,
				ImagePullSecrets:          spec.ImagePullSecrets,
				Containers: []corev1.Container{
					{
						Image:           spec.Image,
						ImagePullPolicy: corev1.PullPolicy(spec.ImagePullPolicy),
						Name:            "splunk",
						Ports:           ports,
					},
				},
			},
		},
	}

	// Add storage volumes
	err = addStorageVolumes(ctx, cr, client, spec, statefulSet, labels)
	if err != nil {
		return statefulSet, err
	}

	// add serviceaccount if configured
	if spec.ServiceAccount != "" {
		namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: spec.ServiceAccount}
		_, err := GetServiceAccount(ctx, client, namespacedName)
		if err == nil {
			// serviceAccount exists
			statefulSet.Spec.Template.Spec.ServiceAccountName = spec.ServiceAccount
		}
	}

	// append labels and annotations from parent
	splcommon.AppendParentMeta(statefulSet.Spec.Template.GetObjectMeta(), cr.GetObjectMeta())
	if len(spec.PodAnnotations) > 0 {
		if statefulSet.Spec.Template.Annotations == nil {
			statefulSet.Spec.Template.Annotations = make(map[string]string)
		}
		for k, v := range spec.PodAnnotations {
			statefulSet.Spec.Template.Annotations[k] = v
		}
	}

	// retrieve the secret to upload to the statefulSet pod
	statefulSetSecret, err := splutil.GetLatestVersionedSecret(ctx, client, cr, cr.GetNamespace(), statefulSet.GetName())
	if err != nil || statefulSetSecret == nil {
		return statefulSet, err
	}

	// update statefulset's pod template with common splunk pod config
	if err = updateSplunkPodTemplateWithConfig(ctx, client, &statefulSet.Spec.Template, cr, spec, instanceType, extraEnv, statefulSetSecret.GetName()); err != nil {
		return statefulSet, err
	}

	// make Splunk Enterprise object the owner
	statefulSet.SetOwnerReferences(append(statefulSet.GetOwnerReferences(), splcommon.AsOwner(cr, true)))

	resources.ApplyStatefulSetOptions(statefulSet, opts...)

	return statefulSet, nil
}

// updateSplunkPodTemplateWithConfig modifies the podTemplateSpec object based on configuration of the Splunk Enterprise resource.
func updateSplunkPodTemplateWithConfig(ctx context.Context, client splcommon.ControllerClient, podTemplateSpec *corev1.PodTemplateSpec, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, instanceType InstanceType, extraEnv []corev1.EnvVar, secretToMount string) error {

	logger := logging.FromContext(ctx).With("func", "updateSplunkPodTemplateWithConfig")
	// Add custom ports to splunk containers
	if spec.ServiceTemplate.Spec.Ports != nil {
		for idx := range podTemplateSpec.Spec.Containers {
			for _, p := range spec.ServiceTemplate.Spec.Ports {

				podTemplateSpec.Spec.Containers[idx].Ports = append(podTemplateSpec.Spec.Containers[idx].Ports, corev1.ContainerPort{
					Name:          p.Name,
					ContainerPort: int32(p.TargetPort.IntValue()),
					Protocol:      p.Protocol,
				})
			}
		}
	}

	// Add custom volumes to splunk containers other than MC(where CR spec volumes are not needed)
	if spec.Volumes != nil {
		podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, spec.Volumes...)
		for idx := range podTemplateSpec.Spec.Containers {
			for v := range spec.Volumes {
				podTemplateSpec.Spec.Containers[idx].VolumeMounts = append(podTemplateSpec.Spec.Containers[idx].VolumeMounts, corev1.VolumeMount{
					Name:      spec.Volumes[v].Name,
					MountPath: "/mnt/" + spec.Volumes[v].Name,
				})
			}
		}
	}

	// TODO(SPL-306631): remove once the `splunk-provision` is available in the Splunk docker image
	// TODO(SPL-306655): and once the `entrypoint.sh` has been modified in the Splunk docker image
	crAnnotations := cr.GetAnnotations()
	if strings.ToLower(crAnnotations[enterpriseApi.SplunkProvisionAnnotation]) == "true" &&
		resources.SplunkProvisionSupportsRole(instanceType) {
		splunkProvisionImage := os.Getenv("SPLUNK_PROVISION_IMAGE")
		if splunkProvisionImage == "" || splunkProvisionImage == "SPLUNK_PROVISION_IMAGE_VALUE" {
			logger.WarnContext(ctx, "skipping splunk-provision injection", "reason", "SPLUNK_PROVISION_IMAGE not set or unresolved placeholder")
		} else {
			logger.Info("injecting splunk-provision as volume via init-container")
			resources.InjectSplunkProvision(splunkProvisionImage, podTemplateSpec, &extraEnv)
		}
	}

	// Explicitly set the default value here so we can compare for changes correctly with current statefulset.
	secretVolDefaultMode := corev1.SecretVolumeSourceDefaultMode
	resources.AddSplunkVolumeToTemplate(podTemplateSpec, "mnt-splunk-secrets", "/mnt/splunk-secrets", corev1.VolumeSource{
		Secret: &corev1.SecretVolumeSource{
			SecretName:  secretToMount,
			DefaultMode: &secretVolDefaultMode,
		},
	})

	// Explicitly set the default value here so we can compare for changes correctly with current statefulset.
	configMapVolDefaultMode := corev1.ConfigMapVolumeSourceDefaultMode

	// add inline defaults to all splunk containers other than MC(where CR spec defaults are not needed)
	if spec.Defaults != "" {
		configMapName := splutil.GetSplunkDefaultsName(cr.GetName(), instanceType)
		resources.AddSplunkVolumeToTemplate(podTemplateSpec, "mnt-splunk-defaults", "/mnt/splunk-defaults", corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
				DefaultMode: &configMapVolDefaultMode,
			},
		})

		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

		// We stamp a content hash of configMap.Data (not ResourceVersion) so that
		// owner-reference-only writes during bootstrap do not trigger pod restarts.
		configMapObj, err := GetConfigMap(ctx, client, namespacedName)
		if err == nil {
			podTemplateSpec.ObjectMeta.Annotations["defaultConfigRev"] = splutil.ConfigDataHash(configMapObj.Data)
		} else {
			logger.ErrorContext(ctx, "updation of default configMap annotation failed", "error", err)
		}
	}

	// Stamp splcommon.ConfigMapRevAnnotationPrefix+<vol-name> annotation for each user-supplied
	// ConfigMap volume using a content hash rather than ResourceVersion. ResourceVersion changes
	// on any metadata update (labels, annotations) and would cause spurious pod rolls; the hash
	// only changes when the mounted data itself changes.
	// The annotation key uses the volume name (a valid DNS label, ≤63 chars) as the suffix,
	// not the ConfigMap name, which can exceed Kubernetes' 63-char annotation-suffix limit.
	// Projected volumes that reference ConfigMaps are handled via the Sources loop.
	for _, vol := range spec.Volumes {
		switch {
		case vol.ConfigMap != nil:
			cmNS := types.NamespacedName{Namespace: cr.GetNamespace(), Name: vol.ConfigMap.Name}
			cm, err := GetConfigMap(ctx, client, cmNS)
			if err != nil {
				logger.ErrorContext(ctx, "Failed to fetch ConfigMap for restart annotation", "volume", vol.Name, "error", err)
				break
			}
			if cm.Annotations[splcommon.ConfigMapRestartOptOutAnnotation] == "false" {
				// Consumer handles dynamic reload; skip the restart-triggering annotation.
				break
			}
			hash, err := GetConfigMapDataHash(ctx, client, cmNS, vol.ConfigMap.Items)
			if err == nil {
				podTemplateSpec.ObjectMeta.Annotations[splcommon.ConfigMapRevAnnotationPrefix+vol.Name] = hash
			} else {
				logger.ErrorContext(ctx, "Failed to get ConfigMap data hash for annotation", "volume", vol.Name, "error", err)
			}
		case vol.Projected != nil:
			for i, src := range vol.Projected.Sources {
				if src.ConfigMap == nil {
					continue
				}
				cmNS := types.NamespacedName{Namespace: cr.GetNamespace(), Name: src.ConfigMap.Name}
				cm, err := GetConfigMap(ctx, client, cmNS)
				if err != nil {
					logger.ErrorContext(ctx, "Failed to fetch projected ConfigMap for restart annotation", "volume", vol.Name, "configMap", src.ConfigMap.Name, "error", err)
					continue
				}
				if cm.Annotations[splcommon.ConfigMapRestartOptOutAnnotation] == "false" {
					continue
				}
				hash, err := GetConfigMapDataHash(ctx, client, cmNS, src.ConfigMap.Items)
				if err == nil {
					// Build a collision-free annotation key suffix ≤63 chars.
					// vol.Name is a DNS label (≤63 chars); appending ".<n>" can push past the
					// Kubernetes annotation name-segment limit. When the combined length exceeds
					// 63, replace vol.Name with "p.<8-hex-digest>" — the "p." prefix contains a
					// dot, which is legal in annotation name segments but cannot appear in a
					// Kubernetes DNS-label volume name, making hashed keys structurally distinct
					// from any real short volume name and preventing false collisions.
					idxStr := strconv.Itoa(i)
					volNamePart := vol.Name
					if len(volNamePart)+1+len(idxStr) > 63 {
						sum := sha256.Sum256([]byte(vol.Name))
						volNamePart = "p." + hex.EncodeToString(sum[:])[:8]
					}
					podTemplateSpec.ObjectMeta.Annotations[splcommon.ConfigMapRevAnnotationPrefix+volNamePart+"."+idxStr] = hash
				} else {
					logger.ErrorContext(ctx, "Failed to get ConfigMap data hash for projected annotation", "volume", vol.Name, "configMap", src.ConfigMap.Name, "error", err)
				}
			}
		}
	}

	smartstoreConfigMap := GetSmartstoreConfigMap(ctx, client, cr, instanceType)
	if smartstoreConfigMap != nil {
		resources.AddSplunkVolumeToTemplate(podTemplateSpec, "mnt-splunk-operator", "/mnt/splunk-operator/local/", corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: smartstoreConfigMap.GetName(),
				},
				DefaultMode: &configMapVolDefaultMode,
				Items: []corev1.KeyToPath{
					{Key: "indexes.conf", Path: "indexes.conf", Mode: &configMapVolDefaultMode},
					{Key: "server.conf", Path: "server.conf", Mode: &configMapVolDefaultMode},
					{Key: configToken, Path: configToken, Mode: &configMapVolDefaultMode},
				},
			},
		})

		// 1. For Indexer cluster case, do not set the annotation on CM pod. smartstore config is
		// propagated through the CM manager apps bundle push
		// 2. In case of Standalone, reset the Pod by updating the content hash of the
		// smartstore config map so that only real data changes trigger a pod restart.
		if instanceType == SplunkStandalone {
			podTemplateSpec.ObjectMeta.Annotations[smartStoreConfigRev] = splutil.ConfigDataHash(smartstoreConfigMap.Data)
		}
	}

	// update security context
	runAsUser := int64(41812)
	fsGroup := int64(41812)
	runAsNonRoot := true
	fsGroupChangePolicy := corev1.FSGroupChangeOnRootMismatch
	podTemplateSpec.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsUser:           &runAsUser,
		FSGroup:             &fsGroup,
		RunAsNonRoot:        &runAsNonRoot,
		FSGroupChangePolicy: &fsGroupChangePolicy,
	}

	livenessProbe := resources.GetLivenessProbe(spec.LivenessProbe, spec.LivenessInitialDelaySeconds)
	readinessProbe := resources.GetReadinessProbe(spec.ReadinessProbe, spec.ReadinessInitialDelaySeconds)
	startupProbe := resources.GetStartupProbe(spec.StartupProbe)
	probeLogger := logging.FromContext(ctx)
	probeLogger.DebugContext(ctx, "livenessProbe", "Configured", livenessProbe)
	probeLogger.DebugContext(ctx, "readinessProbe", "Configured", readinessProbe)
	probeLogger.DebugContext(ctx, "startupProbe", "Configured", startupProbe)

	// prepare defaults variable
	splunkDefaults := "/mnt/splunk-secrets/default.yml"
	// Check for apps defaults and add it to only the standalone or deployer/cm/mc instances
	if spec.DefaultsURLApps != "" && instanceType != SplunkIndexer && instanceType != SplunkSearchHead {
		splunkDefaults = fmt.Sprintf("%s,%s", spec.DefaultsURLApps, splunkDefaults)
	}
	if spec.DefaultsURL != "" {
		splunkDefaults = fmt.Sprintf("%s,%s", spec.DefaultsURL, splunkDefaults)
	}
	if spec.Defaults != "" {
		splunkDefaults = fmt.Sprintf("%s,%s", "/mnt/splunk-defaults/default.yml", splunkDefaults)
	}

	// prepare container env variables
	role := instanceType.ToRole()
	if instanceType == SplunkStandalone && (len(spec.ClusterMasterRef.Name) > 0 || len(spec.ClusterManagerRef.Name) > 0) {
		role = SplunkSearchHead.ToRole()
	}
	domainName := os.Getenv("CLUSTER_DOMAIN")
	if domainName == "" {
		domainName = "cluster.local"
	}
	env := []corev1.EnvVar{
		{Name: "SPLUNK_HOME", Value: "/opt/splunk"},
		{Name: "SPLUNK_START_ARGS", Value: "--accept-license"},
		{Name: "SPLUNK_DEFAULTS_URL", Value: splunkDefaults},
		{Name: "SPLUNK_HOME_OWNERSHIP_ENFORCEMENT", Value: "false"},
		{Name: "SPLUNK_ROLE", Value: role},
		{Name: "SPLUNK_DECLARATIVE_ADMIN_PASSWORD", Value: "true"},
		{Name: livenessProbeDriverPathEnv, Value: splutil.GetLivenessDriverFilePath()},
		{Name: "SPLUNK_GENERAL_TERMS", Value: os.Getenv("SPLUNK_GENERAL_TERMS")},
		{Name: "SPLUNK_SKIP_CLUSTER_BUNDLE_PUSH", Value: "true"},
		{Name: "SPLUNK_NODE_SIDECAR_POSTGRES_DISABLED", Value: "true"},
	}
	if instanceType != SplunkIngestor {
		env = append(env, corev1.EnvVar{Name: splunkKVStoreDefaultTypeEnv, Value: splunkKVStoreTypeLocal})
	}

	// update variables for licensing, if configured
	if spec.LicenseURL != "" {
		env = append(env, corev1.EnvVar{
			Name:  "SPLUNK_LICENSE_URI",
			Value: spec.LicenseURL,
		})
	}
	if instanceType != SplunkLicenseManager && spec.LicenseManagerRef.Name != "" {
		licenseManagerURL := splcommon.GetSplunkServiceName(SplunkLicenseManager, spec.LicenseManagerRef.Name, false)
		if spec.LicenseManagerRef.Namespace != "" {
			licenseManagerURL = splcommon.GetServiceFQDN(spec.LicenseManagerRef.Namespace, licenseManagerURL)
		}
		env = append(env, corev1.EnvVar{
			Name:  splcommon.LicenseManagerURL,
			Value: licenseManagerURL,
		})
	} else if instanceType != SplunkLicenseMaster && spec.LicenseMasterRef.Name != "" {
		licenseMasterURL := splcommon.GetSplunkServiceName(SplunkLicenseMaster, spec.LicenseMasterRef.Name, false)
		if spec.LicenseMasterRef.Namespace != "" {
			licenseMasterURL = splcommon.GetServiceFQDN(spec.LicenseMasterRef.Namespace, licenseMasterURL)
		}
		env = append(env, corev1.EnvVar{
			Name:  splcommon.LicenseManagerURL,
			Value: licenseMasterURL,
		})
	}

	// append URL for cluster manager, if configured
	var clusterManagerURL string
	if isCMDeployed(instanceType) {
		// This makes splunk-ansible configure indexer-discovery on cluster-manager
		clusterManagerURL = "localhost"
	} else if spec.ClusterManagerRef.Name != "" {
		clusterManagerURL = splcommon.GetSplunkServiceName(SplunkClusterManager, spec.ClusterManagerRef.Name, false)
		if spec.ClusterManagerRef.Namespace != "" {
			clusterManagerURL = splcommon.GetServiceFQDN(spec.ClusterManagerRef.Namespace, clusterManagerURL)
		}
		if spec.LicenseManagerRef.Name == "" && spec.LicenseMasterRef.Name == "" {
			//Check if CM is connected to a LicenseManager
			cmNamespace := cr.GetNamespace()
			if spec.ClusterManagerRef.Namespace != "" {
				cmNamespace = spec.ClusterManagerRef.Namespace
			}
			namespacedName := types.NamespacedName{
				Namespace: cmNamespace,
				Name:      spec.ClusterManagerRef.Name,
			}
			managerIdxCluster := &enterpriseApi.ClusterManager{}
			err := client.Get(ctx, namespacedName, managerIdxCluster)
			if err != nil {
				// Return the error so the reconcile loop requeues rather than continuing
				// with a zero-value CR (which would produce an incomplete env and cause a
				// spurious pod restart on the next reconcile when the real value is found).
				logger.ErrorContext(ctx, "unable to get ClusterManager; requeueing", "error", err)
				return err
			}

			if managerIdxCluster.Spec.LicenseManagerRef.Name != "" {
				licenseManagerNamespace := managerIdxCluster.Spec.LicenseManagerRef.Namespace
				if licenseManagerNamespace == "" {
					licenseManagerNamespace = managerIdxCluster.GetNamespace()
				}
				licenseManagerURL := splcommon.GetSplunkServiceName(SplunkLicenseManager, managerIdxCluster.Spec.LicenseManagerRef.Name, false)
				licenseManagerURL = splcommon.GetServiceFQDN(licenseManagerNamespace, licenseManagerURL)
				env = append(env, corev1.EnvVar{
					Name:  splcommon.LicenseManagerURL,
					Value: licenseManagerURL,
				})
			} else if managerIdxCluster.Spec.LicenseMasterRef.Name != "" {
				licenseMasterNamespace := managerIdxCluster.Spec.LicenseMasterRef.Namespace
				if licenseMasterNamespace == "" {
					licenseMasterNamespace = managerIdxCluster.GetNamespace()
				}
				licenseMasterURL := splcommon.GetSplunkServiceName(SplunkLicenseMaster, managerIdxCluster.Spec.LicenseMasterRef.Name, false)
				licenseMasterURL = splcommon.GetServiceFQDN(licenseMasterNamespace, licenseMasterURL)
				env = append(env, corev1.EnvVar{
					Name:  splcommon.LicenseManagerURL,
					Value: licenseMasterURL,
				})
			}
		}
	} else if spec.ClusterMasterRef.Name != "" {
		clusterManagerURL = splcommon.GetSplunkServiceName(SplunkClusterMaster, spec.ClusterMasterRef.Name, false)
		if spec.ClusterMasterRef.Namespace != "" {
			clusterManagerURL = splcommon.GetServiceFQDN(spec.ClusterMasterRef.Namespace, clusterManagerURL)
		}
		if spec.LicenseManagerRef.Name == "" && spec.LicenseMasterRef.Name == "" {
			//Check if CM is connected to a LicenseManager
			cmNamespace := cr.GetNamespace()
			if spec.ClusterMasterRef.Namespace != "" {
				cmNamespace = spec.ClusterMasterRef.Namespace
			}
			namespacedName := types.NamespacedName{
				Namespace: cmNamespace,
				Name:      spec.ClusterMasterRef.Name,
			}
			managerIdxCluster := &enterpriseApiV3.ClusterMaster{}
			err := client.Get(ctx, namespacedName, managerIdxCluster)
			if err != nil {
				// Return the error so the reconcile loop requeues rather than continuing
				// with a zero-value CR (which would produce an incomplete env and cause a
				// spurious pod restart on the next reconcile when the real value is found).
				logger.ErrorContext(ctx, "unable to get ClusterMaster; requeueing", "error", err)
				return err
			}

			if managerIdxCluster.Spec.LicenseManagerRef.Name != "" {
				licenseManagerNamespace := managerIdxCluster.Spec.LicenseManagerRef.Namespace
				if licenseManagerNamespace == "" {
					licenseManagerNamespace = managerIdxCluster.GetNamespace()
				}
				licenseManagerURL := splcommon.GetSplunkServiceName(SplunkLicenseManager, managerIdxCluster.Spec.LicenseManagerRef.Name, false)
				licenseManagerURL = splcommon.GetServiceFQDN(licenseManagerNamespace, licenseManagerURL)
				env = append(env, corev1.EnvVar{
					Name:  splcommon.LicenseManagerURL,
					Value: licenseManagerURL,
				})
			} else if managerIdxCluster.Spec.LicenseMasterRef.Name != "" {
				licenseMasterNamespace := managerIdxCluster.Spec.LicenseMasterRef.Namespace
				if licenseMasterNamespace == "" {
					licenseMasterNamespace = managerIdxCluster.GetNamespace()
				}
				licenseMasterURL := splcommon.GetSplunkServiceName(SplunkLicenseMaster, managerIdxCluster.Spec.LicenseMasterRef.Name, false)
				licenseMasterURL = splcommon.GetServiceFQDN(licenseMasterNamespace, licenseMasterURL)
				env = append(env, corev1.EnvVar{
					Name:  splcommon.LicenseManagerURL,
					Value: licenseMasterURL,
				})
			}
		}
	}

	if clusterManagerURL != "" {
		extraEnv = append(extraEnv, corev1.EnvVar{
			Name:  splcommon.ClusterManagerURL,
			Value: clusterManagerURL,
		})
	}

	// append REF for monitoring console if configured
	if spec.MonitoringConsoleRef.Name != "" {
		extraEnv = append(extraEnv, corev1.EnvVar{
			Name:  "SPLUNK_MONITORING_CONSOLE_REF",
			Value: spec.MonitoringConsoleRef.Name,
		})
	}

	// Add extraEnv from the CommonSplunkSpec config to the extraEnv variable list
	extraEnv = append(spec.ExtraEnv, extraEnv...)

	// append any extra variables adding environment variable from extraEnv in the first
	// so when duplicates are removed the last ones are removed from the list
	env = append(extraEnv, env...)
	//env = append(env, extraEnv...)

	// check if there are any duplicate entries
	// we use orderedmap so the test case can pass as json marshal
	// expects order
	if len(env) > 0 {
		env = resources.RemoveDuplicateEnvVars(env)
	}

	privileged := false
	// update each container in pod
	for idx := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].Resources = spec.Resources
		podTemplateSpec.Spec.Containers[idx].LivenessProbe = livenessProbe
		podTemplateSpec.Spec.Containers[idx].ReadinessProbe = readinessProbe
		podTemplateSpec.Spec.Containers[idx].StartupProbe = startupProbe
		podTemplateSpec.Spec.Containers[idx].Env = env
		podTemplateSpec.Spec.Containers[idx].SecurityContext = &corev1.SecurityContext{
			RunAsUser:                &runAsUser,
			RunAsNonRoot:             &runAsNonRoot,
			AllowPrivilegeEscalation: &[]bool{false}[0],
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{
					"ALL",
				},
				Add: []corev1.Capability{
					"NET_BIND_SERVICE",
				},
			},
			Privileged: &privileged,
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		}
	}
	return nil
}

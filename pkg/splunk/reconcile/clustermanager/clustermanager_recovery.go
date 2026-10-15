// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package clustermanager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// reconcileNodeRecovery evaluates the current cluster state and drives the
// CM pod recovery state machine when node failure is detected.
// Returns true if the caller should requeue and skip normal reconciliation.
func reconcileNodeRecovery(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) (bool, error) {
	logger := logging.FromContext(ctx).With("func", "reconcileNodeRecovery")

	// emptyDir guard — only attempt recovery when both volumes are ephemeral
	if !cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Spec.VarVolumeStorageConfig.EphemeralStorage {
		logger.DebugContext(ctx, "skipping recovery: storage is not ephemeral")
		return false, nil
	}

	// look up the CM pod
	podName := fmt.Sprintf("splunk-%s-cluster-manager-0", cr.GetName())
	pod := &corev1.Pod{}
	if err := c.Get(ctx, types.NamespacedName{Name: podName, Namespace: cr.GetNamespace()}, pod); err != nil {
		if k8serrors.IsNotFound(err) {
			// if recovery already requested pod deletion, a missing pod means it worked —
			// transition to ReplacementPending so recovery can continue after operator restart
			if cr.Status.Recovery.Phase == enterpriseApi.RecoveryPhaseRequested {
				setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseReplacementPending,
					cr.Status.Recovery.NodeName, cr.Status.Recovery.PodUID,
					"pod deleted, waiting for StatefulSet to create replacement")
				return true, nil
			}
			return false, nil
		}
		return false, err
	}

	// pod not yet scheduled to a node
	if pod.Spec.NodeName == "" {
		return false, nil
	}

	// look up the node the pod is on
	node := &corev1.Node{}
	if err := c.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, node); err != nil {
		if k8serrors.IsNotFound(err) {
			// node object gone — cannot safely delete pod without fencing
			setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseBlocked, pod.Spec.NodeName, string(pod.UID),
				"node object not found; cannot safely delete pod without confirmed fencing")
			return true, nil
		}
		return false, err
	}

	nodeStatus := nodeReadyStatus(node)

	switch cr.Status.Recovery.Phase {
	case "", enterpriseApi.RecoveryPhaseHealthy, enterpriseApi.RecoveryPhaseReadyAfterRecovery:
		return handleHealthyPhase(ctx, logger, cr, pod, nodeStatus)
	case enterpriseApi.RecoveryPhaseWaitingForGracePeriod:
		return handleGracePeriodPhase(ctx, logger, cr, nodeStatus)
	case enterpriseApi.RecoveryPhaseRequested:
		return handleRequestedPhase(ctx, logger, c, cr, pod)
	case enterpriseApi.RecoveryPhaseReplacementPending:
		return handleReplacementPendingPhase(ctx, logger, c, cr, pod)
	case enterpriseApi.RecoveryPhaseBlocked:
		return handleBlockedPhase(ctx, logger, cr, nodeStatus)
	default:
		return false, nil
	}
}

// setRecoveryStatus is a convenience helper that updates the recovery status fields.
// TransitionTime is only updated when the phase actually changes.
func setRecoveryStatus(cr *enterpriseApi.ClusterManager, phase enterpriseApi.RecoveryPhase, nodeName, podUID, message string) {
	if cr.Status.Recovery.Phase != phase {
		now := metav1.Now()
		cr.Status.Recovery.TransitionTime = &now
	}
	cr.Status.Recovery.Phase = phase
	cr.Status.Recovery.NodeName = nodeName
	cr.Status.Recovery.PodUID = podUID
	cr.Status.Recovery.Message = message
}

func handleHealthyPhase(ctx context.Context, logger *slog.Logger, cr *enterpriseApi.ClusterManager, pod *corev1.Pod, nodeStatus corev1.ConditionStatus) (bool, error) {
	switch nodeStatus {
	case corev1.ConditionTrue:
		// node is healthy — clear any stale recovery status
		cr.Status.Recovery = enterpriseApi.ClusterManagerRecoveryStatus{}
		return false, nil

	case corev1.ConditionFalse:
		// node is NotReady — start grace period
		now := metav1.Now()
		setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseWaitingForGracePeriod,
			pod.Spec.NodeName, string(pod.UID),
			fmt.Sprintf("node %s is NotReady, waiting for grace period", pod.Spec.NodeName))
		cr.Status.Recovery.TransitionTime = &now
		logger.InfoContext(ctx, "node failure detected, starting grace period",
			"node", pod.Spec.NodeName, "pod", pod.Name)
		return true, nil

	default:
		// node is Unknown — cannot safely act without fencing
		setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseBlocked,
			pod.Spec.NodeName, string(pod.UID),
			fmt.Sprintf("node %s is unreachable; fencing required before pod deletion", pod.Spec.NodeName))
		logger.WarnContext(ctx, "node unreachable, recovery blocked pending fencing",
			"node", pod.Spec.NodeName)
		return true, nil
	}
}

func handleGracePeriodPhase(ctx context.Context, logger *slog.Logger, cr *enterpriseApi.ClusterManager, nodeStatus corev1.ConditionStatus) (bool, error) {
	// node recovered during grace period — reset to healthy
	if nodeStatus == corev1.ConditionTrue {
		logger.InfoContext(ctx, "node recovered during grace period, resetting recovery state",
			"node", cr.Status.Recovery.NodeName)
		cr.Status.Recovery = enterpriseApi.ClusterManagerRecoveryStatus{}
		return false, nil
	}

	// node became unreachable — block instead of proceeding
	if nodeStatus != corev1.ConditionFalse {
		setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseBlocked,
			cr.Status.Recovery.NodeName, cr.Status.Recovery.PodUID,
			fmt.Sprintf("node %s became unreachable during grace period; fencing required", cr.Status.Recovery.NodeName))
		return true, nil
	}

	// safety: transition time missing, reset to start grace period again
	if cr.Status.Recovery.TransitionTime == nil {
		now := metav1.Now()
		cr.Status.Recovery.TransitionTime = &now
		return true, nil
	}

	gracePeriod := int32(300)
	if cr.Spec.Recovery != nil && cr.Spec.Recovery.GracePeriodSeconds > 0 {
		gracePeriod = cr.Spec.Recovery.GracePeriodSeconds
	}

	elapsed := metav1.Now().Sub(cr.Status.Recovery.TransitionTime.Time)
	remaining := time.Duration(gracePeriod)*time.Second - elapsed

	if remaining > 0 {
		logger.InfoContext(ctx, "grace period not yet elapsed",
			"node", cr.Status.Recovery.NodeName,
			"remainingSeconds", int(remaining.Seconds()))
		return true, nil
	}

	// grace period elapsed — request pod deletion
	logger.InfoContext(ctx, "grace period elapsed, requesting pod deletion",
		"node", cr.Status.Recovery.NodeName)
	setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseRequested,
		cr.Status.Recovery.NodeName, cr.Status.Recovery.PodUID,
		fmt.Sprintf("grace period elapsed, deleting stale pod on node %s", cr.Status.Recovery.NodeName))
	return true, nil
}

func handleRequestedPhase(ctx context.Context, logger *slog.Logger, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager, pod *corev1.Pod) (bool, error) {
	// pod already being deleted — move to waiting for replacement
	if pod.DeletionTimestamp != nil {
		logger.InfoContext(ctx, "pod already has deletion timestamp, waiting for replacement",
			"pod", pod.Name)
		setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseReplacementPending,
			cr.Status.Recovery.NodeName, cr.Status.Recovery.PodUID,
			"pod deletion in progress, waiting for StatefulSet to create replacement")
		return true, nil
	}

	// UID mismatch — replacement already arrived, do not delete it
	if string(pod.UID) != cr.Status.Recovery.PodUID {
		logger.InfoContext(ctx, "pod UID changed, replacement already created",
			"pod", pod.Name, "expectedUID", cr.Status.Recovery.PodUID, "actualUID", string(pod.UID))
		invalidateAppFrameworkState(cr)
		setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseReadyAfterRecovery,
			cr.Status.Recovery.NodeName, string(pod.UID),
			"replacement pod already created, waiting for Cluster Manager to become ready")
		return false, nil
	}

	// delete the stale pod with a UID precondition so the API server rejects the
	// delete if the pod was replaced in the window between our GET and DELETE.
	uid := types.UID(cr.Status.Recovery.PodUID)
	if err := c.Delete(ctx, pod, &client.DeleteOptions{
		Preconditions: &metav1.Preconditions{UID: &uid},
	}); err != nil {
		if k8serrors.IsConflict(err) || k8serrors.IsNotFound(err) {
			setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseReplacementPending,
				cr.Status.Recovery.NodeName, cr.Status.Recovery.PodUID,
				"pod already deleted, waiting for StatefulSet to create replacement")
			return true, nil
		}
		return false, err
	}

	logger.InfoContext(ctx, "stale pod deleted, waiting for replacement",
		"pod", pod.Name, "node", cr.Status.Recovery.NodeName)
	setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseReplacementPending,
		cr.Status.Recovery.NodeName, cr.Status.Recovery.PodUID,
		"pod deleted, waiting for StatefulSet to create replacement")
	return true, nil
}

func handleReplacementPendingPhase(ctx context.Context, logger *slog.Logger, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager, _ *corev1.Pod) (bool, error) {
	// look up the pod fresh — the pod passed in may be the old stale one
	podName := fmt.Sprintf("splunk-%s-cluster-manager-0", cr.GetName())
	newPod := &corev1.Pod{}
	if err := c.Get(ctx, types.NamespacedName{Name: podName, Namespace: cr.GetNamespace()}, newPod); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.InfoContext(ctx, "waiting for StatefulSet to create replacement pod", "pod", podName)
			return true, nil
		}
		return false, err
	}

	// same pod UID — not yet replaced
	if string(newPod.UID) == cr.Status.Recovery.PodUID {
		logger.InfoContext(ctx, "replacement pod not yet created, still waiting", "pod", podName)
		return true, nil
	}

	// new pod exists — invalidate App Framework state so rehydration runs
	invalidateAppFrameworkState(cr)

	logger.InfoContext(ctx, "replacement pod detected, app framework state reset",
		"pod", podName, "newUID", string(newPod.UID))
	setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseReadyAfterRecovery,
		cr.Status.Recovery.NodeName, string(newPod.UID),
		"replacement pod created, waiting for Cluster Manager to become ready")
	return false, nil
}

func handleBlockedPhase(ctx context.Context, logger *slog.Logger, cr *enterpriseApi.ClusterManager, nodeStatus corev1.ConditionStatus) (bool, error) {
	switch nodeStatus {
	case corev1.ConditionTrue:
		logger.InfoContext(ctx, "blocked node recovered, clearing recovery state",
			"node", cr.Status.Recovery.NodeName)
		cr.Status.Recovery = enterpriseApi.ClusterManagerRecoveryStatus{}
		return false, nil

	case corev1.ConditionFalse:
		// node now NotReady (not Unknown) — safe to retry with grace period
		logger.InfoContext(ctx, "blocked node now NotReady, restarting grace period",
			"node", cr.Status.Recovery.NodeName)
		now := metav1.Now()
		setRecoveryStatus(cr, enterpriseApi.RecoveryPhaseWaitingForGracePeriod,
			cr.Status.Recovery.NodeName, cr.Status.Recovery.PodUID,
			fmt.Sprintf("node %s transitioned to NotReady, restarting grace period", cr.Status.Recovery.NodeName))
		cr.Status.Recovery.TransitionTime = &now
		return true, nil

	default:
		// still unreachable — stay blocked
		logger.InfoContext(ctx, "node still unreachable, recovery remains blocked",
			"node", cr.Status.Recovery.NodeName)
		return true, nil
	}
}

// nodeReadyStatus returns the status of the Node's Ready condition,
// or ConditionUnknown if the condition is not present.
func nodeReadyStatus(node *corev1.Node) corev1.ConditionStatus {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status
		}
	}
	return corev1.ConditionUnknown
}

// invalidateAppFrameworkState resets local-scoped app deployment state and
// flags bundle push as needed so the new pod receives full app rehydration.
func invalidateAppFrameworkState(cr *enterpriseApi.ClusterManager) {
	cr.Status.BundlePushTracker.NeedToPushManagerApps = true
	if cr.Status.AppContext.AppsSrcDeployStatus != nil {
		for key, srcStatus := range cr.Status.AppContext.AppsSrcDeployStatus {
			if srcStatus.AppDeploymentInfoList != nil {
				for i := range srcStatus.AppDeploymentInfoList {
					srcStatus.AppDeploymentInfoList[i].DeployStatus = enterpriseApi.DeployStatusPending
					srcStatus.AppDeploymentInfoList[i].PhaseInfo = enterpriseApi.PhaseInfo{
						Phase:  enterpriseApi.PhaseDownload,
						Status: enterpriseApi.AppPkgDownloadPending,
					}
					srcStatus.AppDeploymentInfoList[i].AuxPhaseInfo = nil
				}
			}
			cr.Status.AppContext.AppsSrcDeployStatus[key] = srcStatus
		}
	}
}

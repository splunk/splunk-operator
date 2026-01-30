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
	"strconv"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// PodCleanupFinalizer is added to pods that need cleanup before deletion
	PodCleanupFinalizer = "splunk.com/pod-cleanup"

	// PodIntentAnnotation indicates the intended lifecycle operation for a pod
	PodIntentAnnotation = "splunk.com/pod-intent"

	// Intent values
	PodIntentServe     = "serve"      // Pod is actively serving traffic
	PodIntentScaleDown = "scale-down" // Pod is being removed due to scale-down
	PodIntentRestart   = "restart"    // Pod is being restarted/updated
)

// HandlePodDeletion processes pod deletion events and performs cleanup when finalizer is present
// This handles scale-down operations gracefully, working with HPA and manual scale operations
func HandlePodDeletion(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("HandlePodDeletion").WithValues(
		"pod", pod.Name,
		"namespace", pod.Namespace,
	)

	// Check if pod has our finalizer
	if !hasFinalizer(pod, PodCleanupFinalizer) {
		return nil // Not our pod, nothing to do
	}

	// Check if pod is being deleted
	if pod.DeletionTimestamp == nil {
		return nil // Pod not being deleted yet
	}

	scopedLog.Info("Pod deletion detected with finalizer, starting cleanup")

	// Determine pod type and ordinal from labels
	instanceType := getInstanceTypeFromPod(pod)
	ordinal := getPodOrdinal(pod.Name)

	// Get the owning StatefulSet
	statefulSet, err := getOwningStatefulSet(ctx, c, pod)
	if err != nil {
		scopedLog.Error(err, "Failed to get owning StatefulSet")
		return err
	}

	// Detect if this is scale-down or restart
	// Method 1: Check explicit intent annotation (most reliable)
	// Method 2: Fall back to ordinal comparison
	isScaleDown := false

	if intent, ok := pod.Annotations[PodIntentAnnotation]; ok {
		if intent == PodIntentScaleDown {
			isScaleDown = true
			scopedLog.Info("Scale-down detected via annotation",
				"ordinal", ordinal,
				"statefulSetReplicas", *statefulSet.Spec.Replicas,
				"intent", intent,
				"method", "annotation")
		} else {
			scopedLog.Info("Restart/update detected via annotation",
				"ordinal", ordinal,
				"statefulSetReplicas", *statefulSet.Spec.Replicas,
				"intent", intent,
				"method", "annotation")
		}
	} else {
		// Fall back to ordinal comparison
		if statefulSet != nil && ordinal >= *statefulSet.Spec.Replicas {
			isScaleDown = true
			scopedLog.Info("Scale-down detected via ordinal comparison",
				"ordinal", ordinal,
				"statefulSetReplicas", *statefulSet.Spec.Replicas,
				"method", "ordinal-comparison")
		} else {
			scopedLog.Info("Restart/update detected via ordinal comparison",
				"ordinal", ordinal,
				"statefulSetReplicas", *statefulSet.Spec.Replicas,
				"method", "ordinal-comparison")
		}
	}

	// Perform cleanup based on instance type and operation
	var cleanupErr error
	switch instanceType {
	case SplunkIndexer:
		cleanupErr = handleIndexerPodDeletion(ctx, c, pod, statefulSet, isScaleDown)
	case SplunkSearchHead:
		cleanupErr = handleSearchHeadPodDeletion(ctx, c, pod, statefulSet, isScaleDown)
	default:
		scopedLog.Info("Instance type does not require special cleanup", "type", instanceType)
	}

	if cleanupErr != nil {
		scopedLog.Error(cleanupErr, "Cleanup failed")
		return cleanupErr
	}

	// Remove finalizer to allow pod deletion to proceed
	scopedLog.Info("Cleanup completed successfully, removing finalizer")
	return removeFinalizer(ctx, c, pod, PodCleanupFinalizer)
}

// handleIndexerPodDeletion handles cleanup for indexer pods
func handleIndexerPodDeletion(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod, statefulSet *appsv1.StatefulSet, isScaleDown bool) error {
	scopedLog := log.FromContext(ctx).WithName("handleIndexerPodDeletion")

	if !isScaleDown {
		// For restart/update: preStop hook handles decommission (no --enforce-counts)
		// Just verify decommission is complete before removing finalizer
		scopedLog.Info("Restart operation: preStop hook handles decommission")
		return waitForIndexerDecommission(ctx, c, pod)
	}

	// Scale-down: Need special handling
	scopedLog.Info("Scale-down operation: performing full cleanup")

	// 1. Wait for decommission to complete (preStop hook should have started it with --enforce-counts)
	err := waitForIndexerDecommission(ctx, c, pod)
	if err != nil {
		return fmt.Errorf("failed waiting for decommission: %w", err)
	}

	// 2. Remove peer from Cluster Manager
	err = removeIndexerFromClusterManager(ctx, c, pod, statefulSet)
	if err != nil {
		scopedLog.Error(err, "Failed to remove peer from cluster manager")
		// Don't fail - peer might already be removed or CM might be down
	}

	// 3. Delete PVCs synchronously during scale-down (before removing finalizer)
	// IMPORTANT: We only delete PVCs when finalizer is present AND it's a scale-down operation.
	// For restarts, we preserve PVCs as they contain stateful data that customers may want
	// to use later to recreate pods. This ensures PVCs are deleted immediately during scale-down,
	// even if operator crashes, preventing orphaned storage.
	err = deletePVCsForPod(ctx, c, pod, statefulSet)
	if err != nil {
		scopedLog.Error(err, "Failed to delete PVCs")
		// Don't fail - PVCs might already be deleted or will be cleaned up by reconcile
	}

	return nil
}

// handleSearchHeadPodDeletion handles cleanup for search head pods
func handleSearchHeadPodDeletion(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod, statefulSet *appsv1.StatefulSet, isScaleDown bool) error {
	scopedLog := log.FromContext(ctx).WithName("handleSearchHeadPodDeletion")

	if !isScaleDown {
		// For restart/update: preStop hook handles detention
		scopedLog.Info("Restart operation: preStop hook handles detention")
		return nil
	}

	// Scale-down: Verify detention is complete
	scopedLog.Info("Scale-down operation: verifying detention complete")

	// Wait for search head to be fully detained
	// PreStop hook enables detention, we just verify it's complete
	err := waitForSearchHeadDetention(ctx, c, pod)
	if err != nil {
		return err
	}

	// Delete PVCs synchronously during scale-down (before removing finalizer)
	// IMPORTANT: We only delete PVCs when finalizer is present AND it's a scale-down operation.
	// For restarts, we preserve PVCs as they contain stateful data that customers may want
	// to use later to recreate pods.
	err = deletePVCsForPod(ctx, c, pod, statefulSet)
	if err != nil {
		scopedLog.Error(err, "Failed to delete PVCs")
		// Don't fail - PVCs might already be deleted or will be cleaned up by reconcile
	}

	return nil
}

// waitForIndexerDecommission waits for indexer to complete decommission
func waitForIndexerDecommission(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod) error {
	scopedLog := log.FromContext(ctx).WithName("waitForIndexerDecommission")

	// Get cluster manager to check peer status
	cmName, err := getClusterManagerNameFromPod(ctx, c, pod)
	if err != nil {
		scopedLog.Error(err, "Failed to get cluster manager name")
		return err
	}

	// Get admin credentials
	secret, err := getNamespaceScopedSecret(ctx, c, pod.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get admin secret: %w", err)
	}
	password := string(secret.Data["password"])

	// Create cluster manager client using service FQDN
	// Use GetSplunkServiceName to get the correct service name
	cmServiceName := GetSplunkServiceName(SplunkClusterManager, cmName, false)
	cmEndpoint := fmt.Sprintf("https://%s.%s.svc.cluster.local:8089", cmServiceName, pod.Namespace)
	cmClient := splclient.NewSplunkClient(cmEndpoint, "admin", password)

	// Check peer status
	peers, err := cmClient.GetClusterManagerPeers()
	if err != nil {
		scopedLog.Error(err, "Failed to get cluster peers")
		return err
	}

	// Find this pod's peer entry
	peerStatus, found := peers[pod.Name]
	if !found {
		scopedLog.Info("Peer not found in cluster manager (already removed or never joined)")
		return nil
	}

	// Check if decommission is complete
	if peerStatus.Status == "Down" || peerStatus.Status == "GracefulShutdown" {
		scopedLog.Info("Decommission complete", "status", peerStatus.Status)
		return nil
	}

	// Still decommissioning
	scopedLog.Info("Decommission in progress", "status", peerStatus.Status)
	return fmt.Errorf("decommission not complete, status: %s", peerStatus.Status)
}

// removeIndexerFromClusterManager removes indexer peer from cluster manager
func removeIndexerFromClusterManager(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod, statefulSet *appsv1.StatefulSet) error {
	scopedLog := log.FromContext(ctx).WithName("removeIndexerFromClusterManager")

	// Get cluster manager name
	cmName, err := getClusterManagerNameFromPod(ctx, c, pod)
	if err != nil {
		return err
	}

	// Get admin credentials
	secret, err := getNamespaceScopedSecret(ctx, c, pod.Namespace)
	if err != nil {
		return err
	}
	password := string(secret.Data["password"])

	// Create cluster manager client using service FQDN
	// Use GetSplunkServiceName to get the correct service name
	cmServiceName := GetSplunkServiceName(SplunkClusterManager, cmName, false)
	cmEndpoint := fmt.Sprintf("https://%s.%s.svc.cluster.local:8089", cmServiceName, pod.Namespace)
	cmClient := splclient.NewSplunkClient(cmEndpoint, "admin", password)

	// Get peer ID
	peers, err := cmClient.GetClusterManagerPeers()
	if err != nil {
		return err
	}

	peerInfo, found := peers[pod.Name]
	if !found {
		scopedLog.Info("Peer not found in cluster manager")
		return nil
	}

	// Remove peer
	scopedLog.Info("Removing peer from cluster manager", "peerID", peerInfo.ID)
	return cmClient.RemoveIndexerClusterPeer(peerInfo.ID)
}

// waitForSearchHeadDetention waits for search head detention to complete
func waitForSearchHeadDetention(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod) error {
	scopedLog := log.FromContext(ctx).WithName("waitForSearchHeadDetention")
	// For now, just log - preStop hook handles detention
	// In future, could verify via SHC captain API
	scopedLog.Info("Search head detention verification not implemented yet")
	return nil
}

// Helper functions

func hasFinalizer(pod *corev1.Pod, finalizer string) bool {
	for _, f := range pod.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

func removeFinalizer(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod, finalizer string) error {
	scopedLog := log.FromContext(ctx).WithName("removeFinalizer")

	// Remove finalizer from list
	newFinalizers := []string{}
	for _, f := range pod.Finalizers {
		if f != finalizer {
			newFinalizers = append(newFinalizers, f)
		}
	}

	pod.Finalizers = newFinalizers

	// Update pod
	err := c.Update(ctx, pod)
	if err != nil {
		scopedLog.Error(err, "Failed to remove finalizer")
		return err
	}

	scopedLog.Info("Finalizer removed successfully")
	return nil
}

func getInstanceTypeFromPod(pod *corev1.Pod) InstanceType {
	// Check labels for instance type
	if role, ok := pod.Labels["app.kubernetes.io/component"]; ok {
		switch role {
		case "indexer":
			return SplunkIndexer
		case "search-head":
			return SplunkSearchHead
		}
	}
	return SplunkStandalone // Default
}

func getPodOrdinal(podName string) int32 {
	// Extract ordinal from pod name: splunk-test-indexer-2 -> 2
	parts := strings.Split(podName, "-")
	if len(parts) > 0 {
		if ordinal, err := strconv.ParseInt(parts[len(parts)-1], 10, 32); err == nil {
			return int32(ordinal)
		}
	}
	return -1
}

func getOwningStatefulSet(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod) (*appsv1.StatefulSet, error) {
	// Get StatefulSet name from owner references
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "StatefulSet" {
			statefulSet := &appsv1.StatefulSet{}
			namespacedName := types.NamespacedName{
				Name:      owner.Name,
				Namespace: pod.Namespace,
			}
			err := c.Get(ctx, namespacedName, statefulSet)
			if err != nil {
				return nil, err
			}
			return statefulSet, nil
		}
	}
	return nil, fmt.Errorf("no StatefulSet owner found")
}

func getClusterManagerNameFromPod(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod) (string, error) {
	// Get cluster manager name from pod environment or labels
	// For now, extract from StatefulSet name pattern
	// splunk-{cr-name}-indexer-{ordinal} -> cr-name is cluster name
	parts := strings.Split(pod.Name, "-indexer-")
	if len(parts) != 2 {
		return "", fmt.Errorf("unable to parse cluster name from pod name: %s", pod.Name)
	}
	// Remove "splunk-" prefix
	clusterName := strings.TrimPrefix(parts[0], "splunk-")

	// Get IndexerCluster CR to find ClusterManagerRef
	idxc := &enterpriseApi.IndexerCluster{}
	err := c.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: pod.Namespace}, idxc)
	if err != nil {
		return "", fmt.Errorf("failed to get IndexerCluster CR: %w", err)
	}

	if idxc.Spec.ClusterManagerRef.Name != "" {
		return idxc.Spec.ClusterManagerRef.Name, nil
	}
	return "", fmt.Errorf("no cluster manager reference found")
}

func getNamespaceScopedSecret(ctx context.Context, c splcommon.ControllerClient, namespace string) (*corev1.Secret, error) {
	secretName := splcommon.GetNamespaceScopedSecretName(namespace)
	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret)
	return secret, err
}

// deletePVCsForPod deletes PVCs associated with a specific pod during scale-down
// This is called synchronously in the finalizer handler before removing the finalizer
//
// DESIGN DECISION: We only delete PVCs during scale-down when the pod has the finalizer.
// - Scale-down: Pod is being permanently removed → Delete PVCs
// - Restart: Pod will be recreated → Preserve PVCs (stateful data customers may need)
// This ensures we don't lose customer data during routine restarts while properly cleaning
// up storage during scale-down operations.
func deletePVCsForPod(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod, statefulSet *appsv1.StatefulSet) error {
	scopedLog := log.FromContext(ctx).WithName("deletePVCsForPod")

	if statefulSet == nil {
		return fmt.Errorf("statefulSet is nil")
	}

	ordinal := getPodOrdinal(pod.Name)

	// Delete each PVC for this pod based on VolumeClaimTemplates
	for _, template := range statefulSet.Spec.VolumeClaimTemplates {
		pvcName := fmt.Sprintf("%s-%s-%d", template.Name, statefulSet.Name, ordinal)

		pvc := &corev1.PersistentVolumeClaim{}
		err := c.Get(ctx, types.NamespacedName{
			Name:      pvcName,
			Namespace: pod.Namespace,
		}, pvc)

		if err != nil {
			if k8serrors.IsNotFound(err) {
				scopedLog.Info("PVC already deleted", "pvc", pvcName)
				continue
			}
			return fmt.Errorf("failed to get PVC %s: %w", pvcName, err)
		}

		// Delete PVC
		scopedLog.Info("Deleting PVC for scaled-down pod", "pvc", pvcName)
		if err := c.Delete(ctx, pvc); err != nil {
			if k8serrors.IsNotFound(err) {
				scopedLog.Info("PVC already deleted", "pvc", pvcName)
				continue
			}
			return fmt.Errorf("failed to delete PVC %s: %w", pvcName, err)
		}

		scopedLog.Info("Successfully deleted PVC", "pvc", pvcName)
	}

	return nil
}

// MarkPodsForScaleDown updates the intent annotation on pods that will be deleted due to scale-down
// This should be called BEFORE reducing StatefulSet replicas to mark pods with explicit intent
func MarkPodsForScaleDown(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, newReplicas int32) error {
	scopedLog := log.FromContext(ctx).WithName("MarkPodsForScaleDown")

	currentReplicas := *statefulSet.Spec.Replicas

	// Only mark pods if we're scaling down
	if newReplicas >= currentReplicas {
		return nil
	}

	// Mark pods that will be deleted (from newReplicas to currentReplicas-1)
	for i := newReplicas; i < currentReplicas; i++ {
		podName := fmt.Sprintf("%s-%d", statefulSet.Name, i)
		pod := &corev1.Pod{}
		err := c.Get(ctx, types.NamespacedName{
			Name:      podName,
			Namespace: statefulSet.Namespace,
		}, pod)

		if err != nil {
			if k8serrors.IsNotFound(err) {
				scopedLog.Info("Pod already deleted, skipping", "pod", podName)
				continue
			}
			return fmt.Errorf("failed to get pod %s: %w", podName, err)
		}

		// Update intent annotation
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}

		// Only update if annotation is different
		if pod.Annotations[PodIntentAnnotation] != PodIntentScaleDown {
			pod.Annotations[PodIntentAnnotation] = PodIntentScaleDown
			scopedLog.Info("Marking pod for scale-down", "pod", podName, "ordinal", i)

			if err := c.Update(ctx, pod); err != nil {
				return fmt.Errorf("failed to update pod %s annotation: %w", podName, err)
			}
		}
	}

	return nil
}

// CleanupOrphanedPVCs removes PVCs for pods that no longer exist due to scale-down
// This should be called during reconciliation after scale-down is detected
// NOTE: With V2 finalizer implementation, this is a backup cleanup mechanism
// PVCs should already be deleted synchronously by the finalizer handler
func CleanupOrphanedPVCs(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet) error {
	scopedLog := log.FromContext(ctx).WithName("CleanupOrphanedPVCs")

	currentReplicas := *statefulSet.Spec.Replicas

	// Check for PVCs beyond current replica count
	for _, volTemplate := range statefulSet.Spec.VolumeClaimTemplates {
		// Check up to reasonable limit (e.g., 100)
		for i := currentReplicas; i < 100; i++ {
			pvcName := fmt.Sprintf("%s-%s-%d", volTemplate.Name, statefulSet.Name, i)
			pvc := &corev1.PersistentVolumeClaim{}
			err := c.Get(ctx, types.NamespacedName{
				Name:      pvcName,
				Namespace: statefulSet.Namespace,
			}, pvc)

			if err != nil {
				// PVC doesn't exist, we've found all orphaned PVCs
				break
			}

			// PVC exists but pod doesn't - delete it
			scopedLog.Info("Deleting orphaned PVC from scale-down", "pvc", pvcName)
			err = c.Delete(ctx, pvc)
			if err != nil {
				scopedLog.Error(err, "Failed to delete orphaned PVC", "pvc", pvcName)
				return err
			}
		}
	}

	return nil
}

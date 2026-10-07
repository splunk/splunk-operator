// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package noah

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	noahclient "github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/test/testenv"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("Noah scaling", Serial,
	Label("tier:noah-e2e", "sva:c3", "cloud:kraken", "variant:noah", "feature:noah", "feature:scaling", "scenario:scaling"),
	func() {
		It("adds and removes one peer without restarting existing Pods", NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {
			topology := waitForC3Ready(ctx)
			idxc := getIndexerCluster(ctx)
			Expect(idxc.Status.Lifecycle).To(BeNil(), "start scaling only when no lifecycle is active")
			original := idxc.Spec.Replicas
			Expect(original).To(BeNumerically(">", 0))
			noah := newNoahClient(ctx)
			waitForMembership(ctx, noah, topology.IndexerPods)

			// Mutations use uncached reads so cleanup sees even an immediately failed scale-up.
			kubeClient := uncachedClient()
			statefulSet := &appsv1.StatefulSet{}
			Expect(kubeClient.Get(ctx, client.ObjectKey{Namespace: operatorNamespace,
				Name: splutil.GetSplunkStatefulsetName(splcommon.SplunkIndexer, clusterName)}, statefulSet)).To(Succeed())
			addedPodKey := client.ObjectKey{Namespace: operatorNamespace,
				Name: splutil.GetSplunkStatefulsetPodName(splcommon.SplunkIndexer, clusterName, original)}
			Expect(apierrors.IsNotFound(kubeClient.Get(ctx, addedPodKey, &corev1.Pod{}))).To(BeTrue(), "extra ordinal must not already exist")
			for _, template := range statefulSet.Spec.VolumeClaimTemplates {
				key := client.ObjectKey{Namespace: operatorNamespace, Name: template.Name + "-" + addedPodKey.Name}
				Expect(apierrors.IsNotFound(kubeClient.Get(ctx, key, &corev1.PersistentVolumeClaim{}))).To(BeTrue(), "refuse to consume pre-existing PVC %s", key)
			}
			sourceUIDs := make(map[string]types.UID, len(topology.IndexerPods))
			for _, name := range topology.IndexerPods {
				pod := &corev1.Pod{}
				Expect(kubeClient.Get(ctx, client.ObjectKey{Namespace: operatorNamespace, Name: name}, pod)).To(Succeed())
				sourceUIDs[name] = pod.UID
			}

			DeferCleanup(func(cleanupCtx SpecContext) {
				Expect(setIndexerReplicas(cleanupCtx, kubeClient, client.ObjectKeyFromObject(idxc), idxc.UID, original+1, original)).To(Succeed())
				waitForScaledC3(cleanupCtx, noah, original)
			}, NodeTimeout(testenv.MediumTimeout))

			By(fmt.Sprintf("scaling from %d to %d indexers", original, original+1))
			Expect(setIndexerReplicas(ctx, kubeClient, client.ObjectKeyFromObject(idxc), idxc.UID, original, original+1)).To(Succeed())
			waitForScaledC3(ctx, noah, original+1)
			verifyPodUIDs(ctx, kubeClient, sourceUIDs)
			addedPod := &corev1.Pod{}
			Expect(kubeClient.Get(ctx, addedPodKey, addedPod)).To(Succeed())
			removedPeerID := indexerPeerID(addedPod)

			By(fmt.Sprintf("scaling back to %d indexers", original))
			Expect(setIndexerReplicas(ctx, kubeClient, client.ObjectKeyFromObject(idxc), idxc.UID, original+1, original)).To(Succeed())
			waitForScaledC3(ctx, noah, original)
			verifyPodUIDs(ctx, kubeClient, sourceUIDs)
			removed := []client.Object{addedPod.DeepCopy()}
			for _, template := range statefulSet.Spec.VolumeClaimTemplates {
				removed = append(removed, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
					Namespace: operatorNamespace, Name: template.Name + "-" + addedPod.Name,
				}})
			}
			// Noah's peer list is eventually consistent, so poll the removed peer with
			// its Pod and PVCs rather than reading it once.
			Eventually(func() error {
				for _, object := range removed {
					key := client.ObjectKeyFromObject(object)
					err := kubeClient.Get(ctx, key, object)
					if err == nil {
						return fmt.Errorf("removed resource %s still exists", key)
					}
					if !apierrors.IsNotFound(err) {
						return err
					}
				}
				peers, err := noah.ListPeers(ctx)
				stopOnTerminalNoahError(err, "Noah rejected the removed-peer observation")
				if err != nil {
					return err
				}
				return verifyRemovedPeer(peers, removedPeerID)
			}).WithContext(ctx).WithTimeout(testenv.ShortTimeout).
				WithPolling(testenv.PollInterval).Should(Succeed())
		})
	})

// Patch only replicas, refusing to overwrite another user's scaling or a recreated CR.
func setIndexerReplicas(ctx context.Context, kubeClient client.Client, key client.ObjectKey, uid types.UID, from, to int32) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		idxc := &enterpriseApi.IndexerCluster{}
		if err := kubeClient.Get(ctx, key, idxc); err != nil {
			return err
		}
		if idxc.UID != uid || idxc.DeletionTimestamp != nil {
			return fmt.Errorf("IndexerCluster %s was replaced or is being deleted", key)
		}
		if idxc.Spec.Replicas == to {
			return nil
		}
		if idxc.Spec.Replicas != from {
			return fmt.Errorf("IndexerCluster %s replicas changed externally to %d", key, idxc.Spec.Replicas)
		}
		before := idxc.DeepCopy()
		idxc.Spec.Replicas = to
		return kubeClient.Patch(ctx, idxc, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	})
}

func waitForScaledC3(ctx context.Context, noah *noahclient.Client, replicas int32) {
	GinkgoHelper()
	Eventually(func() error {
		topology, err := inspectC3Ready(ctx, testcaseEnvInstance.GetKubeClient(), operatorNamespace, clusterName, operatorName, noahDeployment)
		if isTerminalReadinessError(err) {
			StopTrying("Noah C3 reported a terminal readiness condition").Wrap(err).Now()
		}
		if err != nil {
			return err
		}
		idxc := &enterpriseApi.IndexerCluster{}
		if err := testcaseEnvInstance.GetKubeClient().Get(ctx,
			client.ObjectKey{Namespace: operatorNamespace, Name: clusterName}, idxc); err != nil {
			return err
		}
		if idxc.Spec.Replicas != replicas || idxc.Status.Lifecycle != nil {
			lifecycle := "none"
			if active := idxc.Status.Lifecycle; active != nil {
				lifecycle = fmt.Sprintf("%s/%s", active.Kind, active.Checkpoint)
			}
			return fmt.Errorf("waiting for %d replicas with no active lifecycle: spec replicas=%d, lifecycle=%s",
				replicas, idxc.Spec.Replicas, lifecycle)
		}
		statefulSet := &appsv1.StatefulSet{}
		if err := testcaseEnvInstance.GetKubeClient().Get(ctx, client.ObjectKey{Namespace: operatorNamespace,
			Name: splutil.GetSplunkStatefulsetName(splcommon.SplunkIndexer, clusterName)}, statefulSet); err != nil {
			return err
		}
		if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != replicas ||
			statefulSet.Status.ObservedGeneration != statefulSet.Generation ||
			statefulSet.Status.Replicas != replicas || statefulSet.Status.ReadyReplicas != replicas {
			specReplicas := "unset"
			if statefulSet.Spec.Replicas != nil {
				specReplicas = fmt.Sprint(*statefulSet.Spec.Replicas)
			}
			return fmt.Errorf("waiting for the indexer StatefulSet to converge at %d replicas: spec=%s status=%d ready=%d observedGeneration=%d/%d",
				replicas, specReplicas, statefulSet.Status.Replicas, statefulSet.Status.ReadyReplicas,
				statefulSet.Status.ObservedGeneration, statefulSet.Generation)
		}
		if len(topology.IndexerPods) != int(replicas) {
			return fmt.Errorf("expected %d indexer Pods, observed %d", replicas, len(topology.IndexerPods))
		}
		err = inspectMembership(ctx, noah, topology.IndexerPods)
		stopOnTerminalNoahError(err, "Noah rejected the scaled membership observation")
		return err
	}).WithContext(ctx).WithTimeout(readyTimeout).
		WithPolling(testenv.PollInterval).Should(Succeed())
}

func verifyPodUIDs(ctx context.Context, kubeClient client.Client, expected map[string]types.UID) {
	GinkgoHelper()
	for name, uid := range expected {
		pod := &corev1.Pod{}
		Expect(kubeClient.Get(ctx, client.ObjectKey{Namespace: operatorNamespace, Name: name}, pod)).To(Succeed())
		Expect(pod.UID).To(Equal(uid), "scaling must not restart existing Pod %s", name)
	}
}

func verifyRemovedPeer(peers []noahclient.Peer, id string) error {
	for _, peer := range peers {
		if peer.ID == id && peer.Status != noahclient.PeerStatusDown {
			return fmt.Errorf("removed peer %s still reports %q", id, peer.Status)
		}
	}
	return nil
}

func TestNoahSetIndexerReplicas(t *testing.T) {
	idxc := &enterpriseApi.IndexerCluster{}
	idxc.Name, idxc.Namespace, idxc.UID = "c3", "test", "original"
	idxc.Spec.Replicas = 2
	idxc.Spec.Image = "unchanged-image"
	scheme := runtime.NewScheme()
	assert.NoError(t, enterpriseApi.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(idxc).Build()
	key := client.ObjectKeyFromObject(idxc)
	ctx := t.Context()
	assert.NoError(t, setIndexerReplicas(ctx, kubeClient, key, idxc.UID, 2, 3))
	assert.NoError(t, setIndexerReplicas(ctx, kubeClient, key, idxc.UID, 2, 3), "idempotent retry")
	assert.NoError(t, kubeClient.Get(ctx, key, idxc))
	assert.Equal(t, int32(3), idxc.Spec.Replicas)
	assert.Equal(t, "unchanged-image", idxc.Spec.Image)
	assert.ErrorContains(t, setIndexerReplicas(ctx, kubeClient, key, "replacement", 3, 2), "replaced")
	assert.ErrorContains(t, setIndexerReplicas(ctx, kubeClient, key, idxc.UID, 4, 2), "externally")
	assert.NoError(t, setIndexerReplicas(ctx, kubeClient, key, idxc.UID, 3, 2), "restore original replicas")
}

func TestNoahSetIndexerReplicasRetriesConflict(t *testing.T) {
	idxc := &enterpriseApi.IndexerCluster{}
	idxc.Name, idxc.Namespace, idxc.UID = "c3", "test", "original"
	idxc.Spec.Replicas = 2
	idxc.Spec.Image = "unchanged-image"
	scheme := runtime.NewScheme()
	assert.NoError(t, enterpriseApi.AddToScheme(scheme))
	patches := 0
	// Another writer changes an unrelated field between the read and the first
	// patch, so the optimistic lock rejects that patch and the retry must keep
	// the concurrent change.
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(idxc).
		WithInterceptorFuncs(interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object,
			patch client.Patch, opts ...client.PatchOption) error {
			patches++
			if patches == 1 {
				concurrent := &enterpriseApi.IndexerCluster{}
				if err := c.Get(ctx, client.ObjectKeyFromObject(obj), concurrent); err != nil {
					return err
				}
				concurrent.Labels = map[string]string{"concurrent": "edit"}
				if err := c.Update(ctx, concurrent); err != nil {
					return err
				}
			}
			return c.Patch(ctx, obj, patch, opts...)
		}}).Build()
	key := client.ObjectKeyFromObject(idxc)
	ctx := t.Context()

	assert.NoError(t, setIndexerReplicas(ctx, kubeClient, key, idxc.UID, 2, 3))
	assert.Equal(t, 2, patches, "the conflicting patch must be retried once")
	assert.NoError(t, kubeClient.Get(ctx, key, idxc))
	assert.Equal(t, int32(3), idxc.Spec.Replicas)
	assert.Equal(t, "edit", idxc.Labels["concurrent"], "the retry must not overwrite the concurrent change")
	assert.Equal(t, "unchanged-image", idxc.Spec.Image)
}

func TestNoahSetIndexerReplicasRefusesDeletingCR(t *testing.T) {
	idxc := &enterpriseApi.IndexerCluster{}
	idxc.Name, idxc.Namespace, idxc.UID = "c3", "test", "original"
	idxc.Spec.Replicas = 2
	// The fake client accepts a deletion timestamp only on an object that has a finalizer.
	idxc.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	idxc.DeletionTimestamp = &metav1.Time{Time: time.Unix(100, 0)}
	scheme := runtime.NewScheme()
	assert.NoError(t, enterpriseApi.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(idxc).Build()
	key := client.ObjectKeyFromObject(idxc)
	ctx := t.Context()

	assert.ErrorContains(t, setIndexerReplicas(ctx, kubeClient, key, idxc.UID, 2, 3), "being deleted")
	assert.NoError(t, kubeClient.Get(ctx, key, idxc))
	assert.Equal(t, int32(2), idxc.Spec.Replicas, "a deleting CR must not be scaled")
}

func TestNoahVerifyRemovedPeer(t *testing.T) {
	assert.NoError(t, verifyRemovedPeer(nil, "removed"))
	assert.NoError(t, verifyRemovedPeer([]noahclient.Peer{{ID: "other", Status: noahclient.PeerStatusUp}}, "removed"))
	assert.NoError(t, verifyRemovedPeer([]noahclient.Peer{{ID: "removed", Status: noahclient.PeerStatusDown}}, "removed"))
	assert.Error(t, verifyRemovedPeer([]noahclient.Peer{{ID: "removed", Status: noahclient.PeerStatusUp}}, "removed"))
	assert.Error(t, verifyRemovedPeer([]noahclient.Peer{{ID: "removed", Status: noahclient.PeerStatusDown},
		{ID: "removed", Status: noahclient.PeerStatusUp}}, "removed"))
}

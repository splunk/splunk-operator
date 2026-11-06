/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package majorupgradeadapter

import (
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func observedInstanceTestPod(cluster *cnpgv1.Cluster, name, address string) *corev1.Pod {
	return &corev1.Pod{Name: name, Namespace: cluster.Namespace, UID: types.UID(string(cluster.UID) + "-" + name),
		Labels:          map[string]string{"cnpg.io/cluster": cluster.Name},
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cluster, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))},
		Status:          corev1.PodStatus{Phase: corev1.PodRunning, PodIP: address, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
}

func TestHealthyInstanceDiscoveryRequiresObservedPodIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*cnpgv1.Cluster, *corev1.Pod)
		failed bool
	}{
		{name: "owned ready pod"},
		{name: "IPv6 pod", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.Status.PodIP = "fd00::17" }},
		{name: "foreign UID", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.OwnerReferences[0].UID = "foreign" }, failed: true},
		{name: "foreign owner name", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.OwnerReferences[0].Name = "foreign" }, failed: true},
		{name: "foreign owner kind", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.OwnerReferences[0].Kind = "Job" }, failed: true},
		{name: "foreign owner API", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.OwnerReferences[0].APIVersion = "apps/v1" }, failed: true},
		{name: "foreign label", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.Labels["cnpg.io/cluster"] = "foreign" }, failed: true},
		{name: "missing pod UID", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.UID = "" }, failed: true},
		{name: "missing environment UID", mutate: func(c *cnpgv1.Cluster, _ *corev1.Pod) { c.UID = "" }, failed: true},
		{name: "unready pod", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.Status.Conditions = nil }, failed: true},
		{name: "pending pod", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.Status.Phase = corev1.PodPending }, failed: true},
		{name: "missing IP", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.Status.PodIP = "" }, failed: true},
		{name: "DNS instead of IP", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.Status.PodIP = "pod.example" }, failed: true},
		{name: "loopback IP", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.Status.PodIP = "127.0.0.1" }, failed: true},
		{name: "unspecified IP", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) { p.Status.PodIP = "::" }, failed: true},
		{name: "duplicate healthy inventory", mutate: func(c *cnpgv1.Cluster, _ *corev1.Pod) {
			c.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"blue-1", "blue-1"}
		}, failed: true},
		{name: "terminating pod", mutate: func(_ *cnpgv1.Cluster, p *corev1.Pod) {
			p.DeletionTimestamp = new(metav1.Now())
			p.Finalizers = []string{"test-finalizer"}
		}, failed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			cluster := &cnpgv1.Cluster{Name: "blue", Namespace: "default", UID: "blue-uid", Status: cnpgv1.ClusterStatus{InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"blue-1"}}}}
			pod := observedInstanceTestPod(cluster, "blue-1", "10.42.0.17")
			if tt.mutate != nil {
				tt.mutate(cluster, pod)
			}
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
			names, pods, err := healthyInstancePods(t.Context(), reader, cluster)
			if tt.failed {
				require.Error(t, err)
				assert.Empty(t, names)
				assert.Empty(t, pods)
			} else {
				require.NoError(t, err)
				assert.Equal(t, []string{pod.Name}, names)
				assert.Equal(t, pod.Status.PodIP, pods[pod.Name].Status.PodIP)
				assert.Equal(t, pod.UID, pods[pod.Name].UID)
			}
		})
	}
}

func TestInstanceDiscoveryRejectsDuplicateAddresses(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cluster := &cnpgv1.Cluster{Name: "blue", Namespace: "default", UID: "blue-uid", Status: cnpgv1.ClusterStatus{InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"blue-1", "blue-2"}}}}
	pod1 := observedInstanceTestPod(cluster, "blue-1", "10.42.0.17")
	pod2 := observedInstanceTestPod(cluster, "blue-2", "::ffff:10.42.0.17")
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod1, pod2).Build()
	_, _, err := healthyInstancePods(t.Context(), reader, cluster)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	require.ErrorContains(t, err, "share a direct address")
}

func TestRetainedInstanceDiscoveryPreservesUnavailableSourceRecovery(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cluster := &cnpgv1.Cluster{Name: "blue", Namespace: "default", UID: "blue-uid", Status: cnpgv1.ClusterStatus{InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"blue-missing", "blue-addressless", "blue-unready"}}}}
	addressless := observedInstanceTestPod(cluster, "blue-addressless", "")
	addressless.Status.Phase = corev1.PodPending
	unready := observedInstanceTestPod(cluster, "blue-unready", "10.42.0.19")
	unready.Status.Conditions = nil
	unready.DeletionTimestamp = new(metav1.Now())
	unready.Finalizers = []string{"test-finalizer"}
	unlisted := observedInstanceTestPod(cluster, "blue-unlisted", "10.42.0.20")
	unlisted.Status.Conditions = nil
	bootstrap := observedInstanceTestPod(cluster, "blue-bootstrap", unlisted.Status.PodIP)
	bootstrap.OwnerReferences = []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "initdb", UID: "initdb-job", Controller: new(true)}}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(addressless, unready, unlisted, bootstrap).Build()
	names, pods, err := retainedInstancePods(t.Context(), reader, cluster)
	require.NoError(t, err)
	assert.Equal(t, []string{unlisted.Name, unready.Name}, names, "reachable unready or terminating nodes must still be inspected, including those omitted by status")
	assert.Equal(t, unready.UID, pods[unready.Name].UID)
	assert.Equal(t, unready.Status.PodIP, pods[unready.Name].Status.PodIP)
	_, _, err = healthyInstancePods(t.Context(), reader, cluster)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowPending)

	unready.OwnerReferences[0].UID = "foreign"
	require.NoError(t, reader.Update(t.Context(), unready))
	_, _, err = retainedInstancePods(t.Context(), reader, cluster)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed, "retained proof cannot authorize a foreign pod")
}

func TestRetainedInstanceDiscoveryRejectsUnlistedRetaggedOwnedPod(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cluster := &cnpgv1.Cluster{Name: "blue", Namespace: "default", UID: "blue-uid"}
	pod := observedInstanceTestPod(cluster, "blue-unlisted", "10.42.0.17")
	pod.Labels = nil
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	_, _, err := retainedInstancePods(t.Context(), reader, cluster)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed, "a label selector must not hide an owned-pod identity contradiction")
}

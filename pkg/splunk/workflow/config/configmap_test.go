// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
)

// --- helpers -----------------------------------------------------------------

func scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	return s
}

func fakeClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(scheme()).WithObjects(objs...).Build()
}

// podLabels returns the label set carried by a managed StatefulSet's pods.
func podLabels() map[string]string {
	return map[string]string{"app.kubernetes.io/instance": "splunk-my-indexer-indexer"}
}

// podSelector returns a StatefulSet-style pod label selector, mirroring what the
// reconciler passes from statefulSet.Spec.Selector.
func podSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: podLabels()}
}

// makePod builds a pod carrying the given selector labels that mounts the named
// ConfigMap and Secret as volumes, used to exercise the still-mounted GC guard.
func makePod(ns, name string, labels map[string]string, configMapName, secretName string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels:    labels,
		},
	}
	if configMapName != "" {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "defaults-cm",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
				},
			},
		})
	}
	if secretName != "" {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "defaults-secret",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: secretName},
			},
		})
	}
	return pod
}

func someEntries() []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "outputs",
			Value: common.ConfFileValue{
				Directory: "/opt/splunk/etc/apps/100-sok/local",
				Stanzas: common.ConfFileStanzas{
					"remote_queue:q": {"remote_queue.type": "sqs_smartbus"},
				},
			},
		},
	}
}

func differentEntries() []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "inputs",
			Value: common.ConfFileValue{
				Directory: "/opt/splunk/etc/apps/100-sok/local",
				Stanzas: common.ConfFileStanzas{
					"remote_queue:q": {"remote_queue.type": "sqs_smartbus"},
				},
			},
		},
	}
}

func desiredName(t *testing.T, crKind, crName string, entries []common.ConfFileEntry) string {
	t.Helper()
	name, err := resources.DefaultsConfigMapName(crKind, crName, entries)
	require.NoError(t, err)
	return name
}

// fakeCR returns a minimal client.Object with the given namespace, kind, and name.
// It mirrors what the reconciler provides after assigning cr.Kind = "...".
func fakeCR(namespace, kind, name string) client.Object {
	cm := &corev1.ConfigMap{}
	cm.Namespace = namespace
	cm.Name = name
	cm.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(kind))
	return cm
}

// --- EnsureConfigMap ---------------------------------------------------------

func TestEnsureConfigMap_CreatesOnFirstCall(t *testing.T) {
	c := fakeClient()
	ctx := context.Background()

	ref, err := configworkflow.EnsureConfigMap(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), someEntries(), nil)
	require.NoError(t, err)
	assert.NotEmpty(t, ref.Name)

	var cm corev1.ConfigMap
	err = c.Get(ctx, client.ObjectKey{Namespace: "ns", Name: ref.Name}, &cm)
	require.NoError(t, err, "ConfigMap must exist after EnsureConfigMap")
}

func TestEnsureConfigMap_SecondCallWithSameEntriesIsNoop(t *testing.T) {
	c := fakeClient()
	ctx := context.Background()
	entries := someEntries()

	ref1, err := configworkflow.EnsureConfigMap(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), entries, nil)
	require.NoError(t, err)

	ref2, err := configworkflow.EnsureConfigMap(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), entries, nil)
	require.NoError(t, err)

	assert.Equal(t, ref1.Name, ref2.Name, "same entries must return the same name")

	var cmList corev1.ConfigMapList
	require.NoError(t, c.List(ctx, &cmList, client.InNamespace("ns")))
	assert.Len(t, cmList.Items, 1, "must not create a second ConfigMap")
}

func TestEnsureConfigMap_ChangedEntriesReturnNewName(t *testing.T) {
	c := fakeClient()
	ctx := context.Background()

	ref1, err := configworkflow.EnsureConfigMap(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), someEntries(), nil)
	require.NoError(t, err)

	ref2, err := configworkflow.EnsureConfigMap(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), differentEntries(), nil)
	require.NoError(t, err)

	assert.NotEqual(t, ref1.Name, ref2.Name, "changed entries must produce a different name")

	var cmList corev1.ConfigMapList
	require.NoError(t, c.List(ctx, &cmList, client.InNamespace("ns")))
	assert.Len(t, cmList.Items, 2, "both ConfigMaps must exist until GC runs")
}

func TestEnsureConfigMap_ContentMismatchReturnsError(t *testing.T) {
	ctx := context.Background()
	entries := someEntries()

	// Build the name that EnsureConfigMap would compute.
	name := desiredName(t, "IndexerCluster", "my-indexer", entries)

	// Pre-create a ConfigMap with the same name but different content (simulates a collision).
	impostor := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name},
		Data:       map[string]string{"conf-defaults.yml": "wrong content"},
	}
	c := fakeClient(impostor)

	_, err := configworkflow.EnsureConfigMap(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), entries, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content mismatch")
	assert.True(t, errors.Is(err, reconcile.TerminalError(nil)), "collision error must be terminal")
}

func TestEnsureConfigMap_ConfigMapIsImmutable(t *testing.T) {
	c := fakeClient()
	ctx := context.Background()

	ref, err := configworkflow.EnsureConfigMap(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), someEntries(), nil)
	require.NoError(t, err)

	var cm corev1.ConfigMap
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "ns", Name: ref.Name}, &cm))
	require.NotNil(t, cm.Immutable)
	assert.True(t, *cm.Immutable)
}

func TestEnsureConfigMap_LabelsCarryCRIdentity(t *testing.T) {
	c := fakeClient()
	ctx := context.Background()

	ref, err := configworkflow.EnsureConfigMap(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), someEntries(), nil)
	require.NoError(t, err)

	var cm corev1.ConfigMap
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "ns", Name: ref.Name}, &cm))
	assert.Equal(t, "my-indexer", cm.Labels[resources.LabelCRName])
	assert.Equal(t, "IndexerCluster", cm.Labels[resources.LabelCRKind])
}

// --- GarbageCollectConfigMaps ------------------------------------------------

func makeStaleConfigMap(ns, name, crKind, crName string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Labels: map[string]string{
				resources.LabelCRName: crName,
				resources.LabelCRKind: crKind,
			},
		},
	}
}

func TestGarbageCollectConfigMaps_DeletesStaleOnes(t *testing.T) {
	ctx := context.Background()
	stale := makeStaleConfigMap("ns", "sok-indexercluster-defaults-aabbcc", "IndexerCluster", "my-indexer")
	current := makeStaleConfigMap("ns", "sok-indexercluster-defaults-112233", "IndexerCluster", "my-indexer")
	c := fakeClient(stale, current)

	configworkflow.GarbageCollectConfigMaps(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.ConfigMapList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	require.Len(t, remaining.Items, 1)
	assert.Equal(t, current.Name, remaining.Items[0].Name)
}

func TestGarbageCollectConfigMaps_DeletesMultipleStale(t *testing.T) {
	ctx := context.Background()
	stale1 := makeStaleConfigMap("ns", "sok-indexercluster-defaults-aaaaaa", "IndexerCluster", "my-indexer")
	stale2 := makeStaleConfigMap("ns", "sok-indexercluster-defaults-bbbbbb", "IndexerCluster", "my-indexer")
	current := makeStaleConfigMap("ns", "sok-indexercluster-defaults-cccccc", "IndexerCluster", "my-indexer")
	c := fakeClient(stale1, stale2, current)

	configworkflow.GarbageCollectConfigMaps(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.ConfigMapList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	require.Len(t, remaining.Items, 1)
	assert.Equal(t, current.Name, remaining.Items[0].Name)
}

func TestGarbageCollectConfigMaps_DoesNotTouchOtherCRs(t *testing.T) {
	ctx := context.Background()
	mine := makeStaleConfigMap("ns", "sok-indexercluster-defaults-aabbcc", "IndexerCluster", "my-indexer")
	other := makeStaleConfigMap("ns", "sok-indexercluster-defaults-ddeeff", "IndexerCluster", "other-indexer")
	current := makeStaleConfigMap("ns", "sok-indexercluster-defaults-112233", "IndexerCluster", "my-indexer")
	c := fakeClient(mine, other, current)

	configworkflow.GarbageCollectConfigMaps(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.ConfigMapList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	assert.Len(t, remaining.Items, 2, "other CR's ConfigMap must not be deleted")

	names := make([]string, len(remaining.Items))
	for i, cm := range remaining.Items {
		names[i] = cm.Name
	}
	assert.Contains(t, names, current.Name)
	assert.Contains(t, names, other.Name)
}

func TestGarbageCollectConfigMaps_DoesNotTouchOtherKinds(t *testing.T) {
	ctx := context.Background()
	stale := makeStaleConfigMap("ns", "sok-indexercluster-defaults-aabbcc", "IndexerCluster", "my-indexer")
	// same cr-name annotation, but different cr-kind (e.g. IngestorCluster happens to share the name)
	different := makeStaleConfigMap("ns", "sok-ingestorcluster-defaults-aabbcc", "IngestorCluster", "my-indexer")
	current := makeStaleConfigMap("ns", "sok-indexercluster-defaults-112233", "IndexerCluster", "my-indexer")
	c := fakeClient(stale, different, current)

	configworkflow.GarbageCollectConfigMaps(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.ConfigMapList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	assert.Len(t, remaining.Items, 2)

	names := make([]string, len(remaining.Items))
	for i, cm := range remaining.Items {
		names[i] = cm.Name
	}
	assert.Contains(t, names, current.Name)
	assert.Contains(t, names, different.Name)
}

func TestGarbageCollectConfigMaps_NoopWhenNothingStale(t *testing.T) {
	ctx := context.Background()
	current := makeStaleConfigMap("ns", "sok-indexercluster-defaults-112233", "IndexerCluster", "my-indexer")
	c := fakeClient(current)

	configworkflow.GarbageCollectConfigMaps(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.ConfigMapList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	assert.Len(t, remaining.Items, 1)
}

func TestGarbageCollectConfigMaps_KeepsStaleStillMountedByPod(t *testing.T) {
	ctx := context.Background()
	stale := makeStaleConfigMap("ns", "sok-indexercluster-defaults-aabbcc", "IndexerCluster", "my-indexer")
	current := makeStaleConfigMap("ns", "sok-indexercluster-defaults-112233", "IndexerCluster", "my-indexer")
	// A pod matching the selector still mounts the stale ConfigMap (e.g. mid-roll).
	pod := makePod("ns", "splunk-my-indexer-indexer-0", podLabels(), stale.Name, "")
	c := fakeClient(stale, current, pod)

	configworkflow.GarbageCollectConfigMaps(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.ConfigMapList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	assert.Len(t, remaining.Items, 2, "stale ConfigMap still mounted by a pod must not be deleted")
}

func TestGarbageCollectConfigMaps_BroadSelectorBehavior(t *testing.T) {
	// nil and empty selectors both list all pods; the stale ConfigMap is protected
	// when a pod mounts it, and deleted when none do.
	tests := []struct {
		name          string
		selector      *metav1.LabelSelector
		addPod        bool
		wantRemaining int
	}{
		{"nil selector keeps mounted", nil, true, 2},
		{"nil selector deletes unmounted", nil, false, 1},
		{"empty selector keeps mounted", &metav1.LabelSelector{}, true, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			stale := makeStaleConfigMap("ns", "sok-indexercluster-defaults-aabbcc", "IndexerCluster", "my-indexer")
			current := makeStaleConfigMap("ns", "sok-indexercluster-defaults-112233", "IndexerCluster", "my-indexer")
			objs := []client.Object{stale, current}
			if tc.addPod {
				objs = append(objs, makePod("ns", "splunk-my-indexer-indexer-0", podLabels(), stale.Name, ""))
			}
			c := fakeClient(objs...)

			configworkflow.GarbageCollectConfigMaps(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, tc.selector)

			var remaining corev1.ConfigMapList
			require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
			assert.Len(t, remaining.Items, tc.wantRemaining)
			if tc.wantRemaining == 1 {
				assert.Equal(t, current.Name, remaining.Items[0].Name)
			}
		})
	}
}

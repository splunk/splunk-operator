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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
)

// --- helpers -----------------------------------------------------------------

func someCredEntries() []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "outputs",
			Value: common.ConfFileValue{
				Directory: "/opt/splunk/etc/apps/101-sok-secrets/local",
				Stanzas: common.ConfFileStanzas{
					"remote_queue:q": {
						"remote_queue.sqs_smartbus.access_key": "AKIA",
						"remote_queue.sqs_smartbus.secret_key": "shhh",
					},
				},
			},
		},
	}
}

func differentCredEntries() []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "outputs",
			Value: common.ConfFileValue{
				Directory: "/opt/splunk/etc/apps/101-sok-secrets/local",
				Stanzas: common.ConfFileStanzas{
					"remote_queue:q": {
						"remote_queue.sqs_smartbus.access_key": "AKIA",
						"remote_queue.sqs_smartbus.secret_key": "rotated",
					},
				},
			},
		},
	}
}

func desiredSecretName(t *testing.T, crKind, crName string, entries []common.ConfFileEntry) string {
	t.Helper()
	name, err := resources.DefaultsSecretName(crKind, crName, entries)
	require.NoError(t, err)
	return name
}

// --- EnsureSecret ------------------------------------------------------------

func TestEnsureSecret_CreatesOnFirstCall(t *testing.T) {
	c := fakeClient()
	ctx := context.Background()

	ref, err := configworkflow.EnsureSecret(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), someCredEntries(), nil)
	require.NoError(t, err)
	assert.NotEmpty(t, ref.Name)

	var s corev1.Secret
	err = c.Get(ctx, client.ObjectKey{Namespace: "ns", Name: ref.Name}, &s)
	require.NoError(t, err, "Secret must exist after EnsureSecret")
}

func TestEnsureSecret_SecondCallWithSameEntriesIsNoop(t *testing.T) {
	c := fakeClient()
	ctx := context.Background()
	entries := someCredEntries()

	ref1, err := configworkflow.EnsureSecret(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), entries, nil)
	require.NoError(t, err)

	ref2, err := configworkflow.EnsureSecret(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), entries, nil)
	require.NoError(t, err)

	assert.Equal(t, ref1.Name, ref2.Name, "same entries must return the same name")

	var sList corev1.SecretList
	require.NoError(t, c.List(ctx, &sList, client.InNamespace("ns")))
	assert.Len(t, sList.Items, 1, "must not create a second Secret")
}

func TestEnsureSecret_ChangedEntriesReturnNewName(t *testing.T) {
	c := fakeClient()
	ctx := context.Background()

	ref1, err := configworkflow.EnsureSecret(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), someCredEntries(), nil)
	require.NoError(t, err)

	ref2, err := configworkflow.EnsureSecret(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), differentCredEntries(), nil)
	require.NoError(t, err)

	assert.NotEqual(t, ref1.Name, ref2.Name, "changed entries must produce a different name")

	var sList corev1.SecretList
	require.NoError(t, c.List(ctx, &sList, client.InNamespace("ns")))
	assert.Len(t, sList.Items, 2, "both Secrets must exist until GC runs")
}

func TestEnsureSecret_ContentMismatchReturnsError(t *testing.T) {
	ctx := context.Background()
	entries := someCredEntries()

	// Build the name that EnsureSecret would compute.
	name := desiredSecretName(t, "IndexerCluster", "my-indexer", entries)

	// Pre-create a Secret with the same name but different content (simulates a collision).
	impostor := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name},
		Data:       map[string][]byte{"conf-defaults.yml": []byte("wrong content")},
	}
	c := fakeClient(impostor)

	_, err := configworkflow.EnsureSecret(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), entries, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content mismatch")
	assert.True(t, errors.Is(err, reconcile.TerminalError(nil)), "collision error must be terminal")
}

func TestEnsureSecret_SecretIsImmutable(t *testing.T) {
	c := fakeClient()
	ctx := context.Background()

	ref, err := configworkflow.EnsureSecret(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), someCredEntries(), nil)
	require.NoError(t, err)

	var s corev1.Secret
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "ns", Name: ref.Name}, &s))
	require.NotNil(t, s.Immutable)
	assert.True(t, *s.Immutable)
}

func TestEnsureSecret_LabelsCarryCRIdentity(t *testing.T) {
	c := fakeClient()
	ctx := context.Background()

	ref, err := configworkflow.EnsureSecret(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), someCredEntries(), nil)
	require.NoError(t, err)

	var s corev1.Secret
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "ns", Name: ref.Name}, &s))
	assert.Equal(t, "my-indexer", s.Labels[resources.LabelCRName])
	assert.Equal(t, "IndexerCluster", s.Labels[resources.LabelCRKind])
}

// --- GarbageCollectSecrets ---------------------------------------------------

func makeStaleSecret(ns, name, crKind, crName string) *corev1.Secret {
	return &corev1.Secret{
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

func TestGarbageCollectSecrets_DeletesStaleOnes(t *testing.T) {
	ctx := context.Background()
	stale := makeStaleSecret("ns", "sok-indexercluster-creds-aabbcc", "IndexerCluster", "my-indexer")
	current := makeStaleSecret("ns", "sok-indexercluster-creds-112233", "IndexerCluster", "my-indexer")
	c := fakeClient(stale, current)

	configworkflow.GarbageCollectSecrets(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.SecretList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	require.Len(t, remaining.Items, 1)
	assert.Equal(t, current.Name, remaining.Items[0].Name)
}

func TestGarbageCollectSecrets_DeletesMultipleStale(t *testing.T) {
	ctx := context.Background()
	stale1 := makeStaleSecret("ns", "sok-indexercluster-creds-aaaaaa", "IndexerCluster", "my-indexer")
	stale2 := makeStaleSecret("ns", "sok-indexercluster-creds-bbbbbb", "IndexerCluster", "my-indexer")
	current := makeStaleSecret("ns", "sok-indexercluster-creds-cccccc", "IndexerCluster", "my-indexer")
	c := fakeClient(stale1, stale2, current)

	configworkflow.GarbageCollectSecrets(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.SecretList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	require.Len(t, remaining.Items, 1)
	assert.Equal(t, current.Name, remaining.Items[0].Name)
}

func TestGarbageCollectSecrets_DoesNotTouchOtherCRs(t *testing.T) {
	ctx := context.Background()
	mine := makeStaleSecret("ns", "sok-indexercluster-creds-aabbcc", "IndexerCluster", "my-indexer")
	other := makeStaleSecret("ns", "sok-indexercluster-creds-ddeeff", "IndexerCluster", "other-indexer")
	current := makeStaleSecret("ns", "sok-indexercluster-creds-112233", "IndexerCluster", "my-indexer")
	c := fakeClient(mine, other, current)

	configworkflow.GarbageCollectSecrets(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.SecretList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	assert.Len(t, remaining.Items, 2, "other CR's Secret must not be deleted")

	names := make([]string, len(remaining.Items))
	for i, s := range remaining.Items {
		names[i] = s.Name
	}
	assert.Contains(t, names, current.Name)
	assert.Contains(t, names, other.Name)
}

func TestGarbageCollectSecrets_DoesNotTouchOtherKinds(t *testing.T) {
	ctx := context.Background()
	stale := makeStaleSecret("ns", "sok-indexercluster-creds-aabbcc", "IndexerCluster", "my-indexer")
	// same cr-name annotation, but different cr-kind
	different := makeStaleSecret("ns", "sok-ingestorcluster-creds-aabbcc", "IngestorCluster", "my-indexer")
	current := makeStaleSecret("ns", "sok-indexercluster-creds-112233", "IndexerCluster", "my-indexer")
	c := fakeClient(stale, different, current)

	configworkflow.GarbageCollectSecrets(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.SecretList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	assert.Len(t, remaining.Items, 2)

	names := make([]string, len(remaining.Items))
	for i, s := range remaining.Items {
		names[i] = s.Name
	}
	assert.Contains(t, names, current.Name)
	assert.Contains(t, names, different.Name)
}

func TestGarbageCollectSecrets_NoopWhenNothingStale(t *testing.T) {
	ctx := context.Background()
	current := makeStaleSecret("ns", "sok-indexercluster-creds-112233", "IndexerCluster", "my-indexer")
	c := fakeClient(current)

	configworkflow.GarbageCollectSecrets(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.SecretList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	assert.Len(t, remaining.Items, 1)
}

func TestGarbageCollectSecrets_KeepsStaleStillMountedByPod(t *testing.T) {
	ctx := context.Background()
	stale := makeStaleSecret("ns", "sok-indexercluster-creds-aabbcc", "IndexerCluster", "my-indexer")
	current := makeStaleSecret("ns", "sok-indexercluster-creds-112233", "IndexerCluster", "my-indexer")
	// A pod matching the selector still mounts the stale Secret (e.g. mid-roll).
	pod := makePod("ns", "splunk-my-indexer-indexer-0", podLabels(), "", stale.Name)
	c := fakeClient(stale, current, pod)

	configworkflow.GarbageCollectSecrets(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, podSelector())

	var remaining corev1.SecretList
	require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
	assert.Len(t, remaining.Items, 2, "stale Secret still mounted by a pod must not be deleted")
}

func TestGarbageCollectSecrets_BroadSelectorBehavior(t *testing.T) {
	// nil and empty selectors both list all pods; the stale Secret is protected
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
			stale := makeStaleSecret("ns", "sok-indexercluster-creds-aabbcc", "IndexerCluster", "my-indexer")
			current := makeStaleSecret("ns", "sok-indexercluster-creds-112233", "IndexerCluster", "my-indexer")
			objs := []client.Object{stale, current}
			if tc.addPod {
				objs = append(objs, makePod("ns", "splunk-my-indexer-indexer-0", podLabels(), "", stale.Name))
			}
			c := fakeClient(objs...)

			configworkflow.GarbageCollectSecrets(ctx, c, fakeCR("ns", "IndexerCluster", "my-indexer"), current.Name, tc.selector)

			var remaining corev1.SecretList
			require.NoError(t, c.List(ctx, &remaining, client.InNamespace("ns")))
			assert.Len(t, remaining.Items, tc.wantRemaining)
			if tc.wantRemaining == 1 {
				assert.Equal(t, current.Name, remaining.Items[0].Name)
			}
		})
	}
}

// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type noahIndexerScaleOutTestOptions struct {
	peerStatus       noah.PeerStatus
	peerStart        int64
	cacheWarmEnabled *bool
	timeoutSeconds   *int32
}

type noahIndexerScaleOutTestFixture struct {
	client         *spltest.MockClient
	cr             *enterpriseApi.IndexerCluster
	statefulSet    *appsv1.StatefulSet
	requestHeaders http.Header
}

func newNoahIndexerScaleOutTestFixture(t *testing.T, options noahIndexerScaleOutTestOptions) *noahIndexerScaleOutTestFixture {
	t.Helper()
	if options.peerStart == 0 {
		options.peerStart = time.Now().Unix()
	}

	fixture := &noahIndexerScaleOutTestFixture{client: spltest.NewMockClient()}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fixture.requestHeaders = request.Header.Clone()
		if err := json.NewEncoder(response).Encode([]noah.Peer{{
			ID:     "splunk-main-indexer-0.splunk-main-indexer-headless.test.svc.corp.example",
			Status: options.peerStatus,
			Data:   noah.PeerData{StartTime: options.peerStart},
		}}); err != nil {
			t.Errorf("encode Noah peers: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	fixture.cr = &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "test", Generation: 7},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       3,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
		},
	}
	require.NoError(t, fixture.client.Create(t.Context(), &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: fixture.cr.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:                        server.URL,
			Tenant:                          "tenant",
			AuthSecretRef:                   corev1.LocalObjectReference{Name: "noah-auth"},
			CacheWarmScaleOutEnabled:        options.cacheWarmEnabled,
			CacheWarmScaleOutTimeoutSeconds: options.timeoutSeconds,
		},
	}))
	require.NoError(t, fixture.client.Create(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: fixture.cr.Namespace},
		Data:       map[string][]byte{noahAuthSecretKey: []byte("unit-test-noah-key")},
	}))

	appliedReplicas := int32(1)
	fixture.statefulSet = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-main-indexer", Namespace: fixture.cr.Namespace},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &appliedReplicas,
			ServiceName: "splunk-main-indexer-headless",
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "splunk",
				Env:  []corev1.EnvVar{{Name: resources.ClusterDomainEnvName, Value: "corp.example"}},
			}}}},
		},
	}
	require.NoError(t, fixture.client.Create(t.Context(), fixture.statefulSet))
	require.NoError(t, fixture.client.Create(t.Context(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-main-indexer-0", Namespace: fixture.cr.Namespace},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "splunk",
			Ready: true,
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{
				StartedAt: metav1.NewTime(time.Unix(options.peerStart-1, 0)),
			}},
		}}},
	}))
	return fixture
}

func (fixture *noahIndexerScaleOutTestFixture) podManager() *noahIndexerPodManager {
	mgr := newNoahIndexerPodManager(fixture.client, fixture.cr)
	mgr.statefulSet = fixture.statefulSet
	return mgr
}

func TestApplyNoahIndexerResourcesCreatesIdentityAwareStatefulSet(t *testing.T) {
	t.Setenv(resources.ClusterDomainEnvName, "corp.example")

	ctx := t.Context()
	client := spltest.NewMockClient()
	cr := &enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v4",
			Kind:       "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "main",
			Namespace: "test",
			UID:       types.UID("indexer-cluster-uid"),
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       3,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	require.NoError(t, client.Create(ctx, &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: cr.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint: "https://noah.test.svc:8080",
			Tenant:   "axolotl",
			AuthSecretRef: corev1.LocalObjectReference{
				Name: "noah-auth",
			},
		},
	}))
	require.NoError(t, client.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: cr.Namespace},
		Data:       map[string][]byte{noahAuthSecretKey: []byte("unit-test-noah-key")},
	}))

	statefulSet, phase, err := applyNoahIndexerResources(ctx, client, cr, newNoahIndexerPodManager(client, cr))
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhasePending, phase)

	created := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{
		Name:      statefulSet.Name,
		Namespace: statefulSet.Namespace,
	}, created))
	require.NotNil(t, created.Spec.Replicas)
	assert.Equal(t, int32(3), *created.Spec.Replicas, "initial creation must start every requested replica")
	for key, value := range created.Spec.Selector.MatchLabels {
		assert.Equal(t, value, created.Spec.Template.Labels[key])
	}
	for _, headless := range []bool{true, false} {
		service := &corev1.Service{}
		require.NoError(t, client.Get(ctx, types.NamespacedName{
			Name:      splcommon.GetSplunkServiceName(SplunkIndexer, cr.Name, headless),
			Namespace: cr.Namespace,
		}, service))
		assert.Equal(t, created.Spec.Selector.MatchLabels, service.Spec.Selector)
		assert.Equal(t, headless, service.Spec.PublishNotReadyAddresses)
		if headless {
			assert.Equal(t, created.Spec.ServiceName, service.Name)
		}
	}

	var defaultsConfigMapName string
	for _, volume := range created.Spec.Template.Spec.Volumes {
		if volume.ConfigMap != nil && strings.HasPrefix(volume.ConfigMap.Name, "sok-indexercluster-defaults-") {
			defaultsConfigMapName = volume.ConfigMap.Name
			break
		}
	}
	require.NotEmpty(t, defaultsConfigMapName)
	defaultsConfigMap := &corev1.ConfigMap{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{
		Name:      defaultsConfigMapName,
		Namespace: created.Namespace,
	}, defaultsConfigMap))
	assert.Contains(t, defaultsConfigMap.Data["conf-defaults.yml"], "https://noah.test.svc:8080")
	assert.Contains(t, defaultsConfigMap.Data["conf-defaults.yml"], "tenant: axolotl")

	env := make(map[string]corev1.EnvVar)
	for _, item := range created.Spec.Template.Spec.Containers[0].Env {
		env[item.Name] = item
	}
	assert.Equal(t, "true", env[resources.NoahEnabledEnvName].Value)
	assert.Equal(t, created.Spec.ServiceName, env[resources.NoahHeadlessServiceEnvName].Value)
	assert.Equal(t, "corp.example", env[resources.ClusterDomainEnvName].Value)
	require.NotNil(t, env[resources.PodNameEnvName].ValueFrom)
	require.NotNil(t, env[resources.PodNamespaceEnvName].ValueFrom)

	var initEtc *corev1.Container
	for i := range created.Spec.Template.Spec.InitContainers {
		if created.Spec.Template.Spec.InitContainers[i].Name == "init-etc" {
			initEtc = &created.Spec.Template.Spec.InitContainers[i]
			break
		}
	}
	require.NotNil(t, initEtc)
	require.Len(t, initEtc.Command, 3)
	assert.Contains(t, initEtc.Command[2], `print "[noahService]"`)
	assert.Contains(t, initEtc.Command[2], `print "disabled = true"`)
	assert.Contains(t, initEtc.Command[2], noahAuthMountPath+"/"+noahAuthSecretKey)
	assert.NotContains(t, initEtc.Command[2], "unit-test-noah-key")

	var authVolume *corev1.Volume
	for i := range created.Spec.Template.Spec.Volumes {
		if created.Spec.Template.Spec.Volumes[i].Name == noahAuthVolumeName {
			authVolume = &created.Spec.Template.Spec.Volumes[i]
			break
		}
	}
	require.NotNil(t, authVolume)
	require.NotNil(t, authVolume.Secret)
	assert.Equal(t, "noah-auth", authVolume.Secret.SecretName)
	assert.Contains(t, initEtc.VolumeMounts, corev1.VolumeMount{
		Name:      noahAuthVolumeName,
		MountPath: noahAuthMountPath,
		ReadOnly:  true,
	})
	for _, mount := range created.Spec.Template.Spec.Containers[0].VolumeMounts {
		assert.NotEqual(t, noahAuthVolumeName, mount.Name, "the main container must not mount the plaintext Noah credential")
	}
}

func TestApplyNoahIndexerResourcesRequiresReferencedNoahCluster(t *testing.T) {
	client := spltest.NewMockClient()
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			NoahClusterRef: &corev1.LocalObjectReference{Name: "missing"},
		},
	}

	statefulSet, phase, err := applyNoahIndexerResources(t.Context(), client, cr, newNoahIndexerPodManager(client, cr))
	require.Error(t, err)
	assert.Nil(t, statefulSet)
	assert.Equal(t, enterpriseApi.PhaseError, phase)
	assert.Contains(t, err.Error(), "get referenced NoahCluster test/missing")
}

func TestNoahIndexerPodManagerBlocksUnimplementedLifecycleOperations(t *testing.T) {
	mgr := newNoahIndexerPodManager(spltest.NewMockClient(), &enterpriseApi.IndexerCluster{})
	tests := []struct {
		name      string
		operation func(context.Context, int32) (bool, error)
		wantError string
	}{
		{name: "scale down", operation: mgr.PrepareScaleDown, wantError: "scale-down is not implemented"},
		{name: "prepare recycle", operation: mgr.PrepareRecycle, wantError: "rollout is not implemented"},
		{name: "finish recycle", operation: mgr.FinishRecycle, wantError: "rollout is not implemented"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ready, err := test.operation(t.Context(), 0)
			assert.False(t, ready)
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestResolveNoahAuthSecretRejectsInvalidSecret(t *testing.T) {
	ctx := t.Context()
	client := spltest.NewMockClient()
	require.NoError(t, client.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: "test"},
		Data:       map[string][]byte{noahAuthSecretKey: []byte("valid-value\nsecond-line")},
	}))

	secret, err := resolveNoahAuthSecret(ctx, client, "test", corev1.LocalObjectReference{Name: "noah-auth"})
	require.ErrorContains(t, err, "must be a single line")
	assert.Nil(t, secret)
}

func TestNoahInitEtcUsesRenderedEtcVolume(t *testing.T) {
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "splunk",
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "rendered-etc",
							MountPath: "/opt/splunk/etc",
						}},
					}},
				},
			},
		},
	}

	noahInitEtcOption(&enterpriseApi.CommonSplunkSpec{})(statefulSet)
	require.Len(t, statefulSet.Spec.Template.Spec.InitContainers, 1)
	initEtc := statefulSet.Spec.Template.Spec.InitContainers[0]
	assert.Equal(t, "rendered-etc", initEtc.VolumeMounts[0].Name)
	require.NotNil(t, initEtc.SecurityContext)
	require.NotNil(t, initEtc.SecurityContext.AllowPrivilegeEscalation)
	assert.False(t, *initEtc.SecurityContext.AllowPrivilegeEscalation)
	require.NotNil(t, initEtc.SecurityContext.Capabilities)
	assert.Equal(t, []corev1.Capability{"ALL"}, initEtc.SecurityContext.Capabilities.Drop)
	require.NotNil(t, initEtc.SecurityContext.SeccompProfile)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, initEtc.SecurityContext.SeccompProfile.Type)
}

func TestNoahIndexerStatefulSetOptionsAppliesStableIdentityLast(t *testing.T) {
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "splunk-main-indexer-headless",
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "splunk",
					}},
				},
			},
		},
	}

	callerOption := func(statefulSet *appsv1.StatefulSet) {
		statefulSet.Spec.ServiceName = "caller-selected-headless"
		statefulSet.Spec.Template.Spec.Containers[0].Env = append(
			statefulSet.Spec.Template.Spec.Containers[0].Env,
			corev1.EnvVar{Name: resources.NoahEnabledEnvName, Value: "false"},
		)
	}

	options := noahIndexerStatefulSetOptions("corp.example", callerOption)
	resources.ApplyStatefulSetOptions(statefulSet, options...)

	env := make(map[string]corev1.EnvVar)
	for _, item := range statefulSet.Spec.Template.Spec.Containers[0].Env {
		env[item.Name] = item
	}

	assert.Equal(t, "true", env[resources.NoahEnabledEnvName].Value)
	assert.Equal(t, "caller-selected-headless", env[resources.NoahHeadlessServiceEnvName].Value)
	assert.Equal(t, "corp.example", env[resources.ClusterDomainEnvName].Value)

	for envName, fieldPath := range map[string]string{
		resources.PodNameEnvName:      "metadata.name",
		resources.PodNamespaceEnvName: "metadata.namespace",
	} {
		fieldRef := env[envName].ValueFrom
		require.NotNil(t, fieldRef)
		require.NotNil(t, fieldRef.FieldRef)
		assert.Equal(t, fieldPath, fieldRef.FieldRef.FieldPath)
	}
}

func TestNoahIndexerStatefulSetConverged(t *testing.T) {
	const desiredReplicas int32 = 3

	converged := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 2,
			CurrentRevision:    "revision-2",
			UpdateRevision:     "revision-2",
			UpdatedReplicas:    desiredReplicas,
			ReadyReplicas:      desiredReplicas,
		},
	}

	tests := []struct {
		name        string
		statefulSet *appsv1.StatefulSet
		want        bool
	}{
		{
			name:        "latest revision is fully ready",
			statefulSet: converged,
			want:        true,
		},
		{
			name:        "missing StatefulSet",
			statefulSet: nil,
		},
		{
			name: "latest generation is not observed",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.ObservedGeneration--
				return statefulSet
			}(),
		},
		{
			name: "update revision is not available",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.UpdateRevision = ""
				return statefulSet
			}(),
		},
		{
			name: "pods are on the previous revision",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.CurrentRevision = "revision-1"
				return statefulSet
			}(),
		},
		{
			name: "not all replicas are updated",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.UpdatedReplicas--
				return statefulSet
			}(),
		},
		{
			name: "updated replicas are not all ready",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.ReadyReplicas--
				return statefulSet
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, noahIndexerStatefulSetConverged(tt.statefulSet, desiredReplicas))
		})
	}
}

func TestNoahIndexerWorkloadPhase(t *testing.T) {
	const replicas int32 = 2
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 2,
			CurrentRevision:    "revision-2",
			UpdateRevision:     "revision-2",
			UpdatedReplicas:    replicas,
			ReadyReplicas:      replicas - 1,
		},
	}

	assert.Equal(t, enterpriseApi.PhasePending, noahIndexerWorkloadPhase(enterpriseApi.PhaseReady, statefulSet, replicas),
		"same-revision replica readiness is not a template update")

	statefulSet.Status.CurrentRevision = "revision-1"
	assert.Equal(t, enterpriseApi.PhaseUpdating, noahIndexerWorkloadPhase(enterpriseApi.PhaseReady, statefulSet, replicas),
		"a pending template revision must remain Updating")

	statefulSet.Status.CurrentRevision = statefulSet.Status.UpdateRevision
	statefulSet.Status.ReadyReplicas = replicas
	assert.Equal(t, enterpriseApi.PhaseReady, noahIndexerWorkloadPhase(enterpriseApi.PhaseReady, statefulSet, replicas))
}

func TestExpectedNoahIndexerPeersReady(t *testing.T) {
	const currentStart int64 = 1_700_000_000
	peer0 := "splunk-main-indexer-0.splunk-main-indexer-headless.test.svc.corp.example"
	peer1 := "splunk-main-indexer-1.splunk-main-indexer-headless.test.svc.corp.example"
	expectedPeers := map[string]int64{
		peer0: currentStart,
		peer1: currentStart,
	}
	currentPeer := func(id string, status noah.PeerStatus) noah.Peer {
		return noah.Peer{ID: id, Status: status, Data: noah.PeerData{StartTime: currentStart}}
	}

	tests := []struct {
		name  string
		peers []noah.Peer
		want  bool
	}{
		{
			name: "all expected peers are up",
			peers: []noah.Peer{
				currentPeer(peer0, noah.PeerStatusUp),
				currentPeer(peer1, noah.PeerStatusUp),
			},
			want: true,
		},
		{
			name: "missing expected peer",
			peers: []noah.Peer{
				currentPeer(peer0, noah.PeerStatusUp),
			},
		},
		{
			name: "expected peer is down",
			peers: []noah.Peer{
				currentPeer(peer0, noah.PeerStatusUp),
				currentPeer(peer1, noah.PeerStatusDown),
			},
		},
		{
			name: "bare pod name does not satisfy exact advertised identity",
			peers: []noah.Peer{
				currentPeer(peer0, noah.PeerStatusUp),
				currentPeer("splunk-main-indexer-1", noah.PeerStatusUp),
			},
		},
		{
			name: "foreign cluster domain does not satisfy exact advertised identity",
			peers: []noah.Peer{
				currentPeer(peer0, noah.PeerStatusUp),
				currentPeer("splunk-main-indexer-1.splunk-main-indexer-headless.test.svc.foreign.example", noah.PeerStatusUp),
			},
		},
		{
			name: "unrelated peer does not prevent expected peers becoming ready",
			peers: []noah.Peer{
				currentPeer(peer0, noah.PeerStatusUp),
				currentPeer(peer1, noah.PeerStatusUp),
				currentPeer("splunk-other-indexer-4", noah.PeerStatusUp),
			},
			want: true,
		},
		{
			name: "duplicate active identity is not ready",
			peers: []noah.Peer{
				currentPeer(peer0, noah.PeerStatusUp),
				currentPeer(peer1, noah.PeerStatusUp),
				currentPeer(peer1, noah.PeerStatusWarming),
			},
		},
		{
			name: "stale up incarnation does not satisfy expected peer",
			peers: []noah.Peer{
				currentPeer(peer0, noah.PeerStatusUp),
				{ID: peer1, Status: noah.PeerStatusUp, Data: noah.PeerData{StartTime: currentStart - 1}},
			},
		},
		{
			name: "historical down incarnation is ignored",
			peers: []noah.Peer{
				currentPeer(peer0, noah.PeerStatusUp),
				currentPeer(peer1, noah.PeerStatusUp),
				{ID: peer1, Status: noah.PeerStatusDown, Data: noah.PeerData{StartTime: currentStart - 1}},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, expectedNoahIndexerPeersReady(test.peers, expectedPeers))
		})
	}
}

func TestExpectedNoahIndexerPeersRegistered(t *testing.T) {
	const currentStart int64 = 1_700_000_000
	const peerID = "splunk-main-indexer-0.splunk-main-indexer-headless.test.svc.corp.example"
	expectedPeers := map[string]int64{peerID: currentStart}
	currentPeer := func(status noah.PeerStatus) noah.Peer {
		return noah.Peer{ID: peerID, Status: status, Data: noah.PeerData{StartTime: currentStart}}
	}

	tests := []struct {
		name  string
		peers []noah.Peer
		want  bool
	}{
		{name: "started peer is registered", peers: []noah.Peer{currentPeer(noah.PeerStatusStarted)}, want: true},
		{name: "warming peer is registered", peers: []noah.Peer{currentPeer(noah.PeerStatusWarming)}, want: true},
		{name: "warmed peer is registered", peers: []noah.Peer{currentPeer(noah.PeerStatusWarmed)}, want: true},
		{name: "up peer is registered", peers: []noah.Peer{currentPeer(noah.PeerStatusUp)}, want: true},
		{name: "missing peer is not registered"},
		{name: "down peer is not registered", peers: []noah.Peer{currentPeer(noah.PeerStatusDown)}},
		{name: "decommissioning peer is not registered", peers: []noah.Peer{currentPeer(noah.PeerStatusDecommissioning)}},
		{
			name:  "stale incarnation is not registered",
			peers: []noah.Peer{{ID: peerID, Status: noah.PeerStatusUp, Data: noah.PeerData{StartTime: currentStart - 1}}},
		},
		{
			name:  "duplicate current incarnation is not registered",
			peers: []noah.Peer{currentPeer(noah.PeerStatusUp), currentPeer(noah.PeerStatusWarming)},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, expectedNoahIndexerPeersRegistered(test.peers, expectedPeers))
		})
	}
}

func TestNoahCacheWarmScaleOutPolicyDefaults(t *testing.T) {
	disabled := false
	noTimeout := int32(0)
	customTimeout := int32(90)

	tests := []struct {
		name        string
		spec        enterpriseApi.NoahClusterSpec
		wantEnabled bool
		wantTimeout time.Duration
	}{
		{name: "omitted values use defaults", wantEnabled: true, wantTimeout: time.Hour},
		{
			name: "explicit false and zero are preserved",
			spec: enterpriseApi.NoahClusterSpec{
				CacheWarmScaleOutEnabled:        &disabled,
				CacheWarmScaleOutTimeoutSeconds: &noTimeout,
			},
			wantTimeout: 0,
		},
		{
			name:        "custom timeout is preserved",
			spec:        enterpriseApi.NoahClusterSpec{CacheWarmScaleOutTimeoutSeconds: &customTimeout},
			wantEnabled: true,
			wantTimeout: 90 * time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.wantEnabled, noahCacheWarmScaleOutEnabled(test.spec))
			assert.Equal(t, test.wantTimeout, noahCacheWarmScaleOutTimeout(test.spec))
		})
	}
}

func TestTimedOutNoahIndexerCacheWarmPeer(t *testing.T) {
	const currentStart int64 = 1_700_000_000
	const peerID = "splunk-main-indexer-1.splunk-main-indexer-headless.test.svc.corp.example"
	expectedPeers := map[string]int64{peerID: currentStart}
	now := time.Unix(currentStart+60, 0)

	tests := []struct {
		name    string
		peers   []noah.Peer
		timeout time.Duration
		want    string
	}{
		{
			name:    "warming peer reaches timeout",
			peers:   []noah.Peer{{ID: peerID, Status: noah.PeerStatusWarming, Data: noah.PeerData{StartTime: currentStart}}},
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "started peer reaches timeout",
			peers:   []noah.Peer{{ID: peerID, Status: noah.PeerStatusStarted, Data: noah.PeerData{StartTime: currentStart}}},
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "warmed peer must still become up",
			peers:   []noah.Peer{{ID: peerID, Status: noah.PeerStatusWarmed, Data: noah.PeerData{StartTime: currentStart}}},
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "down peer reaches timeout",
			peers:   []noah.Peer{{ID: peerID, Status: noah.PeerStatusDown, Data: noah.PeerData{StartTime: currentStart}}},
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "warming remains within timeout",
			peers:   []noah.Peer{{ID: peerID, Status: noah.PeerStatusWarming, Data: noah.PeerData{StartTime: currentStart}}},
			timeout: time.Minute + time.Second,
		},
		{
			name:    "up peer does not time out",
			peers:   []noah.Peer{{ID: peerID, Status: noah.PeerStatusUp, Data: noah.PeerData{StartTime: currentStart}}},
			timeout: time.Minute,
		},
		{
			name:    "stale warming incarnation does not mask missing peer timeout",
			peers:   []noah.Peer{{ID: peerID, Status: noah.PeerStatusWarming, Data: noah.PeerData{StartTime: currentStart - 1}}},
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "missing peer reaches timeout",
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "missing peer remains within timeout",
			timeout: time.Minute + time.Second,
		},
		{
			name:    "zero timeout is disabled",
			peers:   []noah.Peer{{ID: peerID, Status: noah.PeerStatusWarming, Data: noah.PeerData{StartTime: currentStart}}},
			timeout: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, timedOutNoahIndexerCacheWarmPeer(test.peers, expectedPeers, test.timeout, now))
		})
	}
}

func TestNoahIndexerScaleOutUsesReferencedAuthentication(t *testing.T) {
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{peerStatus: noah.PeerStatusUp})
	plan, err := fixture.podManager().NextReplicas(t.Context(), 1, fixture.cr.Spec.Replicas)
	require.NoError(t, err)
	assert.Equal(t, int32(2), plan.TargetReplicas)
	assert.NotEmpty(t, fixture.requestHeaders.Get("x-splunk-lm-nonce"))
	assert.NotEmpty(t, fixture.requestHeaders.Get("x-splunk-lm-timestamp"))
	assert.True(t, strings.HasPrefix(fixture.requestHeaders.Get("x-splunk-digest"), "v2,"))
}

func TestNoahIndexerScaleOutPolicy(t *testing.T) {
	disabled := false
	tests := []struct {
		name             string
		peerStatus       noah.PeerStatus
		cacheWarmEnabled *bool
		requested        int32
		wantTarget       int32
		wantComplete     bool
	}{
		{name: "enabled waits for warming peer", peerStatus: noah.PeerStatusWarming, requested: 3, wantTarget: 1},
		{name: "disabled advances after registration", peerStatus: noah.PeerStatusWarming, cacheWarmEnabled: &disabled, requested: 3, wantTarget: 2},
		{name: "final warming peer is incomplete", peerStatus: noah.PeerStatusWarming, cacheWarmEnabled: &disabled, requested: 1, wantTarget: 1},
		{name: "final up peer is complete", peerStatus: noah.PeerStatusUp, requested: 1, wantTarget: 1, wantComplete: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{
				peerStatus:       test.peerStatus,
				cacheWarmEnabled: test.cacheWarmEnabled,
			})
			plan, err := fixture.podManager().NextReplicas(t.Context(), 1, test.requested)
			require.NoError(t, err)
			assert.Equal(t, test.wantTarget, plan.TargetReplicas)
			assert.Equal(t, test.wantComplete, plan.Complete)
		})
	}
}

func TestReconcileReadyNoahIndexerAppliesScaleOutTarget(t *testing.T) {
	disabled := false
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{
		peerStatus:       noah.PeerStatusWarming,
		cacheWarmEnabled: &disabled,
	})

	outcome, err := fixture.podManager().reconcileReady(t.Context(), 1, enterpriseApi.PhaseReady, 1)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, outcome.phase)
	assert.Equal(t, metav1.ConditionFalse, outcome.condition.Status)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
	assert.Equal(t, int32(2), fixture.cr.Status.Replicas)

	stored := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace}, stored))
	require.NotNil(t, stored.Spec.Replicas)
	assert.Equal(t, int32(2), *stored.Spec.Replicas)
}

func TestNoahIndexerPodManagerPropagatesScaleOutUpdateError(t *testing.T) {
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{peerStatus: noah.PeerStatusUp})
	wantErr := errors.New("StatefulSet update failed")
	fixture.client.InduceErrorKind[splcommon.MockClientInduceErrorUpdate] = wantErr

	outcome, err := fixture.podManager().reconcileReady(t.Context(), 1, enterpriseApi.PhaseReady, 1)

	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, noahIndexerOutcome{}, outcome)
}

func TestNoahIndexerCacheWarmTimeoutStopsRequeueAndAllowsReevaluation(t *testing.T) {
	timeoutSeconds := int32(60)
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{
		peerStatus:     noah.PeerStatusWarming,
		peerStart:      time.Now().Add(-2 * time.Minute).Unix(),
		timeoutSeconds: &timeoutSeconds,
	})

	outcome, err := fixture.podManager().reconcileReady(t.Context(), 1, enterpriseApi.PhaseScalingUp, 1)
	require.Error(t, err)
	message, terminal := splcommon.TerminalMessage(err)
	require.True(t, terminal)
	assert.Contains(t, message, "Cache warming timed out")
	reason, _ := splcommon.TerminalReason(err)
	assert.Equal(t, EventReasonNoahCacheWarmTimeout, reason)
	assert.Equal(t, enterpriseApi.PhaseError, outcome.phase)
	assert.Zero(t, outcome.requeueAfter)
	assert.Equal(t, string(enterpriseApi.ReasonNoahCacheWarmTimeout), outcome.condition.Reason)

	noahCluster := &enterpriseApi.NoahCluster{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{Name: "noah", Namespace: fixture.cr.Namespace}, noahCluster))
	cacheWarmDisabled := false
	noahCluster.Spec.CacheWarmScaleOutEnabled = &cacheWarmDisabled
	require.NoError(t, fixture.client.Update(t.Context(), noahCluster))

	outcome, err = fixture.podManager().reconcileReady(t.Context(), 1, enterpriseApi.PhaseError, 1)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, outcome.phase)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
	assert.Equal(t, int32(2), fixture.cr.Status.Replicas)
}

func TestWaitForNoahIndexerWorkloadPreservesUpdatingForPendingTemplateRevision(t *testing.T) {
	outcome := waitForNoahIndexerWorkload(enterpriseApi.PhaseUpdating, enterpriseApi.PhaseScalingUp, 2)

	assert.Equal(t, enterpriseApi.PhaseUpdating, outcome.phase)
	assert.Equal(t, "Waiting for the StatefulSet pod-template revision to be applied", outcome.phaseMessage)
	assert.Equal(t, metav1.ConditionFalse, outcome.condition.Status)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
}

func TestWaitForNoahIndexerWorkloadPreservesScaleOutForReplicaReadiness(t *testing.T) {
	outcome := waitForNoahIndexerWorkload(enterpriseApi.PhasePending, enterpriseApi.PhaseScalingUp, 2)

	assert.Equal(t, enterpriseApi.PhaseScalingUp, outcome.phase)
	assert.Equal(t, "Waiting for 2 applied replicas to become ready before continuing scale-out", outcome.phaseMessage)
	assert.Equal(t, metav1.ConditionFalse, outcome.condition.Status)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
}

func TestSetNoahIndexerPhaseAndConditions(t *testing.T) {
	cr := &enterpriseApi.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
	setNoahIndexerPhaseAndConditions(cr, false, enterpriseApi.PhaseReady, "", newNoahPeersReadyCondition(
		metav1.ConditionTrue,
		enterpriseApi.ReasonNoahPeersReady,
		"All expected Noah peers are up",
	))

	assert.Equal(t, enterpriseApi.PhaseReady, cr.Status.Phase)
	assert.Equal(t, int64(7), cr.Status.ObservedGeneration)
	assert.True(t, splcommon.IsReady(cr.Status.Conditions))
	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahPeersReady)
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionTrue, condition.Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahPeersReady), condition.Reason)
	assert.Equal(t, int64(7), condition.ObservedGeneration)
}

func TestSetNoahIndexerPhaseAndConditionsPreservesUnspecifiedConditions(t *testing.T) {
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Generation: 7},
		Status: enterpriseApi.IndexerClusterStatus{Conditions: []metav1.Condition{
			{
				Type:               string(enterpriseApi.ConditionNoahPeersReady),
				Status:             metav1.ConditionTrue,
				Reason:             string(enterpriseApi.ReasonNoahPeersReady),
				ObservedGeneration: 6,
			},
		}},
	}

	setNoahIndexerPhaseAndConditions(cr, false, enterpriseApi.PhaseError, "resource application failed")

	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahPeersReady)
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionTrue, condition.Status)
	assert.Equal(t, int64(6), condition.ObservedGeneration)
}

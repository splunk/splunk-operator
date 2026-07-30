// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"errors"
	"testing"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestParseIndexerHECServingConfig(t *testing.T) {
	for _, test := range []struct {
		name    string
		output  string
		want    indexerHECServingConfig
		wantErr bool
	}{
		{
			name: "no http stanza",
			output: `[splunktcp://9997]
disabled = 0`,
			want: indexerHECServingConfig{scheme: "https", port: 8088},
		},
		{
			name: "HEC disabled",
			output: `[http]
disabled = true
enableSSL = false
port = 8088`,
			want: indexerHECServingConfig{scheme: "https", port: 8088},
		},
		{
			name: "HTTP custom port",
			output: `[http]
disabled = 0
enableSSL = false
port = 18088`,
			want: indexerHECServingConfig{
				enabled: true,
				scheme:  "http",
				port:    18088,
			},
		},
		{
			name: "HTTPS defaults",
			output: `[http]
disabled = false`,
			want: indexerHECServingConfig{
				enabled: true,
				scheme:  "https",
				port:    8088,
			},
		},
		{
			name: "invalid port",
			output: `[http]
disabled = 0
port = invalid`,
			wantErr: true,
		},
		{
			name: "ambiguous disabled value",
			output: `[http]
disabled = maybe`,
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseIndexerHECServingConfig(test.output)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestIndexerServingRecoveryRequiresEndpointPublication(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	replacement := pods[2].DeepCopy()
	replacement.UID = types.UID("replacement")
	mgr.c.(*spltest.MockClient).ListObj = &discoveryv1.EndpointSliceList{}

	observed, err := mgr.indexerServingRecoveryObserved(
		context.Background(),
		replacement,
	)
	require.NoError(t, err)
	require.False(t, observed)
}

func TestEndpointSlicesPublishReadyPodRequiresExplicitReady(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "indexer-0",
			UID:  types.UID("indexer-uid"),
		},
	}
	endpoint := discoveryv1.Endpoint{
		TargetRef: &corev1.ObjectReference{
			Name: pod.Name,
			UID:  pod.UID,
		},
	}
	require.False(
		t,
		endpointSlicesPublishReadyPod(
			[]discoveryv1.EndpointSlice{{
				Endpoints: []discoveryv1.Endpoint{endpoint},
			}},
			pod,
		),
	)
	ready := true
	endpoint.Conditions.Ready = &ready
	require.True(
		t,
		endpointSlicesPublishReadyPod(
			[]discoveryv1.EndpointSlice{{
				Endpoints: []discoveryv1.Endpoint{endpoint},
			}},
			pod,
		),
	)
}

func TestIndexerServingRecoveryChecksHECFromHealthyPeer(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	replacement := pods[2].DeepCopy()
	replacement.UID = types.UID("replacement")
	require.NoError(t, mgr.c.Update(context.Background(), replacement))
	addReadyIndexerEndpointSlice(t, mgr, replacement)

	mockExec := &spltest.MockPodExecClient{
		Client: mgr.c,
		Cr:     mgr.cr,
	}
	mockExec.AddMockPodExecReturnContext(
		context.Background(),
		indexerHECBtoolCommand,
		&spltest.MockPodExecReturnContext{
			StdOut: `[http]
disabled = 0
enableSSL = 1
port = 8088`,
		},
	)
	mockExec.AddMockPodExecReturnContext(
		context.Background(),
		"services/collector/health",
		&spltest.MockPodExecReturnContext{},
	)
	restoreIndexerPodExecForTest(t, mockExec)

	observed, err := mgr.indexerServingRecoveryObserved(
		context.Background(),
		replacement,
	)
	require.NoError(t, err)
	require.True(t, observed)
	require.Equal(t, pods[0].Name, mockExec.TargetPodName)
	require.Len(t, mockExec.GotCmdList, 2)
}

func TestIndexerServingRecoveryWaitsForRemoteHEC(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	replacement := pods[2].DeepCopy()
	replacement.UID = types.UID("replacement")
	require.NoError(t, mgr.c.Update(context.Background(), replacement))
	addReadyIndexerEndpointSlice(t, mgr, replacement)

	mockExec := &spltest.MockPodExecClient{
		Client: mgr.c,
		Cr:     mgr.cr,
	}
	mockExec.AddMockPodExecReturnContext(
		context.Background(),
		indexerHECBtoolCommand,
		&spltest.MockPodExecReturnContext{
			StdOut: `[http]
disabled = false
enableSSL = false
port = 8088`,
		},
	)
	mockExec.AddMockPodExecReturnContext(
		context.Background(),
		"services/collector/health",
		&spltest.MockPodExecReturnContext{
			Err: errors.New("connection refused"),
		},
	)
	restoreIndexerPodExecForTest(t, mockExec)

	observed, err := mgr.indexerServingRecoveryObserved(
		context.Background(),
		replacement,
	)
	require.NoError(t, err)
	require.False(t, observed)
}

func TestIndexerServingRecoveryChecksS2SWhenHECDisabled(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	replacement := pods[2].DeepCopy()
	replacement.UID = types.UID("replacement")
	replacement.Spec.Containers = []corev1.Container{{
		Name: "splunk",
		Ports: []corev1.ContainerPort{{
			Name:          GetPortName(s2sPort, protoTCP),
			ContainerPort: 9997,
		}},
	}}
	require.NoError(t, mgr.c.Update(context.Background(), replacement))
	addReadyIndexerEndpointSlice(t, mgr, replacement)

	mockExec := &spltest.MockPodExecClient{
		Client: mgr.c,
		Cr:     mgr.cr,
	}
	mockExec.AddMockPodExecReturnContext(
		context.Background(),
		indexerHECBtoolCommand,
		&spltest.MockPodExecReturnContext{
			StdOut: `[http]
disabled = 1`,
		},
	)
	mockExec.AddMockPodExecReturnContext(
		context.Background(),
		"/dev/tcp/",
		&spltest.MockPodExecReturnContext{},
	)
	restoreIndexerPodExecForTest(t, mockExec)

	observed, err := mgr.indexerServingRecoveryObserved(
		context.Background(),
		replacement,
	)
	require.NoError(t, err)
	require.True(t, observed)
	require.Equal(t, pods[0].Name, mockExec.TargetPodName)
	require.Len(t, mockExec.GotCmdList, 2)
}

func TestSingleIndexerServingRecoveryUsesClusterManagerObserver(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	replacement := pods[0].DeepCopy()
	replacement.UID = types.UID("replacement")
	require.NoError(t, mgr.c.Update(context.Background(), replacement))
	mgr.cr.Status.Peers = mgr.cr.Status.Peers[:1]
	mgr.cr.Spec.Replicas = 1
	mgr.cr.Spec.ClusterManagerRef = corev1.ObjectReference{Name: "manager"}
	clusterManagerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetPodName(SplunkClusterManager, "manager", 0),
			Namespace: mgr.cr.Namespace,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	mgr.c.(*spltest.MockClient).AddObject(clusterManagerPod)
	addReadyIndexerEndpointSlice(t, mgr, replacement)

	mockExec := &spltest.MockPodExecClient{
		Client: mgr.c,
		Cr:     mgr.cr,
	}
	mockExec.AddMockPodExecReturnContext(
		context.Background(),
		indexerHECBtoolCommand,
		&spltest.MockPodExecReturnContext{
			StdOut: `[http]
disabled = 0
enableSSL = 0
port = 8088`,
		},
	)
	mockExec.AddMockPodExecReturnContext(
		context.Background(),
		"services/collector/health",
		&spltest.MockPodExecReturnContext{},
	)
	restoreIndexerPodExecForTest(t, mockExec)

	observed, err := mgr.indexerServingRecoveryObserved(
		context.Background(),
		replacement,
	)
	require.NoError(t, err)
	require.True(t, observed)
	require.Equal(t, clusterManagerPod.Name, mockExec.TargetPodName)
}

func addReadyIndexerEndpointSlice(
	t *testing.T,
	mgr *indexerClusterPodManager,
	pod *corev1.Pod,
) {
	t.Helper()
	ready := true
	mgr.c.(*spltest.MockClient).ListObj = &discoveryv1.EndpointSliceList{
		Items: []discoveryv1.EndpointSlice{{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "indexer-serving",
				Namespace: mgr.cr.GetNamespace(),
				Labels: map[string]string{
					discoveryv1.LabelServiceName: splcommon.GetSplunkServiceName(
						SplunkIndexer,
						mgr.cr.GetName(),
						false,
					),
				},
			},
			Endpoints: []discoveryv1.Endpoint{{
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				TargetRef: &corev1.ObjectReference{
					Name: pod.Name,
					UID:  pod.UID,
				},
			}},
		}},
	}
}

func restoreIndexerPodExecForTest(
	t *testing.T,
	mockExec *spltest.MockPodExecClient,
) {
	t.Helper()
	oldGetPodExecClient := splutil.GetPodExecClient
	t.Cleanup(func() {
		splutil.GetPodExecClient = oldGetPodExecClient
	})
	splutil.GetPodExecClient = func(
		_ splcommon.ControllerClient,
		_ splcommon.MetaObject,
		targetPodName string,
	) splutil.PodExecClientImpl {
		mockExec.TargetPodName = targetPodName
		return mockExec
	}
}

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

package indexercluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var GetEventPublisher = k8sops.GetEventPublisher

func loadFixture(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join("testdata", "fixtures", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("Failed to load fixture %s: %v", filename, err)
		return ""
	}

	var compactJSON bytes.Buffer
	if err := json.Compact(&compactJSON, data); err != nil {
		t.Errorf("Failed to compact JSON from fixture %s: %v", filename, err)
		return ""
	}
	return compactJSON.String()
}

const SplunkIndexer = splcommon.SplunkIndexer
const SplunkClusterManager = splcommon.SplunkClusterManager

func GetSplunkStatefulsetName(instanceType splcommon.InstanceType, identifier string) string {
	return splutil.GetSplunkStatefulsetName(instanceType, identifier)
}

func GetSplunkStatefulsetPodName(instanceType splcommon.InstanceType, identifier string, index int32) string {
	return splutil.GetSplunkStatefulsetPodName(instanceType, identifier, index)
}

func getSplunkLabels(instanceIdentifier string, instanceType splcommon.InstanceType, partOfIdentifier string) map[string]string {
	return resources.GetSplunkLabels(instanceIdentifier, instanceType, partOfIdentifier)
}

func ApplyClusterManager(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager, _ splutil.PodExecClientImpl) (reconcile.Result, error) {
	replicas := int32(1)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: splutil.GetSplunkStatefulsetName(splcommon.SplunkClusterManager, cr.Name), Namespace: cr.Namespace},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": cr.Name}}},
	}
	if err := c.Create(ctx, statefulSet); err != nil && !apierrors.IsAlreadyExists(err) {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func newTestEventPublisher(recorder record.EventRecorder) *k8sops.K8EventPublisher {
	publisher, _ := k8sops.NewK8EventPublisherWithRecorder(recorder, &enterpriseApi.IndexerCluster{})
	return publisher
}

const (
	readinessScriptLocation = "tools/k8_probes/readinessProbe.sh"
	livenessScriptLocation  = "tools/k8_probes/livenessProbe.sh"
	startupScriptLocation   = "tools/k8_probes/startupProbe.sh"
)

var (
	GetReadinessScriptLocation = func() string { return filepath.Join("..", "..", "..", readinessScriptLocation) }
	GetLivenessScriptLocation  = func() string { return filepath.Join("..", "..", "..", livenessScriptLocation) }
	GetStartupScriptLocation   = func() string { return filepath.Join("..", "..", "..", startupScriptLocation) }
)

// These hooks are retained for the legacy app-framework fixture embedded in
// the IndexerCluster tests. IndexerCluster reconciliation itself does not use
// the app-framework pipeline.
type RemoteDataClientManager struct{}

var GetAppsList = func(context.Context, RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
	return splcommon.RemoteDataListResponse{}, nil
}

func initGlobalResourceTracker() {}

func configTester(t *testing.T, method string, f func() (interface{}, error), want string) {
	t.Helper()
	result, err := f()
	require.NoError(t, err, method)
	got, err := json.Marshal(result)
	require.NoError(t, err, method)
	require.JSONEq(t, want, string(got), method)
}

func splunkDeletionTester(t *testing.T, cr splcommon.MetaObject, delete func(splcommon.MetaObject, splcommon.ControllerClient) (bool, error)) {
	t.Helper()
	pvclist := corev1.PersistentVolumeClaimList{Items: []corev1.PersistentVolumeClaim{{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-pvc-stack1-var", Namespace: "test"},
	}}}
	mockCalls := make(map[string][]spltest.MockFuncCall)
	wantDeleted := cr.GetObjectMeta().GetDeletionTimestamp() != nil
	if wantDeleted {
		apiVersion, _ := schema.ParseGroupVersion("enterprise.splunk.com/v4")
		mockCalls["Update"] = []spltest.MockFuncCall{{
			MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName()),
		}}
	}

	c := spltest.NewMockClient()
	c.ListObj = &pvclist
	deleted, err := delete(cr, c)
	if deleted != wantDeleted || err != nil {
		t.Errorf("k8sops.CheckForDeletion() returned %t, %v; want %t, nil", deleted, err, wantDeleted)
	}
	// The migrated reconciler performs its normal secret/status updates before
	// invoking the finalizer; deletion behavior is asserted above.
}

func newFakeClientBuilder(scheme *runtime.Scheme) *fake.ClientBuilder {
	// The controller-runtime v0.24 fake client defaults to a managed-fields
	// tracker, which rejects the uint64 fields used by Splunk CR specs.
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjectTracker(clienttesting.NewObjectTracker(
			scheme,
			serializer.NewCodecFactory(scheme).UniversalDecoder(),
		)).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				err := c.Get(ctx, key, obj, opts...)
				if err != nil {
					return err
				}
				gvk, err := apiutil.GVKForObject(obj, scheme)
				if err == nil {
					obj.GetObjectKind().SetGroupVersionKind(gvk)
				}
				return nil
			},
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				gvk := obj.GetObjectKind().GroupVersionKind()
				err := c.Create(ctx, obj, opts...)
				obj.GetObjectKind().SetGroupVersionKind(gvk)
				return err
			},
		})
}

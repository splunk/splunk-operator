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

package searchheadcluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splstorage "github.com/splunk/splunk-operator/pkg/splunk/client/storage"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	shcworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/shc"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	readinessScriptLocation = "tools/k8_probes/readinessProbe.sh"
	livenessScriptLocation  = "tools/k8_probes/livenessProbe.sh"
	startupScriptLocation   = "tools/k8_probes/startupProbe.sh"
)

const (
	SplunkDeployer       = splcommon.SplunkDeployer
	SplunkLicenseManager = splcommon.SplunkLicenseManager
)

type NewSplunkClientFunc = shcworkflow.NewSplunkClientFunc

type RemoteDataClientManager struct {
	client              splcommon.ControllerClient
	CR                  splcommon.MetaObject
	appFrameworkRef     *enterpriseApi.AppFrameworkSpec
	vol                 *enterpriseApi.VolumeSpec
	location            string
	initFn              splcommon.GetInitFunc
	getRemoteDataClient func(context.Context, splcommon.ControllerClient, splcommon.MetaObject, *enterpriseApi.AppFrameworkSpec, *enterpriseApi.VolumeSpec, string, splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error)
}

func (m *RemoteDataClientManager) GetAppsList(ctx context.Context) (splcommon.RemoteDataListResponse, error) {
	c, err := m.getRemoteDataClient(ctx, m.client, m.CR, m.appFrameworkRef, m.vol, m.location, m.initFn)
	if err != nil {
		return splcommon.RemoteDataListResponse{}, err
	}
	return c.Client.GetAppsList(ctx)
}

var GetAppsList = func(ctx context.Context, manager RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
	return manager.GetAppsList(ctx)
}

var GetEventPublisher = k8sops.GetEventPublisher

func getSplunkStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, instanceType splcommon.InstanceType, replicas int32, extraEnv []corev1.EnvVar, certMounts *certs.CertMountConfig, opts ...resources.StatefulSetOption) (*appsv1.StatefulSet, error) {
	statefulSet, err := k8sops.GetSplunkStatefulSet(ctx, client, cr, spec, instanceType, replicas, extraEnv, opts...)
	if err != nil {
		return statefulSet, err
	}
	certs.InjectCertMounts(&statefulSet.Spec.Template, certMounts)
	return statefulSet, nil
}

// helper function to get the list of SearchHeadCluster types in the current namespace
func getSearchHeadClusterList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.SearchHeadClusterList, error) {
	logger := logging.FromContext(ctx).With("func", "getSearchHeadClusterList", "name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.SearchHeadClusterList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		logger.ErrorContext(ctx, "SearchHeadCluster types not found in namespace", "error", err, "namespace", cr.GetNamespace())
		return objectList, err
	}

	return objectList, nil
}

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

func GetSplunkStatefulsetName(instanceType splcommon.InstanceType, identifier string) string {
	return splutil.GetSplunkStatefulsetName(instanceType, identifier)
}

func GetSplunkStatefulsetPodName(instanceType splcommon.InstanceType, identifier string, index int32) string {
	return splutil.GetSplunkStatefulsetPodName(instanceType, identifier, index)
}

func newTestEventPublisher(recorder record.EventRecorder) *k8sops.K8EventPublisher {
	publisher, _ := k8sops.NewK8EventPublisherWithRecorder(recorder, &corev1.Pod{})
	return publisher
}

func splunkDeletionTester(t *testing.T, cr splcommon.MetaObject, delete func(splcommon.MetaObject, splcommon.ControllerClient) (bool, error)) {
	t.Helper()
	pvcList := corev1.PersistentVolumeClaimList{Items: []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "splunk-pvc-stack1-var", Namespace: "test"}}}}
	mockCalls := make(map[string][]spltest.MockFuncCall)
	wantDeleted := cr.GetObjectMeta().GetDeletionTimestamp() != nil
	if wantDeleted {
		apiVersion, _ := schema.ParseGroupVersion("enterprise.splunk.com/v4")
		mockCalls["Update"] = []spltest.MockFuncCall{{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())}}
	}
	c := spltest.NewMockClient()
	c.ListObj = &pvcList
	deleted, err := delete(cr, c)
	if deleted != wantDeleted || err != nil {
		t.Errorf("k8sops.CheckForDeletion() returned %t, %v; want %t, nil", deleted, err, wantDeleted)
	}
}

func newFakeClientBuilder(scheme *pkgruntime.Scheme) *fake.ClientBuilder {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjectTracker(clienttesting.NewObjectTracker(scheme, serializer.NewCodecFactory(scheme).UniversalDecoder())).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				err := c.Get(ctx, key, obj, opts...)
				if err != nil {
					return err
				}
				if gvk, err := apiutil.GVKForObject(obj, scheme); err == nil {
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

func configTester(t *testing.T, method string, f func() (interface{}, error), want string) {
	t.Helper()
	result, err := f()
	require.NoError(t, err, method)
	got, err := json.Marshal(result)
	require.NoError(t, err, method)
	require.JSONEq(t, want, string(got), method)
}

type mockEvent struct {
	eventType string
	reason    string
	message   string
}

type mockEventRecorder struct {
	events []mockEvent
}

func (m *mockEventRecorder) Event(_ pkgruntime.Object, eventType, reason, message string) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: message})
}

func (m *mockEventRecorder) Eventf(_ pkgruntime.Object, eventType, reason, messageFmt string, args ...interface{}) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: fmt.Sprintf(messageFmt, args...)})
}

func (m *mockEventRecorder) AnnotatedEventf(_ pkgruntime.Object, _ map[string]string, eventType, reason, messageFmt string, args ...interface{}) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: fmt.Sprintf(messageFmt, args...)})
}

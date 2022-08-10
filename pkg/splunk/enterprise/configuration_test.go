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
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func configTester(t *testing.T, method string, f func() (interface{}, error), want string) {
	result, err := f()
	if err != nil {
		t.Errorf("%s returned error: %v", method, err)
	}

	// Marshall the result and compare
	marshalAndCompare(t, result, method, want)
}

func marshalAndCompare(t *testing.T, compare interface{}, method string, want string) {
	got, err := json.Marshal(compare)
	if err != nil {
		t.Errorf("%s failed to marshall", err)
	}
	//if string(got) != want {
	//	t.Errorf("Method %s, got = %s;\nwant %s", method, got, want)
	//}
	require.JSONEq(t, string(got), want)
}

func TestGetSplunkService(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(instanceType InstanceType, isHeadless bool, want string) {
		f := func() (interface{}, error) {
			return getSplunkService(ctx, &cr, &cr.Spec.CommonSplunkSpec, instanceType, isHeadless), nil
		}
		configTester(t, fmt.Sprintf("getSplunkService(\"%s\",%t)", instanceType, isHeadless), f, want)
	}

	test(SplunkIndexer, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-service","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"http-splunkweb","protocol":"TCP","port":8000,"targetPort":8000},{"name":"http-hec","protocol":"TCP","port":8088,"targetPort":8088},{"name":"https-splunkd","protocol":"TCP","port":8089,"targetPort":8089},{"name":"tcp-s2s","protocol":"TCP","port":9997,"targetPort":9997}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"status":{"loadBalancer":{}}}`)
	test(SplunkIndexer, true, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-headless","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"http-splunkweb","protocol":"TCP","port":8000,"targetPort":8000},{"name":"http-hec","protocol":"TCP","port":8088,"targetPort":8088},{"name":"https-splunkd","protocol":"TCP","port":8089,"targetPort":8089},{"name":"tcp-s2s","protocol":"TCP","port":9997,"targetPort":9997}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"clusterIP":"None","type":"ClusterIP"},"status":{"loadBalancer":{}}}`)
	// Multipart IndexerCluster - test part-of and instance labels for child part
	cr.Spec.ClusterMasterRef.Name = "cluster1"
	test(SplunkIndexer, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-service","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-cluster1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"http-splunkweb","protocol":"TCP","port":8000,"targetPort":8000},{"name":"http-hec","protocol":"TCP","port":8088,"targetPort":8088},{"name":"https-splunkd","protocol":"TCP","port":8089,"targetPort":8089},{"name":"tcp-s2s","protocol":"TCP","port":9997,"targetPort":9997}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-cluster1-indexer"}},"status":{"loadBalancer":{}}}`)
	cr.Spec.ClusterMasterRef.Name = ""

	cr.Spec.ServiceTemplate.Spec.Type = "LoadBalancer"
	cr.Spec.ServiceTemplate.ObjectMeta.Labels = map[string]string{"1": "2"}
	cr.ObjectMeta.Labels = map[string]string{"one": "two"}
	cr.ObjectMeta.Annotations = map[string]string{"a": "b"}

	test(SplunkSearchHead, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-search-head-service","namespace":"test","creationTimestamp":null,"labels":{"1":"2","app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head","one":"two"},"annotations":{"a":"b"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"http-splunkweb","protocol":"TCP","port":8000,"targetPort":8000},{"name":"https-splunkd","protocol":"TCP","port":8089,"targetPort":8089}],"selector":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"type":"LoadBalancer"},"status":{"loadBalancer":{}}}`)
	test(SplunkSearchHead, true, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-search-head-headless","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head","one":"two"},"annotations":{"a":"b"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"http-splunkweb","protocol":"TCP","port":8000,"targetPort":8000},{"name":"https-splunkd","protocol":"TCP","port":8089,"targetPort":8089}],"selector":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"clusterIP":"None","type":"ClusterIP","publishNotReadyAddresses":true},"status":{"loadBalancer":{}}}`)
}

func TestGetSplunkDefaults(t *testing.T) {
	cr := enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Defaults: "defaults_string"},
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			return getSplunkDefaults(cr.GetName(), cr.GetNamespace(), SplunkIndexer, cr.Spec.Defaults), nil
		}
		configTester(t, "getSplunkDefaults()", f, want)
	}

	test(`{"metadata":{"name":"splunk-stack1-indexer-defaults","namespace":"test","creationTimestamp":null},"data":{"default.yml":"defaults_string"}}`)
}

func TestGetService(t *testing.T) {

	ctx := context.TODO()
	cr := enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ServiceTemplate: corev1.Service{
						Spec: corev1.ServiceSpec{
							Ports: []corev1.ServicePort{{Name: "user-defined", Port: 32000, TargetPort: intstr.FromInt(6443)}},
						},
					},
				},
			},
		},
	}

	test := func(instanceType InstanceType, want string) {
		f := func() (interface{}, error) {
			return getSplunkService(ctx, &cr, &cr.Spec.CommonSplunkSpec, instanceType, false), nil
		}
		configTester(t, "getSplunkService()", f, want)
	}

	test(SplunkIndexer, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-service","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"user-defined","port":32000,"targetPort":6443},{"name":"http-splunkweb","protocol":"TCP","port":8000,"targetPort":8000},{"name":"http-hec","protocol":"TCP","port":8088,"targetPort":8088},{"name":"https-splunkd","protocol":"TCP","port":8089,"targetPort":8089},{"name":"tcp-s2s","protocol":"TCP","port":9997,"targetPort":9997}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"status":{"loadBalancer":{}}}`)
}

func TestSetVolumeDefault(t *testing.T) {
	cr := enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	if cr.Spec.CommonSplunkSpec.Volumes == nil {
		t.Errorf("setVolumeDefaults() returns nil for Volumes")
	}

	mode := int32(644)
	cr.Spec.CommonSplunkSpec.Volumes = []corev1.Volume{
		{
			Name: "vol1",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "top-secret1",
				},
			},
		},
		{
			Name: "vol2",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  "top-secret2",
					DefaultMode: &mode,
				},
			},
		},
		{
			Name: "vol3",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "config3",
					},
				},
			},
		},
	}

	// Make sure the default mode is set correcty
	setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	if cr.Spec.CommonSplunkSpec.Volumes == nil {
		t.Errorf("setVolumeDefaults() returns nil for Volumes")
	}

	for _, v := range cr.Spec.CommonSplunkSpec.Volumes {
		if v.Name == "vol1" {
			if *v.Secret.DefaultMode != int32(corev1.SecretVolumeSourceDefaultMode) {
				t.Errorf("setVolumeDefaults() did not set defaultMode correctly. Want %d, Got %d", int32(corev1.SecretVolumeSourceDefaultMode), *v.Secret.DefaultMode)
			}
		} else if v.Name == "vol2" {
			if *v.Secret.DefaultMode != mode {
				t.Errorf("setVolumeDefaults() did not set defaultMode correctly. Want %d, Got %d", mode, *v.Secret.DefaultMode)
			}
		} else if v.Name == "vol3" {
			if *v.ConfigMap.DefaultMode != int32(corev1.ConfigMapVolumeSourceDefaultMode) {
				t.Errorf("setVolumeDefaults() did not set defaultMode correctly. Want %d, Got %d", int32(corev1.ConfigMapVolumeSourceDefaultMode), *v.ConfigMap.DefaultMode)
			}
		}
	}
}

func TestSmartstoreApplyClusterManagerFailsOnInvalidSmartStoreConfig(t *testing.T) {
	cr := enterpriseApi.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idxCluster",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterMasterSpec{
			SmartStore: enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "", Path: "testbucket-rs-london"},
				},

				IndexList: []enterpriseApi.IndexSpec{
					{Name: "salesdata1"},
					{Name: "salesdata2", RemotePath: "salesdata2"},
					{Name: "salesdata3", RemotePath: ""},
				},
			},
		},
	}

	var client splcommon.ControllerClient

	_, err := ApplyClusterManager(context.Background(), client, &cr)
	if err == nil {
		t.Errorf("ApplyClusterManager should fail on invalid smartstore config")
	}
}

func TestSmartstoreApplyStandaloneFailsOnInvalidSmartStoreConfig(t *testing.T) {
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "standalone",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
			SmartStore: enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "", Path: "testbucket-rs-london"},
				},
				IndexList: []enterpriseApi.IndexSpec{
					{Name: "salesdata1",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "salesdata2"},
					{Name: "salesdata3", RemotePath: ""},
				},
			},
		},
	}

	var client splcommon.ControllerClient

	_, err := ApplyStandalone(context.Background(), client, &cr)
	if err == nil {
		t.Errorf("ApplyStandalone should fail on invalid smartstore config")
	}
}

func TestSmartStoreConfigDoesNotFailOnClusterManagerCR(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()
	cr := enterpriseApi.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "CM",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterMasterSpec{
			SmartStore: enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
				},

				IndexList: []enterpriseApi.IndexSpec{
					{Name: "salesdata1", RemotePath: "remotepath1", IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "remotepath2", IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata3", RemotePath: "remotepath3", IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
				},
			},
		},
	}

	err := validateClusterManagerSpec(ctx, c, &cr)

	if err != nil {
		t.Errorf("Smartstore configuration should not fail on ClusterManager CR: %v", err)
	}
}

func TestValidateSplunkSmartstoreSpec(t *testing.T) {
	var err error
	ctx := context.TODO()
	// Valid smartstore config
	SmartStore := enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStore)
	if err != nil {
		t.Errorf("Valid Smartstore configuration should not cause error: %v", err)
	}

	// Missing Secret object reference with Volume config should fail
	SmartStoreMultipleVolumes := enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol_1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london"},
			{Name: "msos_s2s3_vol_2", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret2"},
		},
		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreMultipleVolumes)
	if err == nil {
		t.Errorf("Missing Secret Object reference should error out")
	}

	// Smartstore config with missing endpoint for the volume errors out
	SmartStoreVolumeWithNoRemoteEndPoint := enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "", Path: "testbucket-rs-london"},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreVolumeWithNoRemoteEndPoint)
	if err == nil {
		t.Errorf("Should not accept a volume with missing Endpoint")
	}

	// Smartstore config with missing remote name for the volume
	SmartStoreWithVolumeNameMissing := enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london"},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreWithVolumeNameMissing)
	if err == nil {
		t.Errorf("Should not accept a volume with missing Remotename")
	}

	// Smartstore config with missing path for the volume
	SmartStoreWithVolumePathMissing := enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: ""},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreWithVolumePathMissing)
	if err == nil {
		t.Errorf("Should not accept a volume with missing Remote Path")
	}

	// Smartstore config with missing index name
	SmartStoreWithMissingIndexName := enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterpriseApi.IndexSpec{
			{Name: "",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreWithMissingIndexName)
	if err == nil {
		t.Errorf("Should not accept an Index with missing indexname ")
	}

	//Smartstore config Index with VolName, but missing RemotePath errors out
	SmartStoreWithMissingIndexLocation := enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreWithMissingIndexLocation)
	if err != nil {
		t.Errorf("An index with missing remotePath should use index name as path, but failed with error: %v", err)
	}

	// Having defaults volume and remote path should not complain an index missing the volume and remotepath info.
	SmartStoreConfWithDefaults := enterpriseApi.SmartStoreSpec{
		Defaults: enterpriseApi.IndexConfDefaultsSpec{
			IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
				VolName: "msos_s2s3_vol"},
		},
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1"},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3"},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreConfWithDefaults)
	if err != nil {
		t.Errorf("Should accept an Index with missing remotePath location, when defaults are configured. But, got the error: %v", err)
	}

	// Empty smartstore config
	err = ValidateSplunkSmartstoreSpec(ctx, nil)
	if err != nil {
		t.Errorf("Smartstore config is optional, should not cause an error. But, got the error: %v", err)
	}

	// Configuring indexes without volume config should return error
	SmartStoreWithoutVolumes := enterpriseApi.SmartStoreSpec{
		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreWithoutVolumes)
	if err == nil {
		t.Errorf("Smartstore config without volume details should return error")
	}

	// Duplicate volume names should be rejected
	SmartStoreWithDuplicateVolumes := enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol-1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
			{Name: "msos_s2s3_vol-2", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
			{Name: "msos_s2s3_vol-1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreWithDuplicateVolumes)
	if err == nil {
		t.Errorf("Duplicate volume configuration should return an error")
	}

	// Defaults with invalid volume reference should return error
	SmartStoreDefaultsWithNonExistingVolume := enterpriseApi.SmartStoreSpec{
		Defaults: enterpriseApi.IndexConfDefaultsSpec{
			IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
				VolName: "msos_s2s3_vol-2"},
		},
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol-1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreDefaultsWithNonExistingVolume)
	if err == nil {
		t.Errorf("Volume referred in the indexes defaults should be a valid volume")
	}

	//Duplicate index names should return an error
	SmartStoreWithDuplicateIndexes := enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata1", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreWithDuplicateIndexes)
	if err == nil {
		t.Errorf("Duplicate index names should return an error")
	}

	// If the default volume is not configured, then each index should be configured
	// with an explicit volume info. If not, should return an error
	SmartStoreVolumeMissingBothFromDefaultsAndIndex := enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol-1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1"},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreVolumeMissingBothFromDefaultsAndIndex)
	if err == nil {
		t.Errorf("If no default volume, index with missing volume info should return an error")
	}

	// Volume referenced from an index must be a valid volume
	SmartStoreIndexesWithInvalidVolumeName := enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol-1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol-2"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(ctx, &SmartStoreIndexesWithInvalidVolumeName)
	if err == nil {
		t.Errorf("Index with an invalid volume name should return error")
	}
}

func TestValidateAppFrameworkSpec(t *testing.T) {
	var err error
	ctx := context.TODO()

	currentDownloadVolume := splcommon.AppDownloadVolume
	splcommon.AppDownloadVolume = fmt.Sprintf("/tmp/appdownload-%d", rand.Intn(1000))
	defer func() {
		// remove the AppDownloadVolume if exist just to make sure
		// previous test case have not created directory
		err = os.RemoveAll(splcommon.AppDownloadVolume)
		if err != nil {
			t.Errorf("unable to delete directory %s", splcommon.AppDownloadVolume)
		}
		splcommon.AppDownloadVolume = currentDownloadVolume
	}()

	// Valid app framework config
	AppFramework := enterpriseApi.AppFrameworkSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret", Type: "s3", Provider: "aws"},
		},
		AppSources: []enterpriseApi.AppSourceSpec{
			{Name: "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
			{Name: "securityApps",
				Location: "securityAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
			{Name: "authenticationApps",
				Location: "authenticationAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
		},
	}

	appFrameworkContext := enterpriseApi.AppDeploymentContext{
		AppsRepoStatusPollInterval: 60,
	}

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil {
		t.Errorf("App Framework configuration should have returned error as we have not mounted app download volume: %v", err)
	}

	// to pass the validation stage, add the directory to download apps
	err = os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)

	if err != nil {
		t.Errorf("Valid App Framework configuration should not cause error: %v", err)
	}

	AppFramework.VolList[0].SecretRef = ""
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("Missing Secret Object reference is a valid config that should not cause error: %v", err)
	}
	AppFramework.VolList[0].SecretRef = "s3-secret"

	// App Framework config with missing App Source name
	AppFramework.AppSources[0].Name = ""

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.HasPrefix(err.Error(), "app Source name is missing for AppSource at:") {

		t.Errorf("Should not accept an app source with missing name ")
	}

	//App Framework config app source config with missing location(withot default location) should errro out
	AppFramework.AppSources[0].Name = "adminApps"
	AppFramework.AppSources[0].Location = ""
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.HasPrefix(err.Error(), "app Source location is missing for AppSource") {
		t.Errorf("An App Source with missing location should cause an error, when there is no default location configured")
	}
	AppFramework.AppSources[0].Location = "adminAppsRepo"

	// Having defaults volume and location should not complain an app source missing the volume and remote location info.
	AppFramework.Defaults.Scope = enterpriseApi.ScopeCluster
	AppFramework.Defaults.VolName = "msos_s2s3_vol"
	AppFramework.AppSources[0].Scope = ""

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("Should accept an App Source with missing scope, when default scope is configured. But, got the error: %v", err)
	}
	AppFramework.AppSources[0].Location = "adminAppsRepo"
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopeLocal

	// Empty App Repo config should not cause an error
	err = ValidateAppFrameworkSpec(ctx, nil, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("App Repo config is optional, should not cause an error. But, got the error: %v", err)
	}

	// Configuring indexes without volume config should return error
	AppFrameworkWithoutVolumeSpec := enterpriseApi.AppFrameworkSpec{
		AppSources: []enterpriseApi.AppSourceSpec{
			{Name: "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeCluster},
			},
			{Name: "securityApps",
				Location: "securityAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
			{Name: "authenticationApps",
				Location: "authenticationAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
		},
	}

	err = ValidateAppFrameworkSpec(ctx, &AppFrameworkWithoutVolumeSpec, &appFrameworkContext, false)
	if err == nil || !strings.HasPrefix(err.Error(), "invalid Volume Name for App Source") {
		t.Errorf("App Repo config without volume details should return error")
	}

	// Defaults with invalid volume reference should return error
	tmpVolume := AppFramework.Defaults.VolName
	AppFramework.Defaults.VolName = "UnknownVolume"

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.HasPrefix(err.Error(), "invalid Volume Name for Defaults") {
		t.Errorf("Volume referred in the defaults should be a valid volume")
	}
	AppFramework.Defaults.VolName = tmpVolume

	//Duplicate App Source locations should return an error
	tmpVolume = AppFramework.AppSources[1].VolName
	tmpLocation := AppFramework.AppSources[1].Location

	AppFramework.AppSources[1].VolName = AppFramework.AppSources[0].VolName
	AppFramework.AppSources[1].Location = AppFramework.AppSources[0].Location

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.HasPrefix(err.Error(), "duplicate App Source configured") {
		t.Errorf("Duplicate app sources should return an error")
	}

	// Duplicate App Source locations across different scopes should not return an error
	tmpScope := AppFramework.AppSources[1].Scope
	AppFramework.AppSources[1].Scope = enterpriseApi.ScopeCluster
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("App Sources with different app scopes can have duplicate paths, but failed with error: %v", err)
	}

	AppFramework.AppSources[1].VolName = tmpVolume
	AppFramework.AppSources[1].Location = tmpLocation
	AppFramework.AppSources[1].Scope = tmpScope

	// Duplicate app sources names should cause an error
	tmpAppSourceName := AppFramework.AppSources[1].Name
	AppFramework.AppSources[1].Name = AppFramework.AppSources[0].Name

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.HasPrefix(err.Error(), "multiple app sources with the name adminApps is not allowed") {
		t.Errorf("Failed to detect duplicate app source names")
	}
	AppFramework.AppSources[1].Name = tmpAppSourceName

	// If the default volume is not configured, then each index should be configured
	// with an explicit volume info. If not, should return an error
	AppFramework.AppSources[0].VolName = ""
	AppFramework.Defaults.VolName = ""

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.HasPrefix(err.Error(), "volumeName is missing for App Source") {
		t.Errorf("If no default volume, App Source with missing volume info should return an error")
	}

	// If the AppSource doesn't have VolName, and if the defaults have it, shouldn't cause an error
	AppFramework.Defaults.VolName = "msos_s2s3_vol"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("If default volume, App Source with missing volume should not return an error, but got error %v", err)
	}

	// Volume referenced from an index must be a valid volume
	AppFramework.AppSources[0].VolName = "UnknownVolume"

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.HasPrefix(err.Error(), "invalid Volume Name for App Source") {
		t.Errorf("Index with an invalid volume name should return error")
	}
	AppFramework.AppSources[0].VolName = "msos_s2s3_vol"

	// if the CR supports only local apps, and if the app source scope is not local, should return error
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopeCluster
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, true)
	if err == nil || !strings.HasPrefix(err.Error(), "invalid scope for App Source") {
		t.Errorf("When called with App scope local, any app sources with the cluster scope should return an error")
	}

	// If the app scope value other than "local" or "cluster" should return an error
	AppFramework.AppSources[0].Scope = "unknown"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.Contains(err.Error(), "should be either local or cluster or clusterWithPreConfig") {
		t.Errorf("Unsupported app scope should be cause error, but failed to detect")
	}

	// If the CR supports only local apps, and default is configured with "cluster" scope, that should be detected
	AppFramework.AppSources[0].Scope, AppFramework.AppSources[1].Scope, AppFramework.AppSources[2].Scope = enterpriseApi.ScopeLocal, enterpriseApi.ScopeLocal, enterpriseApi.ScopeLocal

	AppFramework.Defaults.Scope = enterpriseApi.ScopeCluster

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, true)
	if err == nil || !strings.HasPrefix(err.Error(), "invalid scope for defaults config. Only local scope is supported for this kind of CR") {
		t.Errorf("When called with App scope local, defaults with the cluster scope should return an error")
	}
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopeLocal

	// Default scope should be either "local" OR "cluster"
	AppFramework.Defaults.Scope = "unknown"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.HasPrefix(err.Error(), "scope for defaults should be either local") {
		t.Errorf("Unsupported default scope should be cause error, but failed to detect")
	}
	AppFramework.Defaults.Scope = enterpriseApi.ScopeCluster

	// Missing scope, if the default scope is not specified should return error
	AppFramework.Defaults.Scope = ""
	AppFramework.AppSources[0].Scope = ""
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.HasPrefix(err.Error(), "app Source scope is missing for") {
		t.Errorf("Missing scope should be detected, but failed")
	}
	AppFramework.Defaults.Scope = enterpriseApi.ScopeLocal
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopeLocal

	// Scope clusteWithPreConfig should not return an error

	AppFramework.Defaults.Scope = ""
	AppFramework.AppSources[0].Scope = "clusterWithPreConfig"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("Valid scope clusterWithPreConfig should not cause an error")
	}
	AppFramework.Defaults.Scope = enterpriseApi.ScopeLocal
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopeLocal

	// AppsRepoPollInterval should be in between the minAppsRepoPollInterval and maxAppsRepoPollInterval
	// Default Poll interval
	if splcommon.DefaultAppsRepoPollInterval < splcommon.MinAppsRepoPollInterval || splcommon.DefaultAppsRepoPollInterval > splcommon.MaxAppsRepoPollInterval {
		t.Errorf("defaultAppsRepoPollInterval should be within the range [%d - %d]", splcommon.MinAppsRepoPollInterval, splcommon.MaxAppsRepoPollInterval)
	}

	AppFramework.AppsRepoPollInterval = 0
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("Got error on valid App Framework configuration. Error: %v", err)
	}

	// Check for minAppsRepoPollInterval
	AppFramework.AppsRepoPollInterval = splcommon.MinAppsRepoPollInterval - 1
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("Got error on valid App Framework configuration. Error: %v", err)
	} else if appFrameworkContext.AppsRepoStatusPollInterval != splcommon.MinAppsRepoPollInterval {
		t.Errorf("Spec validation is not able to set the the AppsRepoPollInterval to minAppsRepoPollInterval. AppsRepoStatusPollInterval=%d, expected=%d", appFrameworkContext.AppsRepoStatusPollInterval, splcommon.MinAppsRepoPollInterval)
	}

	// Check for maxAppsRepoPollInterval
	AppFramework.AppsRepoPollInterval = splcommon.MaxAppsRepoPollInterval + 1
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("Got error on valid App Framework configuration. Error: %v", err)
	} else if appFrameworkContext.AppsRepoStatusPollInterval != splcommon.MaxAppsRepoPollInterval {
		t.Errorf("Spec validation is not able to set the the AppsRepoPollInterval to maxAppsRepoPollInterval. AppsRepoStatusPollInterval=%d, expected=%d", appFrameworkContext.AppsRepoStatusPollInterval, splcommon.MaxAppsRepoPollInterval)
	}

	// Invalid volume name in defaults should return an error
	AppFramework.Defaults.VolName = "unknownVolume"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.HasPrefix(err.Error(), "invalid Volume Name for Defaults") {
		t.Errorf("Configuring Defaults with invalid volume name should return an error, but failed to detect")
	}

	AppFramework.Defaults.VolName = "msos_s2s3_vol"
	// Invalid remote volume type should return error.
	AppFramework.VolList[0].Type = "s4"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.Contains(err.Error(), "storageType 's4' is invalid. Valid values are 's3' and 'blob'") {
		t.Errorf("ValidateAppFrameworkSpec with invalid remote volume type should have returned error.")
	}

	AppFramework.VolList[0].Type = "s3"
	AppFramework.VolList[0].Provider = "invalid-provider"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.Contains(err.Error(), "provider 'invalid-provider' is invalid. Valid values are 'aws', 'minio' and 'azure'") {
		t.Errorf("ValidateAppFrameworkSpec with invalid provider should have returned error.")
	}

	// Validate s3 and azure are not right combination
	AppFramework.VolList[0].Type = "s3"
	AppFramework.VolList[0].Provider = "azure"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.Contains(err.Error(), "storageType 's3' cannot be used with provider 'azure'. Valid combinations are (s3,aws), (s3,minio) and (blob,azure)") {
		t.Errorf("ValidateAppFrameworkSpec with s3 and azure combination should have returned error.")
	}

	// Validate blob and azure are right combination
	AppFramework.VolList[0].Type = "blob"
	AppFramework.VolList[0].Provider = "azure"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("ValidateAppFrameworkSpec with blob and azure combination should not have returned error.")
	}

	// Validate s3 and aws are right combination
	AppFramework.VolList[0].Type = "s3"
	AppFramework.VolList[0].Provider = "aws"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("ValidateAppFrameworkSpec with s3 and aws combination should not have returned error.")
	}

	// Validate s3 and aws are right combination
	AppFramework.VolList[0].Type = "s3"
	AppFramework.VolList[0].Provider = "minio"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err != nil {
		t.Errorf("ValidateAppFrameworkSpec with s3 and minio combination should not have returned error.")
	}

	// Validate blob and aws are not right combination
	AppFramework.VolList[0].Type = "blob"
	AppFramework.VolList[0].Provider = "aws"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.Contains(err.Error(), "storageType 'blob' cannot be used with provider 'aws'. Valid combinations are (s3,aws), (s3,minio) and (blob,azure)") {
		t.Errorf("ValidateAppFrameworkSpec with blob and aws combination should have returned error.")
	}

	// Validate blob and minio are not right combination
	AppFramework.VolList[0].Type = "blob"
	AppFramework.VolList[0].Provider = "minio"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false)
	if err == nil || !strings.Contains(err.Error(), "storageType 'blob' cannot be used with provider 'minio'. Valid combinations are (s3,aws), (s3,minio) and (blob,azure)") {
		t.Errorf("ValidateAppFrameworkSpec with blob and minio combination should have returned error.")
	}
}

func TestGetSmartstoreIndexesConfig(t *testing.T) {
	SmartStoreIndexes := enterpriseApi.SmartStoreSpec{
		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					MaxGlobalDataSizeMB:    6000,
					MaxGlobalRawDataSizeMB: 7000,
					VolName:                "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", // Missing RemotePath should be filled with the default "$_index_name"
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					MaxGlobalDataSizeMB:    2000,
					MaxGlobalRawDataSizeMB: 3000,
					VolName:                "msos_s2s3_vol"},
				IndexAndCacheManagerCommonSpec: enterpriseApi.IndexAndCacheManagerCommonSpec{
					HotlistBloomFilterRecencyHours: 48,
					HotlistRecencySecs:             48 * 60 * 60},
			},
			{Name: "salesdata4", // Missing RemotePath should be filled with the default "$_index_name"
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					MaxGlobalDataSizeMB:    4000,
					MaxGlobalRawDataSizeMB: 5000,
					VolName:                "msos_s2s3_vol"},
				IndexAndCacheManagerCommonSpec: enterpriseApi.IndexAndCacheManagerCommonSpec{
					HotlistBloomFilterRecencyHours: 24,
					HotlistRecencySecs:             24 * 60 * 60},
			},
		},
	}

	expectedINIFormatString := fmt.Sprintf(`
[salesdata1]
remotePath = volume:msos_s2s3_vol/remotepath1
maxGlobalDataSizeMB = 6000
maxGlobalRawDataSizeMB = 7000

[salesdata2]
remotePath = volume:msos_s2s3_vol/remotepath2

[salesdata3]
remotePath = volume:msos_s2s3_vol/$_index_name
hotlist_bloom_filter_recency_hours = 48
hotlist_recency_secs = 172800
maxGlobalDataSizeMB = 2000
maxGlobalRawDataSizeMB = 3000

[salesdata4]
remotePath = volume:msos_s2s3_vol/$_index_name
hotlist_bloom_filter_recency_hours = 24
hotlist_recency_secs = 86400
maxGlobalDataSizeMB = 4000
maxGlobalRawDataSizeMB = 5000
`)

	indexesConfIni := GetSmartstoreIndexesConfig(SmartStoreIndexes.IndexList)
	if indexesConfIni != expectedINIFormatString {
		t.Errorf("expected: %s, returned: %s", expectedINIFormatString, indexesConfIni)
	}
}
func TestGetServerConfigEntries(t *testing.T) {

	SmartStoreCacheManager := enterpriseApi.CacheManagerSpec{
		IndexAndCacheManagerCommonSpec: enterpriseApi.IndexAndCacheManagerCommonSpec{
			HotlistRecencySecs:             24 * 60 * 60,
			HotlistBloomFilterRecencyHours: 24,
		},
		MaxCacheSizeMB:         20 * 1024,
		EvictionPolicy:         "lru",
		EvictionPaddingSizeMB:  2 * 1024,
		MaxConcurrentDownloads: 6,
		MaxConcurrentUploads:   6,
	}

	// Do not change the format
	expectedIniContents := fmt.Sprintf(`[cachemanager]
eviction_padding = 2048
eviction_policy = lru
hotlist_bloom_filter_recency_hours = 24
hotlist_recency_secs = 86400
max_cache_size = 20480
max_concurrent_downloads = 6
max_concurrent_uploads = 6
`)

	serverConfForCacheManager := GetServerConfigEntries(&SmartStoreCacheManager)

	if expectedIniContents != serverConfForCacheManager {
		t.Errorf("Expected: %s \n Received: %s", expectedIniContents, serverConfForCacheManager)
	}

	// Empty config should return empty string
	serverConfForCacheManager = GetServerConfigEntries(nil)
	if serverConfForCacheManager != "" {
		t.Errorf("Expected empty string, but received: %s", serverConfForCacheManager)
	}

}

func TestGetSmartstoreIndexesDefaults(t *testing.T) {

	SmartStoreDefaultsConf := enterpriseApi.IndexConfDefaultsSpec{
		IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
			VolName:                "s2s3_vol",
			MaxGlobalDataSizeMB:    50 * 1024,
			MaxGlobalRawDataSizeMB: 60 * 1024,
		},
	}

	// Do not change the format
	expectedIniContents := fmt.Sprintf(`[default]
repFactor = auto
maxDataSize = auto
homePath = $SPLUNK_DB/$_index_name/db
coldPath = $SPLUNK_DB/$_index_name/colddb
thawedPath = $SPLUNK_DB/$_index_name/thaweddb
remotePath = volume:s2s3_vol/$_index_name
maxGlobalDataSizeMB = 51200
maxGlobalRawDataSizeMB = 61440
`)

	SmartstoreDefaultIniConfig := GetSmartstoreIndexesDefaults(SmartStoreDefaultsConf)

	if expectedIniContents != SmartstoreDefaultIniConfig {
		t.Errorf("Expected: %s \n Received: %s", expectedIniContents, SmartstoreDefaultIniConfig)
	}

}

func TestAreRemoteVolumeKeysChanged(t *testing.T) {

	ctx := context.TODO()
	cr := enterpriseApi.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "CM",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterMasterSpec{
			SmartStore: enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
				},

				IndexList: []enterpriseApi.IndexSpec{
					{Name: "salesdata1", RemotePath: "remotepath1", IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "remotepath2", IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata3", RemotePath: "remotepath3", IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()
	var err error
	ResourceRev := map[string]string{
		"secret1": "12345",
		"secret2": "67890",
	}

	// Missing secret object should return an error
	keysChanged := AreRemoteVolumeKeysChanged(ctx, client, &cr, SplunkClusterManager, &cr.Spec.SmartStore, ResourceRev, &err)
	if err == nil {
		t.Errorf("Missing secret object should return an error. keyChangedFlag: %t", keysChanged)
	} else if keysChanged {
		t.Errorf("When the S3 secret object is not present, Key change should not be reported")
	}

	// First time secret version reference should be updated
	// Just to simplify the test, assume that the keys are stored as part of the splunk-test-scret
	secret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	_, err = splctrl.ApplySecret(ctx, client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	_ = AreRemoteVolumeKeysChanged(ctx, client, &cr, SplunkClusterManager, &cr.Spec.SmartStore, ResourceRev, &err)

	_, ok := ResourceRev["splunk-test-secret"]
	if !ok {
		t.Errorf("Failed to update the Resource Version for first time")
	}

	// Change the Resource Version, and see if that is being detected
	resourceVersion := "3434"
	secret.SetResourceVersion(resourceVersion)

	keysChanged = AreRemoteVolumeKeysChanged(ctx, client, &cr, SplunkClusterManager, &cr.Spec.SmartStore, ResourceRev, &err)
	resourceVersionUpdated := ResourceRev["splunk-test-secret"]
	if !keysChanged || resourceVersion != resourceVersionUpdated {
		t.Errorf("Failed detect the secret object change. Key changed: %t, Expected resource version: %s, Updated resource version %s", keysChanged, resourceVersion, resourceVersionUpdated)
	}

	// No change on the secret object should return false
	keysChanged = AreRemoteVolumeKeysChanged(ctx, client, &cr, SplunkClusterManager, &cr.Spec.SmartStore, ResourceRev, &err)
	resourceVersionUpdated, ok = ResourceRev["splunk-test-secret"]
	if keysChanged {
		t.Errorf("If there is no change on secret object, should return false")
	}

	// Empty volume list should return false
	cr.Spec.SmartStore.VolList = nil
	keysChanged = AreRemoteVolumeKeysChanged(ctx, client, &cr, SplunkClusterManager, &cr.Spec.SmartStore, ResourceRev, &err)
	if keysChanged {
		t.Errorf("Empty volume should not report a key change")
	}
}

func TestAddStorageVolumes(t *testing.T) {
	labels := make(map[string]string)
	var replicas int32 = 1

	// Create CR
	cr := enterpriseApi.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "CM",
			Namespace: "test",
		},
	}

	// create statefulset configuration
	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: "test",
							Name:  "splunk",
						},
					},
				},
			},
		},
	}

	// Default spec
	spec := &enterpriseApi.CommonSplunkSpec{}

	test := func(want string) {
		ss := statefulSet.DeepCopy()
		err := addStorageVolumes(&cr, spec, ss, labels)
		if err != nil {
			t.Errorf("Unable to add storage volumes, error: %s", err.Error())
		}
		marshalAndCompare(t, ss, "TestAddStorageVolumes", want)
	}

	// Test defaults - PVCs for etc & var with 10Gi and 100Gi storage capacity
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"test-statefulset","namespace":"test","creationTimestamp":null},"spec":{"replicas":1,"selector":null,"template":{"metadata":{"creationTimestamp":null},"spec":{"containers":[{"name":"splunk","image":"test","resources":{},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"}]}]}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"","updateStrategy":{}},"status":{"availableReplicas":0, "replicas":0}}`)

	// Define PVCs for etc & var with storage capacity and storage class name defined
	spec = &enterpriseApi.CommonSplunkSpec{
		EtcVolumeStorageConfig: enterpriseApi.StorageClassSpec{
			StorageCapacity:  "25Gi",
			StorageClassName: "gp2",
		},
		VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{
			StorageCapacity:  "35Gi",
			StorageClassName: "gp3",
		},
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"test-statefulset","namespace":"test","creationTimestamp":null},"spec":{"replicas":1,"selector":null,"template":{"metadata":{"creationTimestamp":null},"spec":{"containers":[{"name":"splunk","image":"test","resources":{},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"}]}]}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"25Gi"}},"storageClassName":"gp2"},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"35Gi"}},"storageClassName":"gp3"},"status":{}}],"serviceName":"","updateStrategy":{}},"status":{"availableReplicas":0, "replicas":0}}`)

	// Define PVCs for etc & ephemeral for var
	spec = &enterpriseApi.CommonSplunkSpec{
		EtcVolumeStorageConfig: enterpriseApi.StorageClassSpec{
			StorageCapacity:  "25Gi",
			StorageClassName: "gp2",
		},
		VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{
			EphemeralStorage: true,
		},
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"test-statefulset","namespace":"test","creationTimestamp":null},"spec":{"replicas":1,"selector":null,"template":{"metadata":{"creationTimestamp":null},"spec":{"volumes":[{"name":"mnt-splunk-var","emptyDir":{}}],"containers":[{"name":"splunk","image":"test","resources":{},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"mnt-splunk-var","mountPath":"/opt/splunk/var"}]}]}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"25Gi"}},"storageClassName":"gp2"},"status":{}}],"serviceName":"","updateStrategy":{}},"status":{"availableReplicas":0, "replicas":0}}`)

	// Define ephemeral for etc & PVCs for var
	spec = &enterpriseApi.CommonSplunkSpec{
		EtcVolumeStorageConfig: enterpriseApi.StorageClassSpec{
			EphemeralStorage: true,
		},
		VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{
			StorageCapacity:  "25Gi",
			StorageClassName: "gp2",
		},
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"test-statefulset","namespace":"test","creationTimestamp":null},"spec":{"replicas":1,"selector":null,"template":{"metadata":{"creationTimestamp":null},"spec":{"volumes":[{"name":"mnt-splunk-etc","emptyDir":{}}],"containers":[{"name":"splunk","image":"test","resources":{},"volumeMounts":[{"name":"mnt-splunk-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"}]}]}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"25Gi"}},"storageClassName":"gp2"},"status":{}}],"serviceName":"","updateStrategy":{}},"status":{"availableReplicas":0, "replicas":0}}`)

	// Define ephemeral for etc & var(should ignore storage capacity & storage class name)
	spec = &enterpriseApi.CommonSplunkSpec{
		EtcVolumeStorageConfig: enterpriseApi.StorageClassSpec{
			EphemeralStorage: true,
		},
		VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{
			EphemeralStorage: true,
			StorageCapacity:  "25Gi",
			StorageClassName: "gp2",
		},
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"test-statefulset","namespace":"test","creationTimestamp":null},"spec":{"replicas":1,"selector":null,"template":{"metadata":{"creationTimestamp":null},"spec":{"volumes":[{"name":"mnt-splunk-etc","emptyDir":{}},{"name":"mnt-splunk-var","emptyDir":{}}],"containers":[{"name":"splunk","image":"test","resources":{},"volumeMounts":[{"name":"mnt-splunk-etc","mountPath":"/opt/splunk/etc"},{"name":"mnt-splunk-var","mountPath":"/opt/splunk/var"}]}]}},"serviceName":"","updateStrategy":{}},"status":{"availableReplicas":0, "replicas":0}}`)

	// Define invalid EtcVolumeStorageConfig
	spec = &enterpriseApi.CommonSplunkSpec{
		EtcVolumeStorageConfig: enterpriseApi.StorageClassSpec{
			StorageCapacity: "----",
		},
	}
	err := addStorageVolumes(&cr, spec, statefulSet, labels)
	if err == nil {
		t.Errorf("Unable to idenitfy incorrect EtcVolumeStorageConfig resource quantity")
	}

	// Define invalid VarVolumeStorageConfig
	spec = &enterpriseApi.CommonSplunkSpec{
		VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{
			StorageCapacity: "----",
		},
	}
	err = addStorageVolumes(&cr, spec, statefulSet, labels)
	if err == nil {
		t.Errorf("Unable to idenitfy incorrect VarVolumeStorageConfig resource quantity")
	}

}

func TestGetVolumeSourceMountFromConfigMapData(t *testing.T) {
	var configMapName = "testConfgMap"
	var namespace = "testNameSpace"

	dataMap := make(map[string]string)
	dataMap["a"] = "x"
	dataMap["b"] = "y"
	dataMap["z"] = "z"
	cm := splctrl.PrepareConfigMap(configMapName, namespace, dataMap)
	var mode int32 = 755

	test := func(cm *corev1.ConfigMap, mode *int32, want string) {
		f := func() (interface{}, error) {
			return getVolumeSourceMountFromConfigMapData(cm, mode), nil
		}
		configTester(t, "getVolumeSourceMountFromConfigMapData()", f, want)

	}

	test(cm, &mode, `{"configMap":{"name":"testConfgMap","items":[{"key":"a","path":"a","mode":755},{"key":"b","path":"b","mode":755},{"key":"z","path":"z","mode":755}],"defaultMode":755}}`)
}

func TestGetLivenessProbe(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApi.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "CM",
			Namespace: "test",
		},
	}
	spec := &cr.Spec.CommonSplunkSpec

	// Test if default delay works always
	livenessProbe := getLivenessProbe(ctx, cr, SplunkClusterManager, spec, 0)
	if livenessProbe.InitialDelaySeconds != livenessProbeDefaultDelaySec {
		t.Errorf("Failed to set Liveness probe default delay")
	}

	// Test if the default delay can be overwritten with configured delay
	spec.LivenessInitialDelaySeconds = livenessProbeDefaultDelaySec + 10
	livenessProbe = getLivenessProbe(ctx, cr, SplunkClusterManager, spec, 0)
	if livenessProbe.InitialDelaySeconds != spec.LivenessInitialDelaySeconds {
		t.Errorf("Failed to set Liveness probe initial delay with configured value")
	}

	// Test if the additional Delay can override the default and the cofigured delay values
	livenessProbe = getLivenessProbe(ctx, cr, SplunkClusterManager, spec, 20)
	if livenessProbe.InitialDelaySeconds == livenessProbeDefaultDelaySec+20 {
		t.Errorf("Failed to set the configured Liveness probe initial delay value")
	}
}

func TestGetReadinessProbe(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApi.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "CM",
			Namespace: "test",
		},
	}
	spec := &cr.Spec.CommonSplunkSpec

	// Test if default delay works always
	readinessProbe := getReadinessProbe(ctx, cr, SplunkClusterManager, spec, 0)
	if readinessProbe.InitialDelaySeconds != readinessProbeDefaultDelaySec {
		t.Errorf("Failed to set Readiness probe default delay")
	}

	// Test if the default delay can be overwritten with configured delay
	spec.ReadinessInitialDelaySeconds = readinessProbeDefaultDelaySec + 10
	readinessProbe = getReadinessProbe(ctx, cr, SplunkClusterManager, spec, 0)
	if readinessProbe.InitialDelaySeconds != spec.ReadinessInitialDelaySeconds {
		t.Errorf("Failed to set Readiness probe initial delay with configured value")
	}

	// Test if the additional Delay can override the default and the cofigured delay values
	readinessProbe = getReadinessProbe(ctx, cr, SplunkClusterManager, spec, 20)
	if readinessProbe.InitialDelaySeconds == readinessProbeDefaultDelaySec+20 {
		t.Errorf("Failed to set the configured Readiness probe initial delay value")
	}
}

func TestGetProbe(t *testing.T) {

	command := []string{
		"grep",
		"ready",
		"file.txt",
	}

	test := func(command []string, delay, timeout, period int32, want string) {
		f := func() (interface{}, error) {
			return getProbe(command, delay, timeout, period), nil
		}
		configTester(t, "getProbe()", f, want)

	}

	test(command, 100, 10, 10, `{"exec":{"command":["grep","ready","file.txt"]},"initialDelaySeconds":100,"timeoutSeconds":10,"periodSeconds":10}`)
}

func TestCreateOrUpdateAppUpdateConfigMapShouldNotFail(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "standalone",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret", Type: "s3", Provider: "aws"},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "adminApps",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "securityApps",
						Location: "securityAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "authenticationApps",
						Location: "authenticationAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	// now create another standalone type
	revised := cr
	revised.ObjectMeta.Name = "standalone2"

	// Create the configMap
	configMap, err := createOrUpdateAppUpdateConfigMap(ctx, client, &cr)
	if err != nil {
		t.Errorf("manual app update configMap should have been created successfully")
	}

	configMapName := configMap.Name
	// check the status and refCount
	refCount := getManualUpdateRefCount(ctx, client, &cr, configMapName)
	status := getManualUpdateStatus(ctx, client, &cr, configMapName)
	if refCount != 1 || status != "off" {
		t.Errorf("Got wrong status or/and refCount. Expected status=off, Got=%s. Expected refCount=1, Got=%d", status, refCount)
	}

	// update the configMap
	_, err = createOrUpdateAppUpdateConfigMap(ctx, client, &revised)
	if err != nil {
		t.Errorf("manual app update configMap should have been created successfully")
	}

	// check the status and refCount
	refCount = getManualUpdateRefCount(ctx, client, &revised, configMapName)
	status = getManualUpdateStatus(ctx, client, &revised, configMapName)
	if refCount != 2 || status != "off" {
		t.Errorf("Got wrong status or/and refCount. Expected status=off, Got=%s. Expected refCount=2, Got=%d", status, refCount)
	}
}

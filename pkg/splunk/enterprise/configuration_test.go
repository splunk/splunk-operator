// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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
	"encoding/json"
	"fmt"
	"testing"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func configTester(t *testing.T, method string, f func() (interface{}, error), want string) {
	result, err := f()
	if err != nil {
		t.Errorf("%s returned error: %v", method, err)
	}
	got, err := json.Marshal(result)
	if err != nil {
		t.Errorf("%s failed to marshall: %v", method, err)
	}
	if string(got) != want {
		t.Errorf("%s = %s;\nwant %s", method, got, want)
	}
}

func TestGetSplunkService(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(instanceType InstanceType, isHeadless bool, want string) {
		f := func() (interface{}, error) {
			return getSplunkService(&cr, &cr.Spec.CommonSplunkSpec, instanceType, isHeadless), nil
		}
		configTester(t, fmt.Sprintf("getSplunkService(\"%s\",%t)", instanceType, isHeadless), f, want)
	}

	test(SplunkIndexer, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-service","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb-http","protocol":"TCP","port":8000,"targetPort":8000},{"name":"hec-https","protocol":"TCP","port":8088,"targetPort":8088},{"name":"splunkd-https","protocol":"TCP","port":8089,"targetPort":8089},{"name":"s2s-tcp","protocol":"TCP","port":9997,"targetPort":9997}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"status":{"loadBalancer":{}}}`)
	test(SplunkIndexer, true, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-headless","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb-http","protocol":"TCP","port":8000,"targetPort":8000},{"name":"hec-https","protocol":"TCP","port":8088,"targetPort":8088},{"name":"splunkd-https","protocol":"TCP","port":8089,"targetPort":8089},{"name":"s2s-tcp","protocol":"TCP","port":9997,"targetPort":9997}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"clusterIP":"None","type":"ClusterIP"},"status":{"loadBalancer":{}}}`)
	// Multipart IndexerCluster - test part-of and instance labels for child part
	cr.Spec.ClusterMasterRef.Name = "cluster1"
	test(SplunkIndexer, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-service","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-cluster1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb-http","protocol":"TCP","port":8000,"targetPort":8000},{"name":"hec-https","protocol":"TCP","port":8088,"targetPort":8088},{"name":"splunkd-https","protocol":"TCP","port":8089,"targetPort":8089},{"name":"s2s-tcp","protocol":"TCP","port":9997,"targetPort":9997}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-cluster1-indexer"}},"status":{"loadBalancer":{}}}`)
	cr.Spec.ClusterMasterRef.Name = ""

	cr.Spec.ServiceTemplate.Spec.Type = "LoadBalancer"
	cr.Spec.ServiceTemplate.ObjectMeta.Labels = map[string]string{"1": "2"}
	cr.ObjectMeta.Labels = map[string]string{"one": "two"}
	cr.ObjectMeta.Annotations = map[string]string{"a": "b"}

	test(SplunkSearchHead, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-search-head-service","namespace":"test","creationTimestamp":null,"labels":{"1":"2","app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head","one":"two"},"annotations":{"a":"b"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb-http","protocol":"TCP","port":8000,"targetPort":8000},{"name":"splunkd-https","protocol":"TCP","port":8089,"targetPort":8089},{"name":"dfsmaster","protocol":"TCP","port":9000,"targetPort":9000},{"name":"dfccontrol","protocol":"TCP","port":17000,"targetPort":17000},{"name":"datareceive","protocol":"TCP","port":19000,"targetPort":19000}],"selector":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"type":"LoadBalancer"},"status":{"loadBalancer":{}}}`)
	test(SplunkSearchHead, true, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-search-head-headless","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head","one":"two"},"annotations":{"a":"b"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb-http","protocol":"TCP","port":8000,"targetPort":8000},{"name":"splunkd-https","protocol":"TCP","port":8089,"targetPort":8089},{"name":"dfsmaster","protocol":"TCP","port":9000,"targetPort":9000},{"name":"dfccontrol","protocol":"TCP","port":17000,"targetPort":17000},{"name":"datareceive","protocol":"TCP","port":19000,"targetPort":19000}],"selector":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"clusterIP":"None","type":"ClusterIP","publishNotReadyAddresses":true},"status":{"loadBalancer":{}}}`)
}

func TestGetSplunkDefaults(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.IndexerClusterSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{Defaults: "defaults_string"},
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
	cr := enterprisev1.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.IndexerClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				Spec: splcommon.Spec{
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
			return getSplunkService(&cr, &cr.Spec.CommonSplunkSpec, instanceType, false), nil
		}
		configTester(t, "getSplunkService()", f, want)
	}

	test(SplunkIndexer, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-service","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"user-defined","port":32000,"targetPort":6443},{"name":"splunkweb-http","protocol":"TCP","port":8000,"targetPort":8000},{"name":"hec-https","protocol":"TCP","port":8088,"targetPort":8088},{"name":"splunkd-https","protocol":"TCP","port":8089,"targetPort":8089},{"name":"s2s-tcp","protocol":"TCP","port":9997,"targetPort":9997}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"status":{"loadBalancer":{}}}`)
}

func TestSetVolumeDefault(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
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

func TestSmartstoreApplyClusterMasterFailsOnInvalidSmartStoreConfig(t *testing.T) {
	cr := enterprisev1.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idxCluster",
			Namespace: "test",
		},
		Spec: enterprisev1.ClusterMasterSpec{
			SmartStore: enterprisev1.SmartStoreSpec{
				VolList: []enterprisev1.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "", Path: "testbucket-rs-london"},
				},

				IndexList: []enterprisev1.IndexSpec{
					{Name: "salesdata1"},
					{Name: "salesdata2", RemotePath: "salesdata2"},
					{Name: "salesdata3", RemotePath: ""},
				},
			},
		},
	}

	var client splcommon.ControllerClient

	_, err := ApplyClusterMaster(client, &cr)
	if err == nil {
		t.Errorf("ApplyClusterMaster should fail on invalid smartstore config")
	}
}

func TestSmartstoreApplyStandaloneFailsOnInvalidSmartStoreConfig(t *testing.T) {
	cr := enterprisev1.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "standalone",
			Namespace: "test",
		},
		Spec: enterprisev1.StandaloneSpec{
			Replicas: 1,
			SmartStore: enterprisev1.SmartStoreSpec{
				VolList: []enterprisev1.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "", Path: "testbucket-rs-london"},
				},
				IndexList: []enterprisev1.IndexSpec{
					{Name: "salesdata1",
						IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "salesdata2"},
					{Name: "salesdata3", RemotePath: ""},
				},
			},
		},
	}

	var client splcommon.ControllerClient

	_, err := ApplyStandalone(client, &cr)
	if err == nil {
		t.Errorf("ApplyStandalone should fail on invalid smartstore config")
	}
}

func TestSmartStoreConfigDoesNotFailOnClusterMasterCR(t *testing.T) {
	cr := enterprisev1.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "CM",
			Namespace: "test",
		},
		Spec: enterprisev1.ClusterMasterSpec{
			SmartStore: enterprisev1.SmartStoreSpec{
				VolList: []enterprisev1.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
				},

				IndexList: []enterprisev1.IndexSpec{
					{Name: "salesdata1", RemotePath: "remotepath1", IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "remotepath2", IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata3", RemotePath: "remotepath3", IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
				},
			},
		},
	}

	err := validateClusterMasterSpec(&cr)

	if err != nil {
		t.Errorf("Smartstore configuration should not fail on ClusterMaster CR: %v", err)
	}
}

func TestValidateSplunkSmartstoreSpec(t *testing.T) {
	var err error

	// Valid smartstore config
	SmartStore := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterprisev1.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStore)
	if err != nil {
		t.Errorf("Valid Smartstore configuration should not cause error: %v", err)
	}

	// Missing Secret object reference with Volume config should fail
	SmartStoreMultipleVolumes := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol_1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london"},
			{Name: "msos_s2s3_vol_2", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret2"},
		},
		IndexList: []enterprisev1.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreMultipleVolumes)
	if err == nil {
		t.Errorf("Missing Secret Object reference should error out")
	}

	// Smartstore config with missing endpoint for the volume errors out
	SmartStoreVolumeWithNoRemoteEndPoint := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "", Path: "testbucket-rs-london"},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreVolumeWithNoRemoteEndPoint)
	if err == nil {
		t.Errorf("Should not accept a volume with missing Endpoint")
	}

	// Smartstore config with missing remote name for the volume
	SmartStoreWithVolumeNameMissing := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london"},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreWithVolumeNameMissing)
	if err == nil {
		t.Errorf("Should not accept a volume with missing Remotename")
	}

	// Smartstore config with missing path for the volume
	SmartStoreWithVolumePathMissing := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: ""},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreWithVolumePathMissing)
	if err == nil {
		t.Errorf("Should not accept a volume with missing Remote Path")
	}

	// Smartstore config with missing index name
	SmartStoreWithMissingIndexName := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterprisev1.IndexSpec{
			{Name: "",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreWithMissingIndexName)
	if err == nil {
		t.Errorf("Should not accept an Index with missing indexname ")
	}

	//Smartstore config Index with VolName, but missing RemotePath errors out
	SmartStoreWithMissingIndexLocation := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterprisev1.IndexSpec{
			{Name: "salesdata1",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreWithMissingIndexLocation)
	if err != nil {
		t.Errorf("An index with missing remotePath should use index name as path, but failed with error: %v", err)
	}

	// Having defaults volume and remote path should not complain an index missing the volume and remotepath info.
	SmartStoreConfWithDefaults := enterprisev1.SmartStoreSpec{
		Defaults: enterprisev1.IndexConfDefaultsSpec{
			IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
				VolName: "msos_s2s3_vol"},
		},
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterprisev1.IndexSpec{
			{Name: "salesdata1"},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3"},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreConfWithDefaults)
	if err != nil {
		t.Errorf("Should accept an Index with missing remotePath location, when defaults are configured. But, got the error: %v", err)
	}

	// Empty smartstore config
	err = ValidateSplunkSmartstoreSpec(nil)
	if err != nil {
		t.Errorf("Smartstore config is optional, should not cause an error. But, got the error: %v", err)
	}

	// Configuring indexes without volume config should return error
	SmartStoreWithoutVolumes := enterprisev1.SmartStoreSpec{
		IndexList: []enterprisev1.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreWithoutVolumes)
	if err == nil {
		t.Errorf("Smartstore config without volume details should return error")
	}

	// Duplicate volume names should be rejected
	SmartStoreWithDuplicateVolumes := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol-1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
			{Name: "msos_s2s3_vol-2", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
			{Name: "msos_s2s3_vol-1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterprisev1.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreWithDuplicateVolumes)
	if err == nil {
		t.Errorf("Duplicate volume configuration should return an error")
	}

	// Defaults with invalid volume reference should return error
	SmartStoreDefaultsWithNonExistingVolume := enterprisev1.SmartStoreSpec{
		Defaults: enterprisev1.IndexConfDefaultsSpec{
			IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
				VolName: "msos_s2s3_vol-2"},
		},
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol-1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreDefaultsWithNonExistingVolume)
	if err == nil {
		t.Errorf("Volume referred in the indexes defaults should be a valid volume")
	}

	//Duplicate index names should return an error
	SmartStoreWithDuplicateIndexes := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterprisev1.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata1", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreWithDuplicateIndexes)
	if err == nil {
		t.Errorf("Duplicate index names should return an error")
	}

	// If the default volume is not configured, then each index should be configured
	// with an explicit volume info. If not, should return an error
	SmartStoreVolumeMissingBothFromDefaultsAndIndex := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol-1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterprisev1.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1"},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreVolumeMissingBothFromDefaultsAndIndex)
	if err == nil {
		t.Errorf("If no default volume, index with missing volume info should return an error")
	}

	// Volume referenced from an index must be a valid volume
	SmartStoreIndexesWithInvalidVolumeName := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol-1", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterprisev1.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol-2"},
			},
		},
	}

	err = ValidateSplunkSmartstoreSpec(&SmartStoreIndexesWithInvalidVolumeName)
	if err == nil {
		t.Errorf("Index with an invalid volume name should return error")
	}
}

func TestGetSmartstoreIndexesConfig(t *testing.T) {
	SmartStoreIndexes := enterprisev1.SmartStoreSpec{
		IndexList: []enterprisev1.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					MaxGlobalDataSizeMB:    6000,
					MaxGlobalRawDataSizeMB: 7000,
					VolName:                "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", // Missing RemotePath should be filled with the default "$_index_name"
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					MaxGlobalDataSizeMB:    2000,
					MaxGlobalRawDataSizeMB: 3000,
					VolName:                "msos_s2s3_vol"},
				IndexAndCacheManagerCommonSpec: enterprisev1.IndexAndCacheManagerCommonSpec{
					HotlistBloomFilterRecencyHours: 48,
					HotlistRecencySecs:             48 * 60 * 60},
			},
			{Name: "salesdata4", // Missing RemotePath should be filled with the default "$_index_name"
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					MaxGlobalDataSizeMB:    4000,
					MaxGlobalRawDataSizeMB: 5000,
					VolName:                "msos_s2s3_vol"},
				IndexAndCacheManagerCommonSpec: enterprisev1.IndexAndCacheManagerCommonSpec{
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

	SmartStoreCacheManager := enterprisev1.CacheManagerSpec{
		IndexAndCacheManagerCommonSpec: enterprisev1.IndexAndCacheManagerCommonSpec{
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

	SmartStoreDefaultsConf := enterprisev1.IndexConfDefaultsSpec{
		IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
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

func TestCheckIfVolumeExists(t *testing.T) {
	SmartStoreConfig := enterprisev1.SmartStoreSpec{
		VolList: []enterprisev1.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
		},
		IndexList: []enterprisev1.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	// Volume that doesn't should error out
	_, err := checkIfVolumeExists(SmartStoreConfig.VolList, "random_volume_name")

	if err == nil {
		t.Errorf("if the volume doesn't exists, error should be reported")
	}

	// Volume that exists should not error out
	index := len(SmartStoreConfig.VolList) - 1
	returnedIndex, err := checkIfVolumeExists(SmartStoreConfig.VolList, SmartStoreConfig.VolList[index].Name)

	if err != nil {
		t.Errorf("existing volume should not error out. index id: %d, error: %s", index, err.Error())
	} else if index != returnedIndex {
		t.Errorf("Expected index: %d, but returned index id: %d", index, returnedIndex)
	}
}

func TestAreRemoteVolumeKeysChanged(t *testing.T) {
	cr := enterprisev1.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "CM",
			Namespace: "test",
		},
		Spec: enterprisev1.ClusterMasterSpec{
			SmartStore: enterprisev1.SmartStoreSpec{
				VolList: []enterprisev1.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
				},

				IndexList: []enterprisev1.IndexSpec{
					{Name: "salesdata1", RemotePath: "remotepath1", IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "remotepath2", IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata3", RemotePath: "remotepath3", IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
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
	keysChanged := AreRemoteVolumeKeysChanged(client, &cr, SplunkClusterMaster, &cr.Spec.SmartStore, ResourceRev, &err)
	if err == nil {
		t.Errorf("Missing secret object should return an error. keyChangedFlag: %t", keysChanged)
	} else if keysChanged {
		t.Errorf("When the S3 secret object is not present, Key change should not be reported")
	}

	// First time secret version reference should be updated
	// Just to simplify the test, assume that the keys are stored as part of the splunk-test-scret
	secret, err := splutil.ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	_, err = splctrl.ApplySecret(client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	keysChanged = AreRemoteVolumeKeysChanged(client, &cr, SplunkClusterMaster, &cr.Spec.SmartStore, ResourceRev, &err)

	_, ok := ResourceRev["splunk-test-secret"]
	if !ok {
		t.Errorf("Failed to update the Resource Version for first time")
	}

	// Change the Resource Version, and see if that is being detected
	resourceVersion := "3434"
	secret.SetResourceVersion(resourceVersion)

	keysChanged = AreRemoteVolumeKeysChanged(client, &cr, SplunkClusterMaster, &cr.Spec.SmartStore, ResourceRev, &err)
	resourceVersionUpdated, ok := ResourceRev["splunk-test-secret"]
	if !keysChanged || resourceVersion != resourceVersionUpdated {
		t.Errorf("Failed detect the secret object change. Key changed: %t, Expected resource version: %s, Updated resource version %s", keysChanged, resourceVersion, resourceVersionUpdated)
	}

	// No change on the secret object should return false
	keysChanged = AreRemoteVolumeKeysChanged(client, &cr, SplunkClusterMaster, &cr.Spec.SmartStore, ResourceRev, &err)
	resourceVersionUpdated, ok = ResourceRev["splunk-test-secret"]
	if keysChanged {
		t.Errorf("If there is no change on secret object, should return false")
	}

	// Empty volume list should return false
	cr.Spec.SmartStore.VolList = nil
	keysChanged = AreRemoteVolumeKeysChanged(client, &cr, SplunkClusterMaster, &cr.Spec.SmartStore, ResourceRev, &err)
	if keysChanged {
		t.Errorf("Empty volume should not report a key change")
	}
}

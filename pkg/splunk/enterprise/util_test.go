// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
	"fmt"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

func init() {
}

func TestApplySplunkConfig(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-search-head-defaults"},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[2]}, "Update": {funcCalls[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2]}}
	searchHeadCR := enterprisev1.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearcHead",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	searchHeadCR.Spec.Defaults = "defaults-yaml"
	searchHeadRevised := searchHeadCR.DeepCopy()
	searchHeadRevised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		obj := cr.(*enterprisev1.SearchHeadCluster)
		_, err := ApplySplunkConfig(c, obj, obj.Spec.CommonSplunkSpec, SplunkSearchHead)
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplySplunkConfig", &searchHeadCR, searchHeadRevised, createCalls, updateCalls, reconcile, false)

	// test search head with indexer reference
	searchHeadRevised.Spec.ClusterMasterRef.Name = "stack2"
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplySplunkConfig", &searchHeadCR, searchHeadRevised, createCalls, updateCalls, reconcile, false)

	// test indexer with license master
	indexerCR := enterprisev1.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	indexerRevised := indexerCR.DeepCopy()
	indexerRevised.Spec.Image = "splunk/test"
	indexerRevised.Spec.LicenseMasterRef.Name = "stack2"
	reconcile = func(c *spltest.MockClient, cr interface{}) error {
		obj := cr.(*enterprisev1.IndexerCluster)
		_, err := ApplySplunkConfig(c, obj, obj.Spec.CommonSplunkSpec, SplunkIndexer)
		return err
	}
	funcCalls = []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
	}
	createCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[0]}, "Create": funcCalls, "Update": {funcCalls[0]}}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[0]}}

	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplySplunkConfig", &indexerCR, indexerRevised, createCalls, updateCalls, reconcile, false)
}

func TestGetLicenseMasterURL(t *testing.T) {
	cr := enterprisev1.LicenseMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	cr.Spec.LicenseMasterRef.Name = "stack1"
	got := getLicenseMasterURL(&cr, &cr.Spec.CommonSplunkSpec)
	want := []corev1.EnvVar{
		{
			Name:  "SPLUNK_LICENSE_MASTER_URL",
			Value: "splunk-stack1-license-master-service",
		},
	}
	result := splcommon.CompareEnvs(got, want)
	//if differ then CompareEnvs returns true
	if result == true {
		t.Errorf("getLicenseMasterURL(\"%s\") = %s; want %s", SplunkLicenseMaster, got, want)
	}

	cr.Spec.LicenseMasterRef.Namespace = "test"
	got = getLicenseMasterURL(&cr, &cr.Spec.CommonSplunkSpec)
	want = []corev1.EnvVar{
		{
			Name:  "SPLUNK_LICENSE_MASTER_URL",
			Value: "splunk-stack1-license-master-service.test.svc.cluster.local",
		},
	}

	result = splcommon.CompareEnvs(got, want)
	//if differ then CompareEnvs returns true
	if result == true {
		t.Errorf("getLicenseMasterURL(\"%s\") = %s; want %s", SplunkLicenseMaster, got, want)
	}
}

func TestApplySmartstoreConfigMap(t *testing.T) {
	cr := enterprisev1.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idxCluster",
			Namespace: "test",
		},
		Spec: enterprisev1.ClusterMasterSpec{
			SmartStore: enterprisev1.SmartStoreSpec{
				VolList: []enterprisev1.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
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
			},
		},
	}

	client := spltest.NewMockClient()

	// Create namespace scoped secret
	secret, err := splutil.ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	secret.Data[s3AccessKey] = []byte("abcdJDckRkxhMEdmSk5FekFRRzBFOXV6bGNldzJSWE9IenhVUy80aa")
	secret.Data[s3SecretKey] = []byte("g4NVp0a29PTzlPdGczWk1vekVUcVBSa0o4NkhBWWMvR1NadDV4YVEy")
	_, err = splctrl.ApplySecret(client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	test := func(client *spltest.MockClient, cr splcommon.MetaObject, smartstore *enterprisev1.SmartStoreSpec, want string) {
		f := func() (interface{}, error) {
			configMap, _, err := ApplySmartstoreConfigMap(client, cr, smartstore)
			configMap.Data["conftoken"] = "1601945361"
			return configMap, err
		}
		configTester(t, "ApplySmartstoreConfigMap()", f, want)
	}

	test(client, &cr, &cr.Spec.SmartStore, `{"metadata":{"name":"splunk-idxCluster--smartstore","namespace":"test","creationTimestamp":null},"data":{"conftoken":"1601945361","indexes.conf":"[default]\nrepFactor = auto\nmaxDataSize = auto\nhomePath = $SPLUNK_DB/$_index_name/db\ncoldPath = $SPLUNK_DB/$_index_name/colddb\nthawedPath = $SPLUNK_DB/$_index_name/thaweddb\n \n[volume:msos_s2s3_vol]\nstorageType = remote\npath = s3://testbucket-rs-london\nremote.s3.access_key = abcdJDckRkxhMEdmSk5FekFRRzBFOXV6bGNldzJSWE9IenhVUy80aa\nremote.s3.secret_key = g4NVp0a29PTzlPdGczWk1vekVUcVBSa0o4NkhBWWMvR1NadDV4YVEy\nremote.s3.endpoint = https://s3-eu-west-2.amazonaws.com\n \n[salesdata1]\nremotePath = volume:msos_s2s3_vol/remotepath1\n\n[salesdata2]\nremotePath = volume:msos_s2s3_vol/remotepath2\n\n[salesdata3]\nremotePath = volume:msos_s2s3_vol/remotepath3\n","server.conf":""}}`)

	// Missing Volume config should return an error
	cr.Spec.SmartStore.VolList = nil
	_, _, err = ApplySmartstoreConfigMap(client, &cr, &cr.Spec.SmartStore)
	if err == nil {
		t.Errorf("Configuring Indexes without volumes should return an error")
	}
}

func TestRemoveOwenerReferencesForSecretObjectsReferredBySmartstoreVolumes(t *testing.T) {
	cr := enterprisev1.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idxCluster",
			Namespace: "test",
		},
		Spec: enterprisev1.ClusterMasterSpec{
			SmartStore: enterprisev1.SmartStoreSpec{
				VolList: []enterprisev1.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
					{Name: "msos_s2s3_vol_2", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
					{Name: "msos_s2s3_vol_3", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
					{Name: "msos_s2s3_vol_4", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
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
			},
		},
	}

	client := spltest.NewMockClient()

	// Create namespace scoped secret
	secret, err := splutil.ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	secret.Data[s3AccessKey] = []byte("abcdJDckRkxhMEdmSk5FekFRRzBFOXV6bGNldzJSWE9IenhVUy80aa")
	secret.Data[s3SecretKey] = []byte("g4NVp0a29PTzlPdGczWk1vekVUcVBSa0o4NkhBWWMvR1NadDV4YVEy")
	_, err = splctrl.ApplySecret(client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	// Test existing secret
	err = splutil.SetSecretOwnerRef(client, secret.GetName(), &cr)
	if err != nil {
		t.Errorf("Couldn't set owner ref for secret %s", secret.GetName())
	}

	err = DeleteOwnerReferencesForS3SecretObjects(client, secret, &cr.Spec.SmartStore)

	if err != nil {
		t.Errorf("Couldn't Remove S3 Secret object references %v", err)
	}

	// If the secret object doesn't exist, should return an error
	// Here in the volume references, secrets splunk-test-sec_1, to splunk-test-sec_4 doesn't exist
	cr = enterprisev1.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idxCluster",
			Namespace: "testWithNoSecret",
		},
		Spec: enterprisev1.ClusterMasterSpec{
			SmartStore: enterprisev1.SmartStoreSpec{
				VolList: []enterprisev1.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-sec_1"},
					{Name: "msos_s2s3_vol_2", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-sec_2"},
					{Name: "msos_s2s3_vol_3", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-sec_3"},
					{Name: "msos_s2s3_vol_4", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-sec_4"},
				},
			},
		},
	}

	// S3 secret owner reference removal, with non-existing secret objects
	err = DeleteOwnerReferencesForS3SecretObjects(client, secret, &cr.Spec.SmartStore)
	if err == nil {
		t.Errorf("Should report an error, when the secret object referenced in the volume config doesn't exist")
	}

	// Smartstore volume config with non-existing secret objects
	err = DeleteOwnerReferencesForResources(client, &cr, &cr.Spec.SmartStore)
	if err == nil {
		t.Errorf("Should report an error, when the secret objects doesn't exist")
	}
}

func TestGetSmartstoreRemoteVolumeSecrets(t *testing.T) {
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
			},
		},
	}

	client := spltest.NewMockClient()

	// Just to simplify the test, assume that the keys are stored as part of the splunk-test-secret object, hence create that secret object
	secret, err := splutil.ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	_, err = splctrl.ApplySecret(client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	// Missing S3 access key should return error
	_, _, _, err = GetSmartstoreRemoteVolumeSecrets(cr.Spec.SmartStore.VolList[0], client, &cr, &cr.Spec.SmartStore)
	if err == nil {
		t.Errorf("Missing S3 access key should return an error")
	}

	secret.Data[s3AccessKey] = []byte("abcdJDckRkxhMEdmSk5FekFRRzBFOXV6bGNldzJSWE9IenhVUy80aa")

	// Missing S3 secret key should return error
	_, _, _, err = GetSmartstoreRemoteVolumeSecrets(cr.Spec.SmartStore.VolList[0], client, &cr, &cr.Spec.SmartStore)
	if err == nil {
		t.Errorf("Missing S3 secret key should return an error")
	}

	// When access key and secret keys are present, returned keys should not be empty. Also, should not return an error
	secret.Data[s3SecretKey] = []byte("g4NVp0a29PTzlPdGczWk1vekVUcVBSa0o4NkhBWWMvR1NadDV4YVEy")
	accessKey, secretKey, _, err := GetSmartstoreRemoteVolumeSecrets(cr.Spec.SmartStore.VolList[0], client, &cr, &cr.Spec.SmartStore)
	if accessKey == "" || secretKey == "" || err != nil {
		t.Errorf("Missing S3 Keys / Error not expected, when the Secret object with the S3 specific keys are present")
	}
}

func TestCheckIfAnAppIsActiveOnRemoteStore(t *testing.T) {
	var remoteObjList []*splclient.RemoteObject
	var entry *splclient.RemoteObject

	tmpAppName := "xys.spl"
	entry = allocateRemoteObject("d41d8cd98f00", tmpAppName, 2322, nil)

	remoteObjList = append(remoteObjList, entry)

	if !checkIfAnAppIsActiveOnRemoteStore(tmpAppName, remoteObjList) {
		t.Errorf("Failed to detect for a valid app from remote listing")
	}

	if checkIfAnAppIsActiveOnRemoteStore("app10.tgz", remoteObjList) {
		t.Errorf("Non existing app is reported as existing")
	}

}

func TestHandleAppRepoChanges(t *testing.T) {
	cr := enterprisev1.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "Clustermaster",
			Namespace: "test",
		},
		Spec: enterprisev1.StandaloneSpec{
			Replicas: 1,
			AppFrameworkConfig: enterprisev1.AppFrameworkSpec{
				VolList: []enterprisev1.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
				},
				AppSources: []enterprisev1.AppSourceSpec{
					{Name: "adminApps",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterprisev1.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   "local"},
					},
					{Name: "securityApps",
						Location: "securityAppsRepo",
						AppSourceDefaultSpec: enterprisev1.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   "local"},
					},
					{Name: "authenticationApps",
						Location: "authenticationAppsRepo",
						AppSourceDefaultSpec: enterprisev1.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   "local"},
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	var appDeployContext enterprisev1.AppDeploymentContext
	var remoteObjListMap map[string]splclient.S3Response
	var appFramworkConf enterprisev1.AppFrameworkSpec = cr.Spec.AppFrameworkConfig
	var err error

	var S3Response splclient.S3Response

	// Test-1: Empty remoteObjectList Map should return an error
	err = handleAppRepoChanges(client, &cr, &appDeployContext, remoteObjListMap, &appFramworkConf)

	if err != nil {
		t.Errorf("Empty remote Object list should not trigger an error, but got error : %v", err)
	}

	// Test-2: Valid remoteObjectList should not cause an error
	startAppPathAndName := "bucketpath1/bpath2/locationpath1/lpath2/adminCategoryOne.tgz"
	remoteObjListMap = make(map[string]splclient.S3Response)
	// Prepare a S3Response
	S3Response.Objects = createRemoteObjectList("d41d8cd98f00", startAppPathAndName, 2322, nil, 10)
	// Set the app source with a matching one
	remoteObjListMap[appFramworkConf.AppSources[0].Name] = S3Response

	err = handleAppRepoChanges(client, &cr, &appDeployContext, remoteObjListMap, &appFramworkConf)
	if err != nil {
		t.Errorf("Could not handle a valid remote listing. Error: %v", err)
	}

	_, err = validateAppSrcDeployInfoByStateAndStatus(appFramworkConf.AppSources[0].Name, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateActive, enterprisev1.DeployStatusPending)
	if err != nil {
		t.Errorf("Unexpected app status. Error: %v", err)
	}

	// Test-3: If the App Resource is not found in the remote object listing, all the corresponding Apps should be deleted/disabled
	delete(remoteObjListMap, appFramworkConf.AppSources[0].Name)
	err = handleAppRepoChanges(client, &cr, &appDeployContext, remoteObjListMap, &appFramworkConf)
	if err != nil {
		t.Errorf("Could not handle a valid remote listing. Error: %v", err)
	}

	_, err = validateAppSrcDeployInfoByStateAndStatus(appFramworkConf.AppSources[0].Name, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusPending)
	if err != nil {
		t.Errorf("Unable to delete/disable Apps, when the AppSource is deleted. Unexpected app status. Error: %v", err)
	}
	setStateAndStatusForAppDeployInfoList(appDeployContext.AppsSrcDeployStatus[appFramworkConf.AppSources[0].Name].AppDeploymentInfoList, enterprisev1.RepoStateActive, enterprisev1.DeployStatusPending)

	// Test-4: If the App Resource is not found in the config, all the corresponding Apps should be deleted/disabled
	tmpAppSrcName := appFramworkConf.AppSources[0].Name
	appFramworkConf.AppSources[0].Name = "invalidName"
	err = handleAppRepoChanges(client, &cr, &appDeployContext, remoteObjListMap, &appFramworkConf)
	if err != nil {
		t.Errorf("Could not handle a valid remote listing. Error: %v", err)
	}
	appFramworkConf.AppSources[0].Name = tmpAppSrcName

	_, err = validateAppSrcDeployInfoByStateAndStatus(appFramworkConf.AppSources[0].Name, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusPending)
	if err != nil {
		t.Errorf("Unable to delete/disable Apps, when the AppSource is deleted from the config. Unexpected app status. Error: %v", err)
	}

	// Test-5: Changing the AppSource deployment info should change for all the Apps in the list
	changeAppSrcDeployInfoStatus(appFramworkConf.AppSources[0].Name, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusPending, enterprisev1.DeployStatusInProgress)
	_, err = validateAppSrcDeployInfoByStateAndStatus(appFramworkConf.AppSources[0].Name, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusInProgress)
	if err != nil {
		t.Errorf("Invalid AppSrc deployment info detected. Error: %v", err)
	}

	// Test-6: When an App is deleted on remote store, it should be marked as deleted
	setStateAndStatusForAppDeployInfoList(appDeployContext.AppsSrcDeployStatus[appFramworkConf.AppSources[0].Name].AppDeploymentInfoList, enterprisev1.RepoStateActive, enterprisev1.DeployStatusPending)

	// delete an object on remote store for the app source
	tmpS3Response := S3Response
	tmpS3Response.Objects = append(tmpS3Response.Objects[:0], tmpS3Response.Objects[1:]...)
	remoteObjListMap[appFramworkConf.AppSources[0].Name] = tmpS3Response

	err = handleAppRepoChanges(client, &cr, &appDeployContext, remoteObjListMap, &appFramworkConf)
	if err != nil {
		t.Errorf("Could not handle a valid remote listing. Error: %v", err)
	}

	_, err = validateAppSrcDeployInfoByStateAndStatus(appFramworkConf.AppSources[0].Name, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateActive, enterprisev1.DeployStatusPending)
	if err != nil {
		t.Errorf("Unable to delete/disable an app when the App is deleted from remote store. Error: %v", err)
	}

	// Test-7: Object hash change on the remote store should cause App state and status as Active and Pending.
	S3Response.Objects = createRemoteObjectList("e41d8cd98f00", startAppPathAndName, 2322, nil, 10)
	remoteObjListMap[appFramworkConf.AppSources[0].Name] = S3Response

	setStateAndStatusForAppDeployInfoList(appDeployContext.AppsSrcDeployStatus[appFramworkConf.AppSources[0].Name].AppDeploymentInfoList, enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusComplete)

	err = handleAppRepoChanges(client, &cr, &appDeployContext, remoteObjListMap, &appFramworkConf)
	if err != nil {
		t.Errorf("Could not handle a valid remote listing. Error: %v", err)
	}

	_, err = validateAppSrcDeployInfoByStateAndStatus(appFramworkConf.AppSources[0].Name, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateActive, enterprisev1.DeployStatusPending)
	if err != nil {
		t.Errorf("Unable to detect the change, when the object changed. Error: %v", err)
	}

	// Test-8:  For an AppSrc, when all the Apps are deleted on remote store and re-introduced, should modify the state to active and pending
	setStateAndStatusForAppDeployInfoList(appDeployContext.AppsSrcDeployStatus[appFramworkConf.AppSources[0].Name].AppDeploymentInfoList, enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusComplete)

	err = handleAppRepoChanges(client, &cr, &appDeployContext, remoteObjListMap, &appFramworkConf)
	if err != nil {
		t.Errorf("Could not handle a valid remote listing. Error: %v", err)
	}

	_, err = validateAppSrcDeployInfoByStateAndStatus(appFramworkConf.AppSources[0].Name, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateActive, enterprisev1.DeployStatusPending)
	if err != nil {
		t.Errorf("Unable to delete/disable the Apps when the Apps are deleted from remote store. Error: %v", err)
	}

	// Test-9: Unknown App source in remote obj listing should return an error
	startAppPathAndName = "csecurityApps.spl"
	S3Response.Objects = createRemoteObjectList("d41d8cd98f00", startAppPathAndName, 2322, nil, 10)
	invalidAppSourceName := "UnknownAppSourceInConfig"
	remoteObjListMap[invalidAppSourceName] = S3Response
	err = handleAppRepoChanges(client, &cr, &appDeployContext, remoteObjListMap, &appFramworkConf)

	if err == nil {
		t.Errorf("Unable to return an error, when the remote listing contain unknown App source")
	}
	delete(remoteObjListMap, invalidAppSourceName)

	// Test-10: Setting  all apps in AppSrc to complete should mark all the apps status as complete irrespective of their state
	// 10.1 Check for state=Active and status=Complete
	for appSrc, appSrcDeployStatus := range appDeployContext.AppsSrcDeployStatus {
		setStateAndStatusForAppDeployInfoList(appSrcDeployStatus.AppDeploymentInfoList, enterprisev1.RepoStateActive, enterprisev1.DeployStatusInProgress)
		appDeployContext.AppsSrcDeployStatus[appSrc] = appSrcDeployStatus

		expectedMatchCount := getAppSrcDeployInfoCountByStateAndStatus(appSrc, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateActive, enterprisev1.DeployStatusInProgress)

		markAppsStatusToComplete(appDeployContext.AppsSrcDeployStatus)

		matchCount, err := validateAppSrcDeployInfoByStateAndStatus(appSrc, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateActive, enterprisev1.DeployStatusComplete)
		if err != nil {
			t.Errorf("Unable to change the Apps status to complete, once the changes are reflecting on the Pod. Error: %v", err)
		}
		if expectedMatchCount != matchCount {
			t.Errorf("App status change failed. Expected count %v, returned count %v", expectedMatchCount, matchCount)
		}
	}

	// 10.2 Check for state=Deleted status=Complete
	for appSrc, appSrcDeployStatus := range appDeployContext.AppsSrcDeployStatus {
		setStateAndStatusForAppDeployInfoList(appSrcDeployStatus.AppDeploymentInfoList, enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusInProgress)
		appDeployContext.AppsSrcDeployStatus[appSrc] = appSrcDeployStatus

		expectedMatchCount := getAppSrcDeployInfoCountByStateAndStatus(appSrc, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusInProgress)

		markAppsStatusToComplete(appDeployContext.AppsSrcDeployStatus)

		matchCount, err := validateAppSrcDeployInfoByStateAndStatus(appSrc, appDeployContext.AppsSrcDeployStatus, enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusComplete)
		if err != nil {
			t.Errorf("Unable to delete/disable an app when the App is deleted from remote store. Error: %v", err)
		}
		if expectedMatchCount != matchCount {
			t.Errorf("App status change failed. Expected count %v, returned count %v", expectedMatchCount, matchCount)
		}
	}
}

func TestIsAppExtentionValid(t *testing.T) {
	if !isAppExtentionValid("testapp.spl") || !isAppExtentionValid("testapp.tgz") {
		t.Errorf("failed to detect valid app extension")
	}

	if isAppExtentionValid("testapp.aspl") || isAppExtentionValid("testapp.ttgz") {
		t.Errorf("failed to detect invalid app extension")
	}
}

func TestHasAppRepoCheckTimerExpired(t *testing.T) {
	appFrameworkRef := enterprisev1.AppFrameworkSpec{
		AppsRepoPollInterval: 60,
		VolList: []enterprisev1.VolumeSpec{
			{
				Name:      "msos_s2s3_vol",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london",
				SecretRef: "s3-secret",
				Type:      "s3",
				Provider:  "aws",
			},
		},
		AppSources: []enterprisev1.AppSourceSpec{
			{
				Name:     "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterprisev1.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   "local"},
			},
		},
	}

	// Case 1. This is the case when we first enter the reconcile loop.
	appInfoContext := enterprisev1.AppDeploymentContext{
		LastAppInfoCheckTime: 0,
	}

	if !HasAppRepoCheckTimerExpired(appFrameworkRef, appInfoContext) {
		t.Errorf("ShouldCheckAppStatus should have returned true")
	}

	// Case 2. We just checked the apps status
	SetLastAppInfoCheckTime(&appInfoContext)

	if HasAppRepoCheckTimerExpired(appFrameworkRef, appInfoContext) {
		t.Errorf("ShouldCheckAppStatus should have returned false since we just checked the apps status")
	}

	// Case 3. Lets check after AppsRepoPollInterval has elapsed.
	// We do this by setting some random past timestamp.
	appInfoContext.LastAppInfoCheckTime = 1591464060

	if !HasAppRepoCheckTimerExpired(appFrameworkRef, appInfoContext) {
		t.Errorf("ShouldCheckAppStatus should have returned true")
	}
}

func allocateRemoteObject(etag string, key string, Size int64, lastModified *time.Time) *splclient.RemoteObject {
	var remoteObj splclient.RemoteObject

	remoteObj.Etag = &etag
	remoteObj.Key = &key
	remoteObj.Size = &Size
	//tmpEntry.LastModified = lastModified

	return &remoteObj
}

func createRemoteObjectList(etag string, key string, Size int64, lastModified *time.Time, count uint16) []*splclient.RemoteObject {
	var remoteObjList []*splclient.RemoteObject
	var remoteObj *splclient.RemoteObject

	for i := 1; i <= int(count); i++ {
		tag := strconv.Itoa(i)
		remoteObj = allocateRemoteObject(tag+etag, tag+"_"+key, Size+int64(i), nil)
		remoteObjList = append(remoteObjList, remoteObj)
	}

	return remoteObjList
}

func validateAppSrcDeployInfoByStateAndStatus(appSrc string, appSrcDeployStatus map[string]enterprisev1.AppSrcDeployInfo, repoState enterprisev1.AppRepoState, deployStatus enterprisev1.AppDeploymentStatus) (int, error) {
	var matchCount int
	if appSrcDeploymentInfo, ok := appSrcDeployStatus[appSrc]; ok {
		appDeployInfoList := appSrcDeploymentInfo.AppDeploymentInfoList
		for _, appDeployInfo := range appDeployInfoList {
			// Check if the app status is as expected
			if appDeployInfo.RepoState == repoState && appDeployInfo.DeployStatus != deployStatus {
				return matchCount, fmt.Errorf("Invalid app status for appSrc %s, appName: %s", appSrc, appDeployInfo.AppName)
			}
			matchCount++
		}
	} else {
		return matchCount, fmt.Errorf("Missing app source %s, shouldn't not happen", appSrc)
	}

	return matchCount, nil
}

func getAppSrcDeployInfoCountByStateAndStatus(appSrc string, appSrcDeployStatus map[string]enterprisev1.AppSrcDeployInfo, repoState enterprisev1.AppRepoState, deployStatus enterprisev1.AppDeploymentStatus) int {
	var matchCount int
	if appSrcDeploymentInfo, ok := appSrcDeployStatus[appSrc]; ok {
		appDeployInfoList := appSrcDeploymentInfo.AppDeploymentInfoList
		for _, appDeployInfo := range appDeployInfoList {
			// Check if the app status is as expected
			if appDeployInfo.RepoState == repoState && appDeployInfo.DeployStatus == deployStatus {
				matchCount++
			}
		}
	}

	return matchCount
}

func TestSetLastAppInfoCheckTime(t *testing.T) {
	appInfoStatus := &enterprisev1.AppDeploymentContext{}
	SetLastAppInfoCheckTime(appInfoStatus)

	if appInfoStatus.LastAppInfoCheckTime != time.Now().Unix() {
		t.Errorf("LastAppInfoCheckTime should have been set to current time")
	}
}

func TestGetNextRequeueTime(t *testing.T) {
	appFrameworkRef := enterprisev1.AppFrameworkSpec{}
	appFrameworkRef.AppsRepoPollInterval = 60
	nextRequeueTime := GetNextRequeueTime(appFrameworkRef.AppsRepoPollInterval, (time.Now().Unix() - int64(40)))
	if nextRequeueTime > time.Second*20 {
		t.Errorf("Got wrong next requeue time")
	}
}

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

package clustermanager

import (
	"context"
	"os"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	splstorage "github.com/splunk/splunk-operator/pkg/splunk/client/storage"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/appframework"
)

func TestClusterManagerGetAppsListForAWSS3ClientShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				Defaults: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol2",
					Scope:   enterpriseApi.ScopeLocal,
				},
				VolList: []enterpriseApi.VolumeSpec{
					{
						Name:      "msos_s2s3_vol",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Type:      "s3",
						Provider:  "aws",
					},
					{
						Name:      "msos_s2s3_vol2",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london-2",
						SecretRef: "s3-secret",
						Type:      "s3",
						Provider:  "aws",
					},
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
					{
						Name:     "authenticationApps",
						Location: "authenticationAppsRepo",
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	splstorage.RegisterRemoteDataClient(ctx, "aws")

	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd8"}
	Keys := []string{"admin_app.tgz", "security_app.tgz", "authentication_app.tgz"}
	Sizes := []int64{10, 20, 30}
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockAwsHandler := spltest.MockAWSS3Handler{}

	mockAwsObjects := []spltest.MockAWSS3Client{
		{
			Objects: []*spltest.MockRemoteDataObject{
				{
					Etag:         &Etags[0],
					Key:          &Keys[0],
					LastModified: &randomTime,
					Size:         &Sizes[0],
					StorageClass: &StorageClass,
				},
			},
		},
		{
			Objects: []*spltest.MockRemoteDataObject{
				{
					Etag:         &Etags[1],
					Key:          &Keys[1],
					LastModified: &randomTime,
					Size:         &Sizes[1],
					StorageClass: &StorageClass,
				},
			},
		},
		{
			Objects: []*spltest.MockRemoteDataObject{
				{
					Etag:         &Etags[2],
					Key:          &Keys[2],
					LastModified: &randomTime,
					Size:         &Sizes[2],
					StorageClass: &StorageClass,
				},
			},
		},
	}

	appFrameworkRef := cm.Spec.AppFrameworkConfig

	mockAwsHandler.AddObjects(appFrameworkRef, mockAwsObjects...)

	var vol enterpriseApi.VolumeSpec
	var allSuccess bool = true
	for index, appSource := range appFrameworkRef.AppSources {

		vol, err = splutil.GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		// Update the GetS3Client with our mock call which initializes mock AWS client
		getClientWrapper := splstorage.RemoteDataClientsMap[vol.Provider]
		getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, splstorage.NewMockAWSS3Client)

		remoteDataClientMgr := appframework.NewRemoteDataClientManager(client, &cm, &cm.Spec.AppFrameworkConfig, &vol, appSource.Location,
			func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
				cl := spltest.MockAWSS3Client{}
				cl.Objects = mockAwsObjects[index].Objects
				return cl
			},
			func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
				appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
				location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {
				// Get the mock client
				c, err := appframework.GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
				return c, err
			})

		s3Response, err := remoteDataClientMgr.GetAppsList(ctx)
		if err != nil {
			allSuccess = false
			continue
		}

		var mockResponse spltest.MockRemoteDataClient
		mockResponse, err = splstorage.ConvertRemoteDataListResponse(ctx, s3Response)
		if err != nil {
			allSuccess = false
			continue
		}

		if mockAwsHandler.GotSourceAppListResponseMap == nil {
			mockAwsHandler.GotSourceAppListResponseMap = make(map[string]spltest.MockAWSS3Client)
		}

		mockAwsHandler.GotSourceAppListResponseMap[appSource.Name] = spltest.MockAWSS3Client(mockResponse)
	}

	if allSuccess == false {
		t.Errorf("Unable to get apps list for all the app sources")
	}
	method := "GetAppsList"
	mockAwsHandler.CheckAWSRemoteDataListResponse(t, method)
}

func TestClusterManagerGetAppsListForAWSS3ClientShouldFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Type:      "s3",
						Provider:  "aws"},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "adminApps",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	splstorage.RegisterRemoteDataClient(ctx, "aws")

	Etags := []string{"cc707187b036405f095a8ebb43a782c1"}
	Keys := []string{"admin_app.tgz"}
	Sizes := []int64{10}
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockAwsHandler := spltest.MockAWSS3Handler{}

	mockAwsObjects := []spltest.MockAWSS3Client{
		{
			Objects: []*spltest.MockRemoteDataObject{
				{
					Etag:         &Etags[0],
					Key:          &Keys[0],
					LastModified: &randomTime,
					Size:         &Sizes[0],
					StorageClass: &StorageClass,
				},
			},
		},
	}

	appFrameworkRef := cm.Spec.AppFrameworkConfig

	mockAwsHandler.AddObjects(appFrameworkRef, mockAwsObjects...)

	var vol enterpriseApi.VolumeSpec

	appSource := appFrameworkRef.AppSources[0]
	vol, err = splutil.GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get Volume due to error=%s", err)
	}

	// Update the GetS3Client with our mock call which initializes mock AWS client
	getClientWrapper := splstorage.RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, splstorage.NewMockAWSS3Client)

	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		// Purposefully return nil here so that we test the error scenario
		return nil
	}
	remoteDataClientMgr := appframework.NewRemoteDataClientManager(client, &cm, &cm.Spec.AppFrameworkConfig, &vol, appSource.Location,
		func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
			return initFn(ctx, region, accessKeyID, secretAccessKey)
		},
		func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
			appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
			location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {
			// Get the mock client
			c, err := appframework.GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
			return c, err
		})

	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as there is no S3 secret provided")
	}

	// Create empty S3 secret
	s3Secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s3-secret",
			Namespace: "test",
		},
		Data: map[string][]byte{},
	}

	client.AddObject(&s3Secret)

	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty keys")
	}

	accessKey := []byte{'1'}
	s3Secret.Data = map[string][]byte{spltest.S3AccessKey: accessKey}
	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty s3_secret_key")
	}

	secretKey := []byte{'2'}
	s3Secret.Data = map[string][]byte{spltest.S3SecretKey: secretKey}
	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty s3_access_key")
	}

	// Create S3 secret
	s3Secret = spltest.GetMockS3SecretKeys("s3-secret")

	// This should return an error as we have initialized initFn for remoteDataClientMgr
	// to return a nil client.
	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as we could not get the S3 client")
	}

	initFn = func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		// To test the error scenario, do no set the Objects member yet
		cl := spltest.MockAWSS3Client{}
		return cl
	}

	s3Resp, err := remoteDataClientMgr.GetAppsList(ctx)
	if err != nil {
		t.Errorf("GetAppsList should not have returned error since empty appSources are allowed.")
	}
	if len(s3Resp.Objects) != 0 {
		t.Errorf("GetAppsList should return an empty response since we have empty objects in MockAWSS3Client")
	}
}

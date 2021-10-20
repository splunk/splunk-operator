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
	"os"
	"path/filepath"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// initialize some data structures to be used in this file
func init() {
	appSrcName = "appSrc1"
	scope = "local"
	crName = "dummyStandalone"
	randomTime = "1633975734"
	storageClass = "gp2"

	appNames = []string{"app1.tgz", "app2.tgz", "app3.tgz", "app4.tgz"}
	objectHashes = []string{"abcd1111", "efgh2222", "ijkl3333", "wxyz4444"}
	sizes = []int64{10, 20, 30, 40}

	// get the appframework spec
	appFrameworkConfig = newTestAppFrameworkConfig()

	for idx := 0; idx < len(appNames); idx++ {
		// create app deploy info list for all the apps
		appDeployInfo := newTestAppDeployInfo(appNames[idx], objectHashes[idx], sizes[idx])
		appDeployInfoList = append(appDeployInfoList, appDeployInfo)

		// create the S3Response
		object := newTestRemoteObject(appNames[idx], objectHashes[idx], sizes[idx])
		s3Response.Objects = append(s3Response.Objects, &object)
	}
}

var appSrcName, scope, crName, randomTime, storageClass string
var appNames, objectHashes []string
var sizes []int64
var s3Response splclient.S3Response
var appDeployInfoList []enterpriseApi.AppDeploymentInfo
var appFrameworkConfig enterpriseApi.AppFrameworkSpec

func newTestAppFrameworkConfig() enterpriseApi.AppFrameworkSpec {
	return enterpriseApi.AppFrameworkSpec{
		AppsRepoPollInterval: 60,
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "vol2",
			Scope:   enterpriseApi.ScopeCluster,
		},

		VolList: []enterpriseApi.VolumeSpec{
			{
				Name:      "vol1",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london",
				SecretRef: "s3-secret",
				Type:      "s3",
				Provider:  "aws",
			},
			{
				Name:      "vol2",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london-2",
				SecretRef: "s3-secret",
				Type:      "s3",
				Provider:  "aws",
			},
		},
		AppSources: []enterpriseApi.AppSourceSpec{
			{
				Name:     appSrcName,
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "vol1",
					Scope:   scope,
				},
			},
		},
	}
}

func TestAppInstallWorkerPoolStart(t *testing.T) {
	var vol enterpriseApi.VolumeSpec
	appSrc, err := getAppSrcSpec(appFrameworkConfig.AppSources, appSrcName)
	if err != nil {
		t.Errorf("Unable to create the download directory")
	}

	vol, err = splclient.GetAppSrcVolume(*appSrc, &appFrameworkConfig)
	if err != nil {
		t.Errorf("Unable to create the download directory")
	}

	client := spltest.NewMockClient()

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)

	// Create namespace scoped secret
	_, err = splutil.ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	splclient.RegisterS3Client("aws")

	standalone := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stand1",
			Namespace: "test",
		},
	}

	// create the local directory
	localPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", "default" /*namespace*/, "Standalone", crName, scope, appSrcName) + "/"
	err = createAppDownloadDir(localPath)
	if err != nil {
		t.Errorf("Unable to create the download directory")
	}
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	for idx := 0; idx < len(appNames); idx++ {
		// create the dummy app packages locally
		appFileName := appNames[idx] + "_" + objectHashes[idx]
		appLoc := filepath.Join(localPath, appFileName)
		err := createOrTruncateAppFileLocally(appLoc, sizes[idx])
		if err != nil {
			t.Errorf("Unable to create the app files locally")
		}
		defer os.Remove(appLoc)
	}

	// Update the GetS3Client with our mock call which initializes mock AWS client
	getClientWrapper := splclient.S3Clients[vol.Provider]
	getClientWrapper.SetS3ClientFuncPtr(vol.Provider, splclient.NewMockAWSS3Client)

	initFunc := getClientWrapper.GetS3ClientInitFuncPtr()

	s3ClientMgr := S3ClientManager{
		client:          client,
		cr:              &standalone,
		appFrameworkRef: &appFrameworkConfig,
		vol:             &vol,
		location:        appSrc.Location,
		initFn:          initFunc,
		getS3Client:     GetRemoteStorageClient,
	}

	workPool := NewAppInstallWorkerPool(splcommon.DefaultMaxConcurrentAppDownloads, appDeployInfoList,
		s3ClientMgr, s3Response, 200 /*dummy available disk space*/, localPath, scope)

	workPool.Start()

	// check if all apps are downloaded
	if ok, err := areAppsDownloadedSuccessfully(appDeployInfoList); !ok {
		t.Errorf("All apps should have been downloaded successfully, error=%v", err)
	}

	// check again, just for code coverage
	workPool.Start()
	// check if all apps are downloaded
	if ok, err := areAppsDownloadedSuccessfully(appDeployInfoList); !ok {
		t.Errorf("All apps should have been downloaded successfully, error=%v", err)
	}

	// Reset the download state for one of the apps, eventually this should be marked DownloadComplete.
	// We will not re-download it again as it was already downloaded before.
	workPool = NewAppInstallWorkerPool(splcommon.DefaultMaxConcurrentAppDownloads, appDeployInfoList,
		s3ClientMgr, s3Response, 200 /*dummy available disk space*/, localPath, scope)

	setAppDownloadState(workPool.JobList[0], enterpriseApi.DownloadNotStarted)
	workPool.Start()
	// check if all apps are downloaded
	if ok, err := areAppsDownloadedSuccessfully(appDeployInfoList); !ok {
		t.Errorf("All apps should have been downloaded successfully, error=%v", err)
	}

	// Check for the case where one of the apps ends up in DownloadError
	workPool = NewAppInstallWorkerPool(splcommon.DefaultMaxConcurrentAppDownloads, appDeployInfoList,
		s3ClientMgr, s3Response, 200 /*dummy available disk space*/, localPath, scope)

	setAppDownloadState(workPool.JobList[0], enterpriseApi.DownloadError)
	workPool.Start()
	// check if all apps are downloaded
	if ok, err := areAppsDownloadedSuccessfully(appDeployInfoList); ok {
		t.Errorf("We should have returned error here since one app is not downloaded, error=%v", err)
	}

	// remove the apps locally to test download again
	for idx := 0; idx < len(appNames); idx++ {
		appFileName := appNames[idx] + "_" + objectHashes[idx]
		appLoc := filepath.Join(localPath, appFileName)
		os.Remove(appLoc)
	}

	// Reduce the availableDiskSpace to test scenario where we dont have enough disk space.
	workPool = NewAppInstallWorkerPool(splcommon.DefaultMaxConcurrentAppDownloads, appDeployInfoList,
		s3ClientMgr, s3Response, 50 /*dummy available disk space*/, localPath, scope)

	// reset the download state of the apps
	for _, appInfo := range workPool.JobList {
		setAppDownloadState(appInfo, enterpriseApi.DownloadNotStarted)
	}

	workPool.Start()
	// check if all apps are downloaded
	if ok, err := areAppsDownloadedSuccessfully(appDeployInfoList); !ok {
		t.Errorf("All apps should have been downloaded successfully, error=%v", err)
	}
}

func TestIsAppAlreadyDownloaded(t *testing.T) {

	// create the local directory
	localPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", "default" /*namespace*/, "Standalone", crName, scope, appSrcName) + "/"
	err := createAppDownloadDir(localPath)
	if err != nil {
		t.Errorf("Unable to create the download directory")
	}
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	for idx := 0; idx < len(appNames); idx++ {
		// create the dummy app packages locally
		appFileName := appNames[idx] + "_" + objectHashes[idx]
		appLoc := filepath.Join(localPath, appFileName)
		err := createOrTruncateAppFileLocally(appLoc, sizes[idx])
		if err != nil {
			t.Errorf("Unable to create the app files locally")
		}
		defer os.Remove(appLoc)
	}

	var vol enterpriseApi.VolumeSpec
	appSrc, err := getAppSrcSpec(appFrameworkConfig.AppSources, appSrcName)
	if err != nil {
		t.Errorf("Unable to create the download directory")
	}

	vol, err = splclient.GetAppSrcVolume(*appSrc, &appFrameworkConfig)
	if err != nil {
		t.Errorf("Unable to create the download directory")
	}

	client := spltest.NewMockClient()
	standalone := enterpriseApi.Standalone{}

	s3ClientWrapper := splclient.S3Clients[vol.Provider]
	initFunc := s3ClientWrapper.GetS3ClientInitFuncPtr()
	s3ClientMgr := S3ClientManager{
		client:          client,
		cr:              &standalone,
		appFrameworkRef: &appFrameworkConfig,
		vol:             &vol,
		location:        appSrc.Location,
		initFn:          initFunc,
		getS3Client:     GetRemoteStorageClient,
	}

	workPool := NewAppInstallWorkerPool(splcommon.DefaultMaxConcurrentAppDownloads, appDeployInfoList,
		s3ClientMgr, s3Response, 100 /*dummy available disk space*/, localPath, scope)

	// Valid scenario
	// check if all the apps are present locally
	for index := 0; index < len(workPool.JobList); index++ {
		app := workPool.JobList[index]
		if !workPool.isAppAlreadyDownloaded(app) {
			t.Errorf("App should already be present on the local storage.")
		}
	}

	// Invalid scenario 1.0
	// app size on disk doesn't match what we got from s3 rsponse
	for idx := 0; idx < len(appNames); idx++ {
		// truncate the dummy app packages to 0 sizes
		appFileName := appNames[idx] + "_" + objectHashes[idx]
		appLoc := filepath.Join(localPath, appFileName)
		err := createOrTruncateAppFileLocally(appLoc, 0)
		if err != nil {
			t.Errorf("Unable to create the app files locally")
		}
	}

	// Now check the sizes again, this should fail now
	for index := 0; index < len(workPool.JobList); index++ {
		app := workPool.JobList[index]
		if workPool.isAppAlreadyDownloaded(app) {
			t.Errorf("isAppAlreadyDownloaded should return false as app size on disk is different from s3 response.")
		}
	}

	// Invalid scenario 2.0
	// app files are not present on disk
	for idx := 0; idx < len(appNames); idx++ {
		// truncate the dummy app packages to 0 sizes
		appFileName := appNames[idx] + "_" + objectHashes[idx]
		appLoc := filepath.Join(localPath, appFileName)
		err := os.Remove(appLoc)
		if err != nil {
			t.Errorf("Unable to remove the app file=%s locally", appLoc)
		}
	}

	// This should fail now as apps are not present locally
	for index := 0; index < len(workPool.JobList); index++ {
		app := workPool.JobList[index]
		if workPool.isAppAlreadyDownloaded(app) {
			t.Errorf("isAppAlreadyDownloaded should return false as apps are not present locally.")
		}
	}
}

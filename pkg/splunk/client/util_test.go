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

package client

import (
	"context"
	"reflect"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestCheckIfVolumeExists(t *testing.T) {
	SmartStoreConfig := enterpriseApi.SmartStoreSpec{
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

	// Volume that doesn't should error out
	_, err := CheckIfVolumeExists(SmartStoreConfig.VolList, "random_volume_name")

	if err == nil {
		t.Errorf("if the volume doesn't exists, error should be reported")
	}

	// Volume that exists should not error out
	index := len(SmartStoreConfig.VolList) - 1
	returnedIndex, err := CheckIfVolumeExists(SmartStoreConfig.VolList, SmartStoreConfig.VolList[index].Name)

	if err != nil {
		t.Errorf("existing volume should not error out. index id: %d, error: %s", index, err.Error())
	} else if index != returnedIndex {
		t.Errorf("Expected index: %d, but returned index id: %d", index, returnedIndex)
	}
}

func TestNewMockAWSS3Client(t *testing.T) {
	ctx := context.TODO()
	// Test 1. Test the valid case
	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		cl := spltest.MockAWSS3Client{}
		return cl
	}

	_, err := NewMockAWSS3Client(ctx, "sample_bucket", "abcd", "1234", "admin/", "admin", "us-west-2", "htts://s3.us-west-2.amazonaws.com", initFn)

	if err != nil {
		t.Errorf("NewMockAWSS3Client should have returned a Mock AWS client.")
	}

	// Test 2. Test the invalid case by returning nil client
	initFn = func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		return nil
	}
	_, err = NewMockAWSS3Client(ctx, "sample_bucket", "abcd", "1234", "admin/", "admin", "us-west-2", "htts://s3.us-west-2.amazonaws.com", initFn)

	if err == nil {
		t.Errorf("NewMockAWSS3Client should have returned an error since we passed nil client in init function.")
	}
}

func TestGetVolume(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
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
				Name:     "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "vol1",
					Scope:   enterpriseApi.ScopeLocal,
				},
			},
			{
				Name:     "securityApps",
				Location: "securityAppsRepo",
			},
		},
	}

	// test for valid volumes
	for index, appSource := range appFrameworkRef.AppSources {
		vol, err := GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			t.Errorf("GetVolume should not have returned error")
		}

		if !reflect.DeepEqual(vol, appFrameworkRef.VolList[index]) {
			t.Errorf("returned volume spec is not correct")
		}
	}

	// test for an invalid volume
	appFrameworkRef.AppSources = []enterpriseApi.AppSourceSpec{
		{
			Name:     "adminApps",
			Location: "adminAppsRepo",
			AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
				VolName: "invalid_volume",
				Scope:   enterpriseApi.ScopeLocal,
			},
		},
	}

	_, err := GetAppSrcVolume(ctx, appFrameworkRef.AppSources[0], &appFrameworkRef)
	if err == nil {
		t.Errorf("GetVolume should have returned error for an invalid volume name")
	}
}
